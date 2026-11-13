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
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/0xmattg/go-press/core/plugin"
	"github.com/0xmattg/go-press/pkg/semver"
	"github.com/0xmattg/go-press/pkg/sign"
	"github.com/0xmattg/go-press/version"
)

// Catalog wire format.
//
// A market serves its catalog as an envelope:
//
//	{"format": "gopress-catalog-v1",
//	 "payload": "<base64 of the catalog JSON bytes>",
//	 "signatures": [{"kid": "…", "alg": "ed25519", "sig": "…"}]}
//
// Each signature covers sign.CatalogStatement(sequence, hex SHA-256 of the
// payload bytes), so the catalog JSON is signed exactly as sent and never
// needs to be canonicalized. Several signatures let a market sign with an old
// and a new key while rotating.
const (
	CatalogFormat = "gopress-catalog-v1"
	CatalogSchema = 1
)

// Catalog limits.
const (
	maxEnvelopeBytes      = 7 << 20
	maxPayloadBytes       = 5 << 20
	maxCatalogSignatures  = 4
	maxCatalogItems       = 5000
	maxCatalogRevocations = 5000
	maxPackageBytes       = 10 << 20
	maxItemExternal       = 20
	maxItemRequires       = 20
	maxItemLocales        = 20
	maxTitleRunes         = 100
	maxSummaryRunes       = 500
	maxAuthorRunes        = 100
	maxNoteRunes          = 1000
	maxURLBytes           = 2048
	maxVersionBytes       = 64
)

// Pricing values. Only free packages can be installed in this version.
const (
	PricingFree = "free"
	PricingPaid = "paid"
)

// Revocation severities and reasons. Unknown values fall back to
// SeverityHigh and ReasonOther, so an entry is never ignored for using a
// value this version does not know.
const (
	SeverityCritical = "critical"
	SeverityHigh     = "high"
	SeverityMedium   = "medium"
	SeverityLow      = "low"

	ReasonMalware       = "malware"
	ReasonVulnerability = "vulnerability"
	ReasonInfringement  = "infringement"
	ReasonOther         = "other"
)

// allVersions in a revocation's versions list revokes every version.
const allVersions = "*"

// Catalog errors. Fetch errors are in market.go.
var (
	// ErrCatalogFormat reports a catalog that does not follow the format.
	ErrCatalogFormat = errors.New("extension: malformed catalog")
	// ErrCatalogSchema reports a catalog schema this version cannot read.
	ErrCatalogSchema = errors.New("extension: unsupported catalog schema; upgrade GoPress")
	// ErrCatalogSignature reports a catalog without a valid signature by a
	// trusted catalog key.
	ErrCatalogSignature = errors.New("extension: catalog signature does not verify")
	// ErrCatalogExpired reports a catalog past its expires_at.
	ErrCatalogExpired = errors.New("extension: catalog has expired")
	// ErrCatalogTooLarge reports a catalog over the size limits.
	ErrCatalogTooLarge = errors.New("extension: catalog is too large")
)

var (
	// strictVersionPattern is semver 2.0.0 without build metadata: exactly
	// MAJOR.MINOR.PATCH, an optional pre-release, no "v" prefix. Catalog
	// versions are matched as strings, so each version has one spelling.
	strictVersionPattern = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(-(0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*)(\.(0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*))*)?$`)
	kindPattern          = regexp.MustCompile(`^[a-z]+$`)
	hostnamePattern      = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z][a-z0-9-]{0,61}[a-z0-9]$`)
	localePattern        = regexp.MustCompile(`^[a-z]{2,3}(-[A-Za-z0-9]{2,8})*$`)
)

// knownKinds are the package kinds this version installs. Items and
// revocations of other kinds are skipped, so a market can add kinds without
// breaking older sites.
var knownKinds = map[string]bool{KindTheme: true}

