package agent

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/0xmattg/go-press/core/content"
	"github.com/0xmattg/go-press/core/media"
	"github.com/0xmattg/go-press/core/user"

	"gorm.io/gorm"
)

type mutationCounter struct {
	mu      sync.Mutex
	content []content.MutationKind
	media   int
}

func writeToolRuntime(t *testing.T, principal Principal) (*gorm.DB, *Executor, *Registry, *content.Registry, *content.CommandService, *content.Repository, *media.Repository, *memoryAuditRecorder, *mutationCounter) {
	t.Helper()
	db := agentTestDB(t)
	registry := content.NewRegistry()
	registry.RegisterType(content.ContentTypeDef{Name: "post", Supports: []string{"title", "content", "excerpt", "thumbnail", "comments"}, MetaFields: []content.MetaFieldDef{{Key: "subtitle"}}, Hierarchical: true, Rewrite: content.RewriteRule{Slug: "posts"}})
	registry.RegisterType(content.ContentTypeDef{Name: "service", Supports: []string{"title", "content"}, Rewrite: content.RewriteRule{Slug: "services"}})
	commands := content.NewCommandService(db, registry)
	contentRepo := content.NewRepository(db)
	mediaRepo := media.NewRepository(db)
	counter := &mutationCounter{}
	commands.SetMutationObserver(func(_ context.Context, mutation content.Mutation) {
		counter.mu.Lock()
		counter.content = append(counter.content, mutation.Kind)
		counter.mu.Unlock()
	})
	mediaRepo.SetMetadataObserver(func(_ context.Context, _ *media.Media) {
		counter.mu.Lock()
		counter.media++
		counter.mu.Unlock()
	})
	tools := NewRegistry()
	if _, err := RegisterCoreWriteTools(tools, CoreToolServices{ContentRegistry: registry, ContentRepo: contentRepo, ContentCommands: commands, MediaRepo: mediaRepo}); err != nil {
		t.Fatal(err)
	}
	policy := NewPolicy()
	names := make([]string, 0, len(CoreWriteTools()))
	for _, info := range CoreWriteTools() {
		names = append(names, info.Name)
	}
	if err := policy.Configure(ProfileSafeWrite, names); err != nil {
		t.Fatal(err)
	}
	audit := &memoryAuditRecorder{}
	executor := NewExecutor(tools, fixedPrincipalValidator{principal: principal}, NewAuthorizer(user.NewRBAC()), NewIdempotencyStore(db), audit)
	executor.SetRiskPolicy(policy)
	return db, executor, tools, registry, commands, contentRepo, mediaRepo, audit, counter
}

func executeWrite(t *testing.T, executor *Executor, requestID, tool string, args map[string]any) (*Result, error) {
	t.Helper()
	payload, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	return executor.Execute(context.Background(), Call{RequestID: requestID, ToolName: tool, Arguments: payload, Principal: Principal{Kind: PrincipalUser, SubjectID: 999}, Client: ClientInfo{Adapter: "test"}})
}

func decodeContentMutation(t *testing.T, result *Result) contentMutationOutput {
	t.Helper()
	var output contentMutationOutput
	if err := json.Unmarshal(result.Output, &output); err != nil {
		t.Fatal(err)
	}
	return output
}

