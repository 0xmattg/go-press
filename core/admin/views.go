package admin

import (
	"time"

	"github.com/0xmattg/go-press/core/audit"
	"github.com/0xmattg/go-press/core/content"
)

// DynamicContentView is a generic view model for any registered content type.
// Used by the data-driven admin to render list/form pages dynamically.
type DynamicContentView struct {
	ID            uint
	Title         string
	AuthorID      uint
	AuthorName    string
	Slug          string
	Permalink     string
	Content       string
	Excerpt       string
	ImageURL      string
	Status        string
	CommentStatus string
	SortOrder     int
	PublishedAt   *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Meta          map[string]string             // dynamic meta fields from ContentTypeDef.MetaFields
	Taxonomies    map[string][]TaxonomyItemView // taxonomy type name -> items
}

// AdminListColumn declares one configurable table column for an admin list page.
type AdminListColumn struct {
	Key      string
	Label    string
	Required bool
	Visible  bool
}

// AdminListScreenOptions describes the reusable "Screen Options" panel for
// admin list pages. Pages provide columns dynamically; the container only
// renders and persists the chosen state.
type AdminListScreenOptions struct {
	Key            string
	ActionURL      string
	Columns        []AdminListColumn
	PerPage        int
	PerPageChoices []int
}

// AdminPaginationView contains service-side pagination metadata and URLs.
type AdminPaginationView struct {
	Total      int64
	Page       int
	PerPage    int
	TotalPages int
	From       int64
	To         int64
	Offset     int
	FirstURL   string
	PrevURL    string
	NextURL    string
	LastURL    string
}

// AdminListFilterOption is one selectable value in a list filter dropdown.
type AdminListFilterOption struct {
	Value    string
	Label    string
	Selected bool
}

// AdminContentListFilters describes the basic filters above a content table.
type AdminContentListFilters struct {
	ActionURL       string
	DateOptions     []AdminListFilterOption
	TaxonomyLabel   string
	TaxonomyOptions []AdminListFilterOption
}

// AdminHiddenInput preserves non-page query parameters inside small GET forms.
type AdminHiddenInput struct {
	Name  string
	Value string
}

// TaxonomyItemView is a generic taxonomy term view.
type TaxonomyItemView struct {
	ID             uint
	Name           string
	Slug           string
	Description    string
	ParentID       uint
	ParentName     string
	ReferenceCount int64
}

// ContentTypeStats holds a count for a single content type on the dashboard.
type ContentTypeStats struct {
	TypeDef *content.ContentTypeDef
	Count   int64
}

// DashboardStats provides dynamic counts for the dashboard overview.
type DashboardStats struct {
	ContentStats []ContentTypeStats
	MediaCount   int64
	UserCount    int64
}

// AuditLog remains an alias for template and extension compatibility. The
// persistence model is owned by the framework-neutral audit package.
type AuditLog = audit.Event

// SettingItemView maps option.Option to template fields.
type SettingItemView struct {
	Key         string
	Label       string
	Description string
	ReadOnly    bool
	Value       string
	Group       string
	InputType   string
	Options     []SettingOptionView
}

// SettingOptionView describes a selectable setting option.
type SettingOptionView struct {
	Value string
	Label string
}

// MailSettingsView contains SMTP transport config plus notification switches.
type MailSettingsView struct {
	Driver                   string
	DriverOptions            []SettingOptionView
	Enabled                  bool
	Host                     string
	Port                     int
	Encryption               string
	EncryptionOptions        []SettingOptionView
	Username                 string
	HasMailKey               bool
	FromEmail                string
	FromName                 string
	ReplyTo                  string
	TimeoutSeconds           int
	ContactMessageNotify     bool
	ContactMessageRecipients string
	DefaultContactRecipients string
	TestRecipient            string
}

// AdminMenuItem represents a dynamic sidebar menu item.
type AdminMenuItem struct {
	Label   string
	URL     string
	Active  string // key used for active class matching
	Icon    string
	Section string // non-empty means this is a section header
	// Resource/Action optionally declare the capability required to display the
	// item. Empty values preserve the existing behavior for core-owned entries.
	Resource string
	Action   string
}

// TaxonomyFormData holds taxonomy data for rendering form selectors.
type TaxonomyFormData struct {
	TaxDef      *content.TaxonomyDef
	AllItems    []TaxonomyItemView
	SelectedID  uint          // for hierarchical (category-like)
	SelectedMap map[uint]bool // for non-hierarchical (tag-like)
}
