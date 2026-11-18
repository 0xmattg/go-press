// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/0xmattg/go-press/core/extension"
	"github.com/0xmattg/go-press/core/extension/filetheme"
	"github.com/0xmattg/go-press/core/extension/themepkg"
	coreTheme "github.com/0xmattg/go-press/core/theme"
	"github.com/0xmattg/go-press/pkg/logger"
	"github.com/0xmattg/go-press/version"
)

// Theme packages are themes without Go code (GIP-A0001b 4.3.2). A package
// is read into memory and checked with the rules of the running core before
// its theme is built, so the files a site serves are the files that passed
// the check, and a package that no longer passes (a newer core, a tightened
// rule) is not loaded. Loading never stops the site: a failure is logged
// and listed with ThemeLoadIssues, and boot falls back as for any missing
// theme.

// ThemePackageSource names a theme package to load.
type ThemePackageSource struct {
	// Path is a .zip archive or a theme source directory. A directory is
	// read the way "gopress pkg build" reads it: hidden paths and
	// signatures/ are left out.
	Path string
	// Source is coreTheme.PackageSourcePreview or
	// coreTheme.PackageSourceInstalled.
	Source string
	// Digest, when not empty, is the h1 content digest the package must
	// have, for example the one recorded when it was installed.
	Digest string
}

// ThemePackageOptions returns the check options for theme packages on this
// binary: the running core version, and the slugs of the themes and plugins
// built into it, which packages may not take.
func ThemePackageOptions() themepkg.Options {
	return themepkg.Options{CoreVersion: version.String(), Reserved: compiledExtension}
}

// ReadThemePackage reads the package at path, a .zip archive or a theme
// source directory, and checks it with ThemePackageOptions. The Package is
// nil when the check fails; the Report then lists the problems.
func ReadThemePackage(path string) (*themepkg.Package, *themepkg.Report, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, nil, err
	}
	if info.IsDir() {
		return themepkg.ReadDir(os.DirFS(path), ThemePackageOptions())
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	return themepkg.ReadZip(f, info.Size(), ThemePackageOptions())
}

// ErrPackageArchive reports that a theme package's files are missing,
// unreadable, or not the expected content (the digest differs). Such a
// package is broken. A package that reads fine but fails the check is only
// incompatible with this version of core, and LoadThemePackage returns
// another error for it.
var ErrPackageArchive = errors.New("theme package is missing, unreadable or changed")

// LoadThemePackage reads, checks and builds the theme of a package. It does
// not add the theme to the site: pass it to AddTheme or ReplaceTheme under
// its PackageInfo().Slug. Errors about the package's files themselves wrap
// ErrPackageArchive.
func (e *Engine) LoadThemePackage(src ThemePackageSource) (*filetheme.Theme, error) {
	pkg, report, err := ReadThemePackage(src.Path)
	if err != nil {
		return nil, fmt.Errorf("theme package %s: %w: %v", src.Path, ErrPackageArchive, err)
	}
	// A digest mismatch makes the package broken even when it also has
	// check problems; an unreadable entry leaves the digest empty.
	if src.Digest != "" && report.Digest != src.Digest {
		return nil, fmt.Errorf("theme package %s: %w: content digest %q, expected %s", src.Path, ErrPackageArchive, report.Digest, src.Digest)
	}
	if pkg == nil {
		return nil, themePackageCheckError(src.Path, report)
	}
	return filetheme.New(e, pkg, src.Source)
}

// themePackageCheckError summarizes a failed package check.
func themePackageCheckError(path string, report *themepkg.Report) error {
	const shown = 5
	total := len(report.Problems) + report.Omitted
	var lines []string
	for i, p := range report.Problems {
		if i == shown {
			break
		}
		lines = append(lines, p.String())
	}
	more := ""
	if total > shown {
		more = fmt.Sprintf("; and %d more (run gopress pkg check for the full list)", total-shown)
	}
	return fmt.Errorf("theme package %s fails the package check: %s%s", path, strings.Join(lines, "; "), more)
}

// themePackageSources lists the theme packages to load at boot: the
// preview packages given to BuildAndBootstrapWithOptions.
func (e *Engine) themePackageSources() []ThemePackageSource {
	sources := make([]ThemePackageSource, 0, len(e.previewThemePackages))
	for _, path := range e.previewThemePackages {
		sources = append(sources, ThemePackageSource{Path: path, Source: coreTheme.PackageSourcePreview})
	}
	return sources
}

