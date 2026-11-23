// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package updatecheck

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

	"github.com/0xmattg/go-press/pkg/semver"
	"github.com/0xmattg/go-press/pkg/sign"
)

// Official announcements (GIP-F0001 3.3).
//
// The answer to the official core update check may carry a bundle of
// announcements for the admin notice center, such as a security advisory.
// The GoPress project signs each bundle offline, with keys whose public half
// is built into core (builtinNoticeKeys); the update server only stores and
// forwards signed bundles, so whoever controls it cannot write announcements
// that sites accept. The wire form follows the market catalog:
//
//	"notices": {"format": "gopress-notices-v1",
//	            "payload": "<base64 of the bundle JSON bytes>",
//	            "signatures": [{"kid": "…", "alg": "ed25519", "sig": "…"}]}
//
// Each signature covers sign.NoticesStatement(sequence, hex SHA-256 of the
// payload bytes). The bundle JSON is NoticePayload. Its sequence only grows:
// a site keeps the highest sequence it accepted and refuses older bundles,
// so an old bundle cannot be replayed to bring back an announcement that was
// withdrawn.
const (
	NoticesFormat = "gopress-notices-v1"
	NoticesSchema = 1

	// MaxOfficialNotices is how many announcements a bundle may hold.
	MaxOfficialNotices = 10
	// MaxNoticeLifetime bounds how long a bundle, and each announcement in
	// it, may stay valid after it was written.
	MaxNoticeLifetime = 90 * 24 * time.Hour

	maxNoticePayloadBytes  = 16 << 10
	maxNoticeSignatures    = 2
	maxNoticeIDLength      = 64
	maxNoticeTitleRunes    = 120
	maxNoticeMessageRunes  = 1000
	maxNoticeLabelRunes    = 60
	maxNoticeAffects       = 10
	maxNoticeTextLanguages = 10
	maxRemindAfterHours    = 168
)

// Errors from ParseNotices and SignNotices.
var (
	ErrNoticesFormat    = errors.New("updatecheck: invalid announcement bundle")
	ErrNoticesSignature = errors.New("updatecheck: announcement bundle signature does not verify")
	ErrNoticesExpired   = errors.New("updatecheck: announcement bundle expired")
)

// SignedNotices is the "notices" field of an update check response.
type SignedNotices struct {
	Format     string           `json:"format"`
	Payload    string           `json:"payload"`
	Signatures []sign.Signature `json:"signatures"`
}

// NoticePayload is the signed bundle document.
type NoticePayload struct {
	Schema   int   `json:"schema"`
	Sequence int64 `json:"sequence"`
	// GeneratedAt and ExpiresAt are RFC 3339 times; ExpiresAt is after
	// GeneratedAt and at most MaxNoticeLifetime later.
	GeneratedAt string         `json:"generated_at"`
	ExpiresAt   string         `json:"expires_at"`
	Notices     []RemoteNotice `json:"notices"`
}

// RemoteNotice is one announcement. Texts map a language ("en", "zh-CN")
// to plain text; "en" is required for the title and the message.
type RemoteNotice struct {
	ID       string            `json:"id"`
	Revision int               `json:"revision"`
	Category string            `json:"category"`
	Severity string            `json:"severity"`
	Title    map[string]string `json:"title"`
	Message  map[string]string `json:"message"`
	// LinkURL is an https address on gopress.xyz or its subdomains, or in
	// github.com/0xmattg/go-press.
	LinkURL   string            `json:"link_url,omitempty"`
	LinkLabel map[string]string `json:"link_label,omitempty"`
	// Affects limits the announcement to sites running a matching core,
	// theme or plugin version; empty means every site.
	Affects []NoticeTarget `json:"affects,omitempty"`
	// RemindAfterHours is how long "×" hides it: 1 to 168, 24 when 0.
	RemindAfterHours int    `json:"remind_after_hours,omitempty"`
	PublishedAt      string `json:"published_at"`
	ExpiresAt        string `json:"expires_at"`
}

