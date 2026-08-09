package content

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func TestContentScopesUseStandardContextWithoutSiblingMutation(t *testing.T) {
	base := context.Background()
	drafts := WithContentScope(base, func(db *gorm.DB) *gorm.DB { return db.Where("status = ?", StatusDraft) })
	published := WithContentScope(base, func(db *gorm.DB) *gorm.DB { return db.Where("status = ?", StatusPublished) })
	combined := WithContentScope(drafts, func(db *gorm.DB) *gorm.DB { return db.Where("author_id = ?", 7) })

	if len(ContentScopes(base)) != 0 || len(ContentScopes(drafts)) != 1 || len(ContentScopes(published)) != 1 || len(ContentScopes(combined)) != 2 {
		t.Fatalf("unexpected scope counts: base=%d drafts=%d published=%d combined=%d",
			len(ContentScopes(base)), len(ContentScopes(drafts)), len(ContentScopes(published)), len(ContentScopes(combined)))
	}
}

func TestRepositoryScopeContextAndGinBridgeSelectSameRows(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, _, repository, _ := contentCommandTestRuntime(t)
	draft := Content{Type: "post", Status: StatusDraft, Title: "Draft", Slug: "shared"}
	published := Content{Type: "post", Status: StatusPublished, Title: "Published", Slug: "shared"}
	if err := db.Create(&draft).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&published).Error; err != nil {
		t.Fatal(err)
	}

	scope := func(db *gorm.DB) *gorm.DB { return db.Where("status = ?", StatusDraft) }
	standard := WithContentScope(context.Background(), scope)
	item, err := repository.FindBySlugContext(standard, "post", "shared")
	if err != nil || item.ID != draft.ID {
		t.Fatalf("standard context selected %+v, err=%v", item, err)
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("GET", "/shared", nil)
	AddContentScope(c, scope)
	item, err = repository.FindBySlugScoped(c, "post", "shared")
	if err != nil || item.ID != draft.ID || len(ContentScopes(RequestContext(c))) != 1 {
		t.Fatalf("gin bridge selected %+v, scopes=%d err=%v", item, len(ContentScopes(RequestContext(c))), err)
	}

	synthetic := &gin.Context{}
	AddContentScope(synthetic, scope)
	item, err = repository.FindBySlugScoped(synthetic, "post", "shared")
	if err != nil || item.ID != draft.ID {
		t.Fatalf("synthetic gin context selected %+v, err=%v", item, err)
	}
}
