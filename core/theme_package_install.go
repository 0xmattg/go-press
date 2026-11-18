// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/0xmattg/go-press/core/extension"
	"github.com/0xmattg/go-press/core/extension/filetheme"
	"github.com/0xmattg/go-press/core/extension/themepkg"
	"github.com/0xmattg/go-press/core/plugin"
	coreTheme "github.com/0xmattg/go-press/core/theme"
	"github.com/0xmattg/go-press/pkg/logger"
)

// Installing, upgrading, rolling back and uninstalling theme packages (D1,
// GIP-A0001b 4.3.5).
//
// Every package is checked with the running core's rules before anything is
// written, and its theme is built once, so a package that cannot run is
// never recorded. A package from the market must have the content the
// catalog signed; an uploaded package must carry a trusted market
// countersignature, or developer mode must be on and the administrator must
// confirm that exact content. Revoked versions are never installed, rolled
// back to or switched to.
//
// Writes keep the order the boot reconciliation relies on: the archive
// first, then its record, then this process's theme set. Installing does not
// activate a theme; switching to it does. Upgrading or rolling back the
// active theme replaces it at once, and when the new version cannot be
// activated the previous version is made current again, so the records
// always describe what runs. After an install the slug keeps its current
// version and the keepPackageVersions most recent others.
//
// Operations that write are serialized within the process. Several
// processes per site are not supported yet (audit A07).

// Error codes of PackageError. They are stable: the admin translates them.
const (
	PackageErrInvalid      = "invalid_request"
	PackageErrUnavailable  = "unavailable"
	PackageErrMarket       = "market_disabled"
	PackageErrCatalog      = "catalog_unavailable"
	PackageErrNotInCatalog = "not_in_catalog"
	PackageErrRevoked      = "revoked"
	PackageErrPaid         = "paid"
	PackageErrIncompatible = "incompatible"
	PackageErrDownload     = "download_failed"
	PackageErrCheck        = "check_failed"
	PackageErrMismatch     = "content_mismatch"
	PackageErrDependencies = "dependencies_unmet"
	PackageErrUnsigned     = "unsigned_not_allowed"
	PackageErrConfirm      = "confirmation_required"
	PackageErrNotFound     = "not_found"
	PackageErrBroken       = "broken"
	PackageErrDamaged      = "archive_damaged"
	PackageErrActive       = "theme_active"
	PackageErrActivation   = "activation_failed"
	PackageErrInternal     = "internal"
)

// Notices of a successful operation.
const (
	// PackageNoticeAlreadyCurrent: the version was the current one already.
	PackageNoticeAlreadyCurrent = "already_current"
	// PackageNoticePreview: a preview package of the same slug runs instead
	// of the installed one until the site restarts without it.
	PackageNoticePreview = "preview_overrides"
)

// keepPackageVersions is how many versions besides the current one a theme
// package keeps for rollbacks (D1 #7).
const keepPackageVersions = 3

// packageWriteTimeout bounds the write part of an operation. It runs on a
// context the request cannot cancel, so a closed browser tab never leaves an
// archive without its record.
const packageWriteTimeout = time.Minute

// PackageError is a failed package operation. Code is one of the
// PackageErr* constants; Err is the detail for the log.
type PackageError struct {
	Code string
	Err  error
	// Problems lists why a package failed the check (PackageErrCheck).
	Problems []themepkg.Problem
	// Package describes the package, when it was read: what an
	// administrator confirms for an unsigned upload.
	Package *PackageSummary
}

func (e *PackageError) Error() string {
	if e.Err == nil {
		return "theme package: " + e.Code
	}
	return "theme package: " + e.Code + ": " + e.Err.Error()
}

func (e *PackageError) Unwrap() error { return e.Err }

func packageError(code, format string, args ...interface{}) *PackageError {
	return &PackageError{Code: code, Err: fmt.Errorf(format, args...)}
}

