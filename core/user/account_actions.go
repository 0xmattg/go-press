// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package user

import (
	"sort"
	"strings"
	"unicode"
)

// AccountAction is an entry a signed-in member sees on their own account
// page, such as the way into a member area an extension provides. Extensions
// supply the data through the hook.UserAccountActions filter; the theme
// decides how to draw it, so an extension never depends on a theme's markup.
type AccountAction struct {
	// ID identifies the entry, for example "market-developer". Themes may
	// use it as an anchor; it also breaks ties when sorting.
	ID string
	// Title, Text and Label are plain text, already translated for the
	// request. Title and Label are required.
	Title string
	Text  string
	Label string
	// URL is the destination, a path on this site starting with "/".
	URL string
	// Badge is a short status, for example "Pending". Optional.
	Badge string
	// Tone colors the badge: "", AccountToneInfo, AccountToneSuccess or
	// AccountToneWarning.
	Tone string
	// Order sorts entries; lower comes first.
	Order int
}

// Account action badge tones.
const (
	AccountToneInfo    = "info"
	AccountToneSuccess = "success"
	AccountToneWarning = "warning"
)

// MaxAccountActions is how many entries an account page shows at most.
const MaxAccountActions = 6

// NormalizeAccountActions returns the entries a theme may render: entries
// without a title or label, or whose URL is not a path on this site, are
// dropped; an unknown tone becomes ""; the rest are sorted by Order and ID
// and cut to MaxAccountActions. The input is not modified.
func NormalizeAccountActions(in []AccountAction) []AccountAction {
	out := make([]AccountAction, 0, len(in))
	for _, a := range in {
		a.Title, a.Label = strings.TrimSpace(a.Title), strings.TrimSpace(a.Label)
		a.Text, a.Badge = strings.TrimSpace(a.Text), strings.TrimSpace(a.Badge)
		if a.Title == "" || a.Label == "" || !sitePath(a.URL) {
			continue
		}
		switch a.Tone {
		case "", AccountToneInfo, AccountToneSuccess, AccountToneWarning:
		default:
			a.Tone = ""
		}
		out = append(out, a)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Order != out[j].Order {
			return out[i].Order < out[j].Order
		}
		return out[i].ID < out[j].ID
	})
	if len(out) > MaxAccountActions {
		out = out[:MaxAccountActions]
	}
	return out
}

// sitePath reports whether u is a path on this site: it starts with a single
// "/", and has no backslash, which some browsers treat as "/", and no
// whitespace or control character.
func sitePath(u string) bool {
	if !strings.HasPrefix(u, "/") || strings.HasPrefix(u, "//") || strings.Contains(u, `\`) {
		return false
	}
	for _, r := range u {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return true
}
