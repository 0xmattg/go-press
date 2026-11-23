// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/0xmattg/go-press/core/updatecheck"
)

func TestNoticeKeygenSignVerify(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) (int, string, string) {
		var out, errOut bytes.Buffer
		code := runNotice(args, &out, &errOut)
		return code, out.String(), errOut.String()
	}
	prefix := filepath.Join(dir, "notice-a")
	if code, out, _ := run("keygen", "-out", prefix); code != 0 || !strings.Contains(out, "kid:") {
		t.Fatalf("keygen = %d %s", code, out)
	}
	if info, err := os.Stat(prefix + ".key"); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("private key file: %v %v", info, err)
	}
	pub, err := os.ReadFile(prefix + ".pub")
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	doc := updatecheck.NoticePayload{
		Schema: updatecheck.NoticesSchema, Sequence: 1,
		GeneratedAt: now.Format(time.RFC3339), ExpiresAt: now.Add(30 * 24 * time.Hour).Format(time.RFC3339),
		Notices: []updatecheck.RemoteNotice{{
			ID: "gp-2026-001", Severity: "critical", Category: "security",
			Title:       map[string]string{"en": "Security fix"},
			Message:     map[string]string{"en": "Upgrade soon."},
			Affects:     []updatecheck.NoticeTarget{{Kind: updatecheck.KindCore, Slug: "gopress", Versions: "<0.7.52"}},
			PublishedAt: now.Format(time.RFC3339), ExpiresAt: now.Add(14 * 24 * time.Hour).Format(time.RFC3339),
		}},
	}
	raw, _ := json.Marshal(doc)
	docPath := filepath.Join(dir, "bundle.json")
	if err := os.WriteFile(docPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	signed := filepath.Join(dir, "signed.json")
	if code, out, errOut := run("sign", "-key", prefix+".key", "-out", signed, docPath); code != 0 {
		t.Fatalf("sign = %d %s %s", code, out, errOut)
	}
	code, out, errOut := run("verify", "-pub", strings.TrimSpace(string(pub)), signed)
	if code != 0 || !strings.Contains(out, "sequence 1") || !strings.Contains(out, "gp-2026-001 r1 critical security: Security fix") ||
		!strings.Contains(out, "affects core gopress <0.7.52") {
		t.Fatalf("verify = %d\n%s\n%s", code, out, errOut)
	}
	// Without -pub it uses the keys built into GoPress, which did not sign
	// this test bundle.
	if code, _, errOut := run("verify", signed); code != 1 || !strings.Contains(errOut, "no signature by a trusted key") {
		t.Fatalf("verify with the built-in keys = %d %s", code, errOut)
	}
	// Another key does not verify it.
	other := filepath.Join(dir, "notice-b")
	run("keygen", "-out", other)
	otherPub, _ := os.ReadFile(other + ".pub")
	if code, _, _ := run("verify", "-pub", strings.TrimSpace(string(otherPub)), signed); code != 1 {
		t.Fatalf("verify with another key = %d", code)
	}

	// The tool refuses a bundle sites would not take in full.
	doc.Notices[0].LinkURL = "https://example.com/"
	raw, _ = json.Marshal(doc)
	_ = os.WriteFile(docPath, raw, 0o644)
	if code, _, errOut := run("sign", "-key", prefix+".key", docPath); code != 1 || !strings.Contains(errOut, "link_url") {
		t.Fatalf("sign with a foreign link = %d %s", code, errOut)
	}
	if code, _, _ := run("sign", docPath); code != 2 {
		t.Fatalf("sign without -key = %d", code)
	}
	if code, _, _ := run("frobnicate"); code != 2 {
		t.Fatal("unknown command")
	}
}