// PackageSummary describes a package that passed the check.
type PackageSummary struct {
	Name     string
	Slug     string
	Version  string
	Digest   string
	Size     int64
	Files    int
	External []string
	// Signature is one of the extension.Signature* states.
	Signature string
}

// PackageResult is the outcome of a successful operation.
type PackageResult struct {
	// Package is the version that is now current.
	Package extension.Package
	// Notice is empty or one of the PackageNotice* constants.
	Notice string
	// Summary describes the package that was installed, when one was read.
	Summary *PackageSummary
}

// InstalledThemePackage is one installed theme package and its versions.
type InstalledThemePackage struct {
	Slug string
	// Active reports that it is the active theme, or the configured one
	// while a boot fallback runs another.
	Active bool
	// Running is the digest of the version this process serves, empty when
	// it is not loaded.
	Running string
	// Preview reports that a preview package of the same slug runs instead.
	Preview bool
	// Issue is why the current version did not load, if it did not.
	Issue string
	// Versions are the installed versions, the current one first and then
	// newest first.
	Versions []extension.Package
}

// checkedThemePackage is a package that passed the check, with its theme.
type checkedThemePackage struct {
	pkg   *themepkg.Package
	data  []byte
	theme *filetheme.Theme
}

func (c *checkedThemePackage) summary(signature string) *PackageSummary {
	report := c.pkg.Report()
	return &PackageSummary{
		Name:      c.theme.Name(),
		Slug:      report.ID.Slug,
		Version:   report.ID.Version,
		Digest:    report.Digest,
		Size:      int64(len(c.data)),
		Files:     report.Files,
		External:  externalHosts(report.External),
		Signature: signature,
	}
}

// externalHosts returns the declared hosts, lowercased, sorted and unique.
func externalHosts(external []coreTheme.ExternalConfig) []string {
	seen := make(map[string]bool, len(external))
	hosts := make([]string, 0, len(external))
	for _, ext := range external {
		host := strings.ToLower(strings.TrimSpace(ext.Host))
		if host == "" || seen[host] {
			continue
		}
		seen[host] = true
		hosts = append(hosts, host)
	}
	sort.Strings(hosts)
	return hosts
}

func sameHosts(a, b []string) bool {
	norm := func(hosts []string) []string {
		out := make([]string, 0, len(hosts))
		seen := make(map[string]bool, len(hosts))
		for _, h := range hosts {
			h = strings.ToLower(strings.TrimSpace(h))
			if h != "" && !seen[h] {
				seen[h] = true
				out = append(out, h)
			}
		}
		sort.Strings(out)
		return out
	}
	x, y := norm(a), norm(b)
	if len(x) != len(y) {
		return false
	}
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

func actorID(actor uint) *uint {
	if actor == 0 {
		return nil
	}
	return &actor
}

// packagesAvailable reports whether the site can install packages: it needs
// the package store and an archive directory.
func (e *Engine) packagesAvailable() error {
	if !e.installedPackagesEnabled() {
		return packageError(PackageErrUnavailable, "this site has no package storage")
	}
	return nil
}

// packageWriteContext detaches ctx from the request for the write part of
// an operation.
func packageWriteContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), packageWriteTimeout)
}

// checkThemePackage checks a package archive with the running core's rules
// and builds its theme.
func (e *Engine) checkThemePackage(data []byte) (*checkedThemePackage, error) {
	pkg, report, err := themepkg.ReadZip(bytes.NewReader(data), int64(len(data)), ThemePackageOptions())
	if err != nil {
		return nil, &PackageError{Code: PackageErrCheck, Err: err}
	}
	if pkg == nil {
		return nil, &PackageError{Code: PackageErrCheck, Err: errors.New("the package fails the package check"), Problems: report.Problems}
	}
	t, err := filetheme.New(e, pkg, coreTheme.PackageSourceInstalled)
	if err != nil {
		return nil, &PackageError{Code: PackageErrCheck, Err: err}
	}
	return &checkedThemePackage{pkg: pkg, data: data, theme: t}, nil
}