// NoticeTarget names versions an announcement is about.
type NoticeTarget struct {
	Kind Kind   `json:"kind"`
	Slug string `json:"slug"`
	// Versions is a semver constraint, for example ">=0.7.40, <0.7.52".
	Versions string `json:"versions"`
}

// NoticeBundle is a verified bundle.
type NoticeBundle struct {
	Sequence    int64
	GeneratedAt time.Time
	ExpiresAt   time.Time
	// Notices are the announcements that passed the checks; Dropped says
	// why the others were left out.
	Notices []RemoteNotice
	Dropped []string
}

var noticeIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// SignNotices wraps a bundle document in a signed envelope. It refuses a
// document that sites would not accept in full, so a bundle is never
// published with an announcement sites drop. It is for the offline signing
// tool (gopress notice sign).
func SignNotices(payload []byte, now time.Time, keys ...sign.PrivateKey) (SignedNotices, error) {
	if len(keys) == 0 || len(keys) > maxNoticeSignatures {
		return SignedNotices{}, fmt.Errorf("%w: sign with 1 to %d keys", ErrNoticesFormat, maxNoticeSignatures)
	}
	bundle, err := CheckNoticePayload(payload, now)
	if err != nil {
		return SignedNotices{}, err
	}
	sum := sha256.Sum256(payload)
	statement := sign.NoticesStatement(bundle.Sequence, hex.EncodeToString(sum[:]))
	env := SignedNotices{Format: NoticesFormat, Payload: base64.StdEncoding.EncodeToString(payload)}
	for _, key := range keys {
		sig, err := key.Sign(statement)
		if err != nil {
			return SignedNotices{}, err
		}
		env.Signatures = append(env.Signatures, sig)
	}
	return env, nil
}

// CheckNoticePayload checks a bundle document the way sites do, but
// strictly: it fails when any announcement would be dropped or the bundle
// has expired at now. Announcement services use it before export, and
// SignNotices before signing.
func CheckNoticePayload(payload []byte, now time.Time) (*NoticeBundle, error) {
	bundle, err := parseNoticePayload(payload)
	if err != nil {
		return nil, err
	}
	if len(bundle.Dropped) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrNoticesFormat, strings.Join(bundle.Dropped, "; "))
	}
	if !now.Before(bundle.ExpiresAt) {
		return nil, ErrNoticesExpired
	}
	return bundle, nil
}

// MarshalSignedNotices encodes an envelope as the file the signing tool
// writes and the update server stores.
func MarshalSignedNotices(env SignedNotices) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(env); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ParseNotices verifies an envelope against trusted keys and checks the
// bundle. At least one signature must be by a trusted key, and every
// signature by a trusted key must verify. An expired bundle returns its
// sequence together with ErrNoticesExpired. Announcements that fail their
// own checks are dropped and listed in Dropped; the rest are kept.
func ParseNotices(env SignedNotices, keys sign.KeySet, now time.Time) (*NoticeBundle, error) {
	if env.Format != NoticesFormat {
		return nil, fmt.Errorf("%w: format %q, want %q", ErrNoticesFormat, env.Format, NoticesFormat)
	}
	if len(env.Signatures) == 0 || len(env.Signatures) > maxNoticeSignatures {
		return nil, fmt.Errorf("%w: %d signatures, want 1 to %d", ErrNoticesFormat, len(env.Signatures), maxNoticeSignatures)
	}
	if base64.StdEncoding.DecodedLen(len(env.Payload)) > maxNoticePayloadBytes+3 {
		return nil, fmt.Errorf("%w: payload is larger than %d bytes", ErrNoticesFormat, maxNoticePayloadBytes)
	}
	payload, err := base64.StdEncoding.DecodeString(env.Payload)
	if err != nil {
		return nil, fmt.Errorf("%w: payload is not base64", ErrNoticesFormat)
	}
	var head struct {
		Sequence int64 `json:"sequence"`
	}
	if err := json.Unmarshal(payload, &head); err != nil || head.Sequence <= 0 {
		return nil, fmt.Errorf("%w: no positive sequence", ErrNoticesFormat)
	}
	sum := sha256.Sum256(payload)
	statement := sign.NoticesStatement(head.Sequence, hex.EncodeToString(sum[:]))
	trusted := 0
	for _, sig := range env.Signatures {
		key, ok := keys[sig.KID]
		if !ok {
			continue
		}
		if err := sign.Verify(key, statement, sig); err != nil {
			return nil, fmt.Errorf("%w: key %s: %v", ErrNoticesSignature, sig.KID, err)
		}
		trusted++
	}
	if trusted == 0 {
		return nil, fmt.Errorf("%w: no signature by a trusted key", ErrNoticesSignature)
	}
	// The signature is good; from here on the document is the project's own.
	bundle, err := parseNoticePayload(payload)
	if err != nil {
		return nil, err
	}
	if !now.Before(bundle.ExpiresAt) {
		return &NoticeBundle{Sequence: bundle.Sequence, GeneratedAt: bundle.GeneratedAt, ExpiresAt: bundle.ExpiresAt}, ErrNoticesExpired
	}
	return bundle, nil
}

