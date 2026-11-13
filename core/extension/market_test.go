// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package extension

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeMarket is a local market server serving whatever catalog it holds.
type fakeMarket struct {
	t      *testing.T
	server *httptest.Server

	mu       sync.Mutex
	body     []byte
	status   int
	requests []*http.Request
	block    chan struct{} // when set, requests wait on it
	started  chan struct{} // receives once per request when set
}

func newFakeMarket(t *testing.T) *fakeMarket {
	t.Helper()
	f := &fakeMarket{t: t, status: http.StatusOK}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeMarket) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests = append(f.requests, r.Clone(context.Background()))
	body, status, block, started := f.body, f.status, f.block, f.started
	f.mu.Unlock()
	if started != nil {
		started <- struct{}{}
	}
	if block != nil {
		<-block
	}
	if r.URL.Path != "/api/market/v1/catalog" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func (f *fakeMarket) set(body []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.body, f.status = body, http.StatusOK
}

func (f *fakeMarket) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func (f *fakeMarket) endpoint() string { return f.server.URL + "/api/market/v1" }

// testMarket returns a Market over an SQLite store, talking to f with k's keys.
func testMarket(t *testing.T, f *fakeMarket, k testKeys) (*Market, *Store) {
	t.Helper()
	store, _ := sqliteStore(t, true)
	m := NewMarket(store, k.root(t, f.endpoint()), nil, "GoPress/test")
	m.now = func() time.Time { return testNow }
	return m, store
}

func TestMarketRefreshStoresCatalogAnonymously(t *testing.T) {
	k := newTestKeys(t)
	f := newFakeMarket(t)
	m, store := testMarket(t, f, k)
	f.set(newPayload(10, k.signedItem(t, "aurora", "1.0.0")).envelope(t, k.catalog))

	cat, err := m.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cat.Sequence != 10 || len(cat.Items) != 1 {
		t.Fatalf("catalog = %+v", cat)
	}
	req := f.requests[0]
	if req.Method != http.MethodGet || req.URL.Query().Get("schema") != "1" || len(req.URL.Query()) != 1 {
		t.Fatalf("request = %s %s", req.Method, req.URL)
	}
	if req.Header.Get("User-Agent") != "GoPress/test" || req.Header.Get("Cookie") != "" || req.Header.Get("Authorization") != "" {
		t.Fatalf("request headers = %v", req.Header)
	}
	state, _ := store.Catalog(context.Background())
	if state.CatalogSequence != 10 || state.CatalogFetchedAt == nil || !state.CatalogFetchedAt.Equal(testNow) {
		t.Fatalf("stored = seq %d at %v", state.CatalogSequence, state.CatalogFetchedAt)
	}
	stored, err := m.Stored(context.Background())
	if err != nil || stored == nil || stored.Digest() != cat.Digest() {
		t.Fatalf("Stored() = %+v, %v", stored, err)
	}
	if gen := mustGeneration(t, store); gen != 0 {
		t.Fatalf("a catalog alone must not bump the generation, got %d", gen)
	}
}

func TestMarketRejectsRollbackAndEquivocation(t *testing.T) {
	k := newTestKeys(t)
	f := newFakeMarket(t)
	m, store := testMarket(t, f, k)
	ctx := context.Background()
	original := newPayload(5, k.signedItem(t, "aurora", "1.0.0"))
	f.set(original.envelope(t, k.catalog))
	if _, err := m.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	before, _ := store.Catalog(ctx)

	f.set(newPayload(4, k.signedItem(t, "aurora", "1.0.0")).envelope(t, k.catalog))
	if _, err := m.Refresh(ctx); !errors.Is(err, ErrCatalogRollback) {
		t.Fatalf("older catalog: %v", err)
	}
	f.set(newPayload(5, k.signedItem(t, "aurora", "2.0.0")).envelope(t, k.catalog))
	if _, err := m.Refresh(ctx); !errors.Is(err, ErrCatalogEquivocation) {
		t.Fatalf("same sequence, other content: %v", err)
	}
	if after, _ := store.Catalog(ctx); after.CatalogBody != before.CatalogBody {
		t.Fatal("a rejected catalog replaced the stored one")
	}

	// The same payload again, even re-signed, is the same catalog.
	later := testNow.Add(time.Hour)
	m.now = func() time.Time { return later }
	f.set(original.envelope(t, k.catalog))
	if _, err := m.Refresh(ctx); err != nil {
		t.Fatalf("same catalog again: %v", err)
	}
	if after, _ := store.Catalog(ctx); !after.CatalogFetchedAt.Equal(later) {
		t.Fatalf("fetch time not updated: %v", after.CatalogFetchedAt)
	}
	f.set(newPayload(6).envelope(t, k.catalog))
	if cat, err := m.Refresh(ctx); err != nil || cat.Sequence != 6 {
		t.Fatalf("newer catalog: %+v, %v", cat, err)
	}
}