// loadPreviewThemePackages builds the theme of every preview package. A
// package that fails is logged and recorded in issues, under its slug when
// known and its path otherwise; when two packages have the same slug the
// first wins.
func (e *Engine) loadPreviewThemePackages(issues map[string]string) map[string]coreTheme.Theme {
	themes := make(map[string]coreTheme.Theme)
	loadedFrom := make(map[string]string)
	for _, src := range e.themePackageSources() {
		t, err := e.LoadThemePackage(src)
		if err != nil {
			issues[src.Path] = err.Error()
			logger.Error("Theme package failed to load; it is not available", "path", src.Path, "error", err)
			continue
		}
		info := t.PackageInfo()
		if first, dup := loadedFrom[info.Slug]; dup {
			reason := fmt.Sprintf("theme package %s was not loaded: %s already provides theme %q", src.Path, first, info.Slug)
			issues[src.Path] = reason
			logger.Error("Theme package failed to load; it is not available", "path", src.Path, "error", reason)
			continue
		}
		themes[info.Slug] = t
		loadedFrom[info.Slug] = src.Path
		logger.Info("Theme package loaded", "theme", t.Name(), "slug", info.Slug, "version", info.Version,
			"source", info.Source, "digest", info.Digest, "path", src.Path)
	}
	return themes
}

// staleArchiveTempAge is how old a temporary file in the archive store must
// be before boot removes it; a younger one may be a write in progress.
const staleArchiveTempAge = time.Hour

// installedPackagesEnabled reports whether the site can have installed
// packages: it needs the package store and an archive directory.
func (e *Engine) installedPackagesEnabled() bool {
	return e.ExtStore != nil && e.PackageArchives != nil
}

// loadInstalledThemePackages reconciles ext_packages with the stored
// archives at boot and returns the themes of the current versions that load.
// It never stops the boot: problems are logged and recorded in issues.
//
//   - A current version is loaded like any package, with its recorded
//     digest. A missing, unreadable or changed archive moves it from ready
//     to broken; one that loads again moves it from broken back to ready, so
//     restoring packages/ from a backup and restarting is enough. A package
//     that only fails the check (it needs another core, a rule was
//     tightened) keeps its status and is reported.
//   - A revoked current version still loads (D1 4.7.3): switching to it and
//     upgrading to it are refused elsewhere.
//   - Versions kept for rollback are only checked for presence and size
//     here; a rollback loads them in full.
//   - Statuses only move between ready and broken, with conditional
//     updates, so a revocation recorded meanwhile is never undone.
//   - Archives no row refers to are reported but left in place: an install
//     writes the archive before its row. Temporary files older than an hour
//     are removed.
//
// Slugs in skip (preview packages) are not loaded.
func (e *Engine) loadInstalledThemePackages(ctx context.Context, issues map[string]string, skip map[string]coreTheme.Theme) map[string]coreTheme.Theme {
	themes := make(map[string]coreTheme.Theme)
	if !e.installedPackagesEnabled() {
		return themes
	}
	rows, err := e.ExtStore.Packages(ctx, extension.KindTheme)
	if err != nil {
		logger.Error("Installed theme packages could not be listed; none are loaded", "error", err)
		return themes
	}
	registered := make(map[string]bool, len(rows))
	for _, row := range rows {
		path, err := e.PackageArchives.Path(row.Slug, row.Version, row.Digest)
		if err != nil {
			logger.Error("Installed theme package has an invalid identity", "id", row.ID, "slug", row.Slug, "version", row.Version, "error", err)
			continue
		}
		registered[path] = true
		if !row.IsCurrent {
			info, statErr := os.Stat(path)
			e.reconcilePackageStatus(ctx, row, statErr == nil && info.Mode().IsRegular() && info.Size() == row.Size)
			continue
		}
		if _, previewed := skip[row.Slug]; previewed {
			logger.Info("Installed theme package not loaded: a preview package of the same slug replaces it", "slug", row.Slug, "version", row.Version)
			continue
		}
		t, err := e.LoadThemePackage(ThemePackageSource{Path: path, Source: coreTheme.PackageSourceInstalled, Digest: row.Digest})
		if err != nil {
			if errors.Is(err, ErrPackageArchive) {
				e.reconcilePackageStatus(ctx, row, false)
			}
			issues[row.Slug] = err.Error()
			logger.Error("Installed theme package failed to load; it is not available", "slug", row.Slug, "version", row.Version, "error", err)
			continue
		}
		e.reconcilePackageStatus(ctx, row, true)
		if row.Status == extension.StatusRevoked {
			logger.Error("Installed theme package was revoked by the market; it stays available but cannot be switched to", "slug", row.Slug, "version", row.Version)
		}
		themes[row.Slug] = t
		logger.Info("Installed theme package loaded", "theme", t.Name(), "slug", row.Slug, "version", row.Version, "digest", row.Digest)
	}
	if files, err := e.PackageArchives.Files(); err != nil {
		logger.Error("Package archives could not be listed", "dir", e.PackageArchives.Dir(), "error", err)
	} else {
		for _, path := range files {
			if !registered[path] {
				logger.Warn("Package archive is not recorded as installed; it is left in place", "path", path)
			}
		}
	}
	if removed, err := e.PackageArchives.RemoveStaleTemp(staleArchiveTempAge); err != nil {
		logger.Error("Stale package archive files could not be removed", "error", err)
	} else if removed > 0 {
		logger.Info("Removed interrupted package archive writes", "files", removed)
	}
	e.syncPackageUpdateTargets(ctx)
	return themes
}

