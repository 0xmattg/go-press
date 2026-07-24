package theme

import (
	"testing"

	"go-press/core/content"
	"go-press/core/option"
	"go-press/core/taxonomy"
)

func TestNewBasePageServiceWiresSharedRepos(t *testing.T) {
	contentRepo := &content.Repository{}
	tax := &taxonomy.Repository{}
	opts := &option.Store{}

	b := NewBasePageService(nil, contentRepo, tax, opts)
	if b.Content != contentRepo || b.Tax != tax || b.Options != opts {
		t.Fatal("NewBasePageService did not wire the provided repositories")
	}
	if b.ReqCtx != nil {
		t.Fatal("ReqCtx should start nil")
	}
}

func TestNewBasePageServiceDBCreatesRepos(t *testing.T) {
	b := NewBasePageServiceDB(nil)
	if b.Content == nil || b.Tax == nil || b.Options == nil {
		t.Fatal("NewBasePageServiceDB should create non-nil repositories")
	}
}

func TestForRequestNilContextReturnsReceiverUnchanged(t *testing.T) {
	opts := &option.Store{}
	b := NewBasePageService(nil, nil, nil, opts)
	got := b.ForRequest(nil)
	if got.Options != opts || got.ReqCtx != nil {
		t.Fatal("ForRequest(nil) must return the base unchanged")
	}
}

func TestSettingsNilOptionsIsSafe(t *testing.T) {
	var b BasePageService // zero value, Options == nil
	if m := b.Settings(); m == nil || len(m) != 0 {
		t.Fatalf("Settings() with nil Options = %v, want empty non-nil map", m)
	}
}
