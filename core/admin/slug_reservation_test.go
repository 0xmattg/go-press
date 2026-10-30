// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package admin

import "testing"

func TestSlugReservationOnlyForNewOrChangedSlugs(t *testing.T) {
	tests := []struct {
		name, previous, submitted string
		reserve                   bool
	}{
		{"new content with a slug", "", "hello", true},
		{"slug changed on edit", "hello", "hello-world", true},
		{"unchanged slug keeps legacy duplicates as they are", "hello", "hello", false},
		{"empty submission is left to validation", "hello", "", false},
	}
	for _, tt := range tests {
		if got := len(slugReservation(tt.previous, tt.submitted)) > 0; got != tt.reserve {
			t.Errorf("%s: reserve = %v, want %v", tt.name, got, tt.reserve)
		}
	}
}
