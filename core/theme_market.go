// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/0xmattg/go-press/core/extension"
	coreTheme "github.com/0xmattg/go-press/core/theme"
	"github.com/0xmattg/go-press/pkg/logger"
	"github.com/0xmattg/go-press/pkg/semver"
)

// The admin's theme market page (D1 4.7.1) lists what the market offers.
// The catalog is fetched every time the page asks: there is no cached list,
// so a package the market withdrew never shows, and a failed fetch is an
// error rather than an older list. Fetching also applies the catalog's
// revocations to installed packages.

// ThemeCatalog is the market catalog as the market page shows it.
type ThemeCatalog struct {
	Sequence    int64
	GeneratedAt time.Time
	// Entries has one entry per theme, sorted by title.
	Entries []ThemeCatalogEntry
	// RevokedInstalled lists installed current versions the catalog revokes.
	RevokedInstalled []RevokedThemePackage
}

// ThemeCatalogEntry is one theme the market offers.
type ThemeCatalogEntry struct {
	Slug          string
	Title         string
	Summary       string
	Author        string
	ScreenshotURL string
	ListingURL    string
	// Latest is the newest free version that suits this GoPress, or the
	// newest free version when none does (Latest.Compatible tells which).
	Latest extension.Item
	// Installed is the installed current version, if any.
	Installed *extension.Package
	// Update reports that Latest is newer than Installed and suits this
	// GoPress.
	Update bool
}

// RevokedThemePackage is an installed version a catalog revokes.
type RevokedThemePackage struct {
	Slug       string
	Version    string
	Revocation extension.Revocation
}

