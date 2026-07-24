package theme

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"go-press/core/content"
	"go-press/core/option"
	"go-press/core/taxonomy"
)

// BasePageService bundles the request-scoped data-access plumbing that every
// theme's PageService needs: the database handle, core repositories, the option
// store, and per-request language scoping. Themes embed it and implement only
// their own content-assembly methods and view models:
//
//	type PageService struct {
//	    coreTheme.BasePageService
//	}
//
//	func NewPageService(engine *core.Engine) *PageService {
//	    return &PageService{coreTheme.NewBasePageService(engine.DB, engine.Content, engine.Taxonomy, engine.Options)}
//	}
//
//	func NewPageServiceDB(db *gorm.DB) *PageService {
//	    return &PageService{coreTheme.NewBasePageServiceDB(db)}
//	}
//
//	func (s *PageService) ForRequest(c *gin.Context) *PageService {
//	    return &PageService{s.BasePageService.ForRequest(c)}
//	}
//
// Fields are exported so embedding theme code can use them directly (s.DB,
// s.Content, s.Tax, s.Options, s.ReqCtx).
type BasePageService struct {
	DB      *gorm.DB
	Content *content.Repository
	Tax     *taxonomy.Repository
	Options *option.Store
	// ReqCtx is set by ForRequest(c). Detail-page lookups need it so
	// per-language slug disambiguation works. Nil for non-request usage
	// (CLI / tests) — scoped APIs treat nil as "no scope".
	ReqCtx *gin.Context
}

// NewBasePageService wires the base from an application's shared, hook-aware
// repositories (the request path). Prefer this over NewBasePageServiceDB inside
// handlers so content mutations still fire hooks.
func NewBasePageService(db *gorm.DB, contentRepo *content.Repository, tax *taxonomy.Repository, options *option.Store) BasePageService {
	return BasePageService{DB: db, Content: contentRepo, Tax: tax, Options: options}
}

// NewBasePageServiceDB builds the base from a bare DB handle, creating fresh
// repositories. Intended for CLI tools and tests; request handlers should
// prefer NewBasePageService with the app's shared repositories.
func NewBasePageServiceDB(db *gorm.DB) BasePageService {
	return BasePageService{
		DB:      db,
		Content: content.NewRepository(db),
		Tax:     taxonomy.NewRepository(db),
		Options: option.NewStore(db),
	}
}

// ForRequest returns a copy of the base scoped to the request: the
// language-scoped DB and the stored context. A nil context returns the receiver
// unchanged. Themes that carry extra fields should preserve them by copying the
// outer struct and replacing only the embedded base:
//
//	clone := *s
//	clone.BasePageService = s.BasePageService.ForRequest(c)
//	return &clone
func (b BasePageService) ForRequest(c *gin.Context) BasePageService {
	if c == nil {
		return b
	}
	scoped := content.ScopedDB(c, b.DB)
	b.ReqCtx = c
	if scoped != b.DB {
		b.DB = scoped
	}
	return b
}

// Settings returns all option values as a flat map.
func (b BasePageService) Settings() map[string]string {
	if b.Options == nil {
		return map[string]string{}
	}
	return b.Options.All()
}
