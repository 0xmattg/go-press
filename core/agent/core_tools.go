package agent

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/0xmattg/go-press/core/content"
	"github.com/0xmattg/go-press/core/media"
	"github.com/0xmattg/go-press/core/option"
	"github.com/0xmattg/go-press/core/taxonomy"
)

const coreToolOwner = "core"

const (
	ScopeSiteRead     = "gopress:site:read"
	ScopeContentRead  = "gopress:content:read"
	ScopeTaxonomyRead = "gopress:taxonomy:read"
	ScopeMediaRead    = "gopress:media:read"

	// Package-local aliases keep existing Core tests and helpers concise.
	scopeSiteRead     = ScopeSiteRead
	scopeContentRead  = ScopeContentRead
	scopeTaxonomyRead = ScopeTaxonomyRead
	scopeMediaRead    = ScopeMediaRead
)

// CoreReadScopes returns a copy of the scopes that can expose the Phase 2
// read-only Core Tool profile. Protocol adapters may offer only a subset when
// issuing credentials, but must not invent broader Core scopes.
func CoreReadScopes() []string {
	return []string{ScopeSiteRead, ScopeContentRead, ScopeTaxonomyRead, ScopeMediaRead}
}

// CoreToolServices contains only generic Core dependencies used by built-in
// Agent Tools. It intentionally has no Engine, HTTP, theme, plugin, or MCP type.
type CoreToolServices struct {
	ContentRegistry *content.Registry
	ContentRepo     *content.Repository
	ContentCommands *content.CommandService
	TaxonomyRepo    *taxonomy.Repository
	MediaRepo       *media.Repository
	Options         *option.Store
	SiteName        string
	SiteURL         string
	SiteTimezone    string
	CoreVersion     string
}