// Catalog is a market catalog whose signature and fields have been checked.
type Catalog struct {
	Sequence    int64
	GeneratedAt time.Time
	ExpiresAt   time.Time
	// Items are the packages offered, of the kinds this version knows, whose
	// publisher signature and market countersignature both verify.
	Items []Item
	// Revoked lists withdrawn packages of the kinds this version knows.
	Revoked []Revocation
	// Hidden names items left out because their signatures did not verify,
	// for example items countersigned by a market key newer than this
	// release.
	Hidden []string

	digest string
}

// Item is one package version offered by the market.
type Item struct {
	Kind          string              `json:"kind"`
	Slug          string              `json:"slug"`
	Version       string              `json:"version"`
	Digest        string              `json:"digest"`
	Size          int64               `json:"size"`
	DownloadURL   string              `json:"download_url"`
	Core          string              `json:"core,omitempty"`
	Requires      []ItemRequirement   `json:"requires,omitempty"`
	Pricing       string              `json:"pricing"`
	Title         string              `json:"title"`
	Summary       string              `json:"summary,omitempty"`
	Author        string              `json:"author,omitempty"`
	Localized     map[string]ItemText `json:"localized,omitempty"`
	ListingURL    string              `json:"listing_url,omitempty"`
	ScreenshotURL string              `json:"screenshot_url,omitempty"`
	External      []string            `json:"external,omitempty"`
	Publisher     ItemPublisher       `json:"publisher"`
	Signatures    ItemSignatures      `json:"signatures"`

	// Compatible reports whether this GoPress version satisfies Core.
	Compatible bool `json:"-"`
}

// ItemRequirement is a plugin the package needs, as declared in its manifest.
type ItemRequirement struct {
	Slug     string `json:"slug"`
	Version  string `json:"version,omitempty"`
	Optional bool   `json:"optional,omitempty"`
}

// ItemText is an item's title and summary in one language.
type ItemText struct {
	Title   string `json:"title,omitempty"`
	Summary string `json:"summary,omitempty"`
}

// ItemPublisher is the publisher key the catalog vouches for.
type ItemPublisher struct {
	KID       string `json:"kid"`
	PublicKey string `json:"public_key"`
}

// ItemSignatures are the package's signatures, carried by the catalog so an
// install from the market does not depend on the files inside the package.
type ItemSignatures struct {
	Publisher sign.Signature `json:"publisher"`
	Market    sign.Signature `json:"market"`
}

// Revocation withdraws versions of a package. Versions lists exact versions,
// or "*" for all of them; semver ranges are not used because they do not
// match pre-release versions.
type Revocation struct {
	Kind     string   `json:"kind"`
	Slug     string   `json:"slug"`
	Versions []string `json:"versions"`
	Severity string   `json:"severity"`
	Reason   string   `json:"reason"`
	Note     string   `json:"note,omitempty"`
}

// Matches reports whether r revokes the given package version.
func (r Revocation) Matches(kind, slug, version string) bool {
	if r.Kind != kind || r.Slug != slug {
		return false
	}
	for _, v := range r.Versions {
		if v == allVersions || v == version {
			return true
		}
	}
	return false
}

type catalogEnvelope struct {
	Format     string           `json:"format"`
	Payload    string           `json:"payload"`
	Signatures []sign.Signature `json:"signatures"`
}

type catalogPayload struct {
	Schema      int          `json:"schema"`
	Sequence    int64        `json:"sequence"`
	GeneratedAt string       `json:"generated_at"`
	ExpiresAt   string       `json:"expires_at"`
	Items       []Item       `json:"items"`
	Revoked     []Revocation `json:"revoked"`
}

// ParseCatalog verifies a catalog envelope against the trust root and checks
// its fields. It rejects the whole catalog when the signature fails, the
// format is wrong, or the catalog expired before now; items whose own
// signatures fail are only left out (see Catalog.Hidden).
func ParseCatalog(raw []byte, root TrustRoot, now time.Time) (*Catalog, error) {
	return parseCatalog(raw, root, parseOptions{now: now, checkExpiry: true, coreVersion: currentCoreVersion()})
}