// InstallThemePackage installs version of the theme package slug from the
// market, or upgrades to it, and makes it the slug's current version. The
// catalog is fetched now: a version the market withdrew or revoked cannot be
// installed, and a failed fetch is an error rather than a reason to use an
// older catalog. The downloaded package must have the size, identity,
// content digest and external hosts the catalog lists; the catalog's
// signatures cover that digest, which completes the signature check.
func (e *Engine) InstallThemePackage(ctx context.Context, slug, version string, actor uint) (*PackageResult, error) {
	if err := e.packagesAvailable(); err != nil {
		return nil, err
	}
	if !plugin.ValidSlug(slug) || strings.TrimSpace(version) == "" {
		return nil, packageError(PackageErrInvalid, "a valid slug and a version are required")
	}
	if e.Market == nil || !e.Market.Enabled() {
		return nil, packageError(PackageErrMarket, "no extension market is configured")
	}
	cat, err := e.Market.Refresh(ctx)
	if err != nil {
		return nil, &PackageError{Code: PackageErrCatalog, Err: err}
	}
	item := cat.Find(extension.KindTheme, slug, version)
	switch {
	case item == nil:
		return nil, packageError(PackageErrNotInCatalog, "the market does not offer %s %s", slug, version)
	case cat.Revocation(extension.KindTheme, slug, version) != nil:
		return nil, packageError(PackageErrRevoked, "the market revoked %s %s", slug, version)
	case item.Pricing != extension.PricingFree:
		return nil, packageError(PackageErrPaid, "%s %s is not free", slug, version)
	case !item.Compatible:
		return nil, packageError(PackageErrIncompatible, "%s %s needs GoPress %s", slug, version, item.Core)
	}
	data, err := e.Market.Download(ctx, *item)
	if err != nil {
		return nil, &PackageError{Code: PackageErrDownload, Err: err}
	}
	checked, err := e.checkThemePackage(data)
	if err != nil {
		return nil, err
	}
	id := checked.pkg.ID()
	switch {
	case id.Slug != slug || id.Version != version:
		return nil, packageError(PackageErrMismatch, "the download is %s %s, not %s %s", id.Slug, id.Version, slug, version)
	case checked.pkg.Digest() != item.Digest:
		return nil, packageError(PackageErrMismatch, "the download's content digest %s is not the catalog's %s", checked.pkg.Digest(), item.Digest)
	case !sameHosts(externalHosts(checked.pkg.Report().External), item.External):
		return nil, packageError(PackageErrMismatch, "the package declares external hosts %v, the catalog lists %v",
			externalHosts(checked.pkg.Report().External), item.External)
	}
	row := extension.Package{
		Kind: extension.KindTheme, Slug: slug, Version: version, Digest: item.Digest,
		Source: extension.SourceMarket, Signed: true, PublisherKID: item.Publisher.KID,
		InstalledBy: actorID(actor),
	}
	result, err := e.commitThemePackage(ctx, checked, row)
	if result != nil {
		result.Summary = checked.summary(extension.SignatureVerified)
	}
	return result, err
}