// RegisterCoreReadTools installs the Phase 1 read-only capability set. If one
// registration fails, previously registered handles are revoked atomically
// from the caller's perspective.
func RegisterCoreReadTools(registry *Registry, services CoreToolServices) ([]*Handle, error) {
	tools := coreReadTools(registry, services)
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

func coreReadTools(agentRegistry *Registry, services CoreToolServices) []Tool {
	contentPermission := PermissionRequirement{Scope: scopeContentRead, Resource: "content", Action: "read"}
	taxonomyPermission := PermissionRequirement{Scope: scopeTaxonomyRead, Resource: "taxonomy", Action: "read"}
	return []Tool{
		{
			Name: "gopress.site.get", Title: "Get GoPress site", Description: "Return safe site metadata and the current Agent capability revision.",
			InputSchema: emptyObjectSchema, OutputSchema: siteOutputSchema,
			Mutability: MutabilityRead, Risk: RiskRead,
			Permission: PermissionRequirement{Scope: scopeSiteRead, Resource: "dashboard", Action: "read"},
			Timeout:    5 * time.Second, MaxConcurrency: 16,
			Handler: func(_ context.Context, _ Invocation) (any, error) {
				name := strings.TrimSpace(services.SiteName)
				description := ""
				language := ""
				timezone := strings.TrimSpace(services.SiteTimezone)
				if services.Options != nil {
					if configured := strings.TrimSpace(services.Options.Get("site_name")); configured != "" {
						name = configured
					}
					description = services.Options.Get("site_description")
					language = services.Options.Get("site_language")
					if configured := strings.TrimSpace(services.Options.Get("site_timezone")); configured != "" {
						timezone = configured
					}
				}
				revision := uint64(0)
				if agentRegistry != nil {
					revision = agentRegistry.Revision()
				}
				return siteOutput{
					Name: name, Description: description, URL: strings.TrimRight(services.SiteURL, "/"),
					Language: language, Timezone: timezone, CoreVersion: services.CoreVersion,
					AgentRevision: revision,
					Capabilities:  []string{"site", "content_types", "content", "taxonomy", "media"},
				}, nil
			},
		},
		{
			Name: "gopress.content_types.list", Title: "List content types", Description: "List active content types and their editable field declarations.",
			InputSchema: emptyObjectSchema, OutputSchema: contentTypesOutputSchema,
			Mutability: MutabilityRead, Risk: RiskRead, Permission: contentPermission,
			Timeout: 5 * time.Second, MaxConcurrency: 16,
			Handler: func(_ context.Context, _ Invocation) (any, error) {
				if services.ContentRegistry == nil {
					return nil, NewError(CodeInternal, "content registry unavailable")
				}
				definitions := services.ContentRegistry.AllTypes()
				sort.Slice(definitions, func(i, j int) bool { return definitions[i].Name < definitions[j].Name })
				items := make([]contentTypeOutput, 0, len(definitions))
				for _, definition := range definitions {
					if definition == nil {
						continue
					}
					fields := make([]metaFieldOutput, 0, len(definition.MetaFields))
					for _, field := range definition.MetaFields {
						fields = append(fields, metaFieldOutput{
							Key: field.Key, Label: field.Label, Type: field.Type,
							Required: field.Required, Options: nonNilStrings(field.Options),
						})
					}
					items = append(items, contentTypeOutput{
						Name: definition.Name, Label: definition.Label, LabelPlural: definition.LabelPlural,
						ReadOnly: definition.ReadOnly, HasArchive: definition.HasArchive,
						Hierarchical: definition.Hierarchical, Supports: nonNilStrings(definition.Supports),
						Taxonomies: nonNilStrings(definition.Taxonomies), MetaFields: fields,
					})
				}
				return contentTypesOutput{Types: items}, nil
			},
		},
		{
			Name: "gopress.content.list", Title: "List content", Description: "Page through content of one registered type using bounded filters.",
			InputSchema: contentListInputSchema, OutputSchema: contentListOutputSchema,
			Mutability: MutabilityRead, Risk: RiskRead, Permission: contentPermission,
			ResolvePermission: contentTypePermissionResolver(services.ContentRegistry, contentPermission),
			Timeout:           10 * time.Second, MaxConcurrency: 8,
			Handler: func(ctx context.Context, invocation Invocation) (any, error) {
				var input contentListInput
				if err := json.Unmarshal(invocation.Arguments, &input); err != nil {
					return nil, NewError(CodeInvalidArguments, "invalid content list arguments")
				}
				page, perPage := boundedPage(input.Page, input.PerPage)
				if services.ContentRepo == nil {
					return nil, NewError(CodeInternal, "content database unavailable")
				}
				query := services.ContentRepo.QueryContext(ctx).Type(input.ContentType)
				status := strings.TrimSpace(input.Status)
				if status == "" {
					status = content.StatusPublished
				}
				query = query.Status(status)
				if search := strings.TrimSpace(input.Search); search != "" {
					query = query.Search(search)
				}
				if input.Taxonomy != "" && input.Term != "" {
					if !taxonomyBelongsToType(services.ContentRegistry, input.ContentType, input.Taxonomy) {
						return nil, NewError(CodeNotFound, "taxonomy is not attached to content type")
					}
					query = query.Taxonomy(input.Taxonomy, input.Term)
				}
				result, err := query.OrderBy("created_at", "DESC").Paginate(page, perPage)
				if err != nil {
					return nil, WrapError(CodeInternal, "failed to list content", err)
				}
				items := make([]contentSummaryOutput, 0, len(result.Items))
				for index := range result.Items {
					items = append(items, summarizeContent(&result.Items[index]))
				}
				return contentListOutput{
					Items: items, Page: result.Page, PerPage: result.PerPage,
					Total: result.Total, TotalPages: result.TotalPages,
				}, nil
			},
		},
		{
			Name: "gopress.content.get", Title: "Get content", Description: "Read one content item by both registered type and ID.",
			InputSchema: contentGetInputSchema, OutputSchema: contentGetOutputSchema,
			Mutability: MutabilityRead, Risk: RiskRead, Permission: contentPermission,
			ResolvePermission: contentTypePermissionResolver(services.ContentRegistry, contentPermission),
			Timeout:           10 * time.Second, MaxConcurrency: 16,
			Handler: func(ctx context.Context, invocation Invocation) (any, error) {
				var input contentGetInput
				if err := json.Unmarshal(invocation.Arguments, &input); err != nil {
					return nil, NewError(CodeInvalidArguments, "invalid content get arguments")
				}
				if services.ContentRepo == nil {
					return nil, NewError(CodeInternal, "content database unavailable")
				}
				item, err := services.ContentRepo.QueryContext(ctx).ID(input.ID).Type(input.ContentType).WithMeta().First()
				if err != nil {
					return nil, NewError(CodeNotFound, "content not found")
				}
				definition := services.ContentRegistry.GetType(input.ContentType)
				return contentGetOutput{Content: detailedContent(item, definition)}, nil
			},
		},
		{
			Name: "gopress.taxonomy.list", Title: "List taxonomies", Description: "List terms attached to one registered content type.",
			InputSchema: taxonomyListInputSchema, OutputSchema: taxonomyListOutputSchema,
			Mutability: MutabilityRead, Risk: RiskRead, Permission: taxonomyPermission,
			ResolvePermission: contentTypePermissionResolver(services.ContentRegistry, taxonomyPermission),
			Timeout:           10 * time.Second, MaxConcurrency: 8,
			Handler: func(ctx context.Context, invocation Invocation) (any, error) {
				var input taxonomyListInput
				if err := json.Unmarshal(invocation.Arguments, &input); err != nil {
					return nil, NewError(CodeInvalidArguments, "invalid taxonomy list arguments")
				}
				names := taxonomyNamesForType(services.ContentRegistry, input.ContentType)
				if input.Taxonomy != "" {
					if !containsString(names, input.Taxonomy) {
						return nil, NewError(CodeNotFound, "taxonomy is not attached to content type")
					}
					names = []string{input.Taxonomy}
				}
				groups := make([]taxonomyGroupOutput, 0, len(names))
				for _, name := range names {
					items, err := services.TaxonomyRepo.ListByTaxonomyContext(ctx, name)
					if err != nil {
						return nil, WrapError(CodeInternal, "failed to list taxonomy", err)
					}
					terms := make([]taxonomyTermOutput, 0, len(items))
					for _, item := range items {
						terms = append(terms, taxonomyTermOutput{
							ID: item.ID, Name: item.Term.Name, Slug: item.Term.Slug,
							Description: item.Description, ParentID: item.ParentID, Count: item.Count,
						})
					}
					groups = append(groups, taxonomyGroupOutput{Name: name, Terms: terms})
				}
				return taxonomyListOutput{Taxonomies: groups}, nil
			},
		},
		{
			Name: "gopress.media.list", Title: "List media", Description: "Page through media metadata without exposing server filesystem paths.",
			InputSchema: mediaListInputSchema, OutputSchema: mediaListOutputSchema,
			Mutability: MutabilityRead, Risk: RiskRead,
			Permission: PermissionRequirement{Scope: scopeMediaRead, Resource: "media", Action: "read"},
			Timeout:    10 * time.Second, MaxConcurrency: 8,
			Handler: func(ctx context.Context, invocation Invocation) (any, error) {
				var input mediaListInput
				if err := json.Unmarshal(invocation.Arguments, &input); err != nil {
					return nil, NewError(CodeInvalidArguments, "invalid media list arguments")
				}
				page, perPage := boundedPage(input.Page, input.PerPage)
				items, total, err := services.MediaRepo.ListContext(ctx, input.MimeType, page, perPage)
				if err != nil {
					return nil, WrapError(CodeInternal, "failed to list media", err)
				}
				output := make([]mediaOutput, 0, len(items))
				for index := range items {
					item := &items[index]
					output = append(output, mediaOutput{
						ID: item.ID, Filename: item.Filename, OriginalName: item.OriginalName,
						MimeType: item.MimeType, Size: item.Size, URL: safeMediaURL(item, services.SiteURL),
						AltText: item.AltText, Title: item.Title, Caption: item.Caption,
						Width: item.Width, Height: item.Height, UploadedBy: item.UploadedBy,
						CreatedAt: item.CreatedAt.UTC().Format(time.RFC3339),
						UpdatedAt: item.UpdatedAt.UTC().Format(time.RFC3339Nano),
					})
				}
				return mediaListOutput{
					Items: output, Page: page, PerPage: perPage, Total: total,
					TotalPages: int(math.Ceil(float64(total) / float64(perPage))),
				}, nil
			},
		},
	}
}

func contentTypePermissionResolver(registry *content.Registry, base PermissionRequirement) PermissionResolver {
	return func(_ context.Context, _ Principal, arguments json.RawMessage) (PermissionRequirement, error) {
		var input struct {
			ContentType string `json:"content_type"`
		}
		if json.Unmarshal(arguments, &input) != nil || registry == nil || registry.GetType(input.ContentType) == nil {
			return PermissionRequirement{}, NewError(CodeNotFound, "content type not found")
		}
		return base, nil
	}
}

func boundedPage(page, perPage int) (int, int) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 20
	}
	if perPage > 100 {
		perPage = 100
	}
	return page, perPage
}

