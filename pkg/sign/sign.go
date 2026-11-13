// Package sign implements the GoPress signing protocol: Ed25519 keys named by
// a short key ID, signed statements that state their purpose, and the h1
// content digest of extension packages.
//
// A statement is a few lines of text: the protocol version, a purpose, and the
// purpose's fields. Every signature covers one statement, so a signature made
// for one purpose (for example a publisher signing a theme package) can never
// be presented as a signature for another (a market countersignature, a
// catalog, a license). Callers build statements with the constructors in this
// package rather than by hand.
//
// The package has no GoPress dependencies, so market servers and command-line
// tools can use it on their own.
package sign

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

const (
	// Algorithm is the only signature algorithm the protocol uses.
	Algorithm = "ed25519"
	// StatementVersion is the first line of every signed statement.
	StatementVersion = "gopress-sig-v1"
	// KIDLength is the length of a key ID in hex characters.
	KIDLength = 16
)

// Statement purposes.
const (
	PurposePackagePublisher = "package-publisher"
	PurposePackageMarket    = "package-market"
	PurposeCatalog          = "catalog"
)

var (
	// ErrBadSignature reports a signature that does not verify.
	ErrBadSignature = errors.New("sign: signature does not verify")
	// ErrUnknownKey reports a signature by a key that is not trusted.
	ErrUnknownKey = errors.New("sign: signing key is not trusted")
)

// PublicKey is an Ed25519 public key.
type PublicKey struct {
	key ed25519.PublicKey
}

// ParsePublicKey reads the base64 form produced by PublicKey.String.
func ParsePublicKey(s string) (PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return PublicKey{}, fmt.Errorf("sign: public key is not base64: %w", err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return PublicKey{}, fmt.Errorf("sign: public key has %d bytes, want %d", len(raw), ed25519.PublicKeySize)
	}
	return PublicKey{key: ed25519.PublicKey(raw)}, nil
}

// String returns the key as standard base64, the form used in configuration,
// signature files, and catalogs.
func (k PublicKey) String() string {
	return base64.StdEncoding.EncodeToString(k.key)
}

// KID returns the key ID: the first 8 bytes of SHA-256 of the key, in hex.
func (k PublicKey) KID() string {
	sum := sha256.Sum256(k.key)
	return hex.EncodeToString(sum[:KIDLength/2])
}

// IsZero reports whether k holds no key.
func (k PublicKey) IsZero() bool { return len(k.key) == 0 }

// Equal reports whether two keys are the same.
func (k PublicKey) Equal(other PublicKey) bool { return k.key.Equal(other.key) }

// PrivateKey is an Ed25519 private key.
type PrivateKey struct {
	key ed25519.PrivateKey
}

// GenerateKey creates a new key pair. A nil reader uses crypto/rand.
func GenerateKey(random io.Reader) (PrivateKey, error) {
	if random == nil {
		random = rand.Reader
	}
	_, priv, err := ed25519.GenerateKey(random)
	if err != nil {
		return PrivateKey{}, err
	}
	return PrivateKey{key: priv}, nil
}

// Public returns the public half of the key.
func (k PrivateKey) Public() PublicKey {
	return PublicKey{key: k.key.Public().(ed25519.PublicKey)}
}

// MarshalPEM encodes the key as a PKCS #8 "PRIVATE KEY" PEM block, which
// OpenSSL and other tools also read.
func (k PrivateKey) MarshalPEM() ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(k.key)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}

// ParsePrivateKeyPEM reads a key written by MarshalPEM.
func ParsePrivateKeyPEM(data []byte) (PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "PRIVATE KEY" {
		return PrivateKey{}, errors.New("sign: no PRIVATE KEY PEM block found")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return PrivateKey{}, fmt.Errorf("sign: parse private key: %w", err)
	}
	key, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return PrivateKey{}, fmt.Errorf("sign: private key is %T, want ed25519", parsed)
	}
	return PrivateKey{key: key}, nil
}

// Statement is what a signature covers.
type Statement struct {
	Purpose string
	Fields  []string
}

var purposePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Bytes returns the exact bytes that are signed:
//
//	gopress-sig-v1\n<purpose>\n<field 1>\n…<field n>\n
//
// Fields must be non-empty and contain no line breaks, so every statement
// has exactly one encoding.
func (s Statement) Bytes() ([]byte, error) {
	if !purposePattern.MatchString(s.Purpose) {
		return nil, fmt.Errorf("sign: invalid statement purpose %q", s.Purpose)
	}
	var b strings.Builder
	b.WriteString(StatementVersion)
	b.WriteByte('\n')
	b.WriteString(s.Purpose)
	b.WriteByte('\n')
	for i, field := range s.Fields {
		if field == "" || strings.ContainsAny(field, "\r\n") {
			return nil, fmt.Errorf("sign: statement field %d of %q is empty or contains a line break", i, s.Purpose)
		}
		b.WriteString(field)
		b.WriteByte('\n')
	}
	return []byte(b.String()), nil
}

