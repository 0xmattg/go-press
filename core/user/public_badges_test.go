// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package user

import "testing"

func TestNormalizePublicBadges(t *testing.T) {
	in := []PublicBadge{
		{Source: "acme", Key: "fan", Label: "Fan", Level: "gold"},
		{Source: "market", Key: "developer", Label: " Developer ", URL: "/market/developers/lin", Level: BadgeLevelVerified},
		{Source: "market", Key: "developer", Label: "Again", Level: BadgeLevelOfficial},
		{Source: "core", Key: "team", Label: "Team", URL: "https://evil.test/", Level: BadgeLevelOfficial},
		{Source: "Bad Source", Key: "x", Label: "Bad"},
		{Source: "acme", Key: "", Label: "No key"},
		{Source: "acme", Key: "quiet", Label: "  "},
		{Source: "acme", Key: "slash", Label: "Slash", URL: "//evil.test"},
		{Source: "zeta", Key: "extra", Label: "Extra"},
	}
	out := NormalizePublicBadges(in)
	want := []PublicBadge{
		{Source: "core", Key: "team", Label: "Team", URL: "", Level: BadgeLevelOfficial},
		{Source: "market", Key: "developer", Label: "Developer", URL: "/market/developers/lin", Level: BadgeLevelVerified},
		{Source: "acme", Key: "fan", Label: "Fan", Level: BadgeLevelInfo},
	}
	if len(out) != len(want) {
		t.Fatalf("got %d badges %+v, want %d", len(out), out, len(want))
	}
	for i := range want {
		if out[i] != want[i] {
			t.Errorf("badge %d = %+v, want %+v", i, out[i], want[i])
		}
	}
	if in[1].Label != " Developer " {
		t.Fatal("the input was modified")
	}
	if got := NormalizePublicBadges(nil); len(got) != 0 {
		t.Fatalf("nil = %+v", got)
	}
}
