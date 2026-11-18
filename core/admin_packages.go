// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/0xmattg/go-press/core/admin"
	"github.com/0xmattg/go-press/core/extension"
)

// adminPackageManager connects the admin's package endpoints to the
// engine. It turns results into the admin's views and errors into
// *admin.PackageError, with server paths taken out of messages.
func (e *Engine) adminPackageManager() *admin.PackageManager {
	return &admin.PackageManager{
		ListFn: func(ctx context.Context) (*admin.PackageList, error) {
			installed, err := e.InstalledThemePackages(ctx)
			if err != nil {
				return nil, e.adminPackageError(err)
			}
			list := &admin.PackageList{Market: e.adminMarketInfo(), DeveloperMode: e.adminDeveloperMode(), Packages: []admin.PackageView{}}
			for _, p := range installed {
				view := admin.PackageView{Slug: p.Slug, Active: p.Active, Running: p.Running, Preview: p.Preview,
					Issue: e.redactSitePaths(p.Issue), Versions: make([]admin.PackageVersionView, len(p.Versions))}
				if update := e.ThemePackageUpdate(p.Slug); update != nil {
					view.Update = &admin.PackageUpdateView{LatestVersion: update.LatestVersion, Severity: update.Severity, ReleaseURL: update.ReleaseURL}
				}
				for i, v := range p.Versions {
					view.Versions[i] = adminPackageVersion(v)
				}
				list.Packages = append(list.Packages, view)
			}
			return list, nil
		},
		CatalogFn: func(ctx context.Context, lang string) (*admin.CatalogView, error) {
			cat, err := e.ThemeCatalog(ctx, lang)
			if err != nil {
				return nil, e.adminPackageError(err)
			}
			return adminCatalogView(e.adminMarketInfo(), cat), nil
		},
		MarketFn: e.adminMarketInfo,
		RevokedActiveFn: func() *admin.RevokedThemeBanner {
			slug, version, revoked := e.RevokedActiveTheme()
			if !revoked {
				return nil
			}
			return &admin.RevokedThemeBanner{Slug: slug, Version: version}
		},
		InstallFn: func(ctx context.Context, slug, version string, actor uint) (*admin.PackageOutcome, error) {
			result, err := e.InstallThemePackage(ctx, slug, version, actor)
			return adminPackageOutcome(result), e.adminPackageError(err)
		},
		UploadFn: func(ctx context.Context, data []byte, confirmDigest string, actor uint) (*admin.PackageOutcome, error) {
			result, err := e.UploadThemePackage(ctx, data, confirmDigest, actor)
			return adminPackageOutcome(result), e.adminPackageError(err)
		},
		RollbackFn: func(ctx context.Context, id int64) (*admin.PackageOutcome, error) {
			result, err := e.RollbackThemePackage(ctx, id)
			return adminPackageOutcome(result), e.adminPackageError(err)
		},
		UninstallFn: func(ctx context.Context, slug string) (*admin.PackageOutcome, error) {
			removed, err := e.UninstallThemePackage(ctx, slug)
			if err != nil {
				return nil, e.adminPackageError(err)
			}
			out := &admin.PackageOutcome{Slug: slug, Removed: make([]admin.PackageVersionView, len(removed))}
			for i, v := range removed {
				out.Removed[i] = adminPackageVersion(v)
			}
			return out, nil
		},
		DeveloperModeFn: e.adminDeveloperMode,
		SetDeveloperModeFn: func(on bool) error {
			err := e.SetDeveloperMode(on)
			if errors.Is(err, ErrDeveloperModeLocked) {
				return &admin.PackageError{Code: "developer_mode_locked", Detail: err.Error()}
			}
			return err
		},
	}
}

func (e *Engine) adminMarketInfo() admin.MarketInfo {
	if e.Market == nil {
		return admin.MarketInfo{Mode: extension.MarketDisabled}
	}
	root := e.Market.Root()
	return admin.MarketInfo{Enabled: root.Enabled(), Mode: root.Mode(), Host: root.Host()}
}

