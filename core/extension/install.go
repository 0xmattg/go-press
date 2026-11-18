// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package extension

import (
	"errors"
	"io/fs"

	"github.com/0xmattg/go-press/pkg/sign"
)

// OptionDeveloperMode is the option that records whether an administrator
// turned developer mode on ("1"). In developer mode packages without a valid
// market signature can be installed. Only its own admin endpoint writes it:
// the generic settings save refuses the key, and [market]
// lock_developer_mode in config.toml keeps the mode off whatever it says.
const OptionDeveloperMode = "market_developer_mode"

// Signature states of an uploaded package, as CheckUploadSignature reports
// them. Every state but SignatureVerified makes the package unsigned for
// the install flow.
const (
	// SignatureVerified: a trusted market countersigned the package.
	SignatureVerified = "verified"
	// SignatureMissing: the package carries no publisher signature or no
	// market countersignature.
	SignatureMissing = "unsigned"
	// SignatureInvalid: a signature is present but does not verify, for
	// example because a file was changed after signing.
	SignatureInvalid = "bad_signature"
	// SignatureNoMarket: no market is configured, so no countersignature
	// can be trusted.
	SignatureNoMarket = "no_market"
)

// UploadSignature is the outcome of CheckUploadSignature.
type UploadSignature struct {
	// State is one of the Signature* constants.
	State string
	// PublisherKID is the publisher key's ID when State is
	// SignatureVerified.
	PublisherKID string
	// Err says why verification failed, for the log.
	Err error
}

// Verified reports whether a trusted market countersigned the package.
func (s UploadSignature) Verified() bool { return s.State == SignatureVerified }

// CheckUploadSignature verifies the signature files inside an uploaded
// package. With no catalog to vouch for the publisher key, the key the
// publisher signature names is used, and trust rests on the market
// countersignature, which names that key's ID (D1 4.2). fsys holds the
// package files, signatures/ included; id is the identity from its
// theme.toml.
func CheckUploadSignature(fsys fs.FS, id sign.PackageID, root TrustRoot) UploadSignature {
	if !root.Enabled() {
		return UploadSignature{State: SignatureNoMarket}
	}
	result, err := sign.VerifyPackage(fsys, id, sign.VerifyOptions{MarketKeys: root.PackageKeys, RequireMarket: true})
	if err == nil {
		return UploadSignature{State: SignatureVerified, PublisherKID: result.PublisherKID}
	}
	if errors.Is(err, sign.ErrUnsigned) {
		return UploadSignature{State: SignatureMissing, Err: err}
	}
	if _, statErr := fs.Stat(fsys, sign.MarketSignatureFile); errors.Is(statErr, fs.ErrNotExist) {
		return UploadSignature{State: SignatureMissing, Err: err}
	}
	return UploadSignature{State: SignatureInvalid, Err: err}
}