// currentCoreVersion is the running GoPress version, which item core
// constraints are checked against. Tests replace it.
var currentCoreVersion = version.String

type parseOptions struct {
	now         time.Time
	checkExpiry bool
	coreVersion string
}

func parseCatalog(raw []byte, root TrustRoot, opts parseOptions) (*Catalog, error) {
	if len(raw) > maxEnvelopeBytes {
		return nil, ErrCatalogTooLarge
	}
	var env catalogEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("%w: envelope: %v", ErrCatalogFormat, err)
	}
	if env.Format != CatalogFormat {
		return nil, fmt.Errorf("%w: envelope format %q, want %q", ErrCatalogFormat, env.Format, CatalogFormat)
	}
	if len(env.Signatures) == 0 || len(env.Signatures) > maxCatalogSignatures {
		return nil, fmt.Errorf("%w: envelope has %d signatures, want 1 to %d", ErrCatalogFormat, len(env.Signatures), maxCatalogSignatures)
	}
	if base64.StdEncoding.DecodedLen(len(env.Payload)) > maxPayloadBytes+3 {
		return nil, ErrCatalogTooLarge
	}
	payload, err := base64.StdEncoding.DecodeString(env.Payload)
	if err != nil {
		return nil, fmt.Errorf("%w: payload is not base64", ErrCatalogFormat)
	}
	if len(payload) > maxPayloadBytes {
		return nil, ErrCatalogTooLarge
	}

	var doc catalogPayload
	if err := json.Unmarshal(payload, &doc); err != nil {
		return nil, fmt.Errorf("%w: payload: %v", ErrCatalogFormat, err)
	}
	if doc.Sequence <= 0 {
		return nil, fmt.Errorf("%w: sequence must be positive", ErrCatalogFormat)
	}
	sum := sha256.Sum256(payload)
	digest := hex.EncodeToString(sum[:])
	if err := verifyCatalogSignatures(root, sign.CatalogStatement(doc.Sequence, digest), env.Signatures); err != nil {
		return nil, err
	}

	// The signature is good; from here on the payload is the market's own.
	if doc.Schema != CatalogSchema {
		return nil, fmt.Errorf("%w (catalog schema %d, this version reads %d)", ErrCatalogSchema, doc.Schema, CatalogSchema)
	}
	generated, err := time.Parse(time.RFC3339, doc.GeneratedAt)
	if err != nil {
		return nil, fmt.Errorf("%w: generated_at: %v", ErrCatalogFormat, err)
	}
	expires, err := time.Parse(time.RFC3339, doc.ExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("%w: expires_at: %v", ErrCatalogFormat, err)
	}
	if !expires.After(generated) {
		return nil, fmt.Errorf("%w: expires_at is not after generated_at", ErrCatalogFormat)
	}
	if opts.checkExpiry && !opts.now.Before(expires) {
		return nil, fmt.Errorf("%w (expired %s)", ErrCatalogExpired, expires.UTC().Format(time.RFC3339))
	}
	if len(doc.Items) > maxCatalogItems || len(doc.Revoked) > maxCatalogRevocations {
		return nil, ErrCatalogTooLarge
	}

	cat := &Catalog{Sequence: doc.Sequence, GeneratedAt: generated, ExpiresAt: expires, digest: digest}
	seen := make(map[string]bool, len(doc.Items))
	var candidates []Item
	for i := range doc.Items {
		item := doc.Items[i]
		if !kindPattern.MatchString(item.Kind) {
			return nil, fmt.Errorf("%w: item %d: invalid kind %q", ErrCatalogFormat, i, item.Kind)
		}
		if !knownKinds[item.Kind] || (item.Pricing != PricingFree && item.Pricing != PricingPaid) {
			continue // a kind or pricing model this version does not handle
		}
		if err := validateItem(&item, root.allowsLoopbackHTTP()); err != nil {
			return nil, fmt.Errorf("%w: item %d (%s/%s@%s): %v", ErrCatalogFormat, i, item.Kind, item.Slug, item.Version, err)
		}
		key := itemKey(item.Kind, item.Slug, item.Version)
		if seen[key] {
			return nil, fmt.Errorf("%w: item %s is listed twice", ErrCatalogFormat, key)
		}
		seen[key] = true
		item.Compatible = compatible(item.Core, opts.coreVersion)
		candidates = append(candidates, item)
	}
	for i := range doc.Revoked {
		rev := doc.Revoked[i]
		if !kindPattern.MatchString(rev.Kind) {
			return nil, fmt.Errorf("%w: revocation %d: invalid kind %q", ErrCatalogFormat, i, rev.Kind)
		}
		if !knownKinds[rev.Kind] {
			continue
		}
		if err := validateRevocation(&rev); err != nil {
			return nil, fmt.Errorf("%w: revocation %d (%s/%s): %v", ErrCatalogFormat, i, rev.Kind, rev.Slug, err)
		}
		cat.Revoked = append(cat.Revoked, rev)
	}
	for _, item := range candidates {
		if err := verifyItemSignatures(item, root.PackageKeys); err != nil {
			cat.Hidden = append(cat.Hidden, itemKey(item.Kind, item.Slug, item.Version))
			continue
		}
		cat.Items = append(cat.Items, item)
	}
	return cat, nil
}

