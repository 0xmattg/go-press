// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package core

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/0xmattg/go-press/config"
	"github.com/0xmattg/go-press/core/admin"
	"github.com/0xmattg/go-press/core/extension"
	"github.com/0xmattg/go-press/core/extension/themepkg"
	"github.com/0xmattg/go-press/core/option"
	coreTheme "github.com/0xmattg/go-press/core/theme"
	"github.com/0xmattg/go-press/core/updatecheck"
	"github.com/0xmattg/go-press/pkg/sign"
)

// pgMarket is a local market for the install tests. It signs its catalog
// and packages with test keys and serves both over loopback HTTP.
type pgMarket struct {
	t                                 *testing.T
	catalogKey, packageKey, publisher sign.PrivateKey
	server                            *httptest.Server

	mu       sync.Mutex
	sequence int64
	items    []extension.Item
	revoked  []extension.Revocation
	files    map[string][]byte
	envelope []byte
	// updates answers <endpoint>/updates/check; checks records the requests.
	updates []updatecheck.Update
	checks  []updatecheck.Request
}

func newPGMarket(t *testing.T) *pgMarket {
	t.Helper()
	// A built-in theme, so a site without packages can start.
	registerTestThemes(t, map[string]ThemeFactoryE{"aa-good": builds(okTheme("aa-good"))})
	gen := func() sign.PrivateKey {
		key, err := sign.GenerateKey(nil)
		if err != nil {
			t.Fatal(err)
		}
		return key
	}
	m := &pgMarket{t: t, catalogKey: gen(), packageKey: gen(), publisher: gen(), files: map[string][]byte{}}
	m.server = httptest.NewServer(http.HandlerFunc(m.serve))
	t.Cleanup(m.server.Close)
	m.mu.Lock()
	m.publishLocked()
	m.mu.Unlock()
	return m
}

func (m *pgMarket) serve(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r.URL.Path == "/api/market/v1/catalog" {
		_, _ = w.Write(m.envelope)
		return
	}
	if r.URL.Path == "/api/market/v1/updates/check" && r.Method == http.MethodPost {
		var req updatecheck.Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		m.checks = append(m.checks, req)
		_ = json.NewEncoder(w).Encode(updatecheck.Response{ProtocolVersion: updatecheck.ProtocolVersion, TTLSeconds: 86400, Updates: m.updates})
		return
	}
	if data, ok := m.files[r.URL.Path]; ok {
		_, _ = w.Write(data)
		return
	}
	http.NotFound(w, r)
}

// attach makes e use this market. Boot setups call it.
func (m *pgMarket) attach(e *Engine) {
	root, err := extension.NewTrustRoot(config.MarketConfig{
		Endpoint:    m.server.URL + "/api/market/v1",
		CatalogKeys: []string{m.catalogKey.Public().String()},
		PackageKeys: []string{m.packageKey.Public().String()},
	})
	if err != nil {
		m.t.Fatal(err)
	}
	e.Market = extension.NewMarket(e.ExtStore, root, nil, "GoPress/test")
}

// offer lists the package built from dir. change, when not nil, edits the
// item before the market signs it.
func (m *pgMarket) offer(dir string, change func(*extension.Item)) []byte {
	m.t.Helper()
	data, report := buildPackageZip(m.t, dir)
	pub := m.publisher.Public()
	path := "/files/" + report.ID.Slug + "-" + report.ID.Version + ".zip"
	item := extension.Item{
		Kind: extension.KindTheme, Slug: report.ID.Slug, Version: report.ID.Version,
		Digest: report.Digest, Size: int64(len(data)), DownloadURL: m.server.URL + path,
		Pricing: extension.PricingFree, Title: "Minimal Sample", External: externalHosts(report.External),
		Publisher: extension.ItemPublisher{KID: pub.KID(), PublicKey: pub.String()},
	}
	if change != nil {
		change(&item)
	}
	id := sign.PackageID{Kind: item.Kind, Slug: item.Slug, Version: item.Version}
	var err error
	if item.Signatures.Publisher, err = m.publisher.Sign(sign.PublisherStatement(id, item.Digest)); err != nil {
		m.t.Fatal(err)
	}
	if item.Signatures.Market, err = m.packageKey.Sign(sign.MarketStatement(id, pub.KID(), item.Digest)); err != nil {
		m.t.Fatal(err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items = append(m.items, item)
	m.files[path] = data
	m.publishLocked()
	return data
}

func (m *pgMarket) revoke(slug, version string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.revoked = append(m.revoked, extension.Revocation{Kind: extension.KindTheme, Slug: slug, Versions: []string{version},
		Severity: extension.SeverityHigh, Reason: extension.ReasonVulnerability})
	m.publishLocked()
}

func (m *pgMarket) publishLocked() {
	m.sequence++
	items, revoked := m.items, m.revoked
	if items == nil {
		items = []extension.Item{}
	}
	if revoked == nil {
		revoked = []extension.Revocation{}
	}
	now := time.Now().UTC()
	payload, err := json.Marshal(map[string]interface{}{
		"schema": extension.CatalogSchema, "sequence": m.sequence,
		"generated_at": now.Add(-time.Hour).Format(time.RFC3339), "expires_at": now.Add(7 * 24 * time.Hour).Format(time.RFC3339),
		"items": items, "revoked": revoked,
	})
	if err != nil {
		m.t.Fatal(err)
	}
	if m.envelope, err = extension.SignCatalog(payload, m.catalogKey); err != nil {
		m.t.Fatal(err)
	}
}

// signedUpload returns the package built from dir with the market's
// signature files inside, as "gopress pkg sign" would leave it.
func (m *pgMarket) signedUpload(dir string) []byte {
	m.t.Helper()
	data, report := buildPackageZip(m.t, dir)
	id := sign.PackageID{Kind: extension.KindTheme, Slug: report.ID.Slug, Version: report.ID.Version}
	pub, err := m.publisher.Sign(sign.PublisherStatement(id, report.Digest))
	if err != nil {
		m.t.Fatal(err)
	}
	pub.PublicKey = m.publisher.Public().String()
	market, err := m.packageKey.Sign(sign.MarketStatement(id, m.publisher.Public().KID(), report.Digest))
	if err != nil {
		m.t.Fatal(err)
	}
	return addZipFiles(m.t, data, map[string]sign.Signature{sign.PublisherSignatureFile: pub, sign.MarketSignatureFile: market})
}

func buildPackageZip(t *testing.T, dir string) ([]byte, *themepkg.Report) {
	t.Helper()
	var buf bytes.Buffer
	report, err := themepkg.Build(&buf, os.DirFS(dir), themepkg.Options{})
	if err != nil || !report.OK() {
		t.Fatalf("build: %v %v", err, report.Problems)
	}
	return buf.Bytes(), report
}

// addZipFiles returns the archive with signature files added.
func addZipFiles(t *testing.T, data []byte, sigs map[string]sign.Signature) []byte {
	t.Helper()
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var files []themepkg.File
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, themepkg.File{Name: f.Name, Data: content})
	}
	for name, sig := range sigs {
		content, err := sign.MarshalSignature(sig)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, themepkg.File{Name: name, Data: content})
	}
	var out bytes.Buffer
	if err := themepkg.WriteZip(&out, files); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// marketSite boots a site attached to market, with setup applied first.