// UploadThemePackage installs an uploaded package archive and makes it its
// slug's current version. A package with a trusted market countersignature
// is installed as signed. Any other package needs developer mode and a
// confirmation: without confirmDigest equal to the package's digest it is
// not installed and the error carries the summary to confirm. Revocations
// are checked against the stored catalog, so uploads work offline.
func (e *Engine) UploadThemePackage(ctx context.Context, data []byte, confirmDigest string, actor uint) (*PackageResult, error) {
	if err := e.packagesAvailable(); err != nil {
		return nil, err
	}
	checked, err := e.checkThemePackage(data)
	if err != nil {
		return nil, err
	}
	id := checked.pkg.ID()
	signature := extension.UploadSignature{State: extension.SignatureNoMarket}
	if e.Market != nil {
		signature = extension.CheckUploadSignature(checked.pkg.FS(), id, e.Market.Root())
	}
	summary := checked.summary(signature.State)
	if revoked, err := e.storedRevocation(ctx, id.Slug, id.Version); err != nil {
		return nil, &PackageError{Code: PackageErrInternal, Err: err}
	} else if revoked {
		return nil, &PackageError{Code: PackageErrRevoked, Err: fmt.Errorf("the market revoked %s %s", id.Slug, id.Version), Package: summary}
	}
	if !signature.Verified() {
		if !e.DeveloperMode().Enabled {
			return nil, &PackageError{Code: PackageErrUnsigned, Package: summary,
				Err: fmt.Errorf("%s %s has no valid market signature (%s) and developer mode is off", id.Slug, id.Version, signature.State)}
		}
		if confirmDigest != checked.pkg.Digest() {
			return nil, &PackageError{Code: PackageErrConfirm, Package: summary,
				Err: fmt.Errorf("%s %s is unsigned (%s); confirm its digest to install it", id.Slug, id.Version, signature.State)}
		}
		logger.Warn("Installing a theme package without a valid market signature in developer mode",
			"slug", id.Slug, "version", id.Version, "digest", checked.pkg.Digest(), "signature", signature.State, "detail", signature.Err)
	}
	row := extension.Package{
		Kind: extension.KindTheme, Slug: id.Slug, Version: id.Version, Digest: checked.pkg.Digest(),
		Source: extension.SourceUpload, Signed: signature.Verified(), PublisherKID: signature.PublisherKID,
		InstalledBy: actorID(actor),
	}
	result, err := e.commitThemePackage(ctx, checked, row)
	if result != nil {
		result.Summary = summary
	}
	return result, err
}

// storedRevocation applies the stored catalog's revocations and reports
// whether it revokes the theme package version. No stored catalog revokes
// nothing.
func (e *Engine) storedRevocation(ctx context.Context, slug, version string) (bool, error) {
	if e.Market == nil {
		return false, nil
	}
	if err := e.Market.ApplyStoredRevocations(ctx); err != nil {
		return false, err
	}
	cat, err := e.Market.Stored(ctx)
	if err != nil || cat == nil {
		return false, err
	}
	return cat.Revocation(extension.KindTheme, slug, version) != nil, nil
}

// checkActiveUpgrade refuses to replace the running active theme with a
// version whose required plugins are missing or incompatible. A theme that
// is not active is checked when it is switched to.
func (e *Engine) checkActiveUpgrade(slug string, t coreTheme.Theme) error {
	if e.ActiveThemeName() != slug {
		return nil
	}
	if info, ok := e.runningPackage(slug); ok && info.Source == coreTheme.PackageSourcePreview {
		return nil
	}
	var req coreTheme.RequiresConfig
	if rp, ok := t.(coreTheme.RequirementsProvider); ok {
		req = rp.Requires()
	}
	if err := e.dependencyReport(slug, req).BlockingError(); err != nil {
		return &PackageError{Code: PackageErrDependencies, Err: err}
	}
	return nil
}

// runningPackage returns the package information of the theme loaded under
// slug, if it is a package.
func (e *Engine) runningPackage(slug string) (coreTheme.PackageInfo, bool) {
	t, ok := e.lookupTheme(slug)
	if !ok {
		return coreTheme.PackageInfo{}, false
	}
	return coreTheme.PackageInfoOf(t)
}

func (e *Engine) installedLoadIssue(slug string) string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.installedLoadIssues[slug]
}

