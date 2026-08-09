package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/0xmattg/go-press/core/content"
	"github.com/0xmattg/go-press/core/media"
)

const (
	ScopeContentWrite   = "gopress:content:write"
	ScopeContentPublish = "gopress:content:publish"
	ScopeMediaWrite     = "gopress:media:write"

	ToolContentCreateDraft  = "gopress.content.create_draft"
	ToolContentUpdate       = "gopress.content.update"
	ToolContentPublish      = "gopress.content.publish"
	ToolContentTrash        = "gopress.content.move_to_trash"
	ToolContentRestore      = "gopress.content.restore"
	ToolMediaUpdateMetadata = "gopress.media.update_metadata"
)

type CoreWriteToolInfo struct {
	Name        string    `json:"name"`
	Scope       string    `json:"scope"`
	Risk        RiskLevel `json:"risk"`
	Recommended bool      `json:"recommended"`
}

func CoreWriteTools() []CoreWriteToolInfo {
	return []CoreWriteToolInfo{
		{Name: ToolContentCreateDraft, Scope: ScopeContentWrite, Risk: RiskWrite, Recommended: true},
		{Name: ToolContentUpdate, Scope: ScopeContentWrite, Risk: RiskWrite, Recommended: true},
		{Name: ToolContentPublish, Scope: ScopeContentPublish, Risk: RiskPublish},
		{Name: ToolContentTrash, Scope: ScopeContentWrite, Risk: RiskDestructive},
		{Name: ToolContentRestore, Scope: ScopeContentWrite, Risk: RiskWrite},
		{Name: ToolMediaUpdateMetadata, Scope: ScopeMediaWrite, Risk: RiskWrite},
	}
}

func CoreWriteScopes() []string {
	return []string{ScopeContentWrite, ScopeContentPublish, ScopeMediaWrite}
}

func RegisterCoreWriteTools(registry *Registry, services CoreToolServices) ([]*Handle, error) {
	if registry == nil || services.ContentRegistry == nil || services.ContentRepo == nil || services.ContentCommands == nil || services.MediaRepo == nil {
		return nil, ErrInvalidTool
	}
	tools := coreWriteToolDefinitions(services)
	handles := make([]*Handle, 0, len(tools))
	for _, tool := range tools {
		handle, err := registry.Register(coreToolOwner, tool)
		if err != nil {
			for _, registered := range handles {
				registered.Revoke()
			}
			return nil, err
		}
		handles = append(handles, handle)
	}
	return handles, nil
}

type createDraftInput struct {
	ContentType   string            `json:"content_type"`
	Title         string            `json:"title"`
	Slug          string            `json:"slug"`
	Content       string            `json:"content"`
	Excerpt       string            `json:"excerpt"`
	ImageURL      string            `json:"image_url"`
	ParentID      *uint             `json:"parent_id"`
	CommentStatus string            `json:"comment_status"`
	Meta          map[string]string `json:"meta"`
}

type updateContentInput struct {
	ContentType       string            `json:"content_type"`
	ID                uint              `json:"id"`
	ExpectedUpdatedAt string            `json:"expected_updated_at"`
	Title             *string           `json:"title"`
	Slug              *string           `json:"slug"`
	Content           *string           `json:"content"`
	Excerpt           *string           `json:"excerpt"`
	ImageURL          *string           `json:"image_url"`
	ParentID          *uint             `json:"parent_id"`
	ClearParent       bool              `json:"clear_parent"`
	CommentStatus     *string           `json:"comment_status"`
	Meta              map[string]string `json:"meta"`
}

type contentTransitionInput struct {
	ContentType       string `json:"content_type"`
	ID                uint   `json:"id"`
	ExpectedUpdatedAt string `json:"expected_updated_at"`
}

type updateMediaInput struct {
	ID                uint    `json:"id"`
	ExpectedUpdatedAt string  `json:"expected_updated_at"`
	AltText           *string `json:"alt_text"`
	Title             *string `json:"title"`
	Caption           *string `json:"caption"`
}

type contentMutationOutput struct {
	ID        uint   `json:"id"`
	Type      string `json:"type"`
	Status    string `json:"status"`
	Title     string `json:"title"`
	Slug      string `json:"slug"`
	AuthorID  uint   `json:"author_id"`
	UpdatedAt string `json:"updated_at"`
}

