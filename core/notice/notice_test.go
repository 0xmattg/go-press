// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package notice

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

var testNow = time.Date(2026, 11, 23, 8, 0, 0, 0, time.UTC)

func valid(source, id string) Notice {
	return Notice{Source: source, ID: id, Title: "Title " + id}
}

func TestNormalizeFillsDefaults(t *testing.T) {
	out, problems := Normalize([]Notice{valid("commerce", "gateway")}, testNow)
	if len(problems) != 0 || len(out) != 1 {
		t.Fatalf("out = %+v, problems = %+v", out, problems)
	}
	n := out[0]
	if n.Revision != 1 || n.Severity != SeverityInfo || n.Category != CategoryAnnouncement {
		t.Fatalf("defaults = revision %d, severity %q, category %q", n.Revision, n.Severity, n.Category)
	}
	if n.Resource != DefaultResource || n.Action != DefaultAction {
		t.Fatalf("capability = %s.%s, want notice.read", n.Resource, n.Action)
	}
	if n.RemindAfter != DefaultRemindAfter {
		t.Fatalf("RemindAfter = %v, want %v", n.RemindAfter, DefaultRemindAfter)
	}
}

func TestNormalizeClampsRemindAfter(t *testing.T) {
	for _, test := range []struct {
		in, want time.Duration
	}{
		{0, DefaultRemindAfter},
		{time.Minute, MinRemindAfter},
		{30 * 24 * time.Hour, MaxRemindAfter},
		{3 * time.Hour, 3 * time.Hour},
	} {
		n := valid("core.test", "a")
		n.RemindAfter = test.in
		out, _ := Normalize([]Notice{n}, testNow)
		if out[0].RemindAfter != test.want {
			t.Errorf("RemindAfter %v -> %v, want %v", test.in, out[0].RemindAfter, test.want)
		}
	}
}

func TestNormalizeDropsInvalidNotices(t *testing.T) {
	noTitle := valid("commerce", "no-title")
	noTitle.Title = " \u202e\t "
	badCap := valid("commerce", "bad-cap")
	badCap.Resource, badCap.Action = "commerce.settings", "update"
	expired := valid("commerce", "expired")
	expired.ExpiresAt = testNow
	in := []Notice{
		valid("Commerce", "upper-source"),
		valid("commerce", "with/slash"),
		valid("commerce", "with@at"),
		valid("commerce", strings.Repeat("a", 65)),
		noTitle,
		badCap,
		expired,
		valid("commerce", "kept"),
		valid("commerce", "kept"),
	}
	out, problems := Normalize(in, testNow)
	if len(out) != 1 || out[0].ID != "kept" {
		t.Fatalf("out = %+v, want only kept", out)
	}
	reasons := map[string]int{}
	for _, p := range problems {
		reasons[p.Reason]++
	}
	want := map[string]int{
		ReasonInvalidSource: 1, ReasonInvalidID: 3, ReasonNoTitle: 1,
		ReasonInvalidAccess: 1, ReasonDuplicate: 1,
	}
	for reason, count := range want {
		if reasons[reason] != count {
			t.Errorf("%q reported %d times, want %d (all: %+v)", reason, reasons[reason], count, problems)
		}
	}
	if len(problems) != 7 {
		t.Errorf("problems = %+v; an expired notice is not a problem", problems)
	}
}

func TestNormalizeKeepsTenPerSource(t *testing.T) {
	var in []Notice
	for i := 0; i < 12; i++ {
		in = append(in, valid("noisy", fmt.Sprintf("n%02d", i)))
	}
	in = append(in, valid("quiet", "one"))
	out, problems := Normalize(in, testNow)
	counts := map[string]int{}
	for _, n := range out {
		counts[n.Source]++
	}
	if counts["noisy"] != MaxPerSource || counts["quiet"] != 1 {
		t.Fatalf("counts = %v", counts)
	}
	if len(problems) != 2 || problems[0].Reason != ReasonTooMany {
		t.Fatalf("problems = %+v", problems)
	}
	for _, n := range out {
		if n.ID == "n10" || n.ID == "n11" {
			t.Fatalf("kept %s, want the first ten in the order given", n.ID)
		}
	}
}