// commitThemePackage stores a checked package and makes it the slug's
// current version. When the same version with the same content is installed
// already, its record is reused; a damaged archive of it is replaced.
func (e *Engine) commitThemePackage(ctx context.Context, checked *checkedThemePackage, row extension.Package) (*PackageResult, error) {
	if err := e.checkActiveUpgrade(row.Slug, checked.theme); err != nil {
		return nil, err
	}
	ctx, cancel := packageWriteContext(ctx)
	defer cancel()
	e.packageInstallMu.Lock()
	defer e.packageInstallMu.Unlock()

	versions, err := e.ExtStore.Versions(ctx, extension.KindTheme, row.Slug)
	if err != nil {
		return nil, &PackageError{Code: PackageErrInternal, Err: err}
	}
	var prev, same *extension.Package
	for i := range versions {
		if versions[i].IsCurrent {
			prev = &versions[i]
		}
		if versions[i].Version == row.Version && versions[i].Digest == row.Digest {
			same = &versions[i]
		}
	}
	if same != nil {
		reused, err := e.reuseInstalledVersion(ctx, *same)
		if err != nil {
			return nil, err
		}
		if reused != nil {
			if reused.IsCurrent {
				if err := e.ReloadThemePackages(ctx); err != nil {
					return nil, &PackageError{Code: PackageErrInternal, Err: err}
				}
				return &PackageResult{Package: *reused, Notice: PackageNoticeAlreadyCurrent}, nil
			}
			if err := e.ExtStore.SetCurrent(ctx, reused.ID); err != nil {
				return nil, &PackageError{Code: PackageErrInternal, Err: err}
			}
			reused.IsCurrent = true
			return e.applyCurrentVersion(ctx, prev, *reused, false)
		}
		// The record was dropped with its damaged archive: install anew.
		if prev != nil && prev.ID == same.ID {
			prev = nil
		}
	}

	_, size, err := e.PackageArchives.Put(row.Slug, row.Version, row.Digest, checked.data)
	if err != nil {
		return nil, &PackageError{Code: PackageErrInternal, Err: err}
	}
	// A file kept from an earlier install of this content may differ in
	// size from the new copy; the record has the size on disk.
	row.Size = size
	if err := e.ExtStore.Add(ctx, &row, true); err != nil {
		return nil, &PackageError{Code: PackageErrInternal, Err: err}
	}
	result, err := e.applyCurrentVersion(ctx, prev, row, true)
	e.prunePackageVersions(ctx, row.Slug)
	return result, err
}

// reuseInstalledVersion prepares an installed version for reuse by an
// install of the same content. A sound archive keeps its record, which
// becomes ready again if it was marked broken; a damaged archive is removed
// with its record, and nil is returned so the caller stores the new copy.
func (e *Engine) reuseInstalledVersion(ctx context.Context, row extension.Package) (*extension.Package, error) {
	if row.Status == extension.StatusRevoked {
		return nil, packageError(PackageErrRevoked, "%s %s is revoked", row.Slug, row.Version)
	}
	path, err := e.PackageArchives.Path(row.Slug, row.Version, row.Digest)
	if err != nil {
		return nil, &PackageError{Code: PackageErrInternal, Err: err}
	}
	_, loadErr := e.LoadThemePackage(ThemePackageSource{Path: path, Source: coreTheme.PackageSourceInstalled, Digest: row.Digest})
	if loadErr == nil {
		if row.Status == extension.StatusBroken {
			if _, err := e.ExtStore.UpdateStatus(ctx, row.ID, extension.StatusBroken, extension.StatusReady); err != nil {
				return nil, &PackageError{Code: PackageErrInternal, Err: err}
			}
			row.Status = extension.StatusReady
		}
		return &row, nil
	}
	if !errors.Is(loadErr, ErrPackageArchive) {
		return nil, &PackageError{Code: PackageErrInternal, Err: loadErr}
	}
	logger.Warn("Installed theme package archive is damaged; replacing it with the new copy", "slug", row.Slug, "version", row.Version, "error", loadErr)
	if err := e.ExtStore.Delete(ctx, row.ID); err != nil {
		return nil, &PackageError{Code: PackageErrInternal, Err: err}
	}
	if err := e.PackageArchives.Remove(row.Slug, row.Version, row.Digest); err != nil {
		return nil, &PackageError{Code: PackageErrInternal, Err: err}
	}
	return nil, nil
}

