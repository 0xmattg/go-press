// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package menu

import (
	"reflect"
	"testing"
)

func locationNames(s *Store) []string {
	var names []string
	for _, loc := range s.GetLocations() {
		names = append(names, loc.Name)
	}
	return names
}

func TestLocationsKeepRegistrationOrderAndReset(t *testing.T) {
	s := NewStore(nil)
	s.RegisterLocation("header", "Header")
	s.RegisterLocation("footer", "Footer")
	s.RegisterLocation("sidebar", "Sidebar")
	// Re-registering keeps the original position but takes the new label.
	s.RegisterLocation("header", "Top navigation")

	if got, want := locationNames(s), []string{"header", "footer", "sidebar"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("locations = %v, want %v", got, want)
	}
	if got := s.GetLocations()[0].Label; got != "Top navigation" {
		t.Fatalf("re-registered label = %q", got)
	}

	s.ResetLocations()
	if got := s.GetLocations(); len(got) != 0 {
		t.Fatalf("after reset, locations = %v", got)
	}
	s.RegisterLocation("footer", "Footer")
	if got, want := locationNames(s), []string{"footer"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after reset and re-register, locations = %v, want %v", got, want)
	}
}

func TestStagedLocationsReplaceCurrentOnlyOnCommit(t *testing.T) {
	s := NewStore(nil)
	s.RegisterLocation("header", "Header")
	s.RegisterLocation("footer", "Footer")

	s.StageLocations()
	s.RegisterLocation("top-nav", "Top")
	s.RegisterLocation("sidebar", "Sidebar")
	s.RegisterLocation("top-nav", "Top navigation") // relabel keeps first position
	if got := locationNames(s); !reflect.DeepEqual(got, []string{"header", "footer"}) {
		t.Fatalf("while staged, GetLocations = %v, want the current list", got)
	}
	s.CommitLocations()
	if got := locationNames(s); !reflect.DeepEqual(got, []string{"top-nav", "sidebar"}) {
		t.Fatalf("after commit = %v", got)
	}
	if label := s.GetLocations()[0].Label; label != "Top navigation" {
		t.Fatalf("label = %q", label)
	}

	// Discard keeps the current list; registering afterwards is direct again.
	s.StageLocations()
	s.RegisterLocation("other", "Other")
	s.DiscardLocations()
	if got := locationNames(s); !reflect.DeepEqual(got, []string{"top-nav", "sidebar"}) {
		t.Fatalf("after discard = %v", got)
	}
	s.CommitLocations() // nothing staged: no change
	s.RegisterLocation("footer", "Footer")
	if got := locationNames(s); !reflect.DeepEqual(got, []string{"top-nav", "sidebar", "footer"}) {
		t.Fatalf("direct registration after discard = %v", got)
	}
}
