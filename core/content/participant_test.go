package content

import (
	"context"
	"errors"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func countContent(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var total int64
	if err := db.Model(&Content{}).Count(&total).Error; err != nil {
		t.Fatal(err)
	}
	return total
}

func TestTxParticipantCommitsWithTheContentSave(t *testing.T) {
	db, commands, repository, _ := contentCommandTestRuntime(t)
	var seen uint
	item := &Content{Type: "post", Status: StatusDraft, Title: "Joined", Slug: "joined"}
	err := commands.Create(context.Background(), item, map[string]string{"subtitle": "s"},
		WithTxParticipant(func(tx *gorm.DB, saved *Content) error {
			seen = saved.ID
			// Participants see the row inside the same transaction.
			return tx.Create(&ContentMeta{ContentID: saved.ID, MetaKey: "_participant", MetaValue: "ok"}).Error
		}))
	if err != nil {
		t.Fatal(err)
	}
	if seen == 0 || seen != item.ID {
		t.Fatalf("participant saw ID %d, want %d", seen, item.ID)
	}
	meta, err := repository.GetMeta(item.ID)
	if err != nil || meta["_participant"] != "ok" || meta["subtitle"] != "s" {
		t.Fatalf("meta = %v, %v", meta, err)
	}
	if countContent(t, db) != 1 {
		t.Fatal("expected exactly one content row")
	}
}

func TestFailedParticipantRollsBackCreateAndResetsItem(t *testing.T) {
	db, commands, _, _ := contentCommandTestRuntime(t)
	createPost(t, commands, "taken")
	var events int
	commands.SetMutationObserver(func(context.Context, Mutation) { events++ })

	failure := errors.New("relations failed")
	item := &Content{Type: "post", Status: StatusDraft, Title: "Rolled back", Slug: "taken"}
	err := commands.Create(context.Background(), item, map[string]string{"subtitle": "lost"},
		WithUniqueSlug(),
		WithTxParticipant(func(*gorm.DB, *Content) error { return failure }))
	if !errors.Is(err, failure) {
		t.Fatalf("Create() error = %v, want participant error", err)
	}
	if countContent(t, db) != 1 {
		t.Fatal("rolled-back create left a content row behind")
	}
	var metaRows int64
	if err := db.Model(&ContentMeta{}).Where("meta_value = ?", "lost").Count(&metaRows).Error; err != nil {
		t.Fatal(err)
	}
	if metaRows != 0 {
		t.Fatal("rolled-back create left its meta behind")
	}
	if item.ID != 0 || item.Slug != "taken" {
		t.Fatalf("item after rollback = ID %d slug %q, want ID 0 and the submitted slug", item.ID, item.Slug)
	}
	if events != 0 {
		t.Fatalf("content events fired for a rolled-back save: %d", events)
	}
}

func TestFailedParticipantRollsBackUpdate(t *testing.T) {
	db, commands, repository, _ := contentCommandTestRuntime(t)
	item := createPost(t, commands, "keep")
	if err := repository.SaveMeta(item.ID, "subtitle", "before"); err != nil {
		t.Fatal(err)
	}
	originalUpdatedAt := item.UpdatedAt

	failure := errors.New("relations failed")
	item.Title = "Changed"
	item.Slug = "changed"
	err := commands.Update(context.Background(), "post", item, map[string]string{"subtitle": "after"},
		WithUniqueSlug(),
		WithTxParticipant(func(*gorm.DB, *Content) error { return failure }))
	if !errors.Is(err, failure) {
		t.Fatalf("Update() error = %v, want participant error", err)
	}
	var stored Content
	if err := db.First(&stored, item.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Title != "Hello" || stored.Slug != "keep" {
		t.Fatalf("stored row = %q/%q, want the pre-update values", stored.Title, stored.Slug)
	}
	if meta, _ := repository.GetMeta(item.ID); meta["subtitle"] != "before" {
		t.Fatalf("meta after rolled-back update = %v, want before", meta)
	}
	if item.Slug != "changed" || !item.UpdatedAt.Equal(originalUpdatedAt) {
		t.Fatalf("item after rollback = slug %q updated %v, want submitted slug and original timestamp", item.Slug, item.UpdatedAt)
	}
}

// Front-end submission updates write the row and its meta together.
func TestPublicSubmissionUpdateRollsBackRowWhenMetaFails(t *testing.T) {
	ctx, service, db, owner, id := publicSubmissionUpdateFixture(t)
	failure := errors.New("meta write failed")
	if err := db.Callback().Update().Before("gorm:update").Register("test:fail_meta", func(tx *gorm.DB) {
		if tx.Statement.Table == (ContentMeta{}).TableName() {
			_ = tx.AddError(failure)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Update().Remove("test:fail_meta") })

	_, err := service.UpdateOwn(ctx, id, PublicSubmissionInput{
		UserID: owner, ContentType: "question", Title: "Edited title", Content: "Edited body",
		Meta: map[string]string{"topic": "changed"},
	})
	if !errors.Is(err, failure) {
		t.Fatalf("UpdateOwn() error = %v, want the meta write error", err)
	}
	var stored Content
	if err := db.First(&stored, id).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Title == "Edited title" {
		t.Fatal("row was updated although its meta write failed")
	}
}

func publicSubmissionUpdateFixture(t *testing.T) (*gin.Context, *PublicSubmissionService, *gorm.DB, uint, uint) {
	t.Helper()
	service, repository, users := publicSubmissionTestRuntime(t)
	alice, err := users.FindByID(1)
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.CreateOwn(publicSubmissionContext(alice), PublicSubmissionInput{
		ContentType: "question", UserID: alice.ID, Title: "Original title", Content: "Original body",
		Meta: map[string]string{"topic": "initial"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return publicSubmissionContext(alice), service, repository.db, alice.ID, created.ID
}