func marketSite(t *testing.T, site *packageSite, market *pgMarket, setup func(e *Engine)) *Engine {
	t.Helper()
	return site.boot(t, func(e *Engine) {
		market.attach(e)
		if setup != nil {
			setup(e)
		}
	})
}

func wantPackageError(t *testing.T, err error, code string) *PackageError {
	t.Helper()
	var pe *PackageError
	if !errors.As(err, &pe) || pe.Code != code {
		t.Fatalf("error = %v, want code %s", err, code)
	}
	return pe
}

func themeVersions(t *testing.T, e *Engine) []extension.Package {
	t.Helper()
	rows, err := e.ExtStore.Versions(context.Background(), extension.KindTheme, samplePackageSlug)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func currentVersion(t *testing.T, e *Engine) string {
	t.Helper()
	row, err := e.ExtStore.Current(context.Background(), extension.KindTheme, samplePackageSlug)
	if err != nil {
		t.Fatal(err)
	}
	if row == nil {
		return ""
	}
	return row.Version
}

func archiveCount(t *testing.T, e *Engine) int {
	t.Helper()
	files, err := e.PackageArchives.Files()
	if err != nil {
		t.Fatal(err)
	}
	return len(files)
}

func TestPGInstallUpgradeAndRollbackFromTheMarket(t *testing.T) {
	ctx := context.Background()
	market := newPGMarket(t)
	v100 := market.offer(packageVersion(t, "1.0.0"), nil)
	site := newPackageSite(t)
	e := marketSite(t, site, market, nil)

	result, err := e.InstallThemePackage(ctx, samplePackageSlug, "1.0.0", 1)
	if err != nil {
		t.Fatal(err)
	}
	got := result.Package
	if !got.IsCurrent || !got.Signed || got.Source != extension.SourceMarket || got.PublisherKID != market.publisher.Public().KID() ||
		got.Size != int64(len(v100)) || got.InstalledBy == nil || *got.InstalledBy != 1 || result.Summary == nil {
		t.Fatalf("installed = %+v", result)
	}
	if e.ActiveThemeName() == samplePackageSlug {
		t.Fatal("installing must not activate the theme")
	}
	if err := e.SwitchTheme(samplePackageSlug); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(homeBody(t, e), "home 1.0.0") {
		t.Fatal("the installed theme does not serve the site")
	}

	// Upgrading the active theme replaces it at once.
	market.offer(packageVersion(t, "1.1.0"), nil)
	if _, err := e.InstallThemePackage(ctx, samplePackageSlug, "1.1.0", 1); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(homeBody(t, e), "home 1.1.0") || currentVersion(t, e) != "1.1.0" || len(themeVersions(t, e)) != 2 {
		t.Fatalf("after upgrade: current %s, versions %d", currentVersion(t, e), len(themeVersions(t, e)))
	}

	// The same version again changes nothing.
	again, err := e.InstallThemePackage(ctx, samplePackageSlug, "1.1.0", 1)
	if err != nil || again.Notice != PackageNoticeAlreadyCurrent || len(themeVersions(t, e)) != 2 {
		t.Fatalf("reinstall = %+v, %v", again, err)
	}

	// Rolling back makes the kept version current and serves it.
	var old extension.Package
	for _, v := range themeVersions(t, e) {
		if v.Version == "1.0.0" {
			old = v
		}
	}
	if _, err := e.RollbackThemePackage(ctx, old.ID); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(homeBody(t, e), "home 1.0.0") || currentVersion(t, e) != "1.0.0" {
		t.Fatal("rollback did not take effect")
	}
	if r, err := e.RollbackThemePackage(ctx, old.ID); err != nil || r.Notice != PackageNoticeAlreadyCurrent {
		t.Fatalf("rollback to the current version = %+v, %v", r, err)
	}

	restarted := marketSite(t, site, market, nil)
	if restarted.ActiveThemeName() != samplePackageSlug || !strings.Contains(homeBody(t, restarted), "home 1.0.0") {
		t.Fatal("the rolled-back version did not survive a restart")
	}
}

func TestPGMarketInstallRefusesWhatTheCatalogDoesNotVouchFor(t *testing.T) {
	ctx := context.Background()
	market := newPGMarket(t)
	market.offer(packageVersion(t, "1.0.0"), nil)
	market.offer(packageVersion(t, "1.0.1"), nil)
	market.revoke(samplePackageSlug, "1.0.1")
	market.offer(packageVersion(t, "1.0.2"), func(it *extension.Item) { it.Pricing = extension.PricingPaid })
	market.offer(packageVersion(t, "1.0.3"), func(it *extension.Item) { it.Core = ">=99.0.0" })
	market.offer(packageVersion(t, "1.0.4"), func(it *extension.Item) { it.Digest = "h1:" + strings.Repeat("A", 43) + "=" })
	market.offer(packageVersion(t, "1.0.5"), func(it *extension.Item) { it.External = []string{"fonts.googleapis.com"} })
	market.offer(packageVersion(t, "1.0.6"), func(it *extension.Item) { it.Size++ })
	// The market serves another package under 1.0.7's name.
	other, _ := buildPackageZip(t, packageVersion(t, "1.0.0"))
	market.offer(packageVersion(t, "1.0.7"), func(it *extension.Item) {
		it.Size = int64(len(other))
		it.DownloadURL = market.server.URL + "/files/other.zip"
	})
	market.mu.Lock()
	market.files["/files/other.zip"] = other
	market.mu.Unlock()
	e := marketSite(t, newPackageSite(t), market, nil)

	for version, code := range map[string]string{
		"9.9.9": PackageErrNotInCatalog,
		"1.0.1": PackageErrRevoked,
		"1.0.2": PackageErrPaid,
		"1.0.3": PackageErrIncompatible,
		"1.0.4": PackageErrMismatch,
		"1.0.5": PackageErrMismatch,
		"1.0.6": PackageErrDownload,
		"1.0.7": PackageErrMismatch,
	} {
		_, err := e.InstallThemePackage(ctx, samplePackageSlug, version, 1)
		wantPackageError(t, err, code)
	}
	if n := len(themeVersions(t, e)); n != 0 || archiveCount(t, e) != 0 {
		t.Fatalf("refused installs left %d records and %d archives", n, archiveCount(t, e))
	}

	bare := newPackageSite(t).boot(t, nil)
	_, err := bare.InstallThemePackage(ctx, samplePackageSlug, "1.0.0", 1)
	wantPackageError(t, err, PackageErrMarket)
}

func TestPGUploadNeedsASignatureOrAConfirmedDeveloperMode(t *testing.T) {
	ctx := context.Background()
	market := newPGMarket(t)
	site := newPackageSite(t)
	e := marketSite(t, site, market, nil)

	signed, err := e.UploadThemePackage(ctx, market.signedUpload(packageVersion(t, "1.0.0")), "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !signed.Package.Signed || signed.Package.Source != extension.SourceUpload || signed.Summary.Signature != extension.SignatureVerified {
		t.Fatalf("signed upload = %+v %+v", signed.Package, signed.Summary)
	}

	unsigned, _ := buildPackageZip(t, packageVersion(t, "1.1.0"))
	pe := wantPackageError(t, func() error { _, err := e.UploadThemePackage(ctx, unsigned, "", 1); return err }(), PackageErrUnsigned)
	if pe.Package == nil || pe.Package.Signature != extension.SignatureMissing {
		t.Fatalf("summary = %+v", pe.Package)
	}

	// Changed after signing: the signature no longer matches.
	tampered := func(signed []byte) []byte {
		r, err := zip.NewReader(bytes.NewReader(signed), int64(len(signed)))
		if err != nil {
			t.Fatal(err)
		}
		var files []themepkg.File
		for _, f := range r.File {
			rc, _ := f.Open()
			content, _ := io.ReadAll(rc)
			_ = rc.Close()
			if f.Name == "static/css/style.css" {
				content = append(content, []byte("\nbody{color:red}")...)
			}
			files = append(files, themepkg.File{Name: f.Name, Data: content})
		}
		var out bytes.Buffer
		if err := themepkg.WriteZip(&out, files); err != nil {
			t.Fatal(err)
		}
		return out.Bytes()
	}(market.signedUpload(packageVersion(t, "1.2.0")))
	pe = wantPackageError(t, func() error { _, err := e.UploadThemePackage(ctx, tampered, "", 1); return err }(), PackageErrUnsigned)
	if pe.Package.Signature != extension.SignatureInvalid {
		t.Fatalf("tampered package signature = %s", pe.Package.Signature)
	}

	// Developer mode: the exact digest must be confirmed.
	if err := e.SetDeveloperMode(true); err != nil {
		t.Fatal(err)
	}
	pe = wantPackageError(t, func() error { _, err := e.UploadThemePackage(ctx, unsigned, "", 1); return err }(), PackageErrConfirm)
	digest := pe.Package.Digest
	wantPackageError(t, func() error { _, err := e.UploadThemePackage(ctx, unsigned, "h1:not-this-one", 1); return err }(), PackageErrConfirm)
	if len(themeVersions(t, e)) != 1 {
		t.Fatal("an unconfirmed upload was recorded")
	}
	installed, err := e.UploadThemePackage(ctx, unsigned, digest, 1)
	if err != nil {
		t.Fatal(err)
	}
	if installed.Package.Signed || installed.Package.Version != "1.1.0" || !installed.Package.IsCurrent || installed.Summary.Signature != extension.SignatureMissing {
		t.Fatalf("unsigned install = %+v", installed.Package)
	}

	// A version the stored catalog revokes cannot be uploaded, even signed.
	market.offer(packageVersion(t, "1.3.0"), nil)
	market.revoke(samplePackageSlug, "1.3.0")
	if _, err := e.Market.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	wantPackageError(t, func() error {
		_, err := e.UploadThemePackage(ctx, market.signedUpload(packageVersion(t, "1.3.0")), "", 1)
		return err
	}(), PackageErrRevoked)

	// The config lock keeps developer mode off, even if it was turned on.
	locked := marketSite(t, site, market, func(e *Engine) { e.Config.Market.LockDeveloperMode = true })
	if state := locked.DeveloperMode(); state.Enabled || !state.Requested || !state.Locked {
		t.Fatalf("locked state = %+v", state)
	}
	if err := locked.SetDeveloperMode(true); !errors.Is(err, ErrDeveloperModeLocked) {
		t.Fatalf("SetDeveloperMode under the lock = %v", err)
	}
	wantPackageError(t, func() error {
		_, err := locked.UploadThemePackage(ctx, unsigned, digest, 1)
		return err
	}(), PackageErrUnsigned)
}

func TestPGRollbackRefusesUnsafeVersions(t *testing.T) {
	ctx := context.Background()
	market := newPGMarket(t)
	for _, v := range []string{"1.0.0", "1.0.1", "1.1.0"} {
		market.offer(packageVersion(t, v), nil)
	}
	e := marketSite(t, newPackageSite(t), market, nil)
	ids := map[string]int64{}
	for _, v := range []string{"1.0.0", "1.0.1", "1.1.0"} {
		r, err := e.InstallThemePackage(ctx, samplePackageSlug, v, 1)
		if err != nil {
			t.Fatal(err)
		}
		ids[v] = r.Package.ID
	}

	// A damaged archive is found and marked broken.
	if err := os.WriteFile(archivePath(t, e, extension.Package{Slug: samplePackageSlug, Version: "1.0.0", Digest: mustRow(t, e, ids["1.0.0"]).Digest}), []byte("garbage"), 0o640); err != nil {
		t.Fatal(err)
	}
	_, err := e.RollbackThemePackage(ctx, ids["1.0.0"])
	wantPackageError(t, err, PackageErrDamaged)
	if packageStatus(t, e, ids["1.0.0"]) != extension.StatusBroken {
		t.Fatal("damaged version not marked broken")
	}
	_, err = e.RollbackThemePackage(ctx, ids["1.0.0"])
	wantPackageError(t, err, PackageErrBroken)

	// A version the stored catalog revokes.
	market.revoke(samplePackageSlug, "1.0.1")
	if _, err := e.Market.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	_, err = e.RollbackThemePackage(ctx, ids["1.0.1"])
	wantPackageError(t, err, PackageErrRevoked)

	// An unsigned version needs developer mode.
	if err := e.SetDeveloperMode(true); err != nil {
		t.Fatal(err)
	}
	unsigned, _ := buildPackageZip(t, packageVersion(t, "2.0.0"))
	pe := wantPackageError(t, func() error { _, err := e.UploadThemePackage(ctx, unsigned, "", 1); return err }(), PackageErrConfirm)
	up, err := e.UploadThemePackage(ctx, unsigned, pe.Package.Digest, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.RollbackThemePackage(ctx, ids["1.1.0"]); err != nil {
		t.Fatal(err)
	}
	if err := e.SetDeveloperMode(false); err != nil {
		t.Fatal(err)
	}
	_, err = e.RollbackThemePackage(ctx, up.Package.ID)
	wantPackageError(t, err, PackageErrUnsigned)
	_, err = e.RollbackThemePackage(ctx, 999999)
	wantPackageError(t, err, PackageErrNotFound)
	if currentVersion(t, e) != "1.1.0" {
		t.Fatalf("current = %s after refused rollbacks", currentVersion(t, e))
	}
}

func mustRow(t *testing.T, e *Engine, id int64) *extension.Package {
	t.Helper()
	row, err := e.ExtStore.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func TestPGUninstallKeepsTheActiveThemeAndSettings(t *testing.T) {
	ctx := context.Background()
	market := newPGMarket(t)
	market.offer(packageVersion(t, "1.0.0"), nil)
	market.offer(packageVersion(t, "1.1.0"), nil)
	e := marketSite(t, newPackageSite(t), market, nil)
	for _, v := range []string{"1.0.0", "1.1.0"} {
		if _, err := e.InstallThemePackage(ctx, samplePackageSlug, v, 1); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.SwitchTheme(samplePackageSlug); err != nil {
		t.Fatal(err)
	}
	if err := e.Options.Set("theme_"+samplePackageSlug+"_hero_title", "Kept"); err != nil {
		t.Fatal(err)
	}

	_, err := e.UninstallThemePackage(ctx, samplePackageSlug)
	wantPackageError(t, err, PackageErrActive)

	// Configured but not running (a boot fallback): still refused.
	if err := e.SwitchTheme("aa-good"); err != nil {
		t.Fatal(err)
	}
	if err := e.Options.Set("active_theme", samplePackageSlug); err != nil {
		t.Fatal(err)
	}
	_, err = e.UninstallThemePackage(ctx, samplePackageSlug)
	wantPackageError(t, err, PackageErrActive)
	if err := e.Options.Set("active_theme", "aa-good"); err != nil {
		t.Fatal(err)
	}

	removed, err := e.UninstallThemePackage(ctx, samplePackageSlug)
	if err != nil || len(removed) != 2 {
		t.Fatalf("uninstall = %d versions, %v", len(removed), err)
	}
	if len(themeVersions(t, e)) != 0 || archiveCount(t, e) != 0 {
		t.Fatal("records or archives left behind")
	}
	if _, loaded := e.runningPackage(samplePackageSlug); loaded {
		t.Fatal("the uninstalled theme is still loaded")
	}
	if e.Options.Get("theme_"+samplePackageSlug+"_hero_title") != "Kept" {
		t.Fatal("uninstall removed the theme's settings")
	}
	_, err = e.UninstallThemePackage(ctx, samplePackageSlug)
	wantPackageError(t, err, PackageErrNotFound)
	if err := e.SwitchTheme(samplePackageSlug); err == nil {
		t.Fatal("switched to an uninstalled theme")
	}
}

func TestPGInstallKeepsThreeOlderVersions(t *testing.T) {
	ctx := context.Background()
	market := newPGMarket(t)
	versions := []string{"1.0.0", "1.1.0", "1.2.0", "1.3.0", "1.4.0"}
	for _, v := range versions {
		market.offer(packageVersion(t, v), nil)
	}
	e := marketSite(t, newPackageSite(t), market, nil)
	for _, v := range versions {
		if _, err := e.InstallThemePackage(ctx, samplePackageSlug, v, 1); err != nil {
			t.Fatal(err)
		}
	}
	var kept []string
	for _, row := range themeVersions(t, e) {
		kept = append(kept, row.Version)
	}
	if strings.Join(kept, ",") != "1.4.0,1.3.0,1.2.0,1.1.0" || currentVersion(t, e) != "1.4.0" || archiveCount(t, e) != 4 {
		t.Fatalf("kept %v, current %s, %d archives", kept, currentVersion(t, e), archiveCount(t, e))
	}
}

func TestPGSwitchRefusesRevokedPackages(t *testing.T) {
	ctx := context.Background()
	market := newPGMarket(t)
	market.offer(packageVersion(t, "1.0.0"), nil)
	e := marketSite(t, newPackageSite(t), market, nil)
	r, err := e.InstallThemePackage(ctx, samplePackageSlug, "1.0.0", 1)
	if err != nil {
		t.Fatal(err)
	}
	market.revoke(samplePackageSlug, "1.0.0")
	if _, err := e.Market.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	// The stored catalog revokes it even when the record says otherwise.
	if err := e.ExtStore.SetStatus(ctx, r.Package.ID, extension.StatusReady); err != nil {
		t.Fatal(err)
	}
	before := e.ActiveThemeName()
	if err := e.SwitchTheme(samplePackageSlug); err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("SwitchTheme = %v, want a revocation refusal", err)
	}
	if e.ActiveThemeName() != before || packageStatus(t, e, r.Package.ID) != extension.StatusRevoked {
		t.Fatal("the refused switch changed the site")
	}
	// Upgrading to a revoked version is refused too.
	_, err = e.InstallThemePackage(ctx, samplePackageSlug, "1.0.0", 1)
	wantPackageError(t, err, PackageErrRevoked)
}

func TestPGActiveUpgradeNeedsItsPlugins(t *testing.T) {
	ctx := context.Background()
	market := newPGMarket(t)
	market.offer(packageVersion(t, "1.0.0"), nil)
	needy := packageVersion(t, "1.1.0")
	toml, err := os.ReadFile(needy + "/theme.toml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(needy+"/theme.toml", append(toml, []byte("\n[[requires.plugins]]\nslug = \"no-such-plugin\"\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	market.offer(needy, nil)
	e := marketSite(t, newPackageSite(t), market, nil)
	if _, err := e.InstallThemePackage(ctx, samplePackageSlug, "1.0.0", 1); err != nil {
		t.Fatal(err)
	}
	if err := e.SwitchTheme(samplePackageSlug); err != nil {
		t.Fatal(err)
	}
	_, err = e.InstallThemePackage(ctx, samplePackageSlug, "1.1.0", 1)
	wantPackageError(t, err, PackageErrDependencies)
	if currentVersion(t, e) != "1.0.0" || !strings.Contains(homeBody(t, e), "home 1.0.0") || len(themeVersions(t, e)) != 1 {
		t.Fatal("a refused upgrade changed the site")
	}
}

// When the new current version does not replace the running one, the
// previous version becomes current again; a first install is undone.
func TestPGFailedActivationRestoresThePreviousVersion(t *testing.T) {
	ctx := context.Background()
	market := newPGMarket(t)
	market.offer(packageVersion(t, "1.0.0"), nil)
	e := marketSite(t, newPackageSite(t), market, nil)
	r, err := e.InstallThemePackage(ctx, samplePackageSlug, "1.0.0", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.SwitchTheme(samplePackageSlug); err != nil {
		t.Fatal(err)
	}
	prev := r.Package

	// A record whose archive cannot load becomes current.
	_, report := buildPackageZip(t, packageVersion(t, "1.1.0"))
	if _, _, err := e.PackageArchives.Put(samplePackageSlug, "1.1.0", report.Digest, []byte("not a zip")); err != nil {
		t.Fatal(err)
	}
	next := extension.Package{Kind: extension.KindTheme, Slug: samplePackageSlug, Version: "1.1.0", Digest: report.Digest, Size: 9, Source: extension.SourceUpload}
	if err := e.ExtStore.Add(ctx, &next, true); err != nil {
		t.Fatal(err)
	}
	_, err = e.applyCurrentVersion(ctx, &prev, next, false)
	wantPackageError(t, err, PackageErrActivation)
	if currentVersion(t, e) != "1.0.0" || !strings.Contains(homeBody(t, e), "home 1.0.0") {
		t.Fatalf("current = %s after a failed activation", currentVersion(t, e))
	}

	// A first install that cannot load is removed.
	fresh := samplePackageDir(t, nil)
	setThemeTOML(t, fresh, `slug = "minimal-sample"`, `slug = "fresh-sample"`)
	_, freshReport := buildPackageZip(t, fresh)
	if _, _, err := e.PackageArchives.Put("fresh-sample", "1.0.0", freshReport.Digest, []byte("not a zip")); err != nil {
		t.Fatal(err)
	}
	row := extension.Package{Kind: extension.KindTheme, Slug: "fresh-sample", Version: "1.0.0", Digest: freshReport.Digest, Size: 9, Source: extension.SourceUpload}
	if err := e.ExtStore.Add(ctx, &row, true); err != nil {
		t.Fatal(err)
	}
	_, err = e.applyCurrentVersion(ctx, nil, row, true)
	wantPackageError(t, err, PackageErrActivation)
	if rows, _ := e.ExtStore.Versions(ctx, extension.KindTheme, "fresh-sample"); len(rows) != 0 {
		t.Fatal("the failed first install was kept")
	}
	if path, _ := e.PackageArchives.Path("fresh-sample", "1.0.0", freshReport.Digest); fileExists(path) {
		t.Fatal("the failed first install's archive was kept")
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Installing content that is already installed with a damaged archive
// replaces the archive; the listing and the admin see the result without
// server paths.
func TestPGReinstallRepairsADamagedArchive(t *testing.T) {
	ctx := context.Background()
	market := newPGMarket(t)
	market.offer(packageVersion(t, "1.0.0"), nil)
	site := newPackageSite(t)
	e := marketSite(t, site, market, nil)
	r, err := e.InstallThemePackage(ctx, samplePackageSlug, "1.0.0", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archivePath(t, e, r.Package), []byte("garbage"), 0o640); err != nil {
		t.Fatal(err)
	}

	restarted := marketSite(t, site, market, nil)
	list, err := restarted.adminPackageManager().List(ctx)
	if err != nil || len(list.Packages) != 1 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	listed := list.Packages[0]
	if listed.Issue == "" || strings.Contains(listed.Issue, site.siteDir) || !strings.Contains(listed.Issue, "packages/archives/") ||
		listed.Running != "" || listed.Versions[0].Status != extension.StatusBroken {
		t.Fatalf("listed = %+v", listed)
	}
	if !list.Market.Enabled || list.Market.Mode != extension.MarketCustom {
		t.Fatalf("market = %+v", list.Market)
	}

	again, err := restarted.InstallThemePackage(ctx, samplePackageSlug, "1.0.0", 1)
	if err != nil {
		t.Fatal(err)
	}
	if again.Package.Status != extension.StatusReady || len(themeVersions(t, restarted)) != 1 {
		t.Fatalf("reinstall = %+v", again.Package)
	}
	if info, ok := restarted.runningPackage(samplePackageSlug); !ok || info.Digest != r.Package.Digest || info.Source != coreTheme.PackageSourceInstalled {
		t.Fatal("the repaired package is not loaded")
	}
}

func TestPGConcurrentInstallsLeaveOneCurrentVersion(t *testing.T) {
	ctx := context.Background()
	market := newPGMarket(t)
	market.offer(packageVersion(t, "1.0.0"), nil)
	market.offer(packageVersion(t, "1.1.0"), nil)
	e := marketSite(t, newPackageSite(t), market, nil)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, v := range []string{"1.0.0", "1.1.0"} {
		wg.Add(1)
		go func(v string) {
			defer wg.Done()
			_, err := e.InstallThemePackage(ctx, samplePackageSlug, v, 1)
			errs <- err
		}(v)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	current := 0
	for _, row := range themeVersions(t, e) {
		if row.IsCurrent {
			current++
		}
	}
	info, _ := e.runningPackage(samplePackageSlug)
	row, _ := e.ExtStore.Current(ctx, extension.KindTheme, samplePackageSlug)
	if current != 1 || len(themeVersions(t, e)) != 2 || row == nil || info.Digest != row.Digest {
		t.Fatalf("current versions = %d, running %s, recorded %+v", current, info.Digest, row)
	}
}

func TestPGThemeCatalogForTheMarketPage(t *testing.T) {
	ctx := context.Background()
	market := newPGMarket(t)
	market.offer(packageVersion(t, "1.0.0"), nil)
	market.offer(packageVersion(t, "1.1.0"), func(it *extension.Item) {
		it.Localized = map[string]extension.ItemText{"zh": {Title: "极简样例"}}
	})
	market.offer(packageVersion(t, "1.2.0"), func(it *extension.Item) { it.Core = ">=99.0.0" })
	paid := samplePackageDir(t, nil)
	setThemeTOML(t, paid, `slug = "minimal-sample"`, `slug = "paid-sample"`)
	market.offer(paid, func(it *extension.Item) { it.Pricing = extension.PricingPaid })
	e := marketSite(t, newPackageSite(t), market, nil)
	if _, err := e.InstallThemePackage(ctx, samplePackageSlug, "1.0.0", 1); err != nil {
		t.Fatal(err)
	}
	if err := e.SwitchTheme(samplePackageSlug); err != nil {
		t.Fatal(err)
	}

	cat, err := e.ThemeCatalog(ctx, "zh-CN")
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.Entries) != 1 {
		t.Fatalf("entries = %+v, want only the free theme", cat.Entries)
	}
	entry := cat.Entries[0]
	if entry.Latest.Version != "1.1.0" || !entry.Latest.Compatible || entry.Title != "极简样例" ||
		entry.Installed == nil || entry.Installed.Version != "1.0.0" || !entry.Update {
		t.Fatalf("entry = %+v", entry)
	}
	if en, _ := e.ThemeCatalog(ctx, "en"); en.Entries[0].Title != "Minimal Sample" {
		t.Fatalf("English title = %q", en.Entries[0].Title)
	}

	// Revoked versions are not offered; a revoked installed version is
	// reported, and so is the active theme it runs.
	market.revoke(samplePackageSlug, "1.1.0")
	market.revoke(samplePackageSlug, "1.0.0")
	cat, err = e.ThemeCatalog(ctx, "en")
	if err != nil {
		t.Fatal(err)
	}
	entry = cat.Entries[0]
	if entry.Latest.Version != "1.2.0" || entry.Latest.Compatible || entry.Update {
		t.Fatalf("after revocations entry = %+v", entry)
	}
	if len(cat.RevokedInstalled) != 1 || cat.RevokedInstalled[0].Version != "1.0.0" {
		t.Fatalf("revoked installed = %+v", cat.RevokedInstalled)
	}
	if slug, version, revoked := e.RevokedActiveTheme(); !revoked || slug != samplePackageSlug || version != "1.0.0" {
		t.Fatalf("RevokedActiveTheme = %s %s %v", slug, version, revoked)
	}
	display := e.themePackageDisplays(e.themeSnapshot())[samplePackageSlug]
	if !display.Package || display.Source != extension.SourceMarket || !display.Revoked || display.Unsigned ||
		strings.Join(display.External, ",") != "fonts.googleapis.com,fonts.gstatic.com" {
		t.Fatalf("display = %+v", display)
	}

	// A catalog that cannot be used is an error with its reason, never an
	// older list.
	market.mu.Lock()
	market.envelope = []byte(`{"format":"something else"}`)
	market.mu.Unlock()
	_, err = e.adminPackageManager().Catalog(ctx, "en")
	var adminErr *admin.PackageError
	if !errors.As(err, &adminErr) || adminErr.Code != PackageErrCatalog || adminErr.Reason != "format" {
		t.Fatalf("catalog error = %v", err)
	}
}

func TestCatalogFailureReason(t *testing.T) {
	for err, want := range map[error]string{
		extension.ErrMarketDisabled:                               "market_disabled",
		fmt.Errorf("x: %w", extension.ErrCatalogFetch):            "fetch",
		extension.ErrCatalogTooLarge:                              "too_large",
		fmt.Errorf("%w: payload", extension.ErrCatalogFormat):     "format",
		extension.ErrCatalogSchema:                                "schema",
		extension.ErrCatalogSignature:                             "signature",
		fmt.Errorf("%w (expired)", extension.ErrCatalogExpired):   "expired",
		fmt.Errorf("%w (stored 3)", extension.ErrCatalogRollback): "rollback",
		extension.ErrCatalogEquivocation:                          "equivocation",
		errors.New("something else"):                              "",
	} {
		if got := CatalogFailureReason(err); got != want {
			t.Errorf("CatalogFailureReason(%v) = %q, want %q", err, got, want)
		}
	}
}

func TestPGInstalledMarketPackagesAreCheckedForUpdates(t *testing.T) {
	ctx := context.Background()
	market := newPGMarket(t)
	market.offer(packageVersion(t, "1.0.0"), nil)
	market.offer(packageVersion(t, "1.1.0"), nil)
	site := newPackageSite(t)
	e := marketSite(t, site, market, nil)
	if _, err := e.InstallThemePackage(ctx, samplePackageSlug, "1.0.0", 1); err != nil {
		t.Fatal(err)
	}
	target, ok := e.Updates.Registry().Get(updatecheck.KindTheme, samplePackageSlug)
	if !ok || target.CurrentVersion != "1.0.0" || target.Endpoint != e.Market.Root().UpdatesURL() || target.ReportInstallation {
		t.Fatalf("target = %+v, registered %v", target, ok)
	}

	// A start finds a check due, but update checks are off: nothing is sent.
	if err := e.Options.Set(updatecheck.KeyPersistedCheckState, `{"next_check_at":"2020-01-01T00:00:00Z"}`); err != nil {
		t.Fatal(err)
	}
	e = marketSite(t, site, market, nil)
	// Keep the test offline: the core target would call the official server.
	e.Updates.Registry().Unregister(updatecheck.KindCore, "gopress")
	if err := e.Updates.RunDue(ctx); err != nil {
		t.Fatal(err)
	}
	market.mu.Lock()
	sent := len(market.checks)
	market.mu.Unlock()
	if sent != 0 {
		t.Fatalf("%d update checks sent while update checks are off", sent)
	}

	// Turn update checks on, as a later start finds them.
	if err := e.Options.Set(option.KeyUpdateCheckEnabled, "1"); err != nil {
		t.Fatal(err)
	}
	market.mu.Lock()
	market.updates = []updatecheck.Update{{Kind: updatecheck.KindTheme, Slug: samplePackageSlug, LatestVersion: "1.1.0",
		Severity: "security", ReleaseURL: "https://market.example.com/aurora/1.1.0"}}
	market.mu.Unlock()
	e = marketSite(t, site, market, nil)
	// Keep the test offline: the core target would call the official server.
	e.Updates.Registry().Unregister(updatecheck.KindCore, "gopress")
	if err := e.Updates.RunDue(ctx); err != nil {
		t.Fatal(err)
	}
	market.mu.Lock()
	checks := append([]updatecheck.Request(nil), market.checks...)
	market.mu.Unlock()
	if len(checks) != 1 || checks[0].Instance != nil || len(checks[0].Targets) != 1 ||
		checks[0].Targets[0].Slug != samplePackageSlug || checks[0].Targets[0].CurrentVersion != "1.0.0" {
		t.Fatalf("update check requests = %+v", checks)
	}
	update := e.ThemePackageUpdate(samplePackageSlug)
	if update == nil || update.LatestVersion != "1.1.0" || update.Severity != "security" || update.ReleaseURL == "" {
		t.Fatalf("update = %+v", update)
	}
	list, err := e.adminPackageManager().List(ctx)
	if err != nil || len(list.Packages) != 1 || list.Packages[0].Update == nil || list.Packages[0].Update.LatestVersion != "1.1.0" {
		t.Fatalf("listed update = %+v, %v", list, err)
	}
	// Turning update checks off hides what an earlier check found.
	if err := e.Options.Set(option.KeyUpdateCheckEnabled, "0"); err != nil {
		t.Fatal(err)
	}
	if update := e.ThemePackageUpdate(samplePackageSlug); update != nil {
		t.Fatalf("update shown while update checks are off: %+v", update)
	}
	if err := e.Options.Set(option.KeyUpdateCheckEnabled, "1"); err != nil {
		t.Fatal(err)
	}

	// Upgrading clears it; uninstalling unregisters the target.
	if _, err := e.InstallThemePackage(ctx, samplePackageSlug, "1.1.0", 1); err != nil {
		t.Fatal(err)
	}
	if target, _ := e.Updates.Registry().Get(updatecheck.KindTheme, samplePackageSlug); target.CurrentVersion != "1.1.0" {
		t.Fatalf("target after upgrade = %+v", target)
	}
	if update := e.ThemePackageUpdate(samplePackageSlug); update != nil {
		t.Fatalf("update shown after upgrading to it: %+v", update)
	}
	if _, err := e.UninstallThemePackage(ctx, samplePackageSlug); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.Updates.Registry().Get(updatecheck.KindTheme, samplePackageSlug); ok {
		t.Fatal("an uninstalled package is still checked for updates")
	}

	// Unsigned uploads are not checked.
	if err := e.SetDeveloperMode(true); err != nil {
		t.Fatal(err)
	}
	unsigned, _ := buildPackageZip(t, packageVersion(t, "2.0.0"))
	pe := wantPackageError(t, func() error { _, err := e.UploadThemePackage(ctx, unsigned, "", 1); return err }(), PackageErrConfirm)
	if _, err := e.UploadThemePackage(ctx, unsigned, pe.Package.Digest, 1); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.Updates.Registry().Get(updatecheck.KindTheme, samplePackageSlug); ok {
		t.Fatal("an unsigned upload is checked for updates")
	}

	// Without a market, nothing is registered.
	bare := site.boot(t, nil)
	for _, target := range bare.Updates.Registry().All() {
		if target.Kind == updatecheck.KindTheme {
			t.Fatalf("a site without a market registered %+v", target)
		}
	}
}