type mediaMutationOutput struct {
	ID         uint   `json:"id"`
	AltText    string `json:"alt_text"`
	Title      string `json:"title"`
	Caption    string `json:"caption"`
	UploadedBy uint   `json:"uploaded_by"`
	UpdatedAt  string `json:"updated_at"`
}

func coreWriteToolDefinitions(services CoreToolServices) []Tool {
	contentWrite := PermissionRequirement{Scope: ScopeContentWrite, Resource: "content", Action: "update", OwnAction: "update_own"}
	contentDelete := PermissionRequirement{Scope: ScopeContentWrite, Resource: "content", Action: "delete", OwnAction: "delete_own"}
	mediaWrite := PermissionRequirement{Scope: ScopeMediaWrite, Resource: "media", Action: "update", OwnAction: "update_own"}
	return []Tool{
		{
			Name: ToolContentCreateDraft, Title: "Create content draft", Description: "Create one draft in an editable registered content type.",
			InputSchema: createDraftInputSchema, OutputSchema: contentMutationOutputSchema,
			Mutability: MutabilityWrite, Risk: RiskWrite, Idempotent: true,
			Permission:        PermissionRequirement{Scope: ScopeContentWrite, Resource: "content", Action: "create"},
			ResolvePermission: editableContentTypeResolver(services.ContentRegistry, PermissionRequirement{Scope: ScopeContentWrite, Resource: "content", Action: "create"}),
			Timeout:           15 * time.Second, MaxConcurrency: 4,
			Handler: func(ctx context.Context, invocation Invocation) (any, error) {
				var input createDraftInput
				if json.Unmarshal(invocation.Arguments, &input) != nil {
					return nil, NewError(CodeInvalidArguments, "invalid draft arguments")
				}
				definition, err := editableDefinition(services.ContentRegistry, input.ContentType)
				if err != nil {
					return nil, err
				}
				if err := validateDraftFields(definition, input); err != nil {
					return nil, err
				}
				if input.ParentID != nil {
					if _, err := services.ContentRepo.QueryContext(ctx).ID(*input.ParentID).Type(input.ContentType).First(); err != nil {
						return nil, NewError(CodeNotFound, "parent content not found")
					}
				}
				slug := normalizeAgentSlug(input.Slug)
				if slug == "" {
					slug = normalizeAgentSlug(input.Title)
				}
				if slug == "" {
					slug = "draft"
				}
				slug, err = services.ContentRepo.EnsureUniqueSlugContext(ctx, input.ContentType, slug, 0)
				if err != nil {
					return nil, WrapError(CodeInternal, "failed to reserve content slug", err)
				}
				commentStatus := strings.TrimSpace(input.CommentStatus)
				if commentStatus == "" {
					commentStatus = "open"
				}
				item := &content.Content{Type: input.ContentType, Status: content.StatusDraft, Title: strings.TrimSpace(input.Title), Slug: slug,
					Content: input.Content, Excerpt: input.Excerpt, ImageURL: strings.TrimSpace(input.ImageURL), ParentID: input.ParentID,
					CommentStatus: commentStatus, AuthorID: invocation.Principal.SubjectID}
				if err := services.ContentCommands.Create(ctx, item, input.Meta); err != nil {
					return nil, contentCommandError(err)
				}
				return ResultForResource(contentMutation(item), input.ContentType, item.ID), nil
			},
		},
		{
			Name: ToolContentUpdate, Title: "Update content", Description: "Update safe fields on one content item using ownership checks and optimistic locking.",
			InputSchema: updateContentInputSchema, OutputSchema: contentMutationOutputSchema,
			Mutability: MutabilityWrite, Risk: RiskWrite, Idempotent: true, Permission: contentWrite,
			ResolvePermission: contentOwnerResolver(services, contentWrite), Timeout: 15 * time.Second, MaxConcurrency: 4,
			Handler: func(ctx context.Context, invocation Invocation) (any, error) {
				var input updateContentInput
				if json.Unmarshal(invocation.Arguments, &input) != nil {
					return nil, NewError(CodeInvalidArguments, "invalid content update arguments")
				}
				expected, err := parseExpectedTime(input.ExpectedUpdatedAt)
				if err != nil {
					return nil, err
				}
				item, err := services.ContentRepo.QueryContext(ctx).ID(input.ID).Type(input.ContentType).WithMeta().First()
				if err != nil {
					return nil, NewError(CodeNotFound, "content not found")
				}
				if item.Status == content.StatusTrash {
					return nil, NewError(CodeConflict, "trashed content must be restored before editing")
				}
				definition, err := editableDefinition(services.ContentRegistry, input.ContentType)
				if err != nil {
					return nil, err
				}
				if err := applyContentPatch(ctx, services.ContentRepo, definition, item, input); err != nil {
					return nil, err
				}
				if err := services.ContentCommands.UpdateOptimistic(ctx, input.ContentType, item, input.Meta, expected); err != nil {
					return nil, contentCommandError(err)
				}
				return ResultForResource(contentMutation(item), input.ContentType, item.ID), nil
			},
		},
		transitionTool(ToolContentPublish, "Publish content", "Publish one content item after explicit confirmation.", RiskPublish, true,
			PermissionRequirement{Scope: ScopeContentPublish, Resource: "content", Action: "publish"}, services,
			func(ctx context.Context, input contentTransitionInput, expected time.Time) (*content.Content, error) {
				return services.ContentCommands.PublishOne(ctx, input.ContentType, input.ID, expected)
			}),
		transitionTool(ToolContentTrash, "Move content to trash", "Move one content item to trash after explicit confirmation.", RiskDestructive, true, contentDelete, services,
			func(ctx context.Context, input contentTransitionInput, expected time.Time) (*content.Content, error) {
				return services.ContentCommands.MoveToTrash(ctx, input.ContentType, input.ID, expected)
			}),
		transitionTool(ToolContentRestore, "Restore content", "Restore one matching trashed content item as a draft.", RiskWrite, false,
			PermissionRequirement{Scope: ScopeContentWrite, Resource: "content", Action: "update"}, services,
			func(ctx context.Context, input contentTransitionInput, expected time.Time) (*content.Content, error) {
				return services.ContentCommands.Restore(ctx, input.ContentType, input.ID, expected)
			}),
		{
			Name: ToolMediaUpdateMetadata, Title: "Update media metadata", Description: "Update only alt text, title, and caption using ownership checks and optimistic locking.",
			InputSchema: updateMediaInputSchema, OutputSchema: mediaMutationOutputSchema,
			Mutability: MutabilityWrite, Risk: RiskWrite, Idempotent: true, Permission: mediaWrite,
			ResolvePermission: mediaOwnerResolver(services, mediaWrite), Timeout: 15 * time.Second, MaxConcurrency: 4,
			Handler: func(ctx context.Context, invocation Invocation) (any, error) {
				var input updateMediaInput
				if json.Unmarshal(invocation.Arguments, &input) != nil {
					return nil, NewError(CodeInvalidArguments, "invalid media update arguments")
				}
				expected, err := parseExpectedTime(input.ExpectedUpdatedAt)
				if err != nil {
					return nil, err
				}
				if input.AltText == nil && input.Title == nil && input.Caption == nil {
					return nil, NewError(CodeInvalidArguments, "at least one media metadata field is required")
				}
				item, err := services.MediaRepo.UpdateMetadataOptimistic(ctx, input.ID, expected, input.AltText, input.Title, input.Caption)
				if err != nil {
					return nil, mediaCommandError(err)
				}
				output := mediaMutationOutput{ID: item.ID, AltText: item.AltText, Title: item.Title, Caption: item.Caption,
					UploadedBy: item.UploadedBy, UpdatedAt: item.UpdatedAt.UTC().Format(time.RFC3339Nano)}
				return ResultForResource(output, "media", item.ID), nil
			},
		},
	}
}