// reconcilePackageStatus moves an installed version between ready and
// broken according to whether its archive is sound. Revoked versions are
// left alone: revocation wins.
func (e *Engine) reconcilePackageStatus(ctx context.Context, row extension.Package, sound bool) {
	from, to := extension.StatusReady, extension.StatusBroken
	if sound {
		from, to = extension.StatusBroken, extension.StatusReady
	}
	if row.Status != from {
		return
	}
	changed, err := e.ExtStore.UpdateStatus(ctx, row.ID, from, to)
	if err != nil {
		logger.Error("Installed theme package status could not be updated", "slug", row.Slug, "version", row.Version, "to", to, "error", err)
		return
	}
	if !changed {
		return
	}
	if sound {
		logger.Info("Installed theme package archive is sound again", "slug", row.Slug, "version", row.Version)
	} else {
		logger.Error("Installed theme package archive is missing or damaged; marked broken", "slug", row.Slug, "version", row.Version)
	}
}

// ReloadThemePackages brings this process's installed theme packages in line
// with ext_packages: it loads current versions that are new, replaces the
// running instance of a package whose current version changed (an upgrade or
// rollback; an active theme is activated in its new version first, and keeps
// the old one if that fails), and removes packages that are no longer
// installed or are broken. The active theme cannot be removed: it keeps
// running, from memory, until the next start. Preview packages are left
// alone. Statuses are not changed here; boot reconciles them.
//
// The install flow calls it after it changes ext_packages. Only one reload
// runs at a time.
func (e *Engine) ReloadThemePackages(ctx context.Context) error {
	e.packageReloadMu.Lock()
	defer e.packageReloadMu.Unlock()
	if !e.installedPackagesEnabled() {
		return nil
	}
	rows, err := e.ExtStore.Packages(ctx, extension.KindTheme)
	if err != nil {
		return err
	}
	current := make(map[string]extension.Package)
	for _, row := range rows {
		if row.IsCurrent && row.Status != extension.StatusBroken {
			current[row.Slug] = row
		}
	}
	issues := make(map[string]string)
	loaded := e.themeSnapshot()
	for slug, row := range current {
		var running *coreTheme.PackageInfo
		if t, ok := loaded[slug]; ok {
			info, isPackage := coreTheme.PackageInfoOf(t)
			if !isPackage || info.Source == coreTheme.PackageSourcePreview {
				continue
			}
			if info.Digest == row.Digest {
				continue
			}
			running = &info
		}
		path, err := e.PackageArchives.Path(row.Slug, row.Version, row.Digest)
		if err != nil {
			issues[slug] = err.Error()
			continue
		}
		t, err := e.LoadThemePackage(ThemePackageSource{Path: path, Source: coreTheme.PackageSourceInstalled, Digest: row.Digest})
		if err == nil {
			if running == nil {
				err = e.AddTheme(slug, t)
			} else {
				err = e.ReplaceTheme(slug, t)
			}
		}
		if err != nil {
			issues[slug] = err.Error()
			logger.Error("Installed theme package could not be reloaded", "slug", slug, "version", row.Version, "error", err)
			continue
		}
		logger.Info("Installed theme package reloaded", "slug", slug, "version", row.Version, "digest", row.Digest)
	}
	for slug, t := range e.themeSnapshot() {
		info, isPackage := coreTheme.PackageInfoOf(t)
		if !isPackage || info.Source != coreTheme.PackageSourceInstalled {
			continue
		}
		if _, ok := current[slug]; ok {
			continue
		}
		if err := e.RemoveTheme(slug); err != nil {
			reason := fmt.Sprintf("theme package %q is no longer installed or is broken, but it is the active theme; it keeps running until the next start: %v", slug, err)
			issues[slug] = reason
			logger.Error("Installed theme package could not be removed", "slug", slug, "error", err)
			continue
		}
		logger.Info("Installed theme package removed", "slug", slug)
	}
	e.mu.Lock()
	e.installedLoadIssues = issues
	e.mu.Unlock()
	e.syncPackageUpdateTargets(ctx)
	return nil
}
