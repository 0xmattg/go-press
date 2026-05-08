package content

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ContentScope is a request-local GORM scope for content queries.
//
// Scopes are attached to gin.Context instead of global state so plugins can
// constrain front-end lookup per request. For example, a multilingual plugin can
// limit queries to the active language while still allowing the admin area to
// inspect all rows.
type ContentScope func(db *gorm.DB) *gorm.DB

const contentScopesKey = "core.content_scopes"

// AddContentScope registers a request-scoped content filter in gin.Context.
// Multiple scopes can be registered per request; they are applied in order.
//
// Example usage in plugin middleware:
//
//	content.AddContentScope(c, func(db *gorm.DB) *gorm.DB {
//	    return db.Where("id IN (SELECT content_id FROM translations WHERE language_code = ?)", lang)
//	})
func AddContentScope(c *gin.Context, scope ContentScope) {
	var scopes []ContentScope
	if existing, ok := c.Get(contentScopesKey); ok {
		scopes = existing.([]ContentScope)
	}
	scopes = append(scopes, scope)
	c.Set(contentScopesKey, scopes)
}

// ScopedDB returns a gorm.DB with all request-scoped content filters applied.
// If no scopes are registered in the gin.Context, the original DB is returned unchanged.
// The returned DB uses Session clone mode so it is safe for multiple sequential queries.
func ScopedDB(c *gin.Context, db *gorm.DB) *gorm.DB {
	if c == nil {
		return db
	}
	scopesVal, exists := c.Get(contentScopesKey)
	if !exists {
		return db
	}
	scopes, ok := scopesVal.([]ContentScope)
	if !ok {
		return db
	}
	for _, scope := range scopes {
		db = scope(db)
	}
	// Wrap in Session so each subsequent use (Model, Where, etc.) clones the
	// statement instead of mutating in place. Without this, the first query
	// (e.g. products) pollutes the DB instance and breaks later queries (e.g. services).
	return db.Session(&gorm.Session{})
}