func TestMarketSwitchResetsSequenceFloor(t *testing.T) {
	a, b := newTestKeys(t), newTestKeys(t)
	f := newFakeMarket(t)
	m, store := testMarket(t, f, a)
	ctx := context.Background()
	f.set(newPayload(100).envelope(t, a.catalog))
	if _, err := m.Refresh(ctx); err != nil {
		t.Fatal(err)
	}

	// The site is reconfigured to market b, whose numbering starts at 1.
	switched := NewMarket(store, b.root(t, f.endpoint()), nil, "")
	switched.now = m.now
	if stored, err := switched.Stored(ctx); err != nil || stored != nil {
		t.Fatalf("market a's catalog must not verify for market b: %+v, %v", stored, err)
	}
	f.set(newPayload(1).envelope(t, b.catalog))
	if cat, err := switched.Refresh(ctx); err != nil || cat.Sequence != 1 {
		t.Fatalf("Refresh = %+v, %v", cat, err)
	}
}

func TestMarketExpiredCatalogStillGuardsRevocations(t *testing.T) {
	k := newTestKeys(t)
	f := newFakeMarket(t)
	m, _ := testMarket(t, f, k)
	ctx := context.Background()
	p := newPayload(3)
	p.Revoked = []Revocation{{Kind: KindTheme, Slug: "evil-x", Versions: []string{"*"}}}
	f.set(p.envelope(t, k.catalog))
	if _, err := m.Refresh(ctx); err != nil {
		t.Fatal(err)
	}

	m.now = func() time.Time { return testNow.Add(30 * 24 * time.Hour) }
	if _, err := m.Refresh(ctx); !errors.Is(err, ErrCatalogExpired) {
		t.Fatalf("expired catalog: %v", err)
	}
	stored, err := m.Stored(ctx)
	if err != nil || stored == nil || stored.Revocation(KindTheme, "evil-x", "1.0.0") == nil {
		t.Fatalf("Stored() = %+v, %v", stored, err)
	}
}

func TestMarketAppliesRevocations(t *testing.T) {
	k := newTestKeys(t)
	f := newFakeMarket(t)
	m, store := testMarket(t, f, k)
	ctx := context.Background()

	beta := theme("aurora", "1.0.0-beta.1", testDigest("a"))
	broken := theme("aurora", "1.1.0", testDigest("b"))
	broken.Status = StatusBroken
	uploaded := theme("aurora", "1.2.0", testDigest("c"))
	uploaded.Source, uploaded.Signed = SourceUpload, false
	wronglyRevoked := theme("borealis", "2.0.0", testDigest("d"))
	wronglyRevoked.Status = StatusRevoked
	untouched := theme("cascade", "1.0.0", testDigest("e"))
	for _, pkg := range []*Package{beta, broken, uploaded, wronglyRevoked, untouched} {
		if err := store.Add(ctx, pkg, pkg == uploaded); err != nil {
			t.Fatal(err)
		}
	}
	gen := mustGeneration(t, store)

	p := newPayload(1)
	p.Revoked = []Revocation{{Kind: KindTheme, Slug: "aurora", Versions: []string{"*"}, Severity: SeverityCritical, Reason: ReasonMalware}}
	f.set(p.envelope(t, k.catalog))
	if _, err := m.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	statuses := func() map[int64]string {
		out := map[int64]string{}
		packages, err := store.Packages(ctx, KindTheme)
		if err != nil {
			t.Fatal(err)
		}
		for _, pkg := range packages {
			out[pkg.ID] = pkg.Status
		}
		return out
	}
	got := statuses()
	for _, pkg := range []*Package{beta, broken, uploaded} {
		if got[pkg.ID] != StatusRevoked {
			t.Errorf("%s %s = %s, want revoked", pkg.Slug, pkg.Version, got[pkg.ID])
		}
	}
	if got[wronglyRevoked.ID] != StatusReady || got[untouched.ID] != StatusReady {
		t.Errorf("statuses = %v", got)
	}
	if after := mustGeneration(t, store); after != gen+4 {
		t.Fatalf("generation = %d, want %d (one per status change)", after, gen+4)
	}
	if current, _ := store.Current(ctx, KindTheme, "aurora"); current == nil || current.ID != uploaded.ID {
		t.Fatal("revocation must not change which version is current")
	}

	// The market withdraws the revocation.
	f.set(newPayload(2).envelope(t, k.catalog))
	if _, err := m.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	for id, status := range statuses() {
		if status != StatusReady {
			t.Errorf("package %d = %s after the revocation was withdrawn", id, status)
		}
	}
}

