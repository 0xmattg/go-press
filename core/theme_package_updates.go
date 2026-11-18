// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package core

import (
	"context"

	"github.com/0xmattg/go-press/core/extension"
	"github.com/0xmattg/go-press/core/updatecheck"
	"github.com/0xmattg/go-press/pkg/logger"
)

// Update checks for installed market packages (D1 4.7.2).
//
// The current version of every theme package installed from the market and
// signed by it is an update target of the market's update endpoint
// (<market>/updates/check). The targets ride on the core update check: they
// are checked only while the administrator's update check setting is on, on
// the same schedule, and without the installation details the official core
// target sends. Uploaded and unsigned packages are not checked, and nothing
// is registered while no market is configured.
//
// A result is shown, never acted on: upgrading still fetches the signed
// catalog, checks revocations and verifies the package (C11).

// PackageUpdate is a newer version the last update check found for an
// installed package.
type PackageUpdate struct {
	LatestVersion string
	// Severity is "normal", "security" or "unsupported".
	Severity   string
	ReleaseURL string
}

// syncPackageUpdateTargets makes the update targets match the installed
// market packages. It registers and unregisters only the targets it owns,
// so a target another component registered for a theme is left alone.
func (e *Engine) syncPackageUpdateTargets(ctx context.Context) {
	if e.Updates == nil || e.Updates.Registry() == nil {
		return
	}
	wanted := map[string]string{}
	endpoint := ""
	if e.Market != nil && e.Market.Enabled() && e.ExtStore != nil {
		endpoint = e.Market.Root().UpdatesURL()
		rows, err := e.ExtStore.Packages(ctx, extension.KindTheme)
		if err != nil {
			logger.Warn("Installed theme packages could not be listed for update checks", "error", err)
			return
		}
		for _, row := range rows {
			if row.IsCurrent && row.Source == extension.SourceMarket && row.Signed {
				wanted[row.Slug] = row.Version
			}
		}
	}

	registry := e.Updates.Registry()
	e.packageUpdateMu.Lock()
	defer e.packageUpdateMu.Unlock()
	if e.packageUpdateSlugs == nil {
		e.packageUpdateSlugs = map[string]bool{}
	}
	for slug := range e.packageUpdateSlugs {
		if _, ok := wanted[slug]; !ok {
			registry.Unregister(updatecheck.KindTheme, slug)
			delete(e.packageUpdateSlugs, slug)
		}
	}
	for slug, version := range wanted {
		err := registry.Register(updatecheck.Target{
			Kind: updatecheck.KindTheme, Slug: slug, CurrentVersion: version, Endpoint: endpoint,
		})
		if err != nil {
			logger.Warn("Theme package could not be registered for update checks", "slug", slug, "error", err)
			continue
		}
		e.packageUpdateSlugs[slug] = true
	}
}

// ThemePackageUpdate returns the newer version the last update check found
// for an installed market theme package, or nil: none was found, the
// package is not checked, or update checks are off.
func (e *Engine) ThemePackageUpdate(slug string) *PackageUpdate {
	if e.Updates == nil {
		return nil
	}
	e.packageUpdateMu.Lock()
	owned := e.packageUpdateSlugs[slug]
	e.packageUpdateMu.Unlock()
	if !owned {
		return nil
	}
	status := e.Updates.Snapshot(updatecheck.KindTheme, slug)
	if !status.HasUpdate {
		return nil
	}
	return &PackageUpdate{LatestVersion: status.LatestVersion, Severity: status.Severity, ReleaseURL: status.ReleaseURL}
}
