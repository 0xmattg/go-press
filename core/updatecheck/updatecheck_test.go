package updatecheck

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go-press/core/option"
)

func TestRegistryValidatesAndCopiesTargets(t *testing.T) {
	registry := NewRegistry()
	err := registry.Register(Target{
		Kind:           KindPlugin,
		Slug:           "demo-plugin",
		CurrentVersion: "1.2.3",
		Channel:        "stable",
		Endpoint:       "https://updates.example.com/check",
		Parameters:     map[string]string{"edition": "community"},
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	targets := registry.All()
	if len(targets) != 1 || targets[0].Key() != "plugin:demo-plugin" {
		t.Fatalf("targets = %#v", targets)
	}
	targets[0].Parameters["edition"] = "mutated"
	if got := registry.All()[0].Parameters["edition"]; got != "community" {
		t.Fatalf("registry parameter mutated through snapshot: %q", got)
	}

	if err := registry.Register(Target{
		Kind: KindTheme, Slug: "bad", CurrentVersion: "not-semver", Endpoint: "https://updates.example.com",
	}); err == nil {
		t.Fatal("Register() accepted invalid semver")
	}
	if err := registry.Register(Target{
		Kind: KindTheme, Slug: "bad", CurrentVersion: "1.0.0", Endpoint: "http://updates.example.com",
	}); err == nil {
		t.Fatal("Register() accepted non-HTTPS remote endpoint")
	}
}

func TestClientPostsProtocolAndRejectsOversizedResponse(t *testing.T) {
	var gotUserAgent string
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotUserAgent = r.UserAgent()
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("unexpected request: method=%s content-type=%s", r.Method, r.Header.Get("Content-Type"))
		}
		return jsonResponse(http.StatusOK, Response{ProtocolVersion: ProtocolVersion, TTLSeconds: 86400}), nil
	})}

	client := NewClientWithHTTPClient("GoPress/test", httpClient)
	_, err := client.Check(context.Background(), "http://localhost/check", Request{ProtocolVersion: ProtocolVersion})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if gotUserAgent != "GoPress/test" {
		t.Fatalf("User-Agent = %q", gotUserAgent)
	}

	client.httpClient = &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(strings.Repeat("x", int(maxResponseBytes)+1))),
		}, nil
	})}
	if _, err := client.Check(context.Background(), "http://localhost/check", Request{}); err == nil {
		t.Fatal("Check() accepted oversized response")
	}
}

func TestServiceChecksDueTargetsAndCachesUpdate(t *testing.T) {
	fixedNow := time.Date(2026, 8, 5, 8, 0, 0, 0, time.UTC)
	var calls atomic.Int32
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		var request Request
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request.Instance == nil || request.Instance.ID != "instance-123456" || len(request.Targets) != 1 {
			t.Fatalf("request = %#v", request)
		}
		return jsonResponse(http.StatusOK, Response{
			ProtocolVersion: ProtocolVersion,
			TTLSeconds:      60,
			Updates: []Update{{
				Kind: KindCore, Slug: "gopress", LatestVersion: "0.6.52",
				Severity: "security", ReleaseURL: OfficialReleasesURL,
			}},
		}), nil
	})}

	store := option.NewMemoryStore(map[string]string{
		option.KeyUpdateCheckEnabled: "1",
		KeyInstallationID:            "instance-123456",
		KeyPolicyVersion:             CurrentPolicyVersion,
	})
	registry := NewRegistry()
	if err := registry.Register(Target{
		Kind: KindCore, Slug: "gopress", CurrentVersion: "0.6.48", Endpoint: "http://localhost/check", ReportInstallation: true,
	}); err != nil {
		t.Fatal(err)
	}
	service := NewService(store, registry, NewClientWithHTTPClient("GoPress/test", httpClient))
	service.now = func() time.Time { return fixedNow }
	service.state.NextCheckAt = fixedNow.Add(-time.Minute)

	if err := service.RunDue(context.Background()); err != nil {
		t.Fatalf("RunDue() error = %v", err)
	}
	status := service.Snapshot(KindCore, "gopress")
	if !status.HasUpdate || status.LatestVersion != "0.6.52" || status.Severity != "security" {
		t.Fatalf("status = %#v", status)
	}
	if service.state.NextCheckAt != fixedNow.Add(minimumTTL) {
		t.Fatalf("next check = %s, want minimum TTL %s", service.state.NextCheckAt, fixedNow.Add(minimumTTL))
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
	if err := registry.Register(Target{
		Kind: KindCore, Slug: "gopress", CurrentVersion: "0.6.52", Endpoint: "http://localhost/check", ReportInstallation: true,
	}); err != nil {
		t.Fatal(err)
	}
	if got := service.Snapshot(KindCore, "gopress"); got.HasUpdate {
		t.Fatalf("cached status remained stale after local upgrade: %#v", got)
	}

	store.Set(option.KeyUpdateCheckEnabled, "0")
	service.state.NextCheckAt = fixedNow.Add(-time.Minute)
	if err := service.RunDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("disabled service made network request; calls=%d", calls.Load())
	}
	if got := service.Snapshot(KindCore, "gopress"); got != (Status{}) {
		t.Fatalf("disabled snapshot = %#v", got)
	}
}

