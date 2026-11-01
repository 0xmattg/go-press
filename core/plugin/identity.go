// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package plugin

import (
	"regexp"
	"strings"

	"github.com/0xmattg/go-press/pkg/dbprefix"
)

// slugPattern is the syntax shared by theme and plugin slugs: lowercase letters
// and digits, in words joined by single hyphens. Underscores are not allowed,
// so every slug derives exactly one storage slug.
var slugPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// ValidSlug reports whether s is a valid theme or plugin slug: 2–64
// characters matching slugPattern. An extension's directory name must equal
// its slug.
func ValidSlug(s string) bool {
	return len(s) >= 2 && len(s) <= 64 && slugPattern.MatchString(s)
}

// StorageSlug derives the database name segment for a theme or plugin slug by
// replacing hyphens with underscores, e.g. "commerce-usdt" → "commerce_usdt".
// Table names are always derived this way; there is no per-extension override.
func StorageSlug(slug string) string {
	return strings.ReplaceAll(slug, "-", "_")
}

// Table returns the prefixed name of one of this plugin's tables, derived from
// its slug: Table("events") for "gopress-analytics" is
// "<prefix>plgn_gopress_analytics_events".
func (m Meta) Table(name string) string {
	return dbprefix.PluginTable(StorageSlug(m.Slug), name)
}