func adminCatalogView(market admin.MarketInfo, cat *ThemeCatalog) *admin.CatalogView {
	view := &admin.CatalogView{Market: market, Sequence: cat.Sequence, GeneratedAt: cat.GeneratedAt,
		Items: make([]admin.CatalogItemView, 0, len(cat.Entries)), RevokedInstalled: []admin.RevokedPackageView{}}
	for _, entry := range cat.Entries {
		latest := entry.Latest
		item := admin.CatalogItemView{
			Slug: entry.Slug, Title: entry.Title, Summary: entry.Summary, Author: entry.Author,
			ScreenshotURL: entry.ScreenshotURL, ListingURL: entry.ListingURL, Update: entry.Update,
			Latest: admin.CatalogVersionView{
				Version: latest.Version, Size: latest.Size, Core: latest.Core, Compatible: latest.Compatible,
				External: append([]string{}, latest.External...), Requires: []admin.CatalogRequirementView{},
				PublisherKID: latest.Publisher.KID,
			},
		}
		for _, req := range latest.Requires {
			item.Latest.Requires = append(item.Latest.Requires, admin.CatalogRequirementView{Slug: req.Slug, Version: req.Version, Optional: req.Optional})
		}
		if entry.Installed != nil {
			item.Installed = &admin.InstalledVersionView{Version: entry.Installed.Version, Status: entry.Installed.Status,
				Signed: entry.Installed.Signed, Source: entry.Installed.Source}
		}
		view.Items = append(view.Items, item)
	}
	for _, r := range cat.RevokedInstalled {
		view.RevokedInstalled = append(view.RevokedInstalled, admin.RevokedPackageView{Slug: r.Slug, Version: r.Version,
			Severity: r.Revocation.Severity, Reason: r.Revocation.Reason, Note: r.Revocation.Note})
	}
	return view
}

func (e *Engine) adminDeveloperMode() admin.DeveloperModeInfo {
	state := e.DeveloperMode()
	return admin.DeveloperModeInfo{Enabled: state.Enabled, Locked: state.Locked}
}

func adminPackageVersion(v extension.Package) admin.PackageVersionView {
	return admin.PackageVersionView{
		ID: v.ID, Version: v.Version, Digest: v.Digest, Size: v.Size, Source: v.Source,
		Signed: v.Signed, PublisherKID: v.PublisherKID, Status: v.Status, Current: v.IsCurrent,
		InstalledAt: v.InstalledAt, InstalledBy: v.InstalledBy,
	}
}

func adminPackageSummary(s *PackageSummary) *admin.PackageSummaryView {
	if s == nil {
		return nil
	}
	external := s.External
	if external == nil {
		external = []string{}
	}
	return &admin.PackageSummaryView{Name: s.Name, Slug: s.Slug, Version: s.Version, Digest: s.Digest,
		Size: s.Size, Files: s.Files, External: external, Signature: s.Signature}
}

func adminPackageOutcome(r *PackageResult) *admin.PackageOutcome {
	if r == nil {
		return nil
	}
	v := adminPackageVersion(r.Package)
	return &admin.PackageOutcome{Slug: r.Package.Slug, Package: &v, Notice: r.Notice, Summary: adminPackageSummary(r.Summary)}
}

// adminPackageError converts an error of a package operation for the
// admin. Errors without a code become "internal".
func (e *Engine) adminPackageError(err error) error {
	if err == nil {
		return nil
	}
	var pe *PackageError
	if !errors.As(err, &pe) {
		return &admin.PackageError{Code: PackageErrInternal, Detail: e.redactSitePaths(err.Error())}
	}
	out := &admin.PackageError{Code: pe.Code, Package: adminPackageSummary(pe.Package)}
	if pe.Code == PackageErrCatalog || pe.Code == PackageErrMarket {
		out.Reason = CatalogFailureReason(pe.Err)
	}
	if pe.Err != nil {
		out.Detail = e.redactSitePaths(pe.Err.Error())
	}
	for _, p := range pe.Problems {
		out.Problems = append(out.Problems, admin.PackageProblemView{Rule: p.Rule, Path: p.Path, Line: p.Line, Message: p.Message})
	}
	return out
}

// redactSitePaths shortens paths inside the site directory to paths
// relative to it, so messages shown in the admin do not reveal where the
// site lives on the server.
func (e *Engine) redactSitePaths(s string) string {
	if e.SiteDir == "" || s == "" {
		return s
	}
	dir := filepath.Clean(e.SiteDir) + string(filepath.Separator)
	return strings.ReplaceAll(s, dir, "")
}
