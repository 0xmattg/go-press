package sign

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"

	"golang.org/x/mod/sumdb/dirhash"
)

// Signature files inside a package. They live under SignaturesDir, which the
// content digest leaves out, so adding a signature does not change what was
// signed.
const (
	SignaturesDir          = "signatures"
	PublisherSignatureFile = SignaturesDir + "/publisher.json"
	MarketSignatureFile    = SignaturesDir + "/market.json"
)

// ErrUnsigned reports a package without a publisher signature.
var ErrUnsigned = errors.New("sign: package has no publisher signature")

// Digest returns the h1 digest of the regular files in fsys, the same format
// Go modules use (golang.org/x/mod/sumdb/dirhash.Hash1): SHA-256 over the
// sorted lines "<hex sha256 of file>  <path>\n", base64-encoded with an "h1:"
// prefix. It does not depend on file order, timestamps, or compression, so a
// directory and a zip of it have the same digest.
//
// skip, when not nil, leaves out the files and directories it returns true
// for. Anything that is not a regular file or directory, such as a symbolic
// link, is an error.
func Digest(fsys fs.FS, skip func(name string) bool) (string, error) {
	var files []string
	err := fs.WalkDir(fsys, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if name == "." {
			return nil
		}
		if skip != nil && skip(name) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("sign: %s is not a regular file", name)
		}
		files = append(files, name)
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", errors.New("sign: nothing to digest")
	}
	return dirhash.Hash1(files, func(name string) (io.ReadCloser, error) { return fsys.Open(name) })
}

// PackageDigest is Digest without the top-level signatures directory.
func PackageDigest(fsys fs.FS) (string, error) {
	return Digest(fsys, func(name string) bool { return name == SignaturesDir })
}

// ReadSignature reads a signature file. A missing file returns an error
// wrapping fs.ErrNotExist.
func ReadSignature(fsys fs.FS, name string) (Signature, error) {
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return Signature{}, err
	}
	var sig Signature
	if err := json.Unmarshal(data, &sig); err != nil {
		return Signature{}, fmt.Errorf("sign: %s: %w", name, err)
	}
	return sig, nil
}

// MarshalSignature encodes a signature file.
func MarshalSignature(sig Signature) ([]byte, error) {
	data, err := json.MarshalIndent(sig, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// VerifyOptions controls VerifyPackage.
type VerifyOptions struct {
	// PublisherKey, when set, is the publisher key a signed catalog vouches
	// for, and the publisher signature must be by that key. When unset, the
	// key embedded in the publisher signature is used; trust then rests on
	// the market countersignature, which names that key.
	PublisherKey PublicKey
	// MarketKeys are the trusted market keys. When empty the market
	// countersignature is not checked.
	MarketKeys KeySet
	// RequireMarket fails verification unless a trusted market
	// countersignature verifies.
	RequireMarket bool
	// PublisherSignature and MarketSignature, when not nil, are used instead
	// of the signature files inside the package (a catalog carries them).
	PublisherSignature *Signature
	MarketSignature    *Signature
}

// PackageVerification is the outcome of a successful VerifyPackage.
type PackageVerification struct {
	Digest       string
	PublisherKID string
	// Market reports whether a trusted market countersignature verified.
	Market bool
}

// VerifyPackage checks a package's publisher signature and, when market keys
// are given, its market countersignature, both over the package's digest and
// identity. It fails if the package is unsigned, if any signature present is
// invalid, or if RequireMarket is set and no trusted countersignature verifies.
func VerifyPackage(fsys fs.FS, id PackageID, opts VerifyOptions) (PackageVerification, error) {
	if err := id.Validate(); err != nil {
		return PackageVerification{}, err
	}
	digest, err := PackageDigest(fsys)
	if err != nil {
		return PackageVerification{}, err
	}
	result := PackageVerification{Digest: digest}

	publisherSig, err := signatureFrom(fsys, opts.PublisherSignature, PublisherSignatureFile)
	if errors.Is(err, fs.ErrNotExist) {
		return result, ErrUnsigned
	}
	if err != nil {
		return result, err
	}
	publisherKey := opts.PublisherKey
	if publisherKey.IsZero() {
		if publisherKey, err = ParsePublicKey(publisherSig.PublicKey); err != nil {
			return result, fmt.Errorf("sign: publisher signature: %w", err)
		}
	} else if publisherSig.PublicKey != "" && publisherSig.PublicKey != publisherKey.String() {
		return result, fmt.Errorf("%w: package names a different publisher key than the catalog", ErrBadSignature)
	}
	if err := Verify(publisherKey, PublisherStatement(id, digest), publisherSig); err != nil {
		return result, fmt.Errorf("publisher signature: %w", err)
	}
	result.PublisherKID = publisherKey.KID()

	if len(opts.MarketKeys) == 0 {
		if opts.RequireMarket {
			return result, fmt.Errorf("%w: no trusted market keys", ErrUnknownKey)
		}
		return result, nil
	}
	marketSig, err := signatureFrom(fsys, opts.MarketSignature, MarketSignatureFile)
	if errors.Is(err, fs.ErrNotExist) {
		if opts.RequireMarket {
			return result, errors.New("sign: package has no market countersignature")
		}
		return result, nil
	}
	if err != nil {
		return result, err
	}
	if err := opts.MarketKeys.Verify(MarketStatement(id, result.PublisherKID, digest), marketSig); err != nil {
		return result, fmt.Errorf("market signature: %w", err)
	}
	result.Market = true
	return result, nil
}

func signatureFrom(fsys fs.FS, given *Signature, name string) (Signature, error) {
	if given != nil {
		return *given, nil
	}
	return ReadSignature(fsys, name)
}