// ThemeCatalog fetches the market catalog now and arranges it for the market
// page in the given admin language. Paid items are left out: they cannot be
// installed yet. Errors are *PackageError with PackageErrMarket or
// PackageErrCatalog; CatalogFailureReason tells catalog failures apart.
func (e *Engine) ThemeCatalog(ctx context.Context, lang string) (*ThemeCatalog, error) {
	if e.Market == nil || !e.Market.Enabled() {
		return nil, packageError(PackageErrMarket, "no extension market is configured")
	}
	cat, err := e.Market.Refresh(ctx)
	if err != nil {
		return nil, &PackageError{Code: PackageErrCatalog, Err: err}
	}
	current := map[string]extension.Package{}
	if e.ExtStore != nil {
		rows, err := e.ExtStore.Packages(ctx, extension.KindTheme)
		if err != nil {
			return nil, &PackageError{Code: PackageErrInternal, Err: err}
		}
		for _, row := range rows {
			if row.IsCurrent {
				current[row.Slug] = row
			}
		}
	}

	bySlug := map[string][]extension.Item{}
	for _, item := range cat.Items {
		if item.Kind != extension.KindTheme || item.Pricing != extension.PricingFree {
			continue
		}
		if cat.Revocation(item.Kind, item.Slug, item.Version) != nil {
			continue
		}
		bySlug[item.Slug] = append(bySlug[item.Slug], item)
	}
	out := &ThemeCatalog{Sequence: cat.Sequence, GeneratedAt: cat.GeneratedAt}
	for slug, items := range bySlug {
		latest := newestItem(items, true)
		if latest == nil {
			latest = newestItem(items, false)
		}
		entry := ThemeCatalogEntry{
			Slug: slug, Author: latest.Author, ScreenshotURL: latest.ScreenshotURL, ListingURL: latest.ListingURL,
			Latest: *latest,
		}
		entry.Title, entry.Summary = localizedItemText(*latest, lang)
		if row, ok := current[slug]; ok {
			installed := row
			entry.Installed = &installed
			// Only a version this GoPress can run is an update.
			if cmp, err := semver.Compare(latest.Version, row.Version); err == nil && cmp > 0 && latest.Compatible {
				entry.Update = true
			}
		}
		out.Entries = append(out.Entries, entry)
	}
	sort.Slice(out.Entries, func(i, j int) bool {
		a, b := strings.ToLower(out.Entries[i].Title), strings.ToLower(out.Entries[j].Title)
		if a != b {
			return a < b
		}
		return out.Entries[i].Slug < out.Entries[j].Slug
	})
	slugs := make([]string, 0, len(current))
	for slug := range current {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	for _, slug := range slugs {
		row := current[slug]
		if rev := cat.Revocation(extension.KindTheme, slug, row.Version); rev != nil {
			out.RevokedInstalled = append(out.RevokedInstalled, RevokedThemePackage{Slug: slug, Version: row.Version, Revocation: *rev})
		}
	}
	return out, nil
}

// newestItem returns the highest version among items, only compatible ones
// when compatibleOnly is set.
func newestItem(items []extension.Item, compatibleOnly bool) *extension.Item {
	var best *extension.Item
	for i := range items {
		item := &items[i]
		if compatibleOnly && !item.Compatible {
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

// localizedItemText returns an item's title and summary in lang, falling
// back to its base language (zh for zh-CN) and then to the default text.
func localizedItemText(item extension.Item, lang string) (string, string) {
	title, summary := item.Title, item.Summary
	for _, key := range []string{lang, strings.SplitN(lang, "-", 2)[0]} {
		if text, ok := item.Localized[key]; ok {
			if text.Title != "" {
				title = text.Title
			}
			if text.Summary != "" {
				summary = text.Summary
			}
			break
		}
	}
	return title, summary
}

// CatalogFailureReason names why a catalog could not be used, for the
// market page: market_disabled, fetch, too_large, format, schema,
// signature, expired, rollback, equivocation, or "" for other errors.
func CatalogFailureReason(err error) string {
	switch {
	case errors.Is(err, extension.ErrMarketDisabled):
		return "market_disabled"
	case errors.Is(err, extension.ErrCatalogFetch):
		return "fetch"
	case errors.Is(err, extension.ErrCatalogTooLarge):
		return "too_large"
	case errors.Is(err, extension.ErrCatalogSchema):
		return "schema"
	case errors.Is(err, extension.ErrCatalogSignature):
		return "signature"
	case errors.Is(err, extension.ErrCatalogExpired):
		return "expired"
	case errors.Is(err, extension.ErrCatalogRollback):
		return "rollback"
	case errors.Is(err, extension.ErrCatalogEquivocation):
		return "equivocation"
	case errors.Is(err, extension.ErrCatalogFormat):
		return "format"
	}
	return ""
}

// RevokedActiveTheme reports the active theme when it is an installed theme
// package whose current version the market revoked. It stays active (D1
// 4.7.3), and the admin warns about it. Sites whose active theme is not a
// package pay nothing: the database is read only for package themes.
func (e *Engine) RevokedActiveTheme() (slug, version string, revoked bool) {
	name := e.ActiveThemeName()
	info, ok := e.runningPackage(name)
	if !ok || info.Source != coreTheme.PackageSourceInstalled || e.ExtStore == nil {
		return "", "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	row, err := e.ExtStore.Current(ctx, extension.KindTheme, name)
	if err != nil {
		logger.Warn("Active theme package status could not be read", "slug", name, "error", err)
		return "", "", false
	}
	if row == nil || row.Status != extension.StatusRevoked {
		return "", "", false
	}
	return name, row.Version, true
}

// themePackageDisplay describes a theme for the themes page: whether it is
// a package, where it came from, and its signature and revocation state.
type themePackageDisplay struct {
	Package  bool
	Source   string // extension.SourceMarket, SourceUpload, or coreTheme.PackageSourcePreview
	Unsigned bool
	Revoked  bool
	External []string
}

// themePackageDisplays returns the package details of the themes that are
// packages, keyed by slug, with one read of ext_packages.
func (e *Engine) themePackageDisplays(themes map[string]coreTheme.Theme) map[string]themePackageDisplay {
	out := map[string]themePackageDisplay{}
	var rows map[string]extension.Package
	for slug, t := range themes {
		info, ok := coreTheme.PackageInfoOf(t)
		if !ok {
			continue
		}
		d := themePackageDisplay{Package: true, Source: coreTheme.PackageSourcePreview}
		if fp, ok := t.(coreTheme.FileConfigProvider); ok && fp.FileConfig() != nil {
			d.External = externalHosts(fp.FileConfig().External)
		}
		if info.Source == coreTheme.PackageSourceInstalled {
			if rows == nil {
				rows = e.currentPackageRows()
			}
			if row, ok := rows[slug]; ok {
				d.Source = row.Source
				d.Unsigned = !row.Signed
				d.Revoked = row.Status == extension.StatusRevoked
			}
		}
		out[slug] = d
	}
	return out
}

// currentPackageRows returns the current version of each installed theme
// package by slug.
func (e *Engine) currentPackageRows() map[string]extension.Package {
	out := map[string]extension.Package{}
	if e.ExtStore == nil {
		return out
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := e.ExtStore.Packages(ctx, extension.KindTheme)
	if err != nil {
		logger.Warn("Installed theme packages could not be listed", "error", err)
		return out
	}
	for _, row := range rows {
		if row.IsCurrent {
			out[row.Slug] = row
		}
	}
	return out
}
