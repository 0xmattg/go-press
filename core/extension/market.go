// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package extension

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/0xmattg/go-press/pkg/logger"
)

// fetchTimeout bounds one catalog fetch. Catalogs can be a few megabytes, so
// it is longer than the update check's.
const fetchTimeout = 15 * time.Second

var (
	// ErrMarketDisabled reports that no market is configured: the release
	// has no built-in market keys and config.toml has no [market] section.
	ErrMarketDisabled = errors.New("extension: no market is configured")
	// ErrCatalogFetch reports a network failure or a non-200 response.
	ErrCatalogFetch = errors.New("extension: could not fetch the market catalog")
	// ErrCatalogEquivocation reports a catalog with the same sequence as the
	// stored one but different content. A market never does this; it points
	// to a market bug or a leaked catalog key.
	ErrCatalogEquivocation = errors.New("extension: market served different catalogs under the same sequence")
)

// Market fetches and verifies the market catalog and applies its revocation
// list to installed packages.
//
// Nothing fetches the catalog on a schedule: callers ask for a fresh catalog
// when an administrator opens the market or installs or upgrades a package,
// and a failed fetch is reported rather than answered with an older catalog.
// The last verified catalog is stored only so that activation can check
// revocations without a network request.
type Market struct {
	store     *Store
	root      TrustRoot
	client    *http.Client
	userAgent string
	now       func() time.Time

	mu      sync.Mutex
	pending *refreshCall
}

// refreshCall is one in-flight Refresh shared by concurrent callers.
type refreshCall struct {
	done chan struct{}
	cat  *Catalog
	err  error
}

// NewMarket returns a Market. A nil client gets a default one; either way
// the client does not follow redirects and has a timeout.
func NewMarket(store *Store, root TrustRoot, client *http.Client, userAgent string) *Market {
	var c http.Client
	if client != nil {
		c = *client
	}
	if c.Timeout == 0 {
		c.Timeout = fetchTimeout
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	c.Jar = nil
	return &Market{store: store, root: root, client: &c, userAgent: userAgent, now: time.Now}
}

// Root returns the market's trust root.
func (m *Market) Root() TrustRoot { return m.root }

// Enabled reports whether a market is configured.
func (m *Market) Enabled() bool { return m.root.Enabled() }

// Refresh fetches the catalog, verifies it, stores it, and updates the
// status of installed packages from its revocation list. Concurrent calls
// share one fetch.
func (m *Market) Refresh(ctx context.Context) (*Catalog, error) {
	if !m.root.Enabled() {
		return nil, ErrMarketDisabled
	}
	m.mu.Lock()
	call := m.pending
	if call == nil {
		call = &refreshCall{done: make(chan struct{})}
		m.pending = call
		// One caller giving up must not fail the others, so the shared work
		// runs on a context of its own, bounded by the fetch timeout.
		work, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*fetchTimeout)
		go func() {
			defer cancel()
			call.cat, call.err = m.refresh(work)
			m.mu.Lock()
			m.pending = nil
			m.mu.Unlock()
			close(call.done)
		}()
	}
	m.mu.Unlock()

	select {
	case <-call.done:
		return call.cat, call.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (m *Market) refresh(ctx context.Context) (*Catalog, error) {
	raw, err := m.fetch(ctx)
	if err != nil {
		return nil, err
	}
	now := m.now()
	cat, err := ParseCatalog(raw, m.root, now)
	if err != nil {
		return nil, err
	}
	if err := m.save(ctx, cat, raw, now); err != nil {
		return nil, err
	}
	if len(cat.Hidden) > 0 {
		logger.Warn("Market catalog items hidden: their signatures did not verify",
			"count", len(cat.Hidden), "items", cat.Hidden, "sequence", cat.Sequence)
	}
	if err := m.applyRevocations(ctx, cat); err != nil {
		return nil, fmt.Errorf("extension: apply catalog revocations: %w", err)
	}
	return cat, nil
}

func (m *Market) fetch(ctx context.Context) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.root.catalogURL(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCatalogFetch, err)
	}
	req.Header.Set("Accept", "application/json")
	if m.userAgent != "" {
		req.Header.Set("User-Agent", m.userAgent)
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCatalogFetch, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: market returned %s", ErrCatalogFetch, resp.Status)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxEnvelopeBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCatalogFetch, err)
	}
	if len(raw) > maxEnvelopeBytes {
		return nil, ErrCatalogTooLarge
	}
	return raw, nil
}

