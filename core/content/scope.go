package content

import (
	"context"

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

type contentScopesContextKey struct{}

// WithContentScope returns a child context carrying one additional content
// query scope. The stored slice is copied so sibling requests cannot mutate
// each other's scope chain.
func WithContentScope(ctx context.Context, scope ContentScope) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if scope == nil {
		return ctx
	}
	existing := ContentScopes(ctx)
	scopes := make([]ContentScope, 0, len(existing)+1)
	scopes = append(scopes, existing...)
	scopes = append(scopes, scope)
	return context.WithValue(ctx, contentScopesContextKey{}, scopes)
}

// ContentScopes returns a snapshot of the content scopes carried by ctx.
func ContentScopes(ctx context.Context) []ContentScope {
	if ctx == nil {
		return nil
	}
	scopes, _ := ctx.Value(contentScopesContextKey{}).([]ContentScope)
	if len(scopes) == 0 {
		return nil
	}
	return append([]ContentScope(nil), scopes...)
}

// RequestContext bridges a Gin request into the framework-neutral context
// used by repositories and domain services. Legacy scopes stored directly on
// gin.Context remain supported for synthetic contexts created by plugins.
func RequestContext(c *gin.Context) context.Context {
	ctx := context.Background()
	if c == nil {
		return ctx
	}
	if c.Request != nil {
		ctx = c.Request.Context()
		if len(ContentScopes(ctx)) > 0 {
			return ctx
		}
	}
	value, exists := c.Get(contentScopesKey)
	if !exists {
		return ctx
	}
	scopes, ok := value.([]ContentScope)
	if !ok {
		return ctx
	}
	for _, scope := range scopes {
		ctx = WithContentScope(ctx, scope)
	}
	return ctx
}

// AddContentScope registers a request-scoped content filter in gin.Context.
// Multiple scopes can be registered per request; they are applied in order.
//
// Example usage in plugin middleware:
//
//	content.AddContentScope(c, func(db *gorm.DB) *gorm.DB {
//	    return db.Where("id IN (SELECT content_id FROM translations WHERE language_code = ?)", lang)
//	})
func AddContentScope(c *gin.Context, scope ContentScope) {
	if c == nil || scope == nil {
		return
	}
	var scopes []ContentScope
	if existing, ok := c.Get(contentScopesKey); ok {
		scopes, _ = existing.([]ContentScope)
	}
	scopes = append(append([]ContentScope(nil), scopes...), scope)
	c.Set(contentScopesKey, scopes)
	if c.Request != nil {
		ctx := WithContentScope(c.Request.Context(), scope)
		c.Request = c.Request.WithContext(ctx)
	}
}

// ScopedDBContext returns a GORM session with all content scopes from a
// standard context applied in registration order.
func ScopedDBContext(ctx context.Context, db *gorm.DB) *gorm.DB {
	if db == nil {
		return nil
	}
	scopes := ContentScopes(ctx)
	if len(scopes) == 0 {
		return db
	}
	for _, scope := range scopes {
		if scope != nil {
			db = scope(db)
		}
	}
	return db.Session(&gorm.Session{})
}

// ScopedDB returns a gorm.DB with all request-scoped content filters applied.
// If no scopes are registered in the gin.Context, the original DB is returned unchanged.
// The returned DB uses Session clone mode so it is safe for multiple sequential queries.
func ScopedDB(c *gin.Context, db *gorm.DB) *gorm.DB {
	return ScopedDBContext(RequestContext(c), db)
}
