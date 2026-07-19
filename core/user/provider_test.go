package user

import "testing"

func TestProviderRegistryRejectsExternalBeginURLAndSorts(t *testing.T) {
	registry := NewProviderRegistry()
	if err := registry.Register(ProviderDescriptor{ID: "bad", Label: "Bad", BeginURL: "https://evil.example/login"}); err == nil {
		t.Fatal("external provider begin URL accepted")
	}
	if err := registry.Register(ProviderDescriptor{ID: "bad-icon", Label: "Bad Icon", BeginURL: "/auth/bad/start", IconURL: "https://evil.example/track.svg"}); err == nil {
		t.Fatal("external provider icon URL accepted")
	}
	if err := registry.Register(ProviderDescriptor{ID: "second", Label: "Second", BeginURL: "/auth/second/start", Priority: 20}); err != nil {
		t.Fatalf("Register(second) error = %v", err)
	}
	if err := registry.Register(ProviderDescriptor{ID: "first", Label: "First", BeginURL: "/auth/first/start", Priority: 10}); err != nil {
		t.Fatalf("Register(first) error = %v", err)
	}
	all := registry.All()
	if len(all) != 2 || all[0].ID != "first" || all[1].ID != "second" {
		t.Fatalf("provider order = %#v", all)
	}
}
