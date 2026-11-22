// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package user

import (
	"regexp"
	"sort"
	"strings"
)

// PublicBadge is a short public mark next to a member's name, such as
// "GoPress developer". Extensions supply the data through the
// hook.UserPublicBadges filter; the theme decides how to draw it, so an
// extension never depends on a theme's markup.
type PublicBadge struct {
	// Source names the extension that gives the badge, for example its
	// slug. With Key it identifies the badge; themes may style by it.
	// Lowercase letters, digits and single hyphens.
	Source string
	// Key identifies the badge within its source, for example "developer".
	// Same format as Source.
	Key string
	// Label is plain text, already translated for the request. Required.
	Label string
	// URL is an optional destination, a path on this site starting with
	// "/", for example the member's page in the extension's area.
	URL string
	// Level orders badges and lets themes mark them: BadgeLevelOfficial,
	// BadgeLevelVerified or BadgeLevelInfo (the default).
	Level string
}

// Public badge levels, from the most to the least prominent.
const (
	BadgeLevelOfficial = "official"
	BadgeLevelVerified = "verified"
	BadgeLevelInfo     = "info"
)

// MaxPublicBadges is how many badges a theme shows for a member at most.
const MaxPublicBadges = 3

var badgeIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func badgeLevelRank(level string) int {
	switch level {
	case BadgeLevelOfficial:
		return 0
	case BadgeLevelVerified:
		return 1
	default:
		return 2
	}
}

// NormalizePublicBadges returns the badges a theme may render: badges
// without a label, or whose Source or Key is not a lowercase slug, are
// dropped; a URL that is not a path on this site is cleared; an unknown
// level becomes BadgeLevelInfo; a repeated Source and Key keeps its first
// badge; the rest are sorted by level, Source and Key and cut to
// MaxPublicBadges. The input is not modified.
func NormalizePublicBadges(in []PublicBadge) []PublicBadge {
	out := make([]PublicBadge, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, b := range in {
		b.Label = strings.TrimSpace(b.Label)
		if b.Label == "" || !badgeIDPattern.MatchString(b.Source) || !badgeIDPattern.MatchString(b.Key) {
			continue
		}
		id := b.Source + "/" + b.Key
		if seen[id] {
			continue
		}
		seen[id] = true
		if b.URL != "" && !sitePath(b.URL) {
			b.URL = ""
		}
		switch b.Level {
		case BadgeLevelOfficial, BadgeLevelVerified, BadgeLevelInfo:
		default:
			b.Level = BadgeLevelInfo
		}
		out = append(out, b)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if ri, rj := badgeLevelRank(out[i].Level), badgeLevelRank(out[j].Level); ri != rj {
			return ri < rj
		}
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		return out[i].Key < out[j].Key
	})
	if len(out) > MaxPublicBadges {
		out = out[:MaxPublicBadges]
	}
	return out
}