func TestMarketDisabled(t *testing.T) {
	f := newFakeMarket(t)
	store, _ := sqliteStore(t, true)
	m := NewMarket(store, TrustRoot{Endpoint: f.endpoint()}, nil, "")
	if m.Enabled() {
		t.Fatal("a root without keys must be disabled")
	}
	if _, err := m.Refresh(context.Background()); !errors.Is(err, ErrMarketDisabled) {
		t.Fatalf("Refresh = %v", err)
	}
	if cat, err := m.Stored(context.Background()); cat != nil || err != nil {
		t.Fatalf("Stored = %+v, %v", cat, err)
	}
	if f.requestCount() != 0 {
		t.Fatal("a disabled market sent a request")
	}
}

func TestMarketFetchFailures(t *testing.T) {
	k := newTestKeys(t)
	ctx := context.Background()
	cases := map[string]struct {
		handler http.HandlerFunc
		want    error
	}{
		"redirect": {func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://elsewhere.example.com/catalog", http.StatusFound)
		}, ErrCatalogFetch},
		"server error": {func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		}, ErrCatalogFetch},
		"too large": {func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(make([]byte, maxEnvelopeBytes+1))
		}, ErrCatalogTooLarge},
		"not a catalog": {func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("<html>maintenance</html>"))
		}, ErrCatalogFormat},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(tc.handler)
			defer server.Close()
			store, _ := sqliteStore(t, true)
			m := NewMarket(store, k.root(t, server.URL+"/api/market/v1"), nil, "")
			if _, err := m.Refresh(ctx); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if state, _ := store.Catalog(ctx); state.CatalogSequence != 0 {
				t.Fatal("a failed fetch stored something")
			}
		})
	}

	t.Run("timeout", func(t *testing.T) {
		release := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-release }))
		defer server.Close()
		defer close(release)
		store, _ := sqliteStore(t, true)
		m := NewMarket(store, k.root(t, server.URL), &http.Client{Timeout: 50 * time.Millisecond}, "")
		if _, err := m.Refresh(ctx); !errors.Is(err, ErrCatalogFetch) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestMarketSharesConcurrentRefreshes(t *testing.T) {
	k := newTestKeys(t)
	f := newFakeMarket(t)
	m, _ := testMarket(t, f, k)
	f.set(newPayload(8).envelope(t, k.catalog))
	release := make(chan struct{})
	started := make(chan struct{}, 10)
	f.mu.Lock()
	f.block, f.started = release, started
	f.mu.Unlock()

	var wg sync.WaitGroup
	var failures atomic.Int32
	refresh := func(ctx context.Context) {
		defer wg.Done()
		if cat, err := m.Refresh(ctx); err != nil || cat.Sequence != 8 {
			failures.Add(1)
		}
	}
	wg.Add(1)
	go refresh(context.Background())
	<-started // the first fetch is in flight

	// A caller that gives up gets its own context error and does not cancel
	// the shared fetch for anyone else.
	impatient, cancel := context.WithCancel(context.Background())
	impatientErr := make(chan error, 1)
	go func() {
		_, err := m.Refresh(impatient)
		impatientErr <- err
	}()
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go refresh(context.Background())
	}
	time.Sleep(100 * time.Millisecond) // let every caller join the in-flight fetch
	cancel()
	if err := <-impatientErr; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled caller: %v", err)
	}
	close(release)
	wg.Wait()

	if failures.Load() != 0 {
		t.Fatalf("%d callers failed", failures.Load())
	}
	if n := f.requestCount(); n != 1 {
		t.Fatalf("%d requests, want 1", n)
	}
	// The next call fetches again: successful results are not cached.
	f.mu.Lock()
	f.block, f.started = nil, nil
	f.mu.Unlock()
	if _, err := m.Refresh(context.Background()); err != nil || f.requestCount() != 2 {
		t.Fatalf("second refresh: %v, %d requests", err, f.requestCount())
	}
}
