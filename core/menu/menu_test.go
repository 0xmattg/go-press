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