// PackageID names one version of an extension package.
type PackageID struct {
	Kind    string // "theme"; "plugin" later
	Slug    string
	Version string
}

var (
	kindPattern = regexp.MustCompile(`^[a-z]+$`)
	slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
)

// Validate checks the identity fields: a lowercase kind, a slug of lowercase
// letters and digits joined by single hyphens (2–64 characters), and a
// version without whitespace.
func (id PackageID) Validate() error {
	switch {
	case !kindPattern.MatchString(id.Kind):
		return fmt.Errorf("sign: invalid package kind %q", id.Kind)
	case len(id.Slug) < 2 || len(id.Slug) > 64 || !slugPattern.MatchString(id.Slug):
		return fmt.Errorf("sign: invalid package slug %q", id.Slug)
	case id.Version == "" || strings.ContainsAny(id.Version, " \t\r\n"):
		return fmt.Errorf("sign: invalid package version %q", id.Version)
	}
	return nil
}

// PublisherStatement is what a publisher signs for a package.
func PublisherStatement(id PackageID, digest string) Statement {
	return Statement{Purpose: PurposePackagePublisher, Fields: []string{id.Kind, id.Slug, id.Version, digest}}
}

// MarketStatement is what a market countersigns after review. It names the
// publisher's key, so the countersignature also fixes who published the
// package.
func MarketStatement(id PackageID, publisherKID, digest string) Statement {
	return Statement{Purpose: PurposePackageMarket, Fields: []string{id.Kind, id.Slug, id.Version, publisherKID, digest}}
}

// CatalogStatement is what a market signs for a catalog: its sequence number
// and the hex SHA-256 of the catalog document.
func CatalogStatement(sequence int64, documentSHA256 string) Statement {
	return Statement{Purpose: PurposeCatalog, Fields: []string{strconv.FormatInt(sequence, 10), documentSHA256}}
}

// Signature is a detached signature as stored in signature files and
// catalogs. PublicKey is set only where the key is not known in advance,
// such as a publisher's signature inside a package.
type Signature struct {
	KID       string `json:"kid"`
	Alg       string `json:"alg"`
	Sig       string `json:"sig"`
	PublicKey string `json:"public_key,omitempty"`
}

// Sign signs a statement.
func (k PrivateKey) Sign(statement Statement) (Signature, error) {
	msg, err := statement.Bytes()
	if err != nil {
		return Signature{}, err
	}
	return Signature{
		KID: k.Public().KID(),
		Alg: Algorithm,
		Sig: base64.StdEncoding.EncodeToString(ed25519.Sign(k.key, msg)),
	}, nil
}

// Verify checks that sig is pub's signature over statement.
func Verify(pub PublicKey, statement Statement, sig Signature) error {
	if pub.IsZero() {
		return ErrUnknownKey
	}
	if sig.Alg != Algorithm {
		return fmt.Errorf("sign: unsupported algorithm %q", sig.Alg)
	}
	if sig.KID != pub.KID() {
		return fmt.Errorf("%w: signature names key %s, verifying with %s", ErrBadSignature, sig.KID, pub.KID())
	}
	raw, err := base64.StdEncoding.DecodeString(sig.Sig)
	if err != nil || len(raw) != ed25519.SignatureSize {
		return fmt.Errorf("%w: malformed signature", ErrBadSignature)
	}
	msg, err := statement.Bytes()
	if err != nil {
		return err
	}
	if !ed25519.Verify(pub.key, msg, raw) {
		return ErrBadSignature
	}
	return nil
}

// KeySet is a set of trusted keys indexed by key ID.
type KeySet map[string]PublicKey

// NewKeySet builds a set from keys.
func NewKeySet(keys ...PublicKey) KeySet {
	set := make(KeySet, len(keys))
	for _, key := range keys {
		set[key.KID()] = key
	}
	return set
}

// ParseKeySet builds a set from base64 keys, as written in configuration.
func ParseKeySet(encoded []string) (KeySet, error) {
	set := make(KeySet, len(encoded))
	for _, value := range encoded {
		key, err := ParsePublicKey(value)
		if err != nil {
			return nil, err
		}
		set[key.KID()] = key
	}
	return set, nil
}

// Verify checks sig against the trusted key it names.
func (s KeySet) Verify(statement Statement, sig Signature) error {
	key, ok := s[sig.KID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownKey, sig.KID)
	}
	return Verify(key, statement, sig)
}