// parseNoticePayload decodes and checks a bundle document.
func parseNoticePayload(payload []byte) (*NoticeBundle, error) {
	if len(payload) > maxNoticePayloadBytes {
		return nil, fmt.Errorf("%w: document is larger than %d bytes", ErrNoticesFormat, maxNoticePayloadBytes)
	}
	// Unknown fields are ignored, so a later bundle that adds one still
	// reaches older sites; a change they cannot read raises the schema.
	var doc NoticePayload
	if err := json.Unmarshal(payload, &doc); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoticesFormat, err)
	}
	if doc.Schema != NoticesSchema {
		return nil, fmt.Errorf("%w: schema %d, this version reads %d", ErrNoticesFormat, doc.Schema, NoticesSchema)
	}
	if doc.Sequence <= 0 {
		return nil, fmt.Errorf("%w: sequence must be positive", ErrNoticesFormat)
	}
	generated, err := time.Parse(time.RFC3339, doc.GeneratedAt)
	if err != nil {
		return nil, fmt.Errorf("%w: generated_at: %v", ErrNoticesFormat, err)
	}
	expires, err := time.Parse(time.RFC3339, doc.ExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("%w: expires_at: %v", ErrNoticesFormat, err)
	}
	if !expires.After(generated) || expires.Sub(generated) > MaxNoticeLifetime {
		return nil, fmt.Errorf("%w: expires_at must be after generated_at and at most 90 days later", ErrNoticesFormat)
	}
	if len(doc.Notices) > MaxOfficialNotices {
		return nil, fmt.Errorf("%w: %d announcements, at most %d", ErrNoticesFormat, len(doc.Notices), MaxOfficialNotices)
	}
	bundle := &NoticeBundle{Sequence: doc.Sequence, GeneratedAt: generated.UTC(), ExpiresAt: expires.UTC()}
	seen := map[string]bool{}
	for i, n := range doc.Notices {
		if err := validateRemoteNotice(&n); err != nil {
			bundle.Dropped = append(bundle.Dropped, fmt.Sprintf("announcement %d (%q): %v", i+1, n.ID, err))
			continue
		}
		if seen[n.ID] {
			bundle.Dropped = append(bundle.Dropped, fmt.Sprintf("announcement %d (%q): duplicate id", i+1, n.ID))
			continue
		}
		seen[n.ID] = true
		bundle.Notices = append(bundle.Notices, n)
	}
	return bundle, nil
}

