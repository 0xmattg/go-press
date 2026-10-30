// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package content

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"
)

func createPost(t *testing.T, commands *CommandService, slug string, opts ...MutationOption) *Content {
	t.Helper()
	item := &Content{Type: "post", Status: StatusDraft, Title: "Hello", Slug: slug}
	if err := commands.Create(context.Background(), item, nil, opts...); err != nil {
		t.Fatalf("Create(%q) error = %v", slug, err)
	}
	return item
}

func TestWithUniqueSlugSuffixesNewSlugsAndKeepsOwnSlugOnUpdate(t *testing.T) {
	db, commands, _, _ := contentCommandTestRuntime(t)

	first := createPost(t, commands, "hello")
	second := createPost(t, commands, "hello", WithUniqueSlug())
	third := createPost(t, commands, "hello", WithUniqueSlug())
	if first.Slug != "hello" || second.Slug != "hello-1" || third.Slug != "hello-2" {
		t.Fatalf("slugs = %q, %q, %q; want hello, hello-1, hello-2", first.Slug, second.Slug, third.Slug)
	}

	// Saving an item under its own slug must not rename it.
	second.Title = "Renamed"
	if err := commands.Update(context.Background(), "post", second, nil, WithUniqueSlug()); err != nil {
		t.Fatal(err)
	}
	if second.Slug != "hello-1" {
		t.Fatalf("own slug after update = %q, want hello-1", second.Slug)
	}
	// Changing to a taken slug picks the first free variant, ignoring itself.
	third.Slug = "hello"
	if err := commands.Update(context.Background(), "post", third, nil, WithUniqueSlug()); err != nil {
		t.Fatal(err)
	}
	var stored Content
	if err := db.First(&stored, third.ID).Error; err != nil {
		t.Fatal(err)
	}
	if third.Slug != "hello-2" || stored.Slug != "hello-2" {
		t.Fatalf("updated slug = %q (stored %q), want hello-2", third.Slug, stored.Slug)
	}
}

func TestWithUniqueSlugIsPerTypeAndCountsTrashedContent(t *testing.T) {
	_, commands, _, _ := contentCommandTestRuntime(t)
	post := createPost(t, commands, "about")

	service := &Content{Type: "service", Status: StatusDraft, Title: "About", Slug: "about"}
	if err := commands.Create(context.Background(), service, nil, WithUniqueSlug()); err != nil {
		t.Fatal(err)
	}
	if service.Slug != "about" {
		t.Fatalf("other type slug = %q, want about (uniqueness is per type)", service.Slug)
	}

	if _, err := commands.MoveToTrash(context.Background(), "post", post.ID, post.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	next := createPost(t, commands, "about", WithUniqueSlug())
	if next.Slug != "about-1" {
		t.Fatalf("slug next to trashed content = %q, want about-1 (trash keeps its slug for restore)", next.Slug)
	}
}

func TestCreateWithoutUniqueSlugKeepsSubmittedSlug(t *testing.T) {
	_, commands, _, _ := contentCommandTestRuntime(t)
	createPost(t, commands, "same")
	if again := createPost(t, commands, "same"); again.Slug != "same" {
		t.Fatalf("slug without WithUniqueSlug = %q, want the submitted value", again.Slug)
	}
}

func TestReserveUniqueSlugHonorsRequestScopesAndRequiresSlug(t *testing.T) {
	db, commands, repository, _ := contentCommandTestRuntime(t)
	hidden := createPost(t, commands, "news")

	// A scope such as a language filter hides rows from other scopes, so the
	// same slug stays available inside this scope.
	ctx := WithContentScope(context.Background(), func(q *gorm.DB) *gorm.DB {
		return q.Where("id <> ?", hidden.ID)
	})
	var reserved string
	if err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		reserved, err = ReserveUniqueSlug(ctx, tx, "post", "news", 0)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if reserved != "news" {
		t.Fatalf("scoped reservation = %q, want news", reserved)
	}

	clone := &Content{Type: "post", Status: StatusDraft, Title: "News", Slug: "news"}
	if err := repository.CreateWithUniqueSlug(context.Background(), clone); err != nil {
		t.Fatal(err)
	}
	if clone.Slug != "news-1" {
		t.Fatalf("unscoped CreateWithUniqueSlug = %q, want news-1", clone.Slug)
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		_, err := ReserveUniqueSlug(context.Background(), tx, "post", "  ", 0)
		return err
	})
	if !errors.Is(err, ErrSlugRequired) {
		t.Fatalf("empty slug error = %v, want ErrSlugRequired", err)
	}
}