// save stores the catalog unless the stored one forbids it: a lower
// sequence is a rollback, and the same sequence with other content is an
// equivocation. A stored catalog that no longer verifies under the current
// keys, for example after switching markets, sets no lower bound.
func (m *Market) save(ctx context.Context, cat *Catalog, raw []byte, now time.Time) error {
	return m.store.ReplaceCatalog(ctx, now, func(stored SiteState) (int64, string, error) {
		if stored.CatalogSequence > 0 {
			prev, err := m.parseStored(stored)
			switch {
			case err != nil:
				logger.Warn("Stored market catalog no longer verifies; replacing it",
					"stored_sequence", stored.CatalogSequence, "sequence", cat.Sequence, "error", err)
			case cat.Sequence < prev.Sequence:
				return 0, "", fmt.Errorf("%w (stored %d, fetched %d)", ErrCatalogRollback, prev.Sequence, cat.Sequence)
			case cat.Sequence == prev.Sequence && cat.digest != prev.digest:
				logger.Error("Market served a different catalog under a sequence it already used",
					"sequence", cat.Sequence, "market", m.root.Host())
				return 0, "", ErrCatalogEquivocation
			}
		}
		return cat.Sequence, string(raw), nil
	})
}

// Stored returns the last verified catalog, checked again against the
// current keys, or nil when there is none. It does not check expiry: a
// stored catalog only adds restrictions (its revocations), so an old one is
// still safe to consult.
func (m *Market) Stored(ctx context.Context) (*Catalog, error) {
	if !m.root.Enabled() {
		return nil, nil
	}
	state, err := m.store.Catalog(ctx)
	if err != nil {
		return nil, err
	}
	if state.CatalogSequence == 0 {
		return nil, nil
	}
	cat, err := m.parseStored(state)
	if err != nil {
		logger.Warn("Stored market catalog does not verify; ignoring it",
			"sequence", state.CatalogSequence, "error", err)
		return nil, nil
	}
	return cat, nil
}

func (m *Market) parseStored(state SiteState) (*Catalog, error) {
	return parseCatalog([]byte(state.CatalogBody), m.root, parseOptions{coreVersion: currentCoreVersion()})
}

// applyRevocations marks installed packages the catalog revokes as revoked,
// and returns revoked packages the catalog no longer revokes to ready, so the
// catalog stays the single source of truth. Revocation wins over broken.
// Every installed package is matched, whatever its source.
func (m *Market) applyRevocations(ctx context.Context, cat *Catalog) error {
	for kind := range knownKinds {
		packages, err := m.store.Packages(ctx, kind)
		if err != nil {
			return err
		}
		for _, pkg := range packages {
			rev := cat.Revocation(pkg.Kind, pkg.Slug, pkg.Version)
			switch {
			case rev != nil && pkg.Status != StatusRevoked:
				if err := m.store.SetStatus(ctx, pkg.ID, StatusRevoked); err != nil {
					return err
				}
				logger.Error("Installed extension package revoked by the market",
					"kind", pkg.Kind, "slug", pkg.Slug, "version", pkg.Version, "current", pkg.IsCurrent,
					"severity", rev.Severity, "reason", rev.Reason)
			case rev == nil && pkg.Status == StatusRevoked:
				if err := m.store.SetStatus(ctx, pkg.ID, StatusReady); err != nil {
					return err
				}
				logger.Warn("Extension package is no longer revoked by the market",
					"kind", pkg.Kind, "slug", pkg.Slug, "version", pkg.Version)
			}
		}
	}
	return nil
}