// verifyCatalogSignatures requires at least one signature by a trusted
// catalog key and rejects the catalog if any signature by a trusted key
// fails, since that means the payload was changed. Signatures by unknown keys
// are ignored.
func verifyCatalogSignatures(root TrustRoot, statement sign.Statement, signatures []sign.Signature) error {
	trusted := 0
	for _, sig := range signatures {
		key, ok := root.CatalogKeys[sig.KID]
		if !ok {
			continue
		}
		if err := sign.Verify(key, statement, sig); err != nil {
			return fmt.Errorf("%w: key %s: %v", ErrCatalogSignature, sig.KID, err)
		}
		trusted++
	}
	if trusted == 0 {
		return fmt.Errorf("%w: no signature by a trusted catalog key", ErrCatalogSignature)
	}
	return nil
}

func validateItem(item *Item, loopbackHTTP bool) error {
	switch {
	case !plugin.ValidSlug(item.Slug):
		return fmt.Errorf("invalid slug %q", item.Slug)
	case !validVersion(item.Version):
		return fmt.Errorf("version %q is not a strict semver version", item.Version)
	case !validDigest(item.Digest):
		return fmt.Errorf("invalid digest %q", item.Digest)
	case item.Size <= 0 || item.Size > maxPackageBytes:
		return fmt.Errorf("size %d is outside 1 to %d bytes", item.Size, maxPackageBytes)
	case item.Core != "" && !semver.ValidConstraint(item.Core):
		return fmt.Errorf("invalid core constraint %q", item.Core)
	case strings.TrimSpace(item.Title) == "":
		return errors.New("title is empty")
	}
	if err := checkURL("download_url", item.DownloadURL, true, loopbackHTTP); err != nil {
		return err
	}
	if err := checkURL("listing_url", item.ListingURL, false, false); err != nil {
		return err
	}
	if err := checkURL("screenshot_url", item.ScreenshotURL, false, false); err != nil {
		return err
	}
	if err := checkText(item.Title, item.Summary); err != nil {
		return err
	}
	if utf8.RuneCountInString(item.Author) > maxAuthorRunes {
		return fmt.Errorf("author is longer than %d characters", maxAuthorRunes)
	}
	if len(item.Localized) > maxItemLocales {
		return fmt.Errorf("localized has more than %d languages", maxItemLocales)
	}
	for lang, text := range item.Localized {
		if !localePattern.MatchString(lang) {
			return fmt.Errorf("invalid language code %q", lang)
		}
		if err := checkText(text.Title, text.Summary); err != nil {
			return fmt.Errorf("localized %s: %v", lang, err)
		}
	}
	if len(item.Requires) > maxItemRequires {
		return fmt.Errorf("requires has more than %d entries", maxItemRequires)
	}
	for _, req := range item.Requires {
		if !plugin.ValidSlug(req.Slug) {
			return fmt.Errorf("requires: invalid plugin slug %q", req.Slug)
		}
		if req.Version != "" && !semver.ValidConstraint(req.Version) {
			return fmt.Errorf("requires %s: invalid version constraint %q", req.Slug, req.Version)
		}
	}
	if len(item.External) > maxItemExternal {
		return fmt.Errorf("external has more than %d hosts", maxItemExternal)
	}
	for _, host := range item.External {
		if len(host) > 253 || !hostnamePattern.MatchString(host) {
			return fmt.Errorf("external: invalid host name %q", host)
		}
	}
	pub, err := sign.ParsePublicKey(item.Publisher.PublicKey)
	if err != nil {
		return fmt.Errorf("publisher key: %v", err)
	}
	if pub.KID() != item.Publisher.KID {
		return fmt.Errorf("publisher kid %q does not match its key (%s)", item.Publisher.KID, pub.KID())
	}
	if item.Signatures.Publisher.KID != item.Publisher.KID {
		return fmt.Errorf("publisher signature is by %q, not the publisher key %s", item.Signatures.Publisher.KID, item.Publisher.KID)
	}
	return nil
}

