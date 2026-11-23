// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

// Package notice holds the data of the admin notice center: notices that
// core and extensions show as banners at the top of every admin page, and
// each user's record of the notices they snoozed or acknowledged.
//
// Sources add notices through the hook.AdminNotices filter. The admin
// normalizes what the filter returns with Normalize, shows each user the
// notices their role may read and has not dismissed, and stores dismissals
// per user (see Dismissals). Everything here is plain data; the admin owns
// the markup, so a source never returns HTML.
package notice

import (
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Notice is one message for the admin notice center. Title, Message and the
// link label are plain text, already translated for the admin language the
// filter receives; the admin escapes them, so HTML shows as text.
type Notice struct {
	// ID identifies the notice within its source, for example
	// "gateway-config". Letters, digits and single ".", "_", ":" or "-"
	// between them, at most 64 characters.
	ID string
	// Revision is the content revision, 1 or higher. Raise it when the
	// content changes in a way users must read again: users who
	// acknowledged an earlier revision see the notice again. Fixing a typo
	// should not raise it.
	Revision int
	// Source names who provides the notice: a plugin's slug, or
	// "core.<name>" for core. Lowercase letters, digits and single ".",
	// "_" or "-" between them, at most 64 characters.
	Source string
	// Category labels the notice: CategorySecurity, CategoryMaintenance,
	// CategoryAnnouncement, CategoryDeprecation or CategoryConfiguration.
	// Other values are kept and shown with the announcement label, so new
	// categories need no core upgrade. Empty means CategoryAnnouncement.
	Category string
	// Severity sets the color and order: SeverityCritical, SeverityWarning
	// or SeverityInfo. Empty or unknown means SeverityInfo.
	Severity string
	// Title is required: at most MaxTitleRunes characters, longer titles
	// are cut. Message is optional: at most MaxMessageRunes characters,
	// line breaks are kept.
	Title   string
	Message string
	// Link is optional; see Link.
	Link *Link
	// Snoozable shows the "×" button, which hides the notice for the user
	// for RemindAfter; when it has passed and the notice still applies, it
	// shows again. Acknowledgeable shows the "Got it" button, which hides
	// this revision for the user for good. With neither, the notice stays
	// until it expires or its source stops providing it.
	Snoozable       bool
	Acknowledgeable bool
	// RemindAfter is how long "×" hides the notice: MinRemindAfter to
	// MaxRemindAfter, DefaultRemindAfter when zero; values outside the range
	// are clamped.
	RemindAfter time.Duration
	// Resource and Action name the capability needed to see the notice;
	// empty means "notice" and "read". Point them at the capability of the
	// people who can fix the problem, for example a plugin settings
	// capability.
	Resource string
	Action   string
	// PublishedAt orders notices of the same severity, newest first.
	// Optional.
	PublishedAt time.Time
	// ExpiresAt hides the notice from that time on. Zero means no end date:
	// the notice shows while its source keeps providing it, which suits a
	// notice that reports a current state, such as a missing setting.
	ExpiresAt time.Time
}

// Link is a notice's call to action.
type Link struct {
	// URL is an https address, or a path in the admin starting with
	// "/admin/". Any other link is dropped, and the notice is kept.
	URL string
	// Label is plain text, at most MaxLinkLabelRunes characters. Empty
	// shows a generic label.
	Label string
}

// Severities.
const (
	SeverityCritical = "critical"
	SeverityWarning  = "warning"
	SeverityInfo     = "info"
)

// Categories with their own label in the admin.
const (
	CategorySecurity      = "security"
	CategoryMaintenance   = "maintenance"
	CategoryAnnouncement  = "announcement"
	CategoryDeprecation   = "deprecation"
	CategoryConfiguration = "configuration"
)

// Limits applied by Normalize.
const (
	MaxPerSource      = 10
	MaxTitleRunes     = 120
	MaxMessageRunes   = 1000
	MaxLinkLabelRunes = 60
	maxNameLength     = 64
	maxCategoryLength = 32

	DefaultRemindAfter = 24 * time.Hour
	MinRemindAfter     = time.Hour
	MaxRemindAfter     = 7 * 24 * time.Hour

	// DefaultResource and DefaultAction form the capability a notice
	// needs when it names none.
	DefaultResource = "notice"
	DefaultAction   = "read"
)

var (
	sourcePattern   = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)*$`)
	idPattern       = regexp.MustCompile(`^[A-Za-z0-9]+(?:[._:-][A-Za-z0-9]+)*$`)
	categoryPattern = regexp.MustCompile(`^[a-z0-9]+(?:[_-][a-z0-9]+)*$`)
	capPattern      = regexp.MustCompile(`^[a-z0-9_-]+$`)
)

// Problem says why Normalize dropped a notice or part of it.
type Problem struct {
	Source string
	ID     string
	Reason string
}

// Reasons reported in Problem.
const (
	ReasonInvalidSource = "invalid source"
	ReasonInvalidID     = "invalid id"
	ReasonDuplicate     = "duplicate id"
	ReasonNoTitle       = "no title"
	ReasonInvalidAccess = "invalid resource or action"
	ReasonTooMany       = "more than 10 notices from this source"
	ReasonLinkDropped   = "link dropped: not https or an /admin/ path"
)

// Normalize returns the notices the admin may show, from what the filter
// returned, sorted by severity and then by PublishedAt, newest first. It
// fills defaults, cleans and cuts text, drops a link that is neither https
// nor an /admin/ path, drops notices that expired by now, and drops invalid
// notices: no valid source or ID, no title, an invalid capability, the same
// source and ID twice, or more than MaxPerSource from one source. Problems
// lists what it dropped. The input is not modified.
func Normalize(in []Notice, now time.Time) ([]Notice, []Problem) {
	var problems []Problem
	out := make([]Notice, 0, len(in))
	seen := make(map[string]bool, len(in))
	perSource := make(map[string]int)
	for _, n := range in {
		n.Source = strings.TrimSpace(n.Source)
		n.ID = strings.TrimSpace(n.ID)
		problem := func(reason string) {
			problems = append(problems, Problem{Source: n.Source, ID: n.ID, Reason: reason})
		}
		if len(n.Source) > maxNameLength || !sourcePattern.MatchString(n.Source) {
			problem(ReasonInvalidSource)
			continue
		}
		if len(n.ID) > maxNameLength || !idPattern.MatchString(n.ID) {
			problem(ReasonInvalidID)
			continue
		}
		if !n.ExpiresAt.IsZero() && !now.Before(n.ExpiresAt) {
			continue
		}
		n.Title = cleanText(n.Title, false, MaxTitleRunes)
		if n.Title == "" {
			problem(ReasonNoTitle)
			continue
		}
		n.Message = cleanText(n.Message, true, MaxMessageRunes)
		n.Resource, n.Action = strings.TrimSpace(n.Resource), strings.TrimSpace(n.Action)
		if n.Resource == "" && n.Action == "" {
			n.Resource, n.Action = DefaultResource, DefaultAction
		}
		if !capPattern.MatchString(n.Resource) || !capPattern.MatchString(n.Action) {
			problem(ReasonInvalidAccess)
			continue
		}
		key := n.Source + "/" + n.ID
		if seen[key] {
			problem(ReasonDuplicate)
			continue
		}
		if perSource[n.Source] >= MaxPerSource {
			problem(ReasonTooMany)
			continue
		}
		seen[key] = true
		perSource[n.Source]++

		if n.Revision < 1 {
			n.Revision = 1
		}
		n.Severity = normalizeSeverity(n.Severity)
		n.Category = strings.ToLower(strings.TrimSpace(n.Category))
		if n.Category == "" || len(n.Category) > maxCategoryLength || !categoryPattern.MatchString(n.Category) {
			n.Category = CategoryAnnouncement
		}
		n.RemindAfter = clampRemind(n.RemindAfter)
		if n.Link != nil {
			link := Link{URL: strings.TrimSpace(n.Link.URL), Label: cleanText(n.Link.Label, false, MaxLinkLabelRunes)}
			if ValidLinkURL(link.URL) {
				n.Link = &link
			} else {
				n.Link = nil
				problem(ReasonLinkDropped)
			}
		}
		out = append(out, n)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if ra, rb := severityRank(a.Severity), severityRank(b.Severity); ra != rb {
			return ra < rb
		}
		if !a.PublishedAt.Equal(b.PublishedAt) {
			return a.PublishedAt.After(b.PublishedAt)
		}
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		return a.ID < b.ID
	})
	return out, problems
}

// ValidLinkURL reports whether u may be a notice link: an https address
// with a host and no user name or password, or a path in the admin starting
// with "/admin/". Neither may contain a backslash, whitespace or a control
// character.
func ValidLinkURL(u string) bool {
	if u == "" || strings.Contains(u, `\`) {
		return false
	}
	for _, r := range u {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	if strings.HasPrefix(u, "/") {
		return strings.HasPrefix(u, "/admin/")
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return false
	}
	return parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil
}

// IsExternal reports whether the link leaves the admin.
func (l *Link) IsExternal() bool {
	return l != nil && !strings.HasPrefix(l.URL, "/")
}

func normalizeSeverity(s string) string {
	switch s = strings.ToLower(strings.TrimSpace(s)); s {
	case SeverityCritical, SeverityWarning:
		return s
	default:
		return SeverityInfo
	}
}

func severityRank(s string) int {
	switch s {
	case SeverityCritical:
		return 0
	case SeverityWarning:
		return 1
	default:
		return 2
	}
}

func clampRemind(d time.Duration) time.Duration {
	switch {
	case d == 0:
		return DefaultRemindAfter
	case d < MinRemindAfter:
		return MinRemindAfter
	case d > MaxRemindAfter:
		return MaxRemindAfter
	default:
		return d
	}
}

// cleanText trims s, removes control and bidirectional formatting
// characters (which can make text read backwards), keeps line breaks when
// multiline is set, and cuts it to max characters, ending with "…" when
// cut.
func cleanText(s string, multiline bool, max int) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n' && multiline:
			b.WriteRune(r)
		case r == '\n' || r == '\r' || r == '\t':
			b.WriteRune(' ')
		case r == utf8.RuneError, unicode.IsControl(r), unicode.In(r, unicode.Bidi_Control),
			r == '\u2028', r == '\u2029', r == '\ufeff':
		default:
			b.WriteRune(r)
		}
	}
	s = strings.TrimSpace(b.String())
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return strings.TrimSpace(string(runes[:max-1])) + "…"
}