// applyCurrentVersion loads the slug's new current version into this
// process and checks that it runs. When it does not, the previous version
// becomes current again; a first install that does not run is removed
// (fresh). The caller holds packageInstallMu.
func (e *Engine) applyCurrentVersion(ctx context.Context, prev *extension.Package, next extension.Package, fresh bool) (*PackageResult, error) {
	if err := e.ReloadThemePackages(ctx); err != nil {
		return nil, &PackageError{Code: PackageErrInternal, Err: err}
	}
	info, loaded := e.runningPackage(next.Slug)
	switch {
	case loaded && info.Source == coreTheme.PackageSourcePreview:
		return &PackageResult{Package: next, Notice: PackageNoticePreview}, nil
	case loaded && info.Digest == next.Digest:
		return &PackageResult{Package: next}, nil
	}

	reason := e.installedLoadIssue(next.Slug)
	if reason == "" {
		reason = "it did not replace the running version"
	}
	switch {
	case prev != nil && prev.ID != next.ID:
		if err := e.ExtStore.SetCurrent(ctx, prev.ID); err != nil {
			logger.Error("Theme package version could not be restored after a failed activation", "slug", next.Slug, "version", prev.Version, "error", err)
		} else if err := e.ReloadThemePackages(ctx); err != nil {
			logger.Error("Theme packages could not be reloaded after restoring a version", "slug", next.Slug, "error", err)
		}
	case fresh:
		if err := e.ExtStore.Delete(ctx, next.ID); err != nil {
			logger.Error("Theme package that failed to activate could not be removed", "slug", next.Slug, "error", err)
		} else if err := e.PackageArchives.Remove(next.Slug, next.Version, next.Digest); err != nil {
			logger.Error("Theme package archive could not be removed", "slug", next.Slug, "error", err)
		}
		if err := e.ReloadThemePackages(ctx); err != nil {
			logger.Error("Theme packages could not be reloaded", "error", err)
		}
	}
	logger.Error("Theme package version could not be activated", "slug", next.Slug, "version", next.Version, "reason", reason)
	return nil, packageError(PackageErrActivation, "%s %s could not be activated: %s", next.Slug, next.Version, reason)
}

// prunePackageVersions keeps the slug's current version and the
// keepPackageVersions most recently installed other versions, and deletes
// the rest: records first, then archives. Failures are logged; the install
// that triggered it has succeeded.
func (e *Engine) prunePackageVersions(ctx context.Context, slug string) {
	versions, err := e.ExtStore.Versions(ctx, extension.KindTheme, slug)
	if err != nil {
		logger.Error("Old theme package versions could not be listed", "slug", slug, "error", err)
		return
	}
	var others []extension.Package
	for _, v := range versions {
		if !v.IsCurrent {
			others = append(others, v)
		}
	}
	if len(others) <= keepPackageVersions {
		return
	}
	sort.SliceStable(others, func(i, j int) bool {
		if !others[i].InstalledAt.Equal(others[j].InstalledAt) {
			return others[i].InstalledAt.After(others[j].InstalledAt)
		}
		return others[i].ID > others[j].ID
	})
	drop := others[keepPackageVersions:]
	ids := make([]int64, len(drop))
	for i, v := range drop {
		ids[i] = v.ID
	}
	if err := e.ExtStore.Delete(ctx, ids...); err != nil {
		logger.Error("Old theme package versions could not be removed", "slug", slug, "error", err)
		return
	}
	for _, v := range drop {
		if err := e.PackageArchives.Remove(v.Slug, v.Version, v.Digest); err != nil {
			logger.Error("Old theme package archive could not be removed", "slug", v.Slug, "version", v.Version, "error", err)
		}
		logger.Info("Removed an old theme package version", "slug", v.Slug, "version", v.Version)
	}
}