// verifyItemSignatures checks the publisher signature with the key the
// catalog vouches for, and the market countersignature with the trusted
// package keys. Neither needs the package itself.
func verifyItemSignatures(item Item, packageKeys sign.KeySet) error {
	id := sign.PackageID{Kind: item.Kind, Slug: item.Slug, Version: item.Version}
	pub, err := sign.ParsePublicKey(item.Publisher.PublicKey)
	if err != nil {
		return err
	}
	if err := sign.Verify(pub, sign.PublisherStatement(id, item.Digest), item.Signatures.Publisher); err != nil {
		return err
	}
	return packageKeys.Verify(sign.MarketStatement(id, pub.KID(), item.Digest), item.Signatures.Market)
}

func validateRevocation(rev *Revocation) error {
	if !plugin.ValidSlug(rev.Slug) {
		return fmt.Errorf("invalid slug %q", rev.Slug)
	}
	if len(rev.Versions) == 0 {
		return errors.New("versions is empty")
	}
	for _, v := range rev.Versions {
		switch {
		case v == allVersions && len(rev.Versions) != 1:
			return errors.New(`"*" cannot be combined with other versions`)
		case v != allVersions && !validVersion(v):
			return fmt.Errorf("version %q is not a strict semver version or \"*\"", v)
		}
	}
	switch rev.Severity {
	case SeverityCritical, SeverityHigh, SeverityMedium, SeverityLow:
	default:
		rev.Severity = SeverityHigh
	}
	switch rev.Reason {
	case ReasonMalware, ReasonVulnerability, ReasonInfringement, ReasonOther:
	default:
		rev.Reason = ReasonOther
	}
	if utf8.RuneCountInString(rev.Note) > maxNoteRunes {
		rev.Note = string([]rune(rev.Note)[:maxNoteRunes])
	}
	return nil
}

func checkText(title, summary string) error {
	if utf8.RuneCountInString(title) > maxTitleRunes {
		return fmt.Errorf("title is longer than %d characters", maxTitleRunes)
	}
	if utf8.RuneCountInString(summary) > maxSummaryRunes {
		return fmt.Errorf("summary is longer than %d characters", maxSummaryRunes)
	}
	return nil
}

