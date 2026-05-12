package admin

import (
	"testing"

	"go-press/core/content"
)

func TestEnsurePublishedAtForPublishedSetsMissingTime(t *testing.T) {
	item := &content.Content{Status: content.StatusPublished}

	ensurePublishedAtForPublished(item)

	if item.PublishedAt == nil {
		t.Fatal("PublishedAt should be set for published content")
	}
}

func TestEnsurePublishedAtForPublishedPreservesExistingTime(t *testing.T) {
	item := &content.Content{Status: content.StatusPublished}
	ensurePublishedAtForPublished(item)
	first := item.PublishedAt

	ensurePublishedAtForPublished(item)

	if item.PublishedAt != first {
		t.Fatal("PublishedAt should not be replaced when already set")
	}
}

func TestEnsurePublishedAtForPublishedLeavesDraftUnset(t *testing.T) {
	item := &content.Content{Status: content.StatusDraft}

	ensurePublishedAtForPublished(item)

	if item.PublishedAt != nil {
		t.Fatal("PublishedAt should remain nil for draft content")
	}
}