type transitionCommand func(context.Context, contentTransitionInput, time.Time) (*content.Content, error)

func transitionTool(name, title, description string, risk RiskLevel, confirm bool, permission PermissionRequirement, services CoreToolServices, command transitionCommand) Tool {
	return Tool{
		Name: name, Title: title, Description: description, InputSchema: contentTransitionInputSchema,
		OutputSchema: contentMutationOutputSchema, Mutability: MutabilityWrite, Risk: risk,
		Idempotent: true, RequiresConfirmation: confirm, Permission: permission,
		ResolvePermission: contentOwnerResolver(services, permission), Timeout: 15 * time.Second, MaxConcurrency: 4,
		Handler: func(ctx context.Context, invocation Invocation) (any, error) {
			var input contentTransitionInput
			if json.Unmarshal(invocation.Arguments, &input) != nil {
				return nil, NewError(CodeInvalidArguments, "invalid content transition arguments")
			}
			expected, err := parseExpectedTime(input.ExpectedUpdatedAt)
			if err != nil {
				return nil, err
			}
			item, err := command(ctx, input, expected)
			if err != nil {
				return nil, contentCommandError(err)
			}
			return ResultForResource(contentMutation(item), input.ContentType, item.ID), nil
		},
	}
}

func contentOwnerResolver(services CoreToolServices, base PermissionRequirement) PermissionResolver {
	return func(ctx context.Context, _ Principal, arguments json.RawMessage) (PermissionRequirement, error) {
		var input struct {
			ContentType string `json:"content_type"`
			ID          uint   `json:"id"`
		}
		if json.Unmarshal(arguments, &input) != nil {
			return PermissionRequirement{}, NewError(CodeInvalidArguments, "invalid content identity")
		}
		if _, err := editableDefinition(services.ContentRegistry, input.ContentType); err != nil {
			return PermissionRequirement{}, err
		}
		item, err := services.ContentRepo.QueryContext(ctx).ID(input.ID).Type(input.ContentType).First()
		if err != nil {
			return PermissionRequirement{}, NewError(CodeNotFound, "content not found")
		}
		base.ResourceOwnerID = item.AuthorID
		return base, nil
	}
}

