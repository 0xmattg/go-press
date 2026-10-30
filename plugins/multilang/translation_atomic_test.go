package multilang

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/0xmattg/go-press/core/content"
	"github.com/0xmattg/go-press/core/user"

	"gorm.io/gorm"
)

// A translation is the clone, its copied meta, and its translation link. When
// any step fails, none of them may remain: an orphaned clone has no language
// and shows up as a duplicate in the default language.
func TestCreateTranslationRollsBackCloneWhenLinkFails(t *testing.T) {
	p, db := multilangTranslationTestPlugin(t)
	sourceID := seedTranslationSource(t, db, &content.Content{Type: "post", Status: content.StatusPublished, Title: "Shared post", Slug: "shared-post", AuthorID: 1})
	// Put the source in a translation group up front so the only translation
	// row written by the request is the clone's link.
	if err := db.Create(&Translation{Trid: 1, ContentID: sourceID, LanguageCode: "en"}).Error; err != nil {
		t.Fatal(err)
	}
	beforeContents := countContents(t, db)
	var beforeMeta int64
	if err := db.Model(&content.ContentMeta{}).Count(&beforeMeta).Error; err != nil {
		t.Fatal(err)
	}

	if err := db.Callback().Create().Before("gorm:create").Register("test:fail_translation_link", func(tx *gorm.DB) {
		if tx.Statement.Table == (Translation{}).TableName() {
			_ = tx.AddError(errors.New("link write failed"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Create().Remove("test:fail_translation_link") })

	rec := postTranslation(t, p, &user.User{ID: 3, Username: "editor", Role: user.RoleEditor}, sourceID, true)

	if rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), "error=") {
		t.Fatalf("status = %d location = %q, want error redirect", rec.Code, rec.Header().Get("Location"))
	}
	if got := countContents(t, db); got != beforeContents {
		t.Fatalf("contents = %d, want %d (clone must be rolled back)", got, beforeContents)
	}
	var afterMeta int64
	if err := db.Model(&content.ContentMeta{}).Count(&afterMeta).Error; err != nil {
		t.Fatal(err)
	}
	if afterMeta != beforeMeta {
		t.Fatalf("content meta rows = %d, want %d (copied meta must be rolled back)", afterMeta, beforeMeta)
	}
}
