package taxonomy

import (
	"context"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Scope is a request-local taxonomy query constraint. Key is an opaque
// extension-owned identifier (for example a locale code); core never interprets
// it, but mutation observers can use it to associate newly-created taxonomy
// rows with the same request variant that constrained the query.
type Scope struct {
	Key   string
	Apply func(*gorm.DB) *gorm.DB
}

const scopesKey = "core.taxonomy_scopes"

type scopesContextKey struct{}

// WithScope returns a child context carrying one additional taxonomy scope.
func WithScope(ctx context.Context, scope Scope) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if scope.Apply == nil {
		return ctx
	}
	existing := Scopes(ctx)
	scopes := make([]Scope, 0, len(existing)+1)
	scopes = append(scopes, existing...)
	scopes = append(scopes, scope)
	return context.WithValue(ctx, scopesContextKey{}, scopes)
}

// Scopes returns a copy of the taxonomy scopes carried by ctx.
func Scopes(ctx context.Context) []Scope {
	if ctx == nil {
		return nil
	}
	scopes, _ := ctx.Value(scopesContextKey{}).([]Scope)
	if len(scopes) == 0 {
		return nil
	}
	return append([]Scope(nil), scopes...)
}

// ScopeKey returns the last non-empty opaque key in the scope chain.
func ScopeKey(ctx context.Context) string {
	scopes := Scopes(ctx)
	for i := len(scopes) - 1; i >= 0; i-- {
		if scopes[i].Key != "" {
			return scopes[i].Key
		}
	}
	return ""
}

// RequestContext bridges Gin requests into the framework-neutral context used
// by taxonomy repositories and command services.
func RequestContext(c *gin.Context) context.Context {
	ctx := context.Background()
	if c == nil {
		return ctx
	}
	if c.Request != nil {
		ctx = c.Request.Context()
		if len(Scopes(ctx)) > 0 {
			return ctx
		}
	}
	value, exists := c.Get(scopesKey)
	if !exists {
		return ctx
	}
	scopes, ok := value.([]Scope)
	if !ok {
		return ctx
	}
	for _, scope := range scopes {
		ctx = WithScope(ctx, scope)
	}
	return ctx
}

// AddScope registers a request-scoped taxonomy constraint.
func AddScope(c *gin.Context, scope Scope) {
	if c == nil || scope.Apply == nil {
		return
	}
	var scopes []Scope
	if existing, ok := c.Get(scopesKey); ok {
		scopes, _ = existing.([]Scope)
	}
	scopes = append(append([]Scope(nil), scopes...), scope)
	c.Set(scopesKey, scopes)
	if c.Request != nil {
		c.Request = c.Request.WithContext(WithScope(c.Request.Context(), scope))
	}
}

// ScopedDBContext applies all taxonomy scopes from ctx to db in order.
func ScopedDBContext(ctx context.Context, db *gorm.DB) *gorm.DB {
	if db == nil {
		return nil
	}
	for _, scope := range Scopes(ctx) {
		if scope.Apply != nil {
			db = scope.Apply(db)
		}
	}
	return db.Session(&gorm.Session{})
}

// ScopedDB applies taxonomy scopes stored on a Gin request.
func ScopedDB(c *gin.Context, db *gorm.DB) *gorm.DB {
	return ScopedDBContext(RequestContext(c), db)
}