// RollbackThemePackage makes an installed version of a theme package, kept
// for rollbacks, its current version again. The version must be ready, not
// revoked by the stored catalog, signed unless developer mode is on, and
// must load in full with its recorded digest; a damaged archive marks it
// broken.
func (e *Engine) RollbackThemePackage(ctx context.Context, id int64) (*PackageResult, error) {
	if err := e.packagesAvailable(); err != nil {
		return nil, err
	}
	ctx, cancel := packageWriteContext(ctx)
	defer cancel()
	e.packageInstallMu.Lock()
	defer e.packageInstallMu.Unlock()

	if e.Market != nil {
		if err := e.Market.ApplyStoredRevocations(ctx); err != nil {
			return nil, &PackageError{Code: PackageErrInternal, Err: err}
		}
	}
	row, err := e.ExtStore.Get(ctx, id)
	if errors.Is(err, extension.ErrNotFound) || (err == nil && row.Kind != extension.KindTheme) {
		return nil, packageError(PackageErrNotFound, "no installed theme package version %d", id)
	}
	if err != nil {
		return nil, &PackageError{Code: PackageErrInternal, Err: err}
	}
	switch {
	case row.IsCurrent:
		return &PackageResult{Package: *row, Notice: PackageNoticeAlreadyCurrent}, nil
	case row.Status == extension.StatusRevoked:
		return nil, packageError(PackageErrRevoked, "the market revoked %s %s", row.Slug, row.Version)
	case row.Status == extension.StatusBroken:
		return nil, packageError(PackageErrBroken, "%s %s is broken; install it again", row.Slug, row.Version)
	case !row.Signed && !e.DeveloperMode().Enabled:
		return nil, packageError(PackageErrUnsigned, "%s %s is unsigned and developer mode is off", row.Slug, row.Version)
	}
	path, err := e.PackageArchives.Path(row.Slug, row.Version, row.Digest)
	if err != nil {
		return nil, &PackageError{Code: PackageErrInternal, Err: err}
	}
	t, err := e.LoadThemePackage(ThemePackageSource{Path: path, Source: coreTheme.PackageSourceInstalled, Digest: row.Digest})
	if err != nil {
		if errors.Is(err, ErrPackageArchive) {
			e.reconcilePackageStatus(ctx, *row, false)
			return nil, &PackageError{Code: PackageErrDamaged, Err: err}
		}
		return nil, &PackageError{Code: PackageErrIncompatible, Err: err}
	}
	if err := e.checkActiveUpgrade(row.Slug, t); err != nil {
		return nil, err
	}
	prev, err := e.ExtStore.Current(ctx, extension.KindTheme, row.Slug)
	if err != nil {
		return nil, &PackageError{Code: PackageErrInternal, Err: err}
	}
	if err := e.ExtStore.SetCurrent(ctx, row.ID); err != nil {
		return nil, &PackageError{Code: PackageErrInternal, Err: err}
	}
	row.IsCurrent = true
	return e.applyCurrentVersion(ctx, prev, *row, false)
}

// UninstallThemePackage removes every installed version of a theme package
// with its archives and returns them. The active theme cannot be
// uninstalled, nor the configured one while a boot fallback runs another:
// switch to another theme first. Its settings and any demo content it
// imported are kept.
func (e *Engine) UninstallThemePackage(ctx context.Context, slug string) ([]extension.Package, error) {
	if err := e.packagesAvailable(); err != nil {
		return nil, err
	}
	if !plugin.ValidSlug(slug) {
		return nil, packageError(PackageErrInvalid, "invalid slug %q", slug)
	}
	ctx, cancel := packageWriteContext(ctx)
	defer cancel()
	e.packageInstallMu.Lock()
	defer e.packageInstallMu.Unlock()

	removed, err := e.forgetThemePackage(ctx, slug)
	if err != nil {
		return nil, err
	}
	for _, v := range removed {
		if err := e.PackageArchives.Remove(v.Slug, v.Version, v.Digest); err != nil {
			logger.Error("Theme package archive could not be removed; it is reported at the next start", "slug", v.Slug, "version", v.Version, "error", err)
		}
	}
	if err := e.ReloadThemePackages(ctx); err != nil {
		logger.Error("Theme packages could not be reloaded after an uninstall", "slug", slug, "error", err)
	}
	logger.Info("Theme package uninstalled", "slug", slug, "versions", len(removed))
	return removed, nil
}