func TestCoreWriteCreateDraftIsDefaultDeniedThenIdempotentlyAudited(t *testing.T) {
	principal := testPrincipal(user.RoleEditor, ScopeContentWrite)
	db, executor, tools, registry, commands, repo, mediaRepo, audit, counter := writeToolRuntime(t, principal)
	_ = registry
	_ = commands
	_ = repo
	_ = mediaRepo
	executor.SetRiskPolicy(NewPolicy())
	args := map[string]any{"content_type": "post", "title": "Agent Draft", "content": "<p>Hello</p><script>bad()</script>", "meta": map[string]string{"subtitle": "Beta"}, "idempotency_key": "draft-create-0001"}
	if _, err := executeWrite(t, executor, "create-denied", ToolContentCreateDraft, args); !IsErrorCode(err, CodeRiskDenied) {
		t.Fatalf("default profile error=%v", err)
	}
	policy := NewPolicy()
	if err := policy.Configure(ProfileSafeWrite, []string{ToolContentCreateDraft}); err != nil {
		t.Fatal(err)
	}
	executor.SetRiskPolicy(policy)
	subscriber := principal
	subscriber.Role = user.RoleSubscriber
	subscriber.CredentialID = 12
	deniedExecutor := NewExecutor(tools, fixedPrincipalValidator{principal: subscriber}, NewAuthorizer(user.NewRBAC()), NewIdempotencyStore(db), audit)
	deniedExecutor.SetRiskPolicy(policy)
	if _, err := executeWrite(t, deniedExecutor, "create-rbac-denied", ToolContentCreateDraft, args); !IsErrorCode(err, CodePermissionDenied) {
		t.Fatalf("subscriber create error=%v", err)
	}
	foreignParent := content.Content{Type: "service", Status: content.StatusDraft, Title: "Foreign parent", Slug: "foreign-parent", AuthorID: principal.SubjectID}
	if err := db.Create(&foreignParent).Error; err != nil {
		t.Fatal(err)
	}
	forgedParentArgs := map[string]any{"content_type": "post", "title": "Forged child", "parent_id": foreignParent.ID, "idempotency_key": "draft-parent-001"}
	if _, err := executeWrite(t, executor, "create-cross-parent", ToolContentCreateDraft, forgedParentArgs); !IsErrorCode(err, CodeNotFound) {
		t.Fatalf("cross-type parent error=%v", err)
	}
	first, err := executeWrite(t, executor, "create-one", ToolContentCreateDraft, args)
	if err != nil {
		t.Fatal(err)
	}
	second, err := executeWrite(t, executor, "create-two", ToolContentCreateDraft, args)
	if err != nil || !second.Replayed {
		t.Fatalf("replay=%v err=%v", second, err)
	}
	created := decodeContentMutation(t, first)
	if created.Status != content.StatusDraft || created.AuthorID != principal.SubjectID || created.ID == 0 {
		t.Fatalf("created=%+v", created)
	}
	var count int64
	if err := db.Model(&content.Content{}).Where("type = ?", "post").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	var record IdempotencyRecord
	if err := db.Where("credential_id = ? AND tool_name = ? AND status = ?", principal.CredentialID, ToolContentCreateDraft, IdempotencyCompleted).First(&record).Error; err != nil || record.ResourceType != "post" || record.ResourceID != created.ID {
		t.Fatalf("idempotency resource=%+v err=%v", record, err)
	}
	saved, err := repo.FindByID(created.ID)
	if err != nil || saved.GetMeta("subtitle") != "Beta" || saved.Content == args["content"] {
		t.Fatalf("saved=%+v err=%v", saved, err)
	}
	counter.mu.Lock()
	mutations := append([]content.MutationKind(nil), counter.content...)
	counter.mu.Unlock()
	if len(mutations) != 1 || mutations[0] != content.MutationCreated {
		t.Fatalf("mutations=%v", mutations)
	}
	statuses := audit.statuses()
	if !containsString(statuses, AuditDenied) || !containsString(statuses, AuditSucceeded) || !containsString(statuses, AuditReplayed) {
		t.Fatalf("audit=%v", statuses)
	}
}