// checkURL requires an absolute HTTPS URL without credentials. loopbackHTTP
// also accepts HTTP on a loopback host, for a local development market.
func checkURL(field, raw string, required, loopbackHTTP bool) error {
	if raw == "" {
		if required {
			return fmt.Errorf("%s is empty", field)
		}
		return nil
	}
	if len(raw) > maxURLBytes {
		return fmt.Errorf("%s is longer than %d bytes", field, maxURLBytes)
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return fmt.Errorf("%s %q is not an absolute URL without credentials", field, raw)
	}
	if parsed.Scheme == "https" || (loopbackHTTP && parsed.Scheme == "http" && isLoopbackHost(parsed.Hostname())) {
		return nil
	}
	return fmt.Errorf("%s %q must use https", field, raw)
}

func validVersion(v string) bool {
	return len(v) <= maxVersionBytes && strictVersionPattern.MatchString(v)
}

func validDigest(d string) bool {
	encoded, ok := strings.CutPrefix(d, "h1:")
	if !ok {
		return false
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	return err == nil && len(raw) == sha256.Size
}

func compatible(constraint, coreVersion string) bool {
	if constraint == "" {
		return true
	}
	ok, err := semver.Satisfies(coreVersion, constraint)
	return err == nil && ok
}

func itemKey(kind, slug, version string) string {
	return kind + "/" + slug + "@" + version
}

// Digest returns the hex SHA-256 of the catalog payload, which the catalog
// signature covers.
func (c *Catalog) Digest() string { return c.digest }

// Find returns the offered item for an exact package version, or nil.
func (c *Catalog) Find(kind, slug, version string) *Item {
	for i := range c.Items {
		item := &c.Items[i]
		if item.Kind == kind && item.Slug == slug && item.Version == version {
			return item
		}
	}
	return nil
}

// Latest returns the highest offered version of a package, or nil.
func (c *Catalog) Latest(kind, slug string) *Item {
	var best *Item
	for i := range c.Items {
		item := &c.Items[i]
		if item.Kind != kind || item.Slug != slug {
			continue
		}
		if best == nil {
			best = item
			continue
		}
		if cmp, err := semver.Compare(item.Version, best.Version); err == nil && cmp > 0 {
			best = item
		}
	}
	return best
}

// Revocation returns the entry revoking a package version, or nil.
func (c *Catalog) Revocation(kind, slug, version string) *Revocation {
	for i := range c.Revoked {
		if c.Revoked[i].Matches(kind, slug, version) {
			return &c.Revoked[i]
		}
	}
	return nil
}

// SignCatalog wraps catalog JSON in a signed envelope. Markets use it to
// publish their catalog; tests and the gopress pkg command use it to run a
// local market. The payload is signed byte for byte as given.
func SignCatalog(payload []byte, keys ...sign.PrivateKey) ([]byte, error) {
	if len(keys) == 0 || len(keys) > maxCatalogSignatures {
		return nil, fmt.Errorf("extension: sign a catalog with 1 to %d keys", maxCatalogSignatures)
	}
	if len(payload) > maxPayloadBytes {
		return nil, ErrCatalogTooLarge
	}
	var head struct {
		Sequence int64 `json:"sequence"`
	}
	if err := json.Unmarshal(payload, &head); err != nil {
		return nil, fmt.Errorf("%w: payload: %v", ErrCatalogFormat, err)
	}
	if head.Sequence <= 0 {
		return nil, fmt.Errorf("%w: sequence must be positive", ErrCatalogFormat)
	}
	sum := sha256.Sum256(payload)
	statement := sign.CatalogStatement(head.Sequence, hex.EncodeToString(sum[:]))
	env := catalogEnvelope{Format: CatalogFormat, Payload: base64.StdEncoding.EncodeToString(payload)}
	for _, key := range keys {
		sig, err := key.Sign(statement)
		if err != nil {
			return nil, err
		}
		env.Signatures = append(env.Signatures, sig)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(env); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