func mediaOwnerResolver(services CoreToolServices, base PermissionRequirement) PermissionResolver {
	return func(_ context.Context, _ Principal, arguments json.RawMessage) (PermissionRequirement, error) {
		var input struct {
			ID uint `json:"id"`
		}
		if json.Unmarshal(arguments, &input) != nil {
			return PermissionRequirement{}, NewError(CodeInvalidArguments, "invalid media identity")
		}
		item, err := services.MediaRepo.FindByID(input.ID)
		if err != nil {
			return PermissionRequirement{}, NewError(CodeNotFound, "media not found")
		}
		base.ResourceOwnerID = item.UploadedBy
		return base, nil
	}
}

func editableContentTypeResolver(registry *content.Registry, base PermissionRequirement) PermissionResolver {
	return func(_ context.Context, _ Principal, arguments json.RawMessage) (PermissionRequirement, error) {
		var input struct {
			ContentType string `json:"content_type"`
		}
		if json.Unmarshal(arguments, &input) != nil {
			return PermissionRequirement{}, NewError(CodeInvalidArguments, "invalid content type")
		}
		if _, err := editableDefinition(registry, input.ContentType); err != nil {
			return PermissionRequirement{}, err
		}
		return base, nil
	}
}

func editableDefinition(registry *content.Registry, contentType string) (*content.ContentTypeDef, error) {
	if registry == nil {
		return nil, NewError(CodeInternal, "content registry unavailable")
	}
	definition := registry.GetType(strings.TrimSpace(contentType))
	if definition == nil || definition.ReadOnly {
		return nil, NewError(CodeNotFound, "editable content type not found")
	}
	return definition, nil
}

func validateDraftFields(definition *content.ContentTypeDef, input createDraftInput) error {
	if strings.TrimSpace(input.Title) == "" {
		return NewError(CodeInvalidArguments, "title is required")
	}
	if input.Content != "" && !definition.SupportsFeature("content") {
		return unsupportedContentField("content")
	}
	if input.Excerpt != "" && !definition.SupportsFeature("excerpt") {
		return unsupportedContentField("excerpt")
	}
	if input.ImageURL != "" && !definition.SupportsFeature("thumbnail") {
		return unsupportedContentField("image_url")
	}
	if input.ParentID != nil && !definition.Hierarchical {
		return unsupportedContentField("parent_id")
	}
	if input.CommentStatus != "" && !definition.SupportsFeature("comments") {
		return unsupportedContentField("comment_status")
	}
	return validateRequiredMeta(definition, input.Meta)
}