func TestServiceBacksOffAfterFailure(t *testing.T) {
	fixedNow := time.Date(2026, 8, 5, 8, 0, 0, 0, time.UTC)
	httpClient := &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusBadGateway, map[string]string{"error": "unavailable"}), nil
	})}
	store := option.NewMemoryStore(map[string]string{
		option.KeyUpdateCheckEnabled: "1",
		KeyInstallationID:            "instance-123456",
		KeyPolicyVersion:             CurrentPolicyVersion,
	})
	registry := NewRegistry()
	if err := registry.Register(Target{Kind: KindCore, Slug: "gopress", CurrentVersion: "0.6.48", Endpoint: "http://localhost/check", ReportInstallation: true}); err != nil {
		t.Fatal(err)
	}
	service := NewService(store, registry, NewClientWithHTTPClient("GoPress/test", httpClient))
	service.now = func() time.Time { return fixedNow }
	service.state.NextCheckAt = fixedNow.Add(-time.Minute)
	if err := service.RunDue(context.Background()); err == nil {
		t.Fatal("RunDue() error = nil, want endpoint failure")
	}
	if service.state.NextCheckAt != fixedNow.Add(time.Hour) {
		t.Fatalf("next check = %s, want one-hour backoff", service.state.NextCheckAt)
	}
}

func TestServiceSchedulesFirstCheckWhenEnabledAfterBoot(t *testing.T) {
	fixedNow := time.Date(2026, 8, 5, 8, 0, 0, 0, time.UTC)
	store := option.NewMemoryStore(map[string]string{option.KeyUpdateCheckEnabled: "0"})
	service := NewService(store, NewRegistry(), NewClient("GoPress/test"))
	service.now = func() time.Time { return fixedNow }
	service.random = func(time.Duration) time.Duration { return 10 * time.Minute }
	service.Prepare()
	if !service.state.NextCheckAt.IsZero() {
		t.Fatalf("disabled service scheduled a check at %s", service.state.NextCheckAt)
	}
	store.Set(option.KeyUpdateCheckEnabled, "1")
	if err := service.RunDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := fixedNow.Add(15 * time.Minute)
	if service.state.NextCheckAt != want {
		t.Fatalf("next check = %s, want %s", service.state.NextCheckAt, want)
	}
}

func TestThirdPartyTargetDoesNotReceiveOfficialInstallationID(t *testing.T) {
	fixedNow := time.Date(2026, 8, 5, 8, 0, 0, 0, time.UTC)
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var payload Request
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Instance != nil {
			t.Fatalf("third-party endpoint received installation envelope: %#v", payload.Instance)
		}
		return jsonResponse(http.StatusOK, Response{ProtocolVersion: ProtocolVersion, TTLSeconds: 86400}), nil
	})}
	store := option.NewMemoryStore(map[string]string{option.KeyUpdateCheckEnabled: "1"})
	registry := NewRegistry()
	if err := registry.Register(Target{
		Kind: KindPlugin, Slug: "third-party", CurrentVersion: "1.0.0", Endpoint: "http://localhost/check",
	}); err != nil {
		t.Fatal(err)
	}
	service := NewService(store, registry, NewClientWithHTTPClient("GoPress/test", httpClient))
	service.now = func() time.Time { return fixedNow }
	service.state.NextCheckAt = fixedNow.Add(-time.Minute)
	if err := service.RunDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := store.Get(KeyInstallationID); got != "" {
		t.Fatalf("third-party-only check generated official installation ID %q", got)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func jsonResponse(status int, payload interface{}) *http.Response {
	raw, _ := json.Marshal(payload)
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(string(raw))),
	}
}