// forgetThemePackage deletes the records of slug unless it is the active or
// the configured theme. It holds lifecycleMu, as theme switches do, so a
// switch to slug cannot happen between the check and the delete.
func (e *Engine) forgetThemePackage(ctx context.Context, slug string) ([]extension.Package, error) {
	e.lifecycleMu.Lock()
	defer e.lifecycleMu.Unlock()
	versions, err := e.ExtStore.Versions(ctx, extension.KindTheme, slug)
	if err != nil {
		return nil, &PackageError{Code: PackageErrInternal, Err: err}
	}
	if len(versions) == 0 {
		return nil, packageError(PackageErrNotFound, "theme package %q is not installed", slug)
	}
	if e.ActiveThemeName() == slug || (e.Options != nil && e.Options.Get("active_theme") == slug) {
		return nil, packageError(PackageErrActive, "theme %q is in use; switch to another theme first", slug)
	}
	ids := make([]int64, len(versions))
	for i, v := range versions {
		ids[i] = v.ID
	}
	if err := e.ExtStore.Delete(ctx, ids...); err != nil {
		return nil, &PackageError{Code: PackageErrInternal, Err: err}
	}
	return versions, nil
}

// InstalledThemePackages lists the installed theme packages by slug.
func (e *Engine) InstalledThemePackages(ctx context.Context) ([]InstalledThemePackage, error) {
	if !e.installedPackagesEnabled() {
		return nil, nil
	}
	rows, err := e.ExtStore.Packages(ctx, extension.KindTheme)
	if err != nil {
		return nil, err
	}
	configured := ""
	if e.Options != nil {
		configured = e.Options.Get("active_theme")
	}
	active := e.ActiveThemeName()
	var out []InstalledThemePackage
	index := make(map[string]int)
	for _, row := range rows {
		i, ok := index[row.Slug]
		if !ok {
			pkg := InstalledThemePackage{
				Slug:   row.Slug,
				Active: row.Slug == active || row.Slug == configured,
				Issue:  e.installedLoadIssue(row.Slug),
			}
			if info, loaded := e.runningPackage(row.Slug); loaded {
				if info.Source == coreTheme.PackageSourcePreview {
					pkg.Preview = true
				} else {
					pkg.Running = info.Digest
				}
			}
			out = append(out, pkg)
			i = len(out) - 1
			index[row.Slug] = i
		}
		out[i].Versions = append(out[i].Versions, row)
	}
	for i := range out {
		sort.SliceStable(out[i].Versions, func(a, b int) bool {
			return out[i].Versions[a].IsCurrent && !out[i].Versions[b].IsCurrent
		})
	}
	return out, nil
}

// checkThemePackageSwitch refuses a switch to an installed theme package
// that is no longer installed, broken, or revoked (D1 4.7.3). It applies the
// stored catalog's revocations first, without fetching. Other themes pass.
// Called with lifecycleMu held.
func (e *Engine) checkThemePackageSwitch(name string) error {
	info, ok := e.runningPackage(name)
	if !ok || info.Source != coreTheme.PackageSourceInstalled || e.ExtStore == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), packageWriteTimeout)
	defer cancel()
	if e.Market != nil {
		if err := e.Market.ApplyStoredRevocations(ctx); err != nil {
			return fmt.Errorf("theme package %q: %w", name, err)
		}
	}
	row, err := e.ExtStore.Current(ctx, extension.KindTheme, name)
	if err != nil {
		return fmt.Errorf("theme package %q: %w", name, err)
	}
	switch {
	case row == nil:
		return fmt.Errorf("theme package %q is no longer installed", name)
	case row.Status == extension.StatusRevoked:
		return fmt.Errorf("theme package %q %s was revoked by the market and cannot be switched to", name, row.Version)
	case row.Status == extension.StatusBroken:
		return fmt.Errorf("theme package %q %s is broken; install it again", name, row.Version)
	}
	return nil
}
