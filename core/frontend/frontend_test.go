// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package frontend

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestScriptURLAndTag(t *testing.T) {
	data, err := os.ReadFile(ScriptName)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	wantURL := "/static/gopress/ui.js?v=" + hex.EncodeToString(sum[:6])
	if got := ScriptURL(); got != wantURL {
		t.Fatalf("ScriptURL() = %s, want %s", got, wantURL)
	}
	if got := string(ScriptTag()); got != `<script src="`+wantURL+`" defer></script>` {
		t.Fatalf("ScriptTag() = %s", got)
	}
}

func TestContentType(t *testing.T) {
	if ctype, ok := ContentType("ui.js"); !ok || ctype != "text/javascript; charset=utf-8" {
		t.Fatalf("ui.js: %q, %v", ctype, ok)
	}
	for _, name := range []string{"", ".", "/", "missing.js", "frontend.go", "../frontend/ui.js/x"} {
		if ctype, ok := ContentType(name); ok {
			t.Errorf("%q: %q, want no asset", name, ctype)
		}
	}
}

// The script only changes state. It must not create markup or text, run
// strings as code, read URLs from attributes or talk to the network
// (interactions.md, "What the components do not do").
func TestScriptAvoidsForbiddenAPIs(t *testing.T) {
	data, err := os.ReadFile(ScriptName)
	if err != nil {
		t.Fatal(err)
	}
	code := stripComments(string(data))
	if !strings.Contains(code, "'use strict'") {
		t.Fatal("the script is not in strict mode")
	}
	forbidden := map[string]string{
		`\b(innerHTML|outerHTML|insertAdjacent(HTML|Text|Element))\b`:             "writes markup",
		`\bdocument\s*\.\s*write`:                                                 "writes markup",
		`\bcreate(Element|ElementNS|TextNode|DocumentFragment|Comment|Range)\b`:   "creates nodes",
		`\b(appendChild|insertBefore|replaceChild|replaceChildren|replaceWith)\b`: "inserts nodes",
		`\.\s*(append|prepend|before|after)\s*\(`:                                 "inserts nodes",
		`\.\s*(textContent|innerText|outerText|nodeValue|data)\s*=[^=]`:           "writes text",
		`\beval\b|\bFunction\s*\(`:                                                "runs strings as code",
		`\bset(Timeout|Interval)\s*\(\s*['"\x60]`:                                 "runs strings as code",
		`\bimport\s*\(|\bimportScripts\b`:                                         "loads code",
		`\b(fetch|XMLHttpRequest|WebSocket|EventSource|sendBeacon)\b`:             "makes requests",
		`\b(location|open)\s*\(|\blocation\b|\bwindow\s*\.\s*open\b`:              "navigates",
		`\.\s*(href|src|action|formAction)\b|getAttribute\(\s*'(href|src|action)`: "reads URLs",
		`\b(cookie|localStorage|sessionStorage|indexedDB|postMessage)\b`:          "uses storage or messaging",
	}
	for pattern, why := range forbidden {
		if loc := regexp.MustCompile(pattern).FindStringIndex(code); loc != nil {
			line := strings.Count(code[:loc[0]], "\n") + 1
			t.Errorf("ui.js line %d %s: %q", line, why, code[loc[0]:loc[1]])
		}
	}
}

// stripComments blanks // and /* */ comments, keeping line numbers. The
// script has no string or regular expression containing those sequences.
func stripComments(code string) string {
	code = regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllStringFunc(code, func(c string) string {
		return strings.Repeat("\n", strings.Count(c, "\n"))
	})
	return regexp.MustCompile(`(?m)(^|\s)//.*$`).ReplaceAllString(code, "$1")
}