func applyContentPatch(ctx context.Context, repo *content.Repository, definition *content.ContentTypeDef, item *content.Content, input updateContentInput) error {
	changed := false
	if input.Title != nil {
		item.Title = strings.TrimSpace(*input.Title)
		if item.Title == "" {
			return NewError(CodeInvalidArguments, "title cannot be empty")
		}
		changed = true
	}
	if input.Slug != nil {
		slug := normalizeAgentSlug(*input.Slug)
		if slug == "" {
			return NewError(CodeInvalidArguments, "slug cannot be empty")
		}
		unique, err := repo.EnsureUniqueSlugContext(ctx, item.Type, slug, item.ID)
		if err != nil {
			return WrapError(CodeInternal, "failed to reserve content slug", err)
		}
		item.Slug = unique
		changed = true
	}
	if input.Content != nil {
		if !definition.SupportsFeature("content") {
			return unsupportedContentField("content")
		}
		item.Content = *input.Content
		changed = true
	}
	if input.Excerpt != nil {
		if !definition.SupportsFeature("excerpt") {
			return unsupportedContentField("excerpt")
		}
		item.Excerpt = *input.Excerpt
		changed = true
	}
	if input.ImageURL != nil {
		if !definition.SupportsFeature("thumbnail") {
			return unsupportedContentField("image_url")
		}
		item.ImageURL = strings.TrimSpace(*input.ImageURL)
		changed = true
	}
	if input.CommentStatus != nil {
		if !definition.SupportsFeature("comments") {
			return unsupportedContentField("comment_status")
		}
		item.CommentStatus = strings.TrimSpace(*input.CommentStatus)
		changed = true
	}
	if input.ParentID != nil || input.ClearParent {
		if !definition.Hierarchical {
			return unsupportedContentField("parent_id")
		}
		if input.ClearParent {
			item.ParentID = nil
		} else {
			id := *input.ParentID
			if id == item.ID {
				return NewError(CodeInvalidArguments, "parent_id is invalid")
			}
			if _, err := repo.QueryContext(ctx).ID(id).Type(item.Type).First(); err != nil {
				return NewError(CodeNotFound, "parent content not found")
			}
			item.ParentID = &id
		}
		changed = true
	}
	if len(input.Meta) > 0 {
		changed = true
	}
	if !changed {
		return NewError(CodeInvalidArguments, "at least one editable field is required")
	}
	return validateRequiredMetaPatch(definition, item, input.Meta)
}

func validateRequiredMeta(definition *content.ContentTypeDef, meta map[string]string) error {
	for _, field := range definition.MetaFields {
		if field.Required && strings.TrimSpace(meta[field.Key]) == "" {
			return NewError(CodeInvalidArguments, fmt.Sprintf("meta.%s is required", field.Key))
		}
	}
	return nil
}

func validateRequiredMetaPatch(definition *content.ContentTypeDef, item *content.Content, patch map[string]string) error {
	for _, field := range definition.MetaFields {
		if !field.Required {
			continue
		}
		value, present := patch[field.Key]
		if !present {
			value = item.GetMeta(field.Key)
		}
		if strings.TrimSpace(value) == "" {
			return NewError(CodeInvalidArguments, fmt.Sprintf("meta.%s is required", field.Key))
		}
	}
	return nil
}

func unsupportedContentField(name string) error {
	return NewError(CodeInvalidArguments, name+" is not supported by this content type")
}

func parseExpectedTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, NewError(CodeInvalidArguments, "expected_updated_at must be RFC3339")
	}
	return parsed.UTC(), nil
}

func contentMutation(item *content.Content) contentMutationOutput {
	return contentMutationOutput{ID: item.ID, Type: item.Type, Status: item.Status, Title: item.Title, Slug: item.Slug,
		AuthorID: item.AuthorID, UpdatedAt: item.UpdatedAt.UTC().Format(time.RFC3339Nano)}
}

func contentCommandError(err error) error {
	switch {
	case errors.Is(err, content.ErrContentNotFound), errors.Is(err, content.ErrContentTypeNotFound), errors.Is(err, content.ErrContentReadOnly):
		return NewError(CodeNotFound, "content not found")
	case errors.Is(err, content.ErrOptimisticLock):
		return NewError(CodeConflict, "content changed since it was read")
	case errors.Is(err, content.ErrInvalidTransition), errors.Is(err, content.ErrReservedSlug), errors.Is(err, content.ErrUnsupportedMeta), errors.Is(err, content.ErrInvalidStatus):
		return WrapError(CodeInvalidArguments, "content mutation was rejected", err)
	default:
		return WrapError(CodeInternal, "content mutation failed", err)
	}
}

