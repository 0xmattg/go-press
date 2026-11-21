// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package user

import (
	"fmt"
	"testing"
)

func TestNormalizeAccountActionsKeepsSafeEntriesInOrder(t *testing.T) {
	in := []AccountAction{
		{ID: "b", Title: "Orders", Label: "Open", URL: "/account/orders", Order: 20},
		{ID: "a", Title: " Developer ", Label: " Apply ", URL: "/market/join?from=profile#top", Badge: " New ", Tone: AccountToneInfo, Order: 10},
		{ID: "c", Title: "Same order", Label: "Go", URL: "/c", Order: 10, Tone: "loud"},
		{ID: "no-title", Label: "Go", URL: "/x"},
		{ID: "no-label", Title: "T", URL: "/x"},
	}
	for i, u := range []string{"", "https://evil.test/x", "//evil.test/x", `/\evil.test`, "x/y", " /x", "/x y", "/x\n", "javascript:alert(1)"} {
		in = append(in, AccountAction{ID: fmt.Sprintf("bad-%d", i), Title: "T", Label: "L", URL: u})
	}
	got := NormalizeAccountActions(in)
	if len(got) != 3 || got[0].ID != "a" || got[1].ID != "c" || got[2].ID != "b" {
		t.Fatalf("entries = %+v", got)
	}
	if got[0].Title != "Developer" || got[0].Label != "Apply" || got[0].Badge != "New" || got[0].Tone != AccountToneInfo {
		t.Fatalf("first entry not trimmed: %+v", got[0])
	}
	if got[1].Tone != "" {
		t.Fatalf("unknown tone kept: %q", got[1].Tone)
	}
	if in[1].Title != " Developer " {
		t.Fatal("the input was modified")
	}
}

func TestNormalizeAccountActionsCapsTheList(t *testing.T) {
	var in []AccountAction
	for i := 0; i < MaxAccountActions+3; i++ {
		in = append(in, AccountAction{ID: fmt.Sprintf("e%02d", i), Title: "T", Label: "L", URL: "/e", Order: -i})
	}
	got := NormalizeAccountActions(in)
	if len(got) != MaxAccountActions || got[0].ID != fmt.Sprintf("e%02d", MaxAccountActions+2) {
		t.Fatalf("entries = %+v", got)
	}
	if len(NormalizeAccountActions(nil)) != 0 {
		t.Fatal("no entries expected")
	}
}
