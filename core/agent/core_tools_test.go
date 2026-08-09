package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/0xmattg/go-press/core/content"
	"github.com/0xmattg/go-press/core/media"
	"github.com/0xmattg/go-press/core/option"
	"github.com/0xmattg/go-press/core/taxonomy"
	"github.com/0xmattg/go-press/core/user"
)

func TestCoreReadToolsExecuteThroughPipelineAndProtectBoundaries(t *testing.T) {
	db := agentTestDB(t)
	contentRegistry := content.NewRegistry()
	contentRegistry.RegisterType(content.ContentTypeDef{
		Name: "post", Label: "Post", LabelPlural: "Posts", HasArchive: true,
		Supports: []string{"title", "content", "thumbnail"}, Taxonomies: []string{"category"},
		MetaFields: []content.MetaFieldDef{{Key: "subtitle", Label: "Subtitle", Type: "string"}},
		Rewrite:    content.RewriteRule{Slug: "posts"},
	})
	contentRegistry.RegisterType(content.ContentTypeDef{Name: "service", Label: "Service", LabelPlural: "Services"})
	contentRegistry.RegisterTaxonomy(content.TaxonomyDef{
		Name: "category", Label: "Category", LabelPlural: "Categories",
		ContentTypes: []string{"post"}, Hierarchical: true,
	})
	post := content.Content{
		Type: "post", Status: content.StatusPublished, Title: "Agent post", Slug: "agent-post",
		Content: "<p>Body</p>", Excerpt: "Summary", AuthorID: 1, PublishedAt: timePointer(time.Now().UTC()),
	}
	serviceItem := content.Content{Type: "service", Status: content.StatusPublished, Title: "Service", Slug: "service"}
	if err := db.Create(&post).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&serviceItem).Error; err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{
		"subtitle": "Visible", "plugin_private": "Hidden", "embed_code": "<script>hidden</script>",
	} {
		if err := db.Create(&content.ContentMeta{ContentID: post.ID, MetaKey: key, MetaValue: value}).Error; err != nil {
			t.Fatal(err)
		}
	}
	term := taxonomy.Term{Name: "News", Slug: "news"}
	if err := db.Create(&term).Error; err != nil {
		t.Fatal(err)
	}
	category := taxonomy.Taxonomy{TermID: term.ID, Taxonomy: "category", Description: "News posts", Count: 1}
	if err := db.Create(&category).Error; err != nil {
		t.Fatal(err)
	}
	for _, item := range []*media.Media{
		{Filename: "safe.jpg", OriginalName: "safe.jpg", MimeType: "image/jpeg", Path: "/uploads/safe.jpg", CreatedAt: time.Now()},
		{Filename: "private.jpg", OriginalName: "private.jpg", MimeType: "image/jpeg", Path: "/srv/private/private.jpg", CreatedAt: time.Now()},
	} {
		if err := db.Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}

	registry := NewRegistry()
	_, err := RegisterCoreReadTools(registry, CoreToolServices{
		ContentRegistry: contentRegistry, ContentRepo: content.NewRepository(db),
		TaxonomyRepo: taxonomy.NewRepository(db), MediaRepo: media.NewRepository(db),
		Options: option.NewMemoryStore(map[string]string{
			"site_name": "Runtime site", "site_description": "Description", "site_language": "zh-CN",
		}),
		SiteName: "Configured", SiteURL: "https://example.test", SiteTimezone: "Asia/Shanghai", CoreVersion: "0.6.55",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Snapshot().Tools) != 6 {
		t.Fatalf("registered core tools=%d", len(registry.Snapshot().Tools))
	}
	principal := testPrincipal(user.RoleEditor, scopeSiteRead, scopeContentRead, scopeTaxonomyRead, scopeMediaRead)
	executor := NewExecutor(registry, fixedPrincipalValidator{principal: principal}, NewAuthorizer(user.NewRBAC()), nil, &memoryAuditRecorder{})

	calls := []Call{
		{RequestID: "site", ToolName: "gopress.site.get", Arguments: json.RawMessage(`{}`), Principal: principal},
		{RequestID: "types", ToolName: "gopress.content_types.list", Arguments: json.RawMessage(`{}`), Principal: principal},
		{RequestID: "list", ToolName: "gopress.content.list", Arguments: json.RawMessage(`{"content_type":"post","status":"published"}`), Principal: principal},
		{RequestID: "taxonomy", ToolName: "gopress.taxonomy.list", Arguments: json.RawMessage(`{"content_type":"post"}`), Principal: principal},
		{RequestID: "media", ToolName: "gopress.media.list", Arguments: json.RawMessage(`{"mime_type":"image/"}`), Principal: principal},
	}
	for _, call := range calls {
		result, err := executor.Execute(context.Background(), call)
		if err != nil {
			t.Fatalf("execute %s: %v", call.ToolName, err)
		}
		if len(result.Output) == 0 {
			t.Fatalf("execute %s returned empty output", call.ToolName)
		}
		if call.ToolName == "gopress.media.list" && strings.Contains(string(result.Output), "/srv/private") {
			t.Fatalf("media output leaked server path: %s", result.Output)
		}
	}

	getResult, err := executor.Execute(context.Background(), Call{
		RequestID: "get", ToolName: "gopress.content.get",
		Arguments: json.RawMessage(`{"content_type":"post","id":` + uintString(post.ID) + `}`), Principal: principal,
	})
	if err != nil {
		t.Fatal(err)
	}
	output := string(getResult.Output)
	if !strings.Contains(output, `"subtitle":"Visible"`) || strings.Contains(output, "plugin_private") || strings.Contains(output, "embed_code") {
		t.Fatalf("content meta boundary failed: %s", output)
	}
	_, err = executor.Execute(context.Background(), Call{
		RequestID: "idor", ToolName: "gopress.content.get",
		Arguments: json.RawMessage(`{"content_type":"post","id":` + uintString(serviceItem.ID) + `}`), Principal: principal,
	})
	if !IsErrorCode(err, CodeNotFound) {
		t.Fatalf("cross-type content ID error=%v", err)
	}

	protectedCalls := append(append([]Call{}, calls...), Call{
		RequestID: "get-protected", ToolName: "gopress.content.get",
		Arguments: json.RawMessage(`{"content_type":"post","id":` + uintString(post.ID) + `}`),
	})
	missingScope := testPrincipal(user.RoleSuperAdmin)
	missingScopeExecutor := NewExecutor(registry, fixedPrincipalValidator{principal: missingScope}, NewAuthorizer(user.NewRBAC()), nil, &memoryAuditRecorder{})
	noAccessRBAC := user.NewRBAC()
	noAccessRBAC.RegisterRole("agent_no_access", "Agent No Access", 1, map[string]bool{})
	noAccess := testPrincipal("agent_no_access", scopeSiteRead, scopeContentRead, scopeTaxonomyRead, scopeMediaRead)
	noAccessExecutor := NewExecutor(registry, fixedPrincipalValidator{principal: noAccess}, NewAuthorizer(noAccessRBAC), nil, &memoryAuditRecorder{})
	for _, call := range protectedCalls {
		call.Principal = missingScope
		call.RequestID += "-missing-scope"
		if _, err := missingScopeExecutor.Execute(context.Background(), call); !IsErrorCode(err, CodeInsufficientScope) {
			t.Fatalf("%s missing-scope error=%v", call.ToolName, err)
		}
		call.Principal = noAccess
		call.RequestID += "-missing-role"
		if _, err := noAccessExecutor.Execute(context.Background(), call); !IsErrorCode(err, CodePermissionDenied) {
			t.Fatalf("%s missing-role error=%v", call.ToolName, err)
		}
	}
}

func timePointer(value time.Time) *time.Time { return &value }

func uintString(value uint) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