func summarizeContent(item *content.Content) contentSummaryOutput {
	output := contentSummaryOutput{
		ID: item.ID, Type: item.Type, Status: item.Status, Title: item.Title, Slug: item.Slug,
		Excerpt: item.Excerpt, ImageURL: item.ImageURL, AuthorID: item.AuthorID,
		CreatedAt: item.CreatedAt.UTC().Format(time.RFC3339), UpdatedAt: item.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if item.PublishedAt != nil {
		output.PublishedAt = item.PublishedAt.UTC().Format(time.RFC3339)
	}
	return output
}

func detailedContent(item *content.Content, definition *content.ContentTypeDef) contentDetailOutput {
	summary := summarizeContent(item)
	allowed := make(map[string]struct{})
	if definition != nil {
		for _, field := range definition.MetaFields {
			allowed[field.Key] = struct{}{}
		}
		if definition.SupportsFeature("thumbnail") {
			allowed["gallery_images"] = struct{}{}
		}
		if definition.Rewrite.Rootless {
			allowed["page_template"] = struct{}{}
		}
	}
	meta := make(map[string]string)
	for _, row := range item.Meta {
		if _, ok := allowed[row.MetaKey]; ok {
			meta[row.MetaKey] = row.MetaValue
		}
	}
	return contentDetailOutput{contentSummaryOutput: summary, Content: content.SanitizeHTML(item.Content), Meta: meta}
}

func taxonomyNamesForType(registry *content.Registry, contentType string) []string {
	if registry == nil {
		return nil
	}
	seen := make(map[string]struct{})
	if definition := registry.GetType(contentType); definition != nil {
		for _, name := range definition.Taxonomies {
			if registry.GetTaxonomy(name) != nil {
				seen[name] = struct{}{}
			}
		}
	}
	for _, definition := range registry.TaxonomiesForType(contentType) {
		if definition != nil {
			seen[definition.Name] = struct{}{}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func taxonomyBelongsToType(registry *content.Registry, contentType, taxonomyName string) bool {
	return containsString(taxonomyNamesForType(registry, contentType), taxonomyName)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func nonNilStrings(values []string) []string {
	return append([]string{}, values...)
}

func safeMediaURL(item *media.Media, siteURL string) string {
	if item == nil {
		return ""
	}
	path := strings.TrimSpace(item.Path)
	if strings.HasPrefix(path, "https://") || strings.HasPrefix(path, "http://") ||
		strings.HasPrefix(path, "/uploads/") || strings.HasPrefix(path, "uploads/") {
		return item.URL(siteURL)
	}
	return ""
}

type siteOutput struct {
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	URL           string   `json:"url"`
	Language      string   `json:"language"`
	Timezone      string   `json:"timezone"`
	CoreVersion   string   `json:"core_version"`
	AgentRevision uint64   `json:"agent_revision"`
	Capabilities  []string `json:"capabilities"`
}

type metaFieldOutput struct {
	Key      string   `json:"key"`
	Label    string   `json:"label"`
	Type     string   `json:"type"`
	Required bool     `json:"required"`
	Options  []string `json:"options"`
}

type contentTypeOutput struct {
	Name         string            `json:"name"`
	Label        string            `json:"label"`
	LabelPlural  string            `json:"label_plural"`
	ReadOnly     bool              `json:"read_only"`
	HasArchive   bool              `json:"has_archive"`
	Hierarchical bool              `json:"hierarchical"`
	Supports     []string          `json:"supports"`
	Taxonomies   []string          `json:"taxonomies"`
	MetaFields   []metaFieldOutput `json:"meta_fields"`
}

type contentTypesOutput struct {
	Types []contentTypeOutput `json:"types"`
}

type contentListInput struct {
	ContentType string `json:"content_type"`
	Status      string `json:"status"`
	Search      string `json:"search"`
	Taxonomy    string `json:"taxonomy"`
	Term        string `json:"term"`
	Page        int    `json:"page"`
	PerPage     int    `json:"per_page"`
}

type contentGetInput struct {
	ContentType string `json:"content_type"`
	ID          uint   `json:"id"`
}

type contentSummaryOutput struct {
	ID          uint   `json:"id"`
	Type        string `json:"type"`
	Status      string `json:"status"`
	Title       string `json:"title"`
	Slug        string `json:"slug"`
	Excerpt     string `json:"excerpt"`
	ImageURL    string `json:"image_url"`
	AuthorID    uint   `json:"author_id"`
	PublishedAt string `json:"published_at,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type contentDetailOutput struct {
	contentSummaryOutput
	Content string            `json:"content"`
	Meta    map[string]string `json:"meta"`
}

type contentListOutput struct {
	Items      []contentSummaryOutput `json:"items"`
	Page       int                    `json:"page"`
	PerPage    int                    `json:"per_page"`
	Total      int64                  `json:"total"`
	TotalPages int                    `json:"total_pages"`
}

type contentGetOutput struct {
	Content contentDetailOutput `json:"content"`
}

type taxonomyListInput struct {
	ContentType string `json:"content_type"`
	Taxonomy    string `json:"taxonomy"`
}

type taxonomyTermOutput struct {
	ID          uint   `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	ParentID    *uint  `json:"parent_id,omitempty"`
	Count       int    `json:"count"`
}

type taxonomyGroupOutput struct {
	Name  string               `json:"name"`
	Terms []taxonomyTermOutput `json:"terms"`
}

type taxonomyListOutput struct {
	Taxonomies []taxonomyGroupOutput `json:"taxonomies"`
}

type mediaListInput struct {
	MimeType string `json:"mime_type"`
	Page     int    `json:"page"`
	PerPage  int    `json:"per_page"`
}

type mediaOutput struct {
	ID           uint   `json:"id"`
	Filename     string `json:"filename"`
	OriginalName string `json:"original_name"`
	MimeType     string `json:"mime_type"`
	Size         int64  `json:"size"`
	URL          string `json:"url"`
	AltText      string `json:"alt_text"`
	Title        string `json:"title"`
	Caption      string `json:"caption"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	UploadedBy   uint   `json:"uploaded_by"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

type mediaListOutput struct {
	Items      []mediaOutput `json:"items"`
	Page       int           `json:"page"`
	PerPage    int           `json:"per_page"`
	Total      int64         `json:"total"`
	TotalPages int           `json:"total_pages"`
}

var (
	emptyObjectSchema        = json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
	siteOutputSchema         = json.RawMessage(`{"type":"object","required":["name","description","url","language","timezone","core_version","agent_revision","capabilities"],"properties":{"name":{"type":"string"},"description":{"type":"string"},"url":{"type":"string"},"language":{"type":"string"},"timezone":{"type":"string"},"core_version":{"type":"string"},"agent_revision":{"type":"integer","minimum":0},"capabilities":{"type":"array","items":{"type":"string"}}},"additionalProperties":false}`)
	contentTypesOutputSchema = json.RawMessage(`{"type":"object","required":["types"],"properties":{"types":{"type":"array","items":{"type":"object","required":["name","label","label_plural","read_only","has_archive","hierarchical","supports","taxonomies","meta_fields"],"properties":{"name":{"type":"string"},"label":{"type":"string"},"label_plural":{"type":"string"},"read_only":{"type":"boolean"},"has_archive":{"type":"boolean"},"hierarchical":{"type":"boolean"},"supports":{"type":"array","items":{"type":"string"}},"taxonomies":{"type":"array","items":{"type":"string"}},"meta_fields":{"type":"array","items":{"type":"object","required":["key","label","type","required","options"],"properties":{"key":{"type":"string"},"label":{"type":"string"},"type":{"type":"string"},"required":{"type":"boolean"},"options":{"type":"array","items":{"type":"string"}}},"additionalProperties":false}}},"additionalProperties":false}}},"additionalProperties":false}`)
	contentListInputSchema   = json.RawMessage(`{"type":"object","required":["content_type"],"properties":{"content_type":{"type":"string","minLength":1,"maxLength":50},"status":{"type":"string","enum":["published","pending","draft","archived","trash"]},"search":{"type":"string","maxLength":200},"taxonomy":{"type":"string","maxLength":50},"term":{"type":"string","maxLength":200},"page":{"type":"integer","minimum":1,"maximum":100000},"per_page":{"type":"integer","minimum":1,"maximum":100}},"additionalProperties":false}`)
	contentListOutputSchema  = json.RawMessage(`{"type":"object","required":["items","page","per_page","total","total_pages"],"properties":{"items":{"type":"array","items":{"type":"object","required":["id","type","status","title","slug","excerpt","image_url","author_id","created_at","updated_at"],"properties":{"id":{"type":"integer","minimum":1},"type":{"type":"string"},"status":{"type":"string"},"title":{"type":"string"},"slug":{"type":"string"},"excerpt":{"type":"string"},"image_url":{"type":"string"},"author_id":{"type":"integer","minimum":0},"published_at":{"type":"string"},"created_at":{"type":"string"},"updated_at":{"type":"string"}},"additionalProperties":false}},"page":{"type":"integer"},"per_page":{"type":"integer"},"total":{"type":"integer"},"total_pages":{"type":"integer"}},"additionalProperties":false}`)
	contentGetInputSchema    = json.RawMessage(`{"type":"object","required":["content_type","id"],"properties":{"content_type":{"type":"string","minLength":1,"maxLength":50},"id":{"type":"integer","minimum":1}},"additionalProperties":false}`)
	contentGetOutputSchema   = json.RawMessage(`{"type":"object","required":["content"],"properties":{"content":{"type":"object","required":["id","type","status","title","slug","excerpt","image_url","author_id","created_at","updated_at","content","meta"],"properties":{"id":{"type":"integer","minimum":1},"type":{"type":"string"},"status":{"type":"string"},"title":{"type":"string"},"slug":{"type":"string"},"excerpt":{"type":"string"},"image_url":{"type":"string"},"author_id":{"type":"integer","minimum":0},"published_at":{"type":"string"},"created_at":{"type":"string"},"updated_at":{"type":"string"},"content":{"type":"string"},"meta":{"type":"object","additionalProperties":{"type":"string"}}},"additionalProperties":false}},"additionalProperties":false}`)
	taxonomyListInputSchema  = json.RawMessage(`{"type":"object","required":["content_type"],"properties":{"content_type":{"type":"string","minLength":1,"maxLength":50},"taxonomy":{"type":"string","maxLength":50}},"additionalProperties":false}`)
	taxonomyListOutputSchema = json.RawMessage(`{"type":"object","required":["taxonomies"],"properties":{"taxonomies":{"type":"array","items":{"type":"object","required":["name","terms"],"properties":{"name":{"type":"string"},"terms":{"type":"array","items":{"type":"object","required":["id","name","slug","description","count"],"properties":{"id":{"type":"integer"},"name":{"type":"string"},"slug":{"type":"string"},"description":{"type":"string"},"parent_id":{"type":"integer"},"count":{"type":"integer"}},"additionalProperties":false}}},"additionalProperties":false}}},"additionalProperties":false}`)
	mediaListInputSchema     = json.RawMessage(`{"type":"object","properties":{"mime_type":{"type":"string","maxLength":100},"page":{"type":"integer","minimum":1,"maximum":100000},"per_page":{"type":"integer","minimum":1,"maximum":100}},"additionalProperties":false}`)
	mediaListOutputSchema    = json.RawMessage(`{"type":"object","required":["items","page","per_page","total","total_pages"],"properties":{"items":{"type":"array","items":{"type":"object","required":["id","filename","original_name","mime_type","size","url","alt_text","title","caption","width","height","uploaded_by","created_at","updated_at"],"properties":{"id":{"type":"integer"},"filename":{"type":"string"},"original_name":{"type":"string"},"mime_type":{"type":"string"},"size":{"type":"integer"},"url":{"type":"string"},"alt_text":{"type":"string"},"title":{"type":"string"},"caption":{"type":"string"},"width":{"type":"integer"},"height":{"type":"integer"},"uploaded_by":{"type":"integer"},"created_at":{"type":"string"},"updated_at":{"type":"string"}},"additionalProperties":false}},"page":{"type":"integer"},"per_page":{"type":"integer"},"total":{"type":"integer"},"total_pages":{"type":"integer"}},"additionalProperties":false}`)
)
