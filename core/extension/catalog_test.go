// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package extension

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/0xmattg/go-press/pkg/sign"
)

// testNow is the clock the catalog tests run at.
var testNow = time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

// testKeys is a market (catalog key, package key) plus one publisher.
type testKeys struct {
	catalog, packages, publisher sign.PrivateKey
}

func newTestKeys(t *testing.T) testKeys {
	t.Helper()
	gen := func() sign.PrivateKey {
		key, err := sign.GenerateKey(nil)
		if err != nil {
			t.Fatal(err)
		}
		return key
	}
	return testKeys{catalog: gen(), packages: gen(), publisher: gen()}
}

// root trusts k's catalog and package keys at endpoint.
func (k testKeys) root(t *testing.T, endpoint string) TrustRoot {
	t.Helper()
	root, err := newTrustRoot(endpoint, []string{k.catalog.Public().String()}, []string{k.packages.Public().String()}, true)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func testDigest(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return "h1:" + base64.StdEncoding.EncodeToString(sum[:])
}

// signedItem returns a free theme item signed by k's publisher and
// countersigned by k's package key.
func (k testKeys) signedItem(t *testing.T, slug, version string) Item {
	t.Helper()
	pub := k.publisher.Public()
	item := Item{
		Kind: KindTheme, Slug: slug, Version: version,
		Digest:      testDigest(slug + version),
		Size:        4096,
		DownloadURL: "https://market.example.com/download/" + slug + "/" + version,
		Pricing:     PricingFree,
		Title:       strings.ToUpper(slug[:1]) + slug[1:],
		Summary:     "A theme.",
		Author:      "Acme",
		Localized:   map[string]ItemText{"zh-CN": {Title: "主题"}},
		External:    []string{"fonts.googleapis.com"},
		Publisher:   ItemPublisher{KID: pub.KID(), PublicKey: pub.String()},
	}
	k.signItem(t, &item, k.packages)
	return item
}

// signItem (re)signs item with k's publisher and the given market key.
func (k testKeys) signItem(t *testing.T, item *Item, market sign.PrivateKey) {
	t.Helper()
	id := sign.PackageID{Kind: item.Kind, Slug: item.Slug, Version: item.Version}
	var err error
	if item.Signatures.Publisher, err = k.publisher.Sign(sign.PublisherStatement(id, item.Digest)); err != nil {
		t.Fatal(err)
	}
	if item.Signatures.Market, err = market.Sign(sign.MarketStatement(id, k.publisher.Public().KID(), item.Digest)); err != nil {
		t.Fatal(err)
	}
}

// testPayload is a catalog document; tests mutate it before signing.
type testPayload struct {
	Schema      int          `json:"schema"`
	Sequence    int64        `json:"sequence"`
	GeneratedAt string       `json:"generated_at"`
	ExpiresAt   string       `json:"expires_at"`
	Items       []Item       `json:"items"`
	Revoked     []Revocation `json:"revoked"`
}

func newPayload(sequence int64, items ...Item) testPayload {
	return testPayload{
		Schema:      CatalogSchema,
		Sequence:    sequence,
		GeneratedAt: testNow.Add(-time.Hour).Format(time.RFC3339),
		ExpiresAt:   testNow.Add(7 * 24 * time.Hour).Format(time.RFC3339),
		Items:       items,
	}
}

func (p testPayload) json(t *testing.T) []byte {
	t.Helper()
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// envelope signs the payload with the given catalog keys.
func (p testPayload) envelope(t *testing.T, keys ...sign.PrivateKey) []byte {
	t.Helper()
	raw, err := SignCatalog(p.json(t), keys...)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestParseCatalogAcceptsSignedCatalog(t *testing.T) {
	k := newTestKeys(t)
	root := k.root(t, "https://market.example.com/api/market/v1")
	p := newPayload(42, k.signedItem(t, "aurora", "1.2.0"), k.signedItem(t, "aurora", "1.3.0-beta.1"))
	p.Revoked = []Revocation{{Kind: KindTheme, Slug: "evil-x", Versions: []string{"*"}, Severity: SeverityCritical, Reason: ReasonMalware}}
	raw := p.envelope(t, k.catalog)

	cat, err := ParseCatalog(raw, root, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if cat.Sequence != 42 || len(cat.Items) != 2 || len(cat.Revoked) != 1 || len(cat.Hidden) != 0 {
		t.Fatalf("catalog = %+v", cat)
	}
	sum := sha256.Sum256(p.json(t))
	if cat.Digest() != hex.EncodeToString(sum[:]) {
		t.Fatalf("Digest() = %q", cat.Digest())
	}
	if !cat.Items[0].Compatible {
		t.Fatal("item without a core constraint must be compatible")
	}
	if got := cat.Find(KindTheme, "aurora", "1.2.0"); got == nil || got.Title != "Aurora" {
		t.Fatalf("Find = %+v", got)
	}
	if cat.Find(KindTheme, "aurora", "9.9.9") != nil {
		t.Fatal("Find returned a version the catalog does not offer")
	}
	if got := cat.Latest(KindTheme, "aurora"); got == nil || got.Version != "1.3.0-beta.1" {
		t.Fatalf("Latest = %+v", got)
	}
	if cat.Latest(KindTheme, "missing") != nil {
		t.Fatal("Latest returned an item for an unknown slug")
	}
}

func TestParseCatalogDuringKeyRotation(t *testing.T) {
	k := newTestKeys(t)
	next, err := sign.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	raw := newPayload(7, k.signedItem(t, "aurora", "1.0.0")).envelope(t, k.catalog, next)

	// A release that knows only the old key, one that knows only the new
	// key, and one that knows both all accept a catalog signed by both.
	for name, keys := range map[string][]string{
		"old":  {k.catalog.Public().String()},
		"new":  {next.Public().String()},
		"both": {k.catalog.Public().String(), next.Public().String()},
	} {
		root, err := newTrustRoot("https://m.example.com", keys, []string{k.packages.Public().String()}, true)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ParseCatalog(raw, root, testNow); err != nil {
			t.Errorf("%s keys: %v", name, err)
		}
	}
}

func TestParseCatalogRejectsBadSignatures(t *testing.T) {
	k := newTestKeys(t)
	root := k.root(t, "https://m.example.com")
	p := newPayload(3, k.signedItem(t, "aurora", "1.0.0"))
	stranger, _ := sign.GenerateKey(nil)

	tampered := func() []byte {
		var env catalogEnvelope
		_ = json.Unmarshal(p.envelope(t, k.catalog), &env)
		payload := bytes.Replace(p.json(t), []byte(`"Aurora"`), []byte(`"Borealis"`), 1)
		env.Payload = base64.StdEncoding.EncodeToString(payload)
		out, _ := json.Marshal(env)
		return out
	}
	forged := func() []byte {
		// A signature by another key that claims the trusted key's kid.
		var env catalogEnvelope
		_ = json.Unmarshal(p.envelope(t, stranger), &env)
		env.Signatures[0].KID = k.catalog.Public().KID()
		out, _ := json.Marshal(env)
		return out
	}
	withSignatures := func(n int) []byte {
		var env catalogEnvelope
		_ = json.Unmarshal(p.envelope(t, k.catalog), &env)
		for len(env.Signatures) < n {
			env.Signatures = append(env.Signatures, env.Signatures[0])
		}
		env.Signatures = env.Signatures[:n]
		out, _ := json.Marshal(env)
		return out
	}

	cases := map[string]struct {
		raw  []byte
		want error
	}{
		"payload changed":         {tampered(), ErrCatalogSignature},
		"only an unknown key":     {p.envelope(t, stranger), ErrCatalogSignature},
		"trusted kid, wrong key":  {forged(), ErrCatalogSignature},
		"no signatures":           {withSignatures(0), ErrCatalogFormat},
		"too many signatures":     {withSignatures(5), ErrCatalogFormat},
		"package key signs":       {p.envelope(t, k.packages), ErrCatalogSignature},
		"not json":                {[]byte("<html>"), ErrCatalogFormat},
		"wrong envelope format":   {bytes.Replace(p.envelope(t, k.catalog), []byte(CatalogFormat), []byte("gopress-catalog-v0"), 1), ErrCatalogFormat},
		"payload is not base64":   {[]byte(`{"format":"gopress-catalog-v1","payload":"!!!","signatures":[{"kid":"x","alg":"ed25519","sig":"x"}]}`), ErrCatalogFormat},
		"envelope over the limit": {make([]byte, maxEnvelopeBytes+1), ErrCatalogTooLarge},
	}
	for name, tc := range cases {
		if _, err := ParseCatalog(tc.raw, root, testNow); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", name, err, tc.want)
		}
	}
}

func TestParseCatalogRejectsOversizedPayload(t *testing.T) {
	k := newTestKeys(t)
	big := make([]byte, maxPayloadBytes+1)
	env, _ := json.Marshal(catalogEnvelope{
		Format: CatalogFormat, Payload: base64.StdEncoding.EncodeToString(big),
		Signatures: []sign.Signature{{KID: "x", Alg: sign.Algorithm, Sig: "x"}},
	})
	if len(env) > maxEnvelopeBytes {
		t.Fatalf("test envelope is %d bytes; it must fit the envelope limit to reach the payload check", len(env))
	}
	if _, err := ParseCatalog(env, k.root(t, "https://m.example.com"), testNow); !errors.Is(err, ErrCatalogTooLarge) {
		t.Fatalf("err = %v", err)
	}
	if _, err := SignCatalog(big, k.catalog); !errors.Is(err, ErrCatalogTooLarge) {
		t.Fatalf("SignCatalog err = %v", err)
	}
}

func TestParseCatalogRejectsMalformedFields(t *testing.T) {
	k := newTestKeys(t)
	root := k.root(t, "https://m.example.com")
	good := k.signedItem(t, "aurora", "1.0.0")

	cases := map[string]func(p *testPayload){
		"schema 2":                  func(p *testPayload) { p.Schema = 2 },
		"bad generated_at":          func(p *testPayload) { p.GeneratedAt = "yesterday" },
		"expires before generated":  func(p *testPayload) { p.ExpiresAt = p.GeneratedAt },
		"version with v prefix":     func(p *testPayload) { p.Items[0].Version = "v1.0.0" },
		"two-part version":          func(p *testPayload) { p.Items[0].Version = "1.0" },
		"build metadata":            func(p *testPayload) { p.Items[0].Version = "1.0.0+build.1" },
		"invalid slug":              func(p *testPayload) { p.Items[0].Slug = "Bad_Slug" },
		"invalid kind":              func(p *testPayload) { p.Items[0].Kind = "Theme!" },
		"duplicate item":            func(p *testPayload) { p.Items = append(p.Items, p.Items[0]) },
		"digest not h1":             func(p *testPayload) { p.Items[0].Digest = "sha256:abc" },
		"digest wrong length":       func(p *testPayload) { p.Items[0].Digest = "h1:YWJj" },
		"size zero":                 func(p *testPayload) { p.Items[0].Size = 0 },
		"size over 10 MB":           func(p *testPayload) { p.Items[0].Size = maxPackageBytes + 1 },
		"download over http":        func(p *testPayload) { p.Items[0].DownloadURL = "http://market.example.com/x.zip" },
		"download with credentials": func(p *testPayload) { p.Items[0].DownloadURL = "https://u:p@market.example.com/x.zip" },
		"download missing":          func(p *testPayload) { p.Items[0].DownloadURL = "" },
		"listing over http":         func(p *testPayload) { p.Items[0].ListingURL = "http://example.com/" },
		"bad core constraint":       func(p *testPayload) { p.Items[0].Core = ">>0.8" },
		"empty title":               func(p *testPayload) { p.Items[0].Title = " " },
		"long title":                func(p *testPayload) { p.Items[0].Title = strings.Repeat("题", maxTitleRunes+1) },
		"long summary":              func(p *testPayload) { p.Items[0].Summary = strings.Repeat("x", maxSummaryRunes+1) },
		"bad language code":         func(p *testPayload) { p.Items[0].Localized = map[string]ItemText{"Chinese": {}} },
		"bad external host":         func(p *testPayload) { p.Items[0].External = []string{"https://cdn.example.com"} },
		"bad requires slug":         func(p *testPayload) { p.Items[0].Requires = []ItemRequirement{{Slug: "Seo"}} },
		"bad requires version":      func(p *testPayload) { p.Items[0].Requires = []ItemRequirement{{Slug: "seo-extras", Version: "latest"}} },
		"publisher kid mismatch":    func(p *testPayload) { p.Items[0].Publisher.KID = "0000000000000000" },
		"publisher key not base64":  func(p *testPayload) { p.Items[0].Publisher.PublicKey = "???" },
		"signature by another kid":  func(p *testPayload) { p.Items[0].Signatures.Publisher.KID = "0000000000000000" },
		"revocation without versions": func(p *testPayload) {
			p.Revoked = []Revocation{{Kind: KindTheme, Slug: "evil-x"}}
		},
		"revocation mixes * and versions": func(p *testPayload) {
			p.Revoked = []Revocation{{Kind: KindTheme, Slug: "evil-x", Versions: []string{"*", "1.0.0"}}}
		},
		"revocation uses a range": func(p *testPayload) {
			p.Revoked = []Revocation{{Kind: KindTheme, Slug: "evil-x", Versions: []string{">=1.0.0"}}}
		},
		"revocation bad slug": func(p *testPayload) {
			p.Revoked = []Revocation{{Kind: KindTheme, Slug: "Evil", Versions: []string{"*"}}}
		},
	}
	for name, mutate := range cases {
		p := newPayload(9, good)
		mutate(&p)
		_, err := ParseCatalog(p.envelope(t, k.catalog), root, testNow)
		want := ErrCatalogFormat
		if name == "schema 2" {
			want = ErrCatalogSchema
		}
		if !errors.Is(err, want) {
			t.Errorf("%s: err = %v, want %v", name, err, want)
		}
	}
}

func TestParseCatalogExpiry(t *testing.T) {
	k := newTestKeys(t)
	root := k.root(t, "https://m.example.com")
	p := newPayload(1, k.signedItem(t, "aurora", "1.0.0"))
	raw := p.envelope(t, k.catalog)
	expires := testNow.Add(7 * 24 * time.Hour)

	if _, err := ParseCatalog(raw, root, expires.Add(-time.Second)); err != nil {
		t.Fatalf("just before expiry: %v", err)
	}
	if _, err := ParseCatalog(raw, root, expires); !errors.Is(err, ErrCatalogExpired) {
		t.Fatalf("at expiry: %v", err)
	}
	// Stored catalogs are read without the expiry check.
	if _, err := parseCatalog(raw, root, parseOptions{now: expires.Add(time.Hour), coreVersion: "0.7.13"}); err != nil {
		t.Fatalf("stored read: %v", err)
	}
}

func TestParseCatalogHidesItemsWhoseSignaturesFail(t *testing.T) {
	k := newTestKeys(t)
	root := k.root(t, "https://m.example.com")
	newerMarketKey, _ := sign.GenerateKey(nil)

	good := k.signedItem(t, "aurora", "1.0.0")
	rotated := k.signedItem(t, "borealis", "1.0.0")
	k.signItem(t, &rotated, newerMarketKey) // countersigned by a key this release does not know
	badPublisher := k.signedItem(t, "cascade", "1.0.0")
	badPublisher.Signatures.Publisher = good.Signatures.Publisher // covers another package
	wrongDigest := k.signedItem(t, "dune", "1.0.0")
	wrongDigest.Digest = testDigest("something else")

	cat, err := ParseCatalog(newPayload(5, good, rotated, badPublisher, wrongDigest).envelope(t, k.catalog), root, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.Items) != 1 || cat.Items[0].Slug != "aurora" {
		t.Fatalf("items = %+v", cat.Items)
	}
	want := []string{"theme/borealis@1.0.0", "theme/cascade@1.0.0", "theme/dune@1.0.0"}
	if strings.Join(cat.Hidden, ",") != strings.Join(want, ",") {
		t.Fatalf("hidden = %v, want %v", cat.Hidden, want)
	}
}

func TestParseCatalogSkipsKindsAndPricingItDoesNotKnow(t *testing.T) {
	k := newTestKeys(t)
	root := k.root(t, "https://m.example.com")
	plugin := k.signedItem(t, "aurora-tools", "1.0.0")
	plugin.Kind = "plugin"
	plugin.Slug = "Not Validated" // fields of skipped kinds are not checked
	subscription := k.signedItem(t, "borealis", "1.0.0")
	subscription.Pricing = "subscription"
	paid := k.signedItem(t, "cascade", "1.0.0")
	paid.Pricing = PricingPaid
	k.signItem(t, &paid, k.packages)

	p := newPayload(5, plugin, subscription, paid)
	p.Revoked = []Revocation{{Kind: "plugin", Slug: "whatever", Versions: []string{"*"}}}
	cat, err := ParseCatalog(p.envelope(t, k.catalog), root, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.Items) != 1 || cat.Items[0].Pricing != PricingPaid || len(cat.Revoked) != 0 || len(cat.Hidden) != 0 {
		t.Fatalf("catalog = %+v", cat)
	}
}

func TestParseCatalogMarksIncompatibleItems(t *testing.T) {
	k := newTestKeys(t)
	root := k.root(t, "https://m.example.com")
	needsNewer := k.signedItem(t, "aurora", "1.0.0")
	needsNewer.Core = ">=0.8.0"
	fits := k.signedItem(t, "borealis", "1.0.0")
	fits.Core = ">=0.7.0 <0.8.0"

	cat, err := parseCatalog(newPayload(5, needsNewer, fits).envelope(t, k.catalog), root,
		parseOptions{now: testNow, checkExpiry: true, coreVersion: "0.7.13"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.Items) != 2 || cat.Items[0].Compatible || !cat.Items[1].Compatible {
		t.Fatalf("items = %+v", cat.Items)
	}
}

func TestRevocationMatching(t *testing.T) {
	k := newTestKeys(t)
	root := k.root(t, "https://m.example.com")
	p := newPayload(5)
	p.Revoked = []Revocation{
		{Kind: KindTheme, Slug: "evil-x", Versions: []string{"*"}, Severity: "apocalyptic", Reason: "aliens", Note: strings.Repeat("n", maxNoteRunes+5)},
		{Kind: KindTheme, Slug: "aurora", Versions: []string{"1.0.0", "1.1.0-rc.1"}, Severity: SeverityLow, Reason: ReasonVulnerability},
	}
	cat, err := ParseCatalog(p.envelope(t, k.catalog), root, testNow)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		slug, version string
		want          bool
	}{
		{"evil-x", "1.0.0-beta.1", true}, // "*" must cover pre-releases, which semver ranges skip
		{"evil-x", "0.0.1", true},
		{"aurora", "1.0.0", true},
		{"aurora", "1.1.0-rc.1", true},
		{"aurora", "1.1.0", false},
		{"other", "1.0.0", false},
	}
	for _, tc := range cases {
		if got := cat.Revocation(KindTheme, tc.slug, tc.version) != nil; got != tc.want {
			t.Errorf("Revocation(%s, %s) = %v, want %v", tc.slug, tc.version, got, tc.want)
		}
	}
	if cat.Revocation("plugin", "evil-x", "1.0.0") != nil {
		t.Error("revocation matched another kind")
	}
	evil := cat.Revocation(KindTheme, "evil-x", "1.0.0")
	if evil.Severity != SeverityHigh || evil.Reason != ReasonOther || len([]rune(evil.Note)) != maxNoteRunes {
		t.Fatalf("unknown values were not normalized: %+v", evil)
	}
}

func TestSignCatalogRejectsBadInput(t *testing.T) {
	k := newTestKeys(t)
	payload := newPayload(1).json(t)
	if _, err := SignCatalog(payload); err == nil {
		t.Error("signed without keys")
	}
	if _, err := SignCatalog(payload, k.catalog, k.catalog, k.catalog, k.catalog, k.catalog); err == nil {
		t.Error("signed with five keys")
	}
	if _, err := SignCatalog(newPayload(0).json(t), k.catalog); !errors.Is(err, ErrCatalogFormat) {
		t.Errorf("sequence 0: %v", err)
	}
	if _, err := SignCatalog([]byte("not json"), k.catalog); !errors.Is(err, ErrCatalogFormat) {
		t.Errorf("not json: %v", err)
	}
}