func mediaCommandError(err error) error {
	switch {
	case errors.Is(err, media.ErrMediaNotFound):
		return NewError(CodeNotFound, "media not found")
	case errors.Is(err, media.ErrOptimisticLock):
		return NewError(CodeConflict, "media changed since it was read")
	default:
		return WrapError(CodeInternal, "media mutation failed", err)
	}
}

func normalizeAgentSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	dash := false
	count := 0
	for _, char := range value {
		if unicode.IsLetter(char) || unicode.IsDigit(char) {
			if dash && builder.Len() > 0 {
				builder.WriteByte('-')
			}
			dash = false
			builder.WriteRune(char)
			count++
			if count >= 120 {
				break
			}
		} else {
			dash = true
		}
	}
	return strings.Trim(builder.String(), "-")
}

var createDraftInputSchema = json.RawMessage(`{"type":"object","required":["content_type","title","idempotency_key"],"properties":{"content_type":{"type":"string","minLength":1,"maxLength":50},"title":{"type":"string","minLength":1,"maxLength":500},"slug":{"type":"string","maxLength":500},"content":{"type":"string","maxLength":1000000},"excerpt":{"type":"string","maxLength":20000},"image_url":{"type":"string","maxLength":500},"parent_id":{"type":"integer","minimum":1},"comment_status":{"type":"string","enum":["open","closed"]},"meta":{"type":"object","additionalProperties":{"type":"string","maxLength":100000}},"idempotency_key":{"type":"string","minLength":8,"maxLength":200}},"additionalProperties":false}`)
var updateContentInputSchema = json.RawMessage(`{"type":"object","required":["content_type","id","expected_updated_at","idempotency_key"],"properties":{"content_type":{"type":"string","minLength":1,"maxLength":50},"id":{"type":"integer","minimum":1},"expected_updated_at":{"type":"string","maxLength":40},"title":{"type":"string","maxLength":500},"slug":{"type":"string","maxLength":500},"content":{"type":"string","maxLength":1000000},"excerpt":{"type":"string","maxLength":20000},"image_url":{"type":"string","maxLength":500},"parent_id":{"type":"integer","minimum":1},"clear_parent":{"type":"boolean"},"comment_status":{"type":"string","enum":["open","closed"]},"meta":{"type":"object","additionalProperties":{"type":"string","maxLength":100000}},"idempotency_key":{"type":"string","minLength":8,"maxLength":200}},"additionalProperties":false}`)
var contentTransitionInputSchema = json.RawMessage(`{"type":"object","required":["content_type","id","expected_updated_at","idempotency_key"],"properties":{"content_type":{"type":"string","minLength":1,"maxLength":50},"id":{"type":"integer","minimum":1},"expected_updated_at":{"type":"string","maxLength":40},"idempotency_key":{"type":"string","minLength":8,"maxLength":200},"confirm":{"type":"boolean"}},"additionalProperties":false}`)
var updateMediaInputSchema = json.RawMessage(`{"type":"object","required":["id","expected_updated_at","idempotency_key"],"properties":{"id":{"type":"integer","minimum":1},"expected_updated_at":{"type":"string","maxLength":40},"alt_text":{"type":"string","maxLength":255},"title":{"type":"string","maxLength":255},"caption":{"type":"string","maxLength":20000},"idempotency_key":{"type":"string","minLength":8,"maxLength":200}},"additionalProperties":false}`)
var contentMutationOutputSchema = json.RawMessage(`{"type":"object","required":["id","type","status","title","slug","author_id","updated_at"],"properties":{"id":{"type":"integer","minimum":1},"type":{"type":"string"},"status":{"type":"string"},"title":{"type":"string"},"slug":{"type":"string"},"author_id":{"type":"integer"},"updated_at":{"type":"string"}},"additionalProperties":false}`)
var mediaMutationOutputSchema = json.RawMessage(`{"type":"object","required":["id","alt_text","title","caption","uploaded_by","updated_at"],"properties":{"id":{"type":"integer","minimum":1},"alt_text":{"type":"string"},"title":{"type":"string"},"caption":{"type":"string"},"uploaded_by":{"type":"integer"},"updated_at":{"type":"string"}},"additionalProperties":false}`)