// validateRemoteNotice checks one announcement and fills defaults.
func validateRemoteNotice(n *RemoteNotice) error {
	switch {
	case len(n.ID) > maxNoticeIDLength || !noticeIDPattern.MatchString(n.ID):
		return errors.New("id must be lowercase letters, digits and single hyphens, at most 64 characters")
	case n.Revision < 0:
		return errors.New("revision must not be negative")
	case n.RemindAfterHours < 0 || n.RemindAfterHours > maxRemindAfterHours:
		return errors.New("remind_after_hours must be 1 to 168")
	}
	if n.Revision == 0 {
		n.Revision = 1
	}
	switch n.Severity {
	case "critical", "warning", "info":
	default:
		return fmt.Errorf("unknown severity %q", n.Severity)
	}
	if len(n.Category) > 32 {
		return errors.New("category is longer than 32 characters")
	}
	if err := validateTexts("title", n.Title, maxNoticeTitleRunes, true); err != nil {
		return err
	}
	if err := validateTexts("message", n.Message, maxNoticeMessageRunes, true); err != nil {
		return err
	}
	if err := validateTexts("link_label", n.LinkLabel, maxNoticeLabelRunes, false); err != nil {
		return err
	}
	if n.LinkURL != "" && !officialNoticeLink(n.LinkURL) {
		return errors.New("link_url must be https on gopress.xyz or in github.com/0xmattg/go-press")
	}
	if len(n.Affects) > maxNoticeAffects {
		return fmt.Errorf("more than %d affects entries", maxNoticeAffects)
	}
	for _, target := range n.Affects {
		if target.Kind != KindCore && target.Kind != KindTheme && target.Kind != KindPlugin {
			return fmt.Errorf("affects kind %q is not core, theme or plugin", target.Kind)
		}
		if strings.TrimSpace(target.Slug) == "" || len(target.Slug) > 100 {
			return errors.New("affects entry without a slug")
		}
		if !semver.ValidConstraint(target.Versions) {
			return fmt.Errorf("affects versions %q is not a semver constraint", target.Versions)
		}
	}
	published, err := time.Parse(time.RFC3339, n.PublishedAt)
	if err != nil {
		return errors.New("published_at is not an RFC 3339 time")
	}
	expires, err := time.Parse(time.RFC3339, n.ExpiresAt)
	if err != nil {
		return errors.New("expires_at is not an RFC 3339 time")
	}
	if !expires.After(published) || expires.Sub(published) > MaxNoticeLifetime {
		return errors.New("expires_at must be after published_at and at most 90 days later")
	}
	return nil
}

// validateTexts checks a language → text map: languages are short tags,
// texts are non-empty and not longer than max characters (Normalize cleans
// them again before they are shown), and "en" is present when required.
func validateTexts(field string, texts map[string]string, max int, requireEnglish bool) error {
	if len(texts) > maxNoticeTextLanguages {
		return fmt.Errorf("%s has more than %d languages", field, maxNoticeTextLanguages)
	}
	if requireEnglish && strings.TrimSpace(texts["en"]) == "" {
		return fmt.Errorf("%s needs an \"en\" text", field)
	}
	for lang, text := range texts {
		if lang == "" || len(lang) > 16 || strings.ContainsAny(lang, " \t\r\n") {
			return fmt.Errorf("%s has an invalid language tag %q", field, lang)
		}
		if strings.TrimSpace(text) == "" || utf8.RuneCountInString(text) > max {
			return fmt.Errorf("%s %q is empty or longer than %d characters", field, lang, max)
		}
	}
	return nil
}

// officialNoticeLink reports whether an announcement may link to u: https,
// no user name or password, and either gopress.xyz or a subdomain, or the
// GoPress repository on GitHub.
func officialNoticeLink(u string) bool {
	parsed, err := url.Parse(u)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "gopress.xyz" || strings.HasSuffix(host, ".gopress.xyz") {
		return true
	}
	if host == "github.com" {
		return parsed.Path == "/0xmattg/go-press" || strings.HasPrefix(parsed.Path, "/0xmattg/go-press/")
	}
	return false
}