func TestNormalizeCleansAndCutsText(t *testing.T) {
	n := valid("core.test", "text")
	n.Title = "  Pay\u202eattention\x00\nnow " + strings.Repeat("x", 200)
	n.Message = "line one\r\nline two\u2028\ufeff<script>alert(1)</script>"
	n.Link = &Link{URL: "https://gopress.xyz/notes", Label: strings.Repeat("y", 80)}
	out, _ := Normalize([]Notice{n}, testNow)
	got := out[0]
	if strings.ContainsAny(got.Title, "\u202e\x00\n") || !strings.HasPrefix(got.Title, "Payattention now ") {
		t.Fatalf("title = %q", got.Title)
	}
	if utf8.RuneCountInString(got.Title) != MaxTitleRunes || !strings.HasSuffix(got.Title, "…") {
		t.Fatalf("title has %d runes, want %d ending with …", utf8.RuneCountInString(got.Title), MaxTitleRunes)
	}
	if got.Message != "line one\nline two<script>alert(1)</script>" {
		t.Fatalf("message = %q; line breaks kept, HTML left as text for the template to escape", got.Message)
	}
	if utf8.RuneCountInString(got.Link.Label) != MaxLinkLabelRunes {
		t.Fatalf("link label has %d runes", utf8.RuneCountInString(got.Link.Label))
	}
	if n.Title == got.Title || n.Link.Label == got.Link.Label {
		t.Fatal("Normalize modified its input")
	}
}

func TestNormalizeLinks(t *testing.T) {
	for _, test := range []struct {
		url  string
		keep bool
	}{
		{"https://gopress.xyz/security/gp-2026-001", true},
		{"/admin/settings#mail", true},
		{"http://gopress.xyz/", false},
		{"javascript:alert(1)", false},
		{"//evil.example/admin/", false},
		{"/settings", false},
		{"/admin\\..\\x", false},
		{"https://user:pass@gopress.xyz/", false},
		{"https:///no-host", false},
		{"https://gopress.xyz/a b", false},
	} {
		n := valid("core.test", "link")
		n.Link = &Link{URL: test.url}
		out, problems := Normalize([]Notice{n}, testNow)
		if len(out) != 1 {
			t.Fatalf("%s: the notice itself must be kept", test.url)
		}
		if kept := out[0].Link != nil; kept != test.keep {
			t.Errorf("%s: link kept = %v, want %v", test.url, kept, test.keep)
		}
		if !test.keep && (len(problems) != 1 || problems[0].Reason != ReasonLinkDropped) {
			t.Errorf("%s: problems = %+v", test.url, problems)
		}
	}
}

func TestNormalizeSorts(t *testing.T) {
	info := valid("b", "info")
	oldWarning := valid("a", "old-warning")
	oldWarning.Severity, oldWarning.PublishedAt = "WARNING", testNow.Add(-time.Hour)
	newWarning := valid("a", "new-warning")
	newWarning.Severity, newWarning.PublishedAt = SeverityWarning, testNow
	critical := valid("c", "critical")
	critical.Severity = SeverityCritical
	unknown := valid("a", "unknown")
	unknown.Severity = "urgent"
	out, _ := Normalize([]Notice{info, oldWarning, unknown, newWarning, critical}, testNow)
	var order []string
	for _, n := range out {
		order = append(order, n.ID)
	}
	if got := strings.Join(order, ","); got != "critical,new-warning,old-warning,unknown,info" {
		t.Fatalf("order = %s", got)
	}
}

func TestNormalizeCategories(t *testing.T) {
	for in, want := range map[string]string{
		"":         CategoryAnnouncement,
		"Security": CategorySecurity,
		"billing":  "billing",
		"<b>":      CategoryAnnouncement,
	} {
		n := valid("core.test", "c")
		n.Category = in
		out, _ := Normalize([]Notice{n}, testNow)
		if out[0].Category != want {
			t.Errorf("category %q -> %q, want %q", in, out[0].Category, want)
		}
	}
}

func TestLinkIsExternal(t *testing.T) {
	if !(&Link{URL: "https://gopress.xyz"}).IsExternal() || (&Link{URL: "/admin/"}).IsExternal() {
		t.Fatal("IsExternal is wrong")
	}
	var none *Link
	if none.IsExternal() {
		t.Fatal("a nil link is not external")
	}
}
