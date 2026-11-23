// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package updatecheck

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/0xmattg/go-press/core/option"
	"github.com/0xmattg/go-press/pkg/sign"
)

var noticeNow = time.Date(2026, 11, 23, 8, 0, 0, 0, time.UTC)

func testNoticeKey(t *testing.T) sign.PrivateKey {
	t.Helper()
	key, err := sign.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func advisory(id string) RemoteNotice {
	return RemoteNotice{
		ID: id, Revision: 1, Category: "security", Severity: "critical",
		Title:       map[string]string{"en": "Security fix in 0.7.52", "zh-CN": "0.7.52 安全修复"},
		Message:     map[string]string{"en": "Upgrade soon."},
		LinkURL:     "https://gopress.xyz/security/" + id,
		Affects:     []NoticeTarget{{Kind: KindCore, Slug: "gopress", Versions: "<0.7.52"}},
		PublishedAt: noticeNow.Add(-time.Hour).Format(time.RFC3339),
		ExpiresAt:   noticeNow.Add(30 * 24 * time.Hour).Format(time.RFC3339),
	}
}

func bundleDoc(t *testing.T, sequence int64, notices ...RemoteNotice) []byte {
	t.Helper()
	raw, err := json.Marshal(NoticePayload{
		Schema: NoticesSchema, Sequence: sequence,
		GeneratedAt: noticeNow.Add(-time.Hour).Format(time.RFC3339),
		ExpiresAt:   noticeNow.Add(60 * 24 * time.Hour).Format(time.RFC3339),
		Notices:     notices,
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func signedBundle(t *testing.T, key sign.PrivateKey, sequence int64, notices ...RemoteNotice) SignedNotices {
	t.Helper()
	env, err := SignNotices(bundleDoc(t, sequence, notices...), noticeNow, key)
	if err != nil {
		t.Fatal(err)
	}
	return env
}

// Changing this list changes who can publish to every GoPress admin, so the
// keys and their IDs are pinned here.
func TestBuiltinNoticeKeysArePinned(t *testing.T) {
	want := []string{
		"wMKHiAQYiG9OghiMivPTR6ORB9cYqAWLbHuUIp0dKV8=",
		"XUoAGEPkGFWbpEtDSwXCtckAdze5iaDzxWnCymZIo0U=",
	}
	if !reflect.DeepEqual(builtinNoticeKeys, want) {
		t.Fatalf("built-in announcement keys changed: %v", builtinNoticeKeys)
	}
	keys := BuiltinNoticeKeys()
	for _, kid := range []string{"32fb4fc1f1904526", "31ff5ee550b94dcc"} {
		if _, ok := keys[kid]; !ok {
			t.Errorf("key %s missing from %v", kid, keys)
		}
	}
	if len(keys) != 2 {
		t.Fatalf("keys = %d, want a primary and a standby", len(keys))
	}
	// No announcement, market or catalog key is used twice.
	for _, kid := range []string{"c43f20fbbb0fa6b8", "0007fc76614ef75d", "c3753561967aac02", "1f277e92513c8b04"} {
		if _, ok := keys[kid]; ok {
			t.Fatalf("market key %s is also an announcement key", kid)
		}
	}
	// A bundle signed by any other key is refused with the built-in keys.
	other := testNoticeKey(t)
	if _, err := ParseNotices(signedBundle(t, other, 1, advisory("x")), keys, noticeNow); !errors.Is(err, ErrNoticesSignature) {
		t.Fatalf("a bundle by another key = %v", err)
	}
}

func TestNoticesSignAndVerify(t *testing.T) {
	key, other := testNoticeKey(t), testNoticeKey(t)
	env := signedBundle(t, key, 7, advisory("gp-2026-001"))
	keys := sign.NewKeySet(key.Public())

	bundle, err := ParseNotices(env, keys, noticeNow)
	if err != nil || bundle.Sequence != 7 || len(bundle.Notices) != 1 || bundle.Notices[0].ID != "gp-2026-001" {
		t.Fatalf("parse = %+v, %v", bundle, err)
	}
	if _, err := ParseNotices(env, sign.NewKeySet(other.Public()), noticeNow); !errors.Is(err, ErrNoticesSignature) {
		t.Fatalf("unknown key = %v", err)
	}
	// A changed byte in the document breaks the signature.
	raw, _ := base64.StdEncoding.DecodeString(env.Payload)
	tampered := env
	tampered.Payload = base64.StdEncoding.EncodeToString([]byte(strings.Replace(string(raw), "Upgrade soon.", "Download a fix.", 1)))
	if _, err := ParseNotices(tampered, keys, noticeNow); !errors.Is(err, ErrNoticesSignature) {
		t.Fatalf("tampered = %v", err)
	}
	// A signature claiming a trusted key that does not verify fails, even
	// next to a good one.
	forged := env
	forged.Signatures = append([]sign.Signature{}, env.Signatures...)
	bad := forged.Signatures[0]
	bad.Sig = base64.StdEncoding.EncodeToString(make([]byte, 64))
	forged.Signatures = []sign.Signature{bad}
	if _, err := ParseNotices(forged, keys, noticeNow); !errors.Is(err, ErrNoticesSignature) {
		t.Fatalf("forged = %v", err)
	}
	// Two keys while rotating: one unknown to this site is fine.
	both, err := SignNotices(bundleDoc(t, 8), noticeNow, key, other)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseNotices(both, keys, noticeNow); err != nil {
		t.Fatalf("rotation = %v", err)
	}
	if _, err := SignNotices(bundleDoc(t, 9), noticeNow, key, other, testNoticeKey(t)); err == nil {
		t.Fatal("at most two signatures")
	}
	wrongFormat := env
	wrongFormat.Format = "gopress-catalog-v1"
	if _, err := ParseNotices(wrongFormat, keys, noticeNow); !errors.Is(err, ErrNoticesFormat) {
		t.Fatalf("format = %v", err)
	}
	// A catalog signature over the same bytes is not an announcement
	// signature: the statements differ.
	sum := sign.CatalogStatement(7, "x")
	catalogSig, _ := key.Sign(sum)
	asCatalog := env
	asCatalog.Signatures = []sign.Signature{catalogSig}
	if _, err := ParseNotices(asCatalog, keys, noticeNow); !errors.Is(err, ErrNoticesSignature) {
		t.Fatalf("catalog signature = %v", err)
	}
}

func TestNoticeEntriesAreCheckedOneByOne(t *testing.T) {
	key := testNoticeKey(t)
	bad := func(edit func(*RemoteNotice)) RemoteNotice {
		n := advisory("bad")
		edit(&n)
		return n
	}
	cases := map[string]RemoteNotice{
		"id":            bad(func(n *RemoteNotice) { n.ID = "Bad_ID" }),
		"severity":      bad(func(n *RemoteNotice) { n.Severity = "urgent" }),
		"no en title":   bad(func(n *RemoteNotice) { n.Title = map[string]string{"zh-CN": "只有中文"} }),
		"no en message": bad(func(n *RemoteNotice) { n.Message = nil }),
		"long title":    bad(func(n *RemoteNotice) { n.Title["en"] = strings.Repeat("x", 121) }),
		"http link":     bad(func(n *RemoteNotice) { n.LinkURL = "http://gopress.xyz/" }),
		"other host":    bad(func(n *RemoteNotice) { n.LinkURL = "https://gopress.xyz.evil.example/" }),
		"other repo":    bad(func(n *RemoteNotice) { n.LinkURL = "https://github.com/0xmattg/go-press-evil" }),
		"user in link":  bad(func(n *RemoteNotice) { n.LinkURL = "https://me@gopress.xyz/" }),
		"constraint":    bad(func(n *RemoteNotice) { n.Affects[0].Versions = "latest please" }),
		"affects kind":  bad(func(n *RemoteNotice) { n.Affects[0].Kind = "site" }),
		"no expiry":     bad(func(n *RemoteNotice) { n.ExpiresAt = "" }),
		"91 days": bad(func(n *RemoteNotice) {
			n.ExpiresAt = noticeNow.Add(-time.Hour + 91*24*time.Hour).Format(time.RFC3339)
		}),
		"remind": bad(func(n *RemoteNotice) { n.RemindAfterHours = 169 }),
	}
	for name, n := range cases {
		doc := bundleDoc(t, 3, advisory("good"), n)
		if _, err := SignNotices(doc, noticeNow, key); err == nil {
			t.Errorf("%s: the signing tool must refuse it", name)
		}
		// Signed by an older tool, sites drop that entry and keep the rest.
		env := signRaw(t, key, 3, doc)
		bundle, err := ParseNotices(env, sign.NewKeySet(key.Public()), noticeNow)
		if err != nil || len(bundle.Notices) != 1 || bundle.Notices[0].ID != "good" || len(bundle.Dropped) != 1 {
			t.Errorf("%s: parse = %+v %v", name, bundle, err)
		}
	}
	for _, link := range []string{"https://gopress.xyz/security", "https://docs.gopress.xyz/", "https://github.com/0xmattg/go-press/releases/tag/v0.7.52"} {
		n := advisory("ok")
		n.LinkURL = link
		if _, err := SignNotices(bundleDoc(t, 1, n), noticeNow, key); err != nil {
			t.Errorf("%s refused: %v", link, err)
		}
	}
	dup := bundleDoc(t, 3, advisory("same"), advisory("same"))
	if _, err := SignNotices(dup, noticeNow, key); err == nil {
		t.Error("duplicate ids must be refused")
	}
	var many []RemoteNotice
	for i := 0; i <= MaxOfficialNotices; i++ {
		many = append(many, advisory("n"+strings.Repeat("x", i+1)))
	}
	if _, err := ParseNotices(signRaw(t, key, 4, bundleDoc(t, 4, many...)), sign.NewKeySet(key.Public()), noticeNow); !errors.Is(err, ErrNoticesFormat) {
		t.Errorf("more than %d announcements = %v", MaxOfficialNotices, err)
	}
	big := advisory("big")
	big.Message = map[string]string{"en": strings.Repeat("x", 1000)}
	for _, lang := range []string{"zh-CN", "ja", "de", "fr", "es", "it", "ko", "pt", "ru"} {
		big.Message[lang] = strings.Repeat("字", 1000)
	}
	if _, err := SignNotices(bundleDoc(t, 5, big), noticeNow, key); !errors.Is(err, ErrNoticesFormat) {
		t.Errorf("a document over 16 KB = %v", err)
	}
}

// signRaw signs a document without the tool's checks, as an older or
// careless signer might.
func signRaw(t *testing.T, key sign.PrivateKey, sequence int64, doc []byte) SignedNotices {
	t.Helper()
	digest := sha256.Sum256(doc)
	sig, err := key.Sign(sign.NoticesStatement(sequence, hex.EncodeToString(digest[:])))
	if err != nil {
		t.Fatal(err)
	}
	return SignedNotices{Format: NoticesFormat, Payload: base64.StdEncoding.EncodeToString(doc), Signatures: []sign.Signature{sig}}
}

func TestExpiredBundle(t *testing.T) {
	key := testNoticeKey(t)
	env := signedBundle(t, key, 7, advisory("a"))
	bundle, err := ParseNotices(env, sign.NewKeySet(key.Public()), noticeNow.Add(61*24*time.Hour))
	if !errors.Is(err, ErrNoticesExpired) || bundle == nil || bundle.Sequence != 7 || len(bundle.Notices) != 0 {
		t.Fatalf("expired = %+v %v", bundle, err)
	}
	if _, err := SignNotices(bundleDoc(t, 8), noticeNow.Add(61*24*time.Hour), key); !errors.Is(err, ErrNoticesExpired) {
		t.Fatalf("signing an expired bundle = %v", err)
	}
}

// noticeService is an update service whose official endpoint answers with
// whatever bundle respond returns.
func noticeService(t *testing.T, key sign.PrivateKey, respond func() *SignedNotices) (*Service, *option.Store, *Registry, *time.Time) {
	t.Helper()
	clock := noticeNow
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var notices *SignedNotices
		if r.URL.Path == "/official" {
			notices = respond()
		} else {
			// Another endpoint's announcements are ignored even when signed.
			n := signedBundle(t, key, 999, advisory("from-elsewhere"))
			notices = &n
		}
		return jsonResponse(http.StatusOK, Response{ProtocolVersion: ProtocolVersion, TTLSeconds: 86400, Notices: notices}), nil
	})}
	store := option.NewMemoryStore(map[string]string{
		option.KeyUpdateCheckEnabled: "1", KeyInstallationID: "instance-123456", KeyPolicyVersion: CurrentPolicyVersion,
	})
	registry := NewRegistry()
	for _, target := range []Target{
		{Kind: KindCore, Slug: "gopress", CurrentVersion: "0.7.50", Endpoint: "http://localhost/official", ReportInstallation: true},
		{Kind: KindPlugin, Slug: "shop", CurrentVersion: "1.0.0", Endpoint: "http://localhost/elsewhere"},
	} {
		if err := registry.Register(target); err != nil {
			t.Fatal(err)
		}
	}
	svc := NewService(store, registry, NewClientWithHTTPClient("GoPress/test", httpClient))
	svc.now = func() time.Time { return clock }
	svc.noticeEndpoint = "http://localhost/official"
	svc.noticeKeys = sign.NewKeySet(key.Public())
	return svc, store, registry, &clock
}

func (s *Service) checkNow(t *testing.T) {
	t.Helper()
	s.state.NextCheckAt = s.now().Add(-time.Minute)
	if err := s.RunDue(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func noticeIDs(s *Service, lang string) []string {
	var out []string
	for _, n := range s.OfficialNotices(lang) {
		out = append(out, n.ID+":"+n.Title)
	}
	return out
}

func TestServiceAcceptsOfficialAnnouncements(t *testing.T) {
	key := testNoticeKey(t)
	var current *SignedNotices
	svc, store, registry, clock := noticeService(t, key, func() *SignedNotices { return current })

	notAffected := advisory("old-themes")
	notAffected.Affects = []NoticeTarget{{Kind: KindTheme, Slug: "aurora", Versions: "<2.0.0"}}
	maintenance := advisory("maintenance")
	maintenance.Severity, maintenance.Category, maintenance.Affects = "info", "maintenance", nil
	maintenance.LinkURL = ""
	maintenance.Title = map[string]string{"en": "Maintenance on 1 December"}
	b := signedBundle(t, key, 5, advisory("gp-2026-001"), notAffected, maintenance)
	current = &b
	svc.checkNow(t)

	got := noticeIDs(svc, "zh-CN")
	if strings.Join(got, ",") != "gp-2026-001:0.7.52 安全修复,maintenance:Maintenance on 1 December" {
		t.Fatalf("notices = %v (affected only, zh-CN text with English fallback, nothing from another endpoint)", got)
	}
	n := svc.OfficialNotices("en")[0]
	if n.Source != OfficialNoticeSource || !n.Snoozable || !n.Acknowledgeable || n.Link == nil || n.Link.URL != "https://gopress.xyz/security/gp-2026-001" {
		t.Fatalf("notice = %+v", n)
	}
	if !strings.Contains(store.Get(KeyOfficialNotices), `"sequence":5`) {
		t.Fatalf("stored = %s", store.Get(KeyOfficialNotices))
	}

	// Upgrading past the affected versions makes it go away.
	if err := registry.Register(Target{Kind: KindCore, Slug: "gopress", CurrentVersion: "0.7.52", Endpoint: "http://localhost/official", ReportInstallation: true}); err != nil {
		t.Fatal(err)
	}
	if got := noticeIDs(svc, "en"); len(got) != 1 || !strings.HasPrefix(got[0], "maintenance:") {
		t.Fatalf("after upgrading = %v", got)
	}

	// An older bundle is refused; the same one changes nothing.
	older := signedBundle(t, key, 4, advisory("replayed"))
	current = &older
	svc.checkNow(t)
	if got := noticeIDs(svc, "en"); len(got) != 1 || strings.Contains(got[0], "replayed") {
		t.Fatalf("after an older bundle = %v", got)
	}
	// A bundle that does not verify keeps what was there.
	forged := signedBundle(t, testNoticeKey(t), 6, advisory("forged"))
	current = &forged
	svc.checkNow(t)
	if got := noticeIDs(svc, "en"); len(got) != 1 || strings.Contains(got[0], "forged") {
		t.Fatalf("after a forged bundle = %v", got)
	}
	// No bundle clears them, and the sequence stays, so the old bundle
	// still cannot come back.
	current = nil
	svc.checkNow(t)
	if got := noticeIDs(svc, "en"); len(got) != 0 {
		t.Fatalf("after no bundle = %v", got)
	}
	current = &b
	svc.checkNow(t)
	if got := noticeIDs(svc, "en"); len(got) != 0 {
		t.Fatalf("a replayed bundle after clearing = %v", got)
	}
	// A newer bundle is taken.
	newer := signedBundle(t, key, 9, maintenance)
	current = &newer
	svc.checkNow(t)
	if got := noticeIDs(svc, "en"); len(got) != 1 {
		t.Fatalf("after a newer bundle = %v", got)
	}

	// The announcement's own expiry, then update checks off, hide them.
	*clock = noticeNow.Add(31 * 24 * time.Hour)
	if got := noticeIDs(svc, "en"); len(got) != 0 {
		t.Fatalf("after the announcement expired = %v", got)
	}
	*clock = noticeNow
	_ = store.Set(option.KeyUpdateCheckEnabled, "0")
	if got := svc.OfficialNotices("en"); got != nil {
		t.Fatalf("with update checks off = %v", got)
	}
	_ = store.Set(option.KeyUpdateCheckEnabled, "1")

	// A restart reads the stored bundle.
	again := NewService(store, registry, nil)
	again.now = svc.now
	if got := noticeIDs(again, "en"); len(got) != 1 || again.official.Sequence != 9 {
		t.Fatalf("after a restart = %v (sequence %d)", got, again.official.Sequence)
	}
}

// A site trusts only the built-in keys: a bundle by any other key is not
// shown, and with no keys at all nothing is.
func TestServiceTrustsOnlyItsKeys(t *testing.T) {
	key := testNoticeKey(t)
	b := signedBundle(t, key, 5, advisory("gp-2026-001"))
	svc, _, _, _ := noticeService(t, key, func() *SignedNotices { return &b })
	svc.noticeKeys = BuiltinNoticeKeys()
	svc.checkNow(t)
	if got := svc.OfficialNotices("en"); len(got) != 0 {
		t.Fatalf("accepted a bundle by a key that is not built in: %v", got)
	}
	svc.noticeKeys = sign.KeySet{}
	svc.official = officialState{}
	svc.checkNow(t)
	if got := svc.OfficialNotices("en"); len(got) != 0 {
		t.Fatalf("accepted without keys: %v", got)
	}
}

func TestCheckNoticePayloadIsStrict(t *testing.T) {
	if b, err := CheckNoticePayload(bundleDoc(t, 3, advisory("ok")), noticeNow); err != nil || len(b.Notices) != 1 {
		t.Fatalf("valid = %+v %v", b, err)
	}
	bad := advisory("bad")
	bad.Severity = "urgent"
	if _, err := CheckNoticePayload(bundleDoc(t, 3, advisory("ok"), bad), noticeNow); !errors.Is(err, ErrNoticesFormat) {
		t.Fatalf("an entry sites would drop = %v", err)
	}
	if _, err := CheckNoticePayload(bundleDoc(t, 3), noticeNow.Add(61*24*time.Hour)); !errors.Is(err, ErrNoticesExpired) {
		t.Fatalf("expired = %v", err)
	}
}