func TestCoreWriteUpdateEnforcesOwnershipTypeAndOptimisticLock(t *testing.T) {
	principal := testPrincipal(user.RoleAuthor, ScopeContentWrite)
	db, executor, _, _, _, _, _, audit, _ := writeToolRuntime(t, principal)
	own := content.Content{Type: "post", Status: content.StatusDraft, Title: "Own", Slug: "own", AuthorID: principal.SubjectID, CommentStatus: "open"}
	other := content.Content{Type: "post", Status: content.StatusDraft, Title: "Other", Slug: "other", AuthorID: 99, CommentStatus: "open"}
	service := content.Content{Type: "service", Status: content.StatusDraft, Title: "Service", Slug: "service", AuthorID: principal.SubjectID}
	for _, item := range []*content.Content{&own, &other, &service} {
		if err := db.Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := executeWrite(t, executor, "update-idor", ToolContentUpdate, map[string]any{"content_type": "post", "id": other.ID, "expected_updated_at": other.UpdatedAt.Format(time.RFC3339Nano), "title": "Forged", "idempotency_key": "update-other-001"}); !IsErrorCode(err, CodePermissionDenied) {
		t.Fatalf("IDOR error=%v", err)
	}
	if _, err := executeWrite(t, executor, "update-cross", ToolContentUpdate, map[string]any{"content_type": "post", "id": service.ID, "expected_updated_at": service.UpdatedAt.Format(time.RFC3339Nano), "title": "Forged", "idempotency_key": "update-cross-001"}); !IsErrorCode(err, CodeNotFound) {
		t.Fatalf("cross-type error=%v", err)
	}
	args := map[string]any{"content_type": "post", "id": own.ID, "expected_updated_at": own.UpdatedAt.Format(time.RFC3339Nano), "title": "Updated", "idempotency_key": "update-own-0001"}
	result, err := executeWrite(t, executor, "update-own", ToolContentUpdate, args)
	if err != nil {
		t.Fatal(err)
	}
	updated := decodeContentMutation(t, result)
	if updated.Title != "Updated" {
		t.Fatalf("updated=%+v", updated)
	}
	replayed, err := executeWrite(t, executor, "update-own-replay", ToolContentUpdate, args)
	if err != nil || !replayed.Replayed {
		t.Fatalf("update replay=%+v err=%v", replayed, err)
	}
	args["idempotency_key"] = "update-own-stale"
	if _, err := executeWrite(t, executor, "update-stale", ToolContentUpdate, args); !IsErrorCode(err, CodeConflict) {
		t.Fatalf("stale error=%v", err)
	}
	if !containsString(audit.statuses(), AuditDenied) || !containsString(audit.statuses(), AuditFailed) {
		t.Fatalf("audit=%v", audit.statuses())
	}
}

func TestCoreWritePublishTrashRestoreHaveSeparateRBACAndConfirmation(t *testing.T) {
	author := testPrincipal(user.RoleAuthor, ScopeContentWrite, ScopeContentPublish)
	db, authorExecutor, _, registry, commands, repo, mediaRepo, _, _ := writeToolRuntime(t, author)
	item := content.Content{Type: "post", Status: content.StatusDraft, Title: "Lifecycle", Slug: "lifecycle", AuthorID: author.SubjectID, CommentStatus: "open"}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	foreign := content.Content{Type: "post", Status: content.StatusTrash, Title: "Foreign", Slug: "foreign", AuthorID: 99, CommentStatus: "open"}
	if err := db.Create(&foreign).Error; err != nil {
		t.Fatal(err)
	}
	foreignTrash := map[string]any{"content_type": "post", "id": foreign.ID, "expected_updated_at": foreign.UpdatedAt.Format(time.RFC3339Nano), "idempotency_key": "trash-foreign-01", "confirm": true}
	if _, err := executeWrite(t, authorExecutor, "trash-foreign", ToolContentTrash, foreignTrash); !IsErrorCode(err, CodePermissionDenied) {
		t.Fatalf("foreign trash error=%v", err)
	}
	foreignRestore := map[string]any{"content_type": "post", "id": foreign.ID, "expected_updated_at": foreign.UpdatedAt.Format(time.RFC3339Nano), "idempotency_key": "restore-foreign1"}
	if _, err := executeWrite(t, authorExecutor, "restore-foreign", ToolContentRestore, foreignRestore); !IsErrorCode(err, CodePermissionDenied) {
		t.Fatalf("foreign restore error=%v", err)
	}
	publish := map[string]any{"content_type": "post", "id": item.ID, "expected_updated_at": item.UpdatedAt.Format(time.RFC3339Nano), "idempotency_key": "publish-item-001", "confirm": true}
	if _, err := executeWrite(t, authorExecutor, "publish-author", ToolContentPublish, publish); !IsErrorCode(err, CodePermissionDenied) {
		t.Fatalf("author publish error=%v", err)
	}
	editor := testPrincipal(user.RoleEditor, ScopeContentWrite, ScopeContentPublish)
	editor.CredentialID = 11
	tools := NewRegistry()
	if _, err := RegisterCoreWriteTools(tools, CoreToolServices{ContentRegistry: registry, ContentRepo: repo, ContentCommands: commands, MediaRepo: mediaRepo}); err != nil {
		t.Fatal(err)
	}
	policy := NewPolicy()
	names := []string{ToolContentPublish, ToolContentTrash, ToolContentRestore}
	_ = policy.Configure(ProfileSafeWrite, names)
	editorExecutor := NewExecutor(tools, fixedPrincipalValidator{principal: editor}, NewAuthorizer(user.NewRBAC()), NewIdempotencyStore(db), &memoryAuditRecorder{})
	editorExecutor.SetRiskPolicy(policy)
	delete(publish, "confirm")
	if _, err := executeWrite(t, editorExecutor, "publish-confirm", ToolContentPublish, publish); !IsErrorCode(err, CodeConfirmationRequired) {
		t.Fatalf("confirmation error=%v", err)
	}
	publish["confirm"] = true
	publishedResult, err := executeWrite(t, editorExecutor, "publish-editor", ToolContentPublish, publish)
	if err != nil {
		t.Fatal(err)
	}
	published := decodeContentMutation(t, publishedResult)
	if published.Status != content.StatusPublished {
		t.Fatalf("published=%+v", published)
	}
	publishReplay, err := executeWrite(t, editorExecutor, "publish-editor-replay", ToolContentPublish, publish)
	if err != nil || !publishReplay.Replayed {
		t.Fatalf("publish replay=%+v err=%v", publishReplay, err)
	}
	trash := map[string]any{"content_type": "post", "id": item.ID, "expected_updated_at": published.UpdatedAt, "idempotency_key": "trash-item-0001", "confirm": true}
	trashResult, err := executeWrite(t, editorExecutor, "trash-editor", ToolContentTrash, trash)
	if err != nil {
		t.Fatal(err)
	}
	trashed := decodeContentMutation(t, trashResult)
	if trashed.Status != content.StatusTrash {
		t.Fatalf("trashed=%+v", trashed)
	}
	trashReplay, err := executeWrite(t, editorExecutor, "trash-editor-replay", ToolContentTrash, trash)
	if err != nil || !trashReplay.Replayed {
		t.Fatalf("trash replay=%+v err=%v", trashReplay, err)
	}
	restore := map[string]any{"content_type": "post", "id": item.ID, "expected_updated_at": trashed.UpdatedAt, "idempotency_key": "restore-item-01"}
	restoreResult, err := executeWrite(t, editorExecutor, "restore-editor", ToolContentRestore, restore)
	if err != nil {
		t.Fatal(err)
	}
	if got := decodeContentMutation(t, restoreResult); got.Status != content.StatusDraft {
		t.Fatalf("restored=%+v", got)
	}
	restoreReplay, err := executeWrite(t, editorExecutor, "restore-editor-replay", ToolContentRestore, restore)
	if err != nil || !restoreReplay.Replayed {
		t.Fatalf("restore replay=%+v err=%v", restoreReplay, err)
	}
}

func TestCoreWriteMediaMetadataEnforcesOwnershipAndFieldBoundary(t *testing.T) {
	principal := testPrincipal(user.RoleAuthor, ScopeMediaWrite)
	db, executor, _, _, _, _, _, audit, counter := writeToolRuntime(t, principal)
	own := media.Media{Filename: "own.jpg", Path: "/uploads/own.jpg", MimeType: "image/jpeg", UploadedBy: principal.SubjectID}
	other := media.Media{Filename: "other.jpg", Path: "/uploads/other.jpg", MimeType: "image/jpeg", UploadedBy: 99}
	for _, item := range []*media.Media{&own, &other} {
		if err := db.Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := executeWrite(t, executor, "media-idor", ToolMediaUpdateMetadata, map[string]any{"id": other.ID, "expected_updated_at": other.UpdatedAt.Format(time.RFC3339Nano), "title": "Forged", "idempotency_key": "media-other-001"}); !IsErrorCode(err, CodePermissionDenied) {
		t.Fatalf("media IDOR error=%v", err)
	}
	result, err := executeWrite(t, executor, "media-own", ToolMediaUpdateMetadata, map[string]any{"id": own.ID, "expected_updated_at": own.UpdatedAt.Format(time.RFC3339Nano), "alt_text": "Accessible", "caption": "Safe caption", "idempotency_key": "media-own-0001"})
	if err != nil {
		t.Fatal(err)
	}
	var output mediaMutationOutput
	if err := json.Unmarshal(result.Output, &output); err != nil {
		t.Fatal(err)
	}
	if output.AltText != "Accessible" || output.Caption != "Safe caption" || output.UploadedBy != principal.SubjectID {
		t.Fatalf("output=%+v", output)
	}
	replayed, err := executeWrite(t, executor, "media-own-replay", ToolMediaUpdateMetadata, map[string]any{"id": own.ID, "expected_updated_at": own.UpdatedAt.Format(time.RFC3339Nano), "alt_text": "Accessible", "caption": "Safe caption", "idempotency_key": "media-own-0001"})
	if err != nil || !replayed.Replayed {
		t.Fatalf("media replay=%+v err=%v", replayed, err)
	}
	counter.mu.Lock()
	mediaMutations := counter.media
	counter.mu.Unlock()
	if mediaMutations != 1 {
		t.Fatalf("media mutations=%d", mediaMutations)
	}
	if !containsString(audit.statuses(), AuditDenied) || !containsString(audit.statuses(), AuditSucceeded) {
		t.Fatalf("audit=%v", audit.statuses())
	}
}
