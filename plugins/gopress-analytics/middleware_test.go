package gopressanalytics

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type captureWriter struct {
	batches chan []Event
}

func (w *captureWriter) RecordBatch(_ context.Context, events []Event) error {
	copied := append([]Event(nil), events...)
	w.batches <- copied
	return nil
}

func TestAnalyticsMiddlewareCapturesIPAndDeviceData(t *testing.T) {
	gin.SetMode(gin.TestMode)
	writer := &captureWriter{batches: make(chan []Event, 1)}
	p := New()
	p.hashKey = []byte("test-analytics-key")
	p.location = time.UTC
	p.active.Store(true)
	p.collectionOverride = func() bool { return true }
	p.collector = newCollector(writer, 100)

	router := gin.New()
	router.Use(p.analyticsMiddleware())
	router.GET("/products/hepa", func(c *gin.Context) {
		c.Set("current_lang", "en")
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusOK, "<html></html>")
	})

	req := httptest.NewRequest(http.MethodGet, "/products/hepa?utm_source=newsletter&utm_medium=email", nil)
	req.RemoteAddr = "203.0.113.8:4321"
	req.Header.Set("User-Agent", "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 Chrome/125.0 Mobile Safari/537.36")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	p.collector.stopAndFlush()

	var batch []Event
	select {
	case batch = <-writer.batches:
	case <-time.After(time.Second):
		t.Fatal("analytics event was not flushed")
	}
	if len(batch) != 1 {
		t.Fatalf("events = %d, want 1", len(batch))
	}
	event := batch[0]
	if event.IPAddress != "203.0.113.8" {
		t.Fatalf("ip address = %q", event.IPAddress)
	}
	if event.IPHash == "" || event.IPHash == event.IPAddress {
		t.Fatalf("ip hash was not generated safely: %q", event.IPHash)
	}
	if event.DeviceType != "mobile" || event.Platform != "Android" || event.OS != "Android" || event.Browser != "Chrome" {
		t.Fatalf("unexpected device parsing: %#v", event)
	}
	if event.DeviceVendor != "Google" {
		t.Fatalf("device vendor = %q, want Google", event.DeviceVendor)
	}
	if event.NormalizedPath != "/products/hepa" || event.PathHash == "" {
		t.Fatalf("path fields not populated: path=%q hash=%q", event.NormalizedPath, event.PathHash)
	}
	if event.Source != "newsletter" || event.Medium != "email" {
		t.Fatalf("attribution = %q/%q", event.Source, event.Medium)
	}
	if event.Language != "en" {
		t.Fatalf("language = %q", event.Language)
	}

	cookies := rec.Result().Cookies()
	if len(cookies) != 2 {
		t.Fatalf("cookies = %d, want visitor and session cookies", len(cookies))
	}
	for _, cookie := range cookies {
		if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
			t.Fatalf("cookie security attributes missing: %#v", cookie)
		}
	}
}

func TestAnalyticsRequestFiltering(t *testing.T) {
	tests := []struct {
		method string
		path   string
		skip   bool
	}{
		{http.MethodGet, "/", false},
		{http.MethodHead, "/", true},
		{http.MethodPost, "/", true},
		{http.MethodGet, "/admin", true},
		{http.MethodGet, "/admin/posts", true},
		{http.MethodGet, "/api/v1/posts", true},
		{http.MethodGet, "/static/theme.css", true},
		{http.MethodGet, "/health", true},
	}
	for _, tt := range tests {
		req := httptest.NewRequest(tt.method, tt.path, nil)
		if got := shouldSkipRequest(req); got != tt.skip {
			t.Errorf("%s %s skip = %v, want %v", tt.method, tt.path, got, tt.skip)
		}
	}
	if !isBotUserAgent("Mozilla/5.0 compatible; Googlebot/2.1") {
		t.Fatal("known crawler was not detected")
	}
	if isBotUserAgent("Mozilla/5.0 Chrome/125.0 Safari/537.36") {
		t.Fatal("normal browser was classified as bot")
	}
	if got := normalizePath(strings.Repeat("a", 1100)); len(got) != 1024 {
		t.Fatalf("normalized path length = %d, want 1024", len(got))
	}
}

func TestCollectorRejectsEventsAfterStop(t *testing.T) {
	writer := &captureWriter{batches: make(chan []Event, 1)}
	collector := newCollector(writer, 100)
	collector.stopAndFlush()
	if collector.publish(Event{EventUUID: "late-event"}) {
		t.Fatal("collector accepted an event after shutdown")
	}
}

func TestCollectorFlushesOneWindowAsSingleBatch(t *testing.T) {
	writer := &captureWriter{batches: make(chan []Event, 1)}
	collector := newCollectorWithInterval(writer, 100, 50*time.Millisecond)
	defer collector.stopAndFlush()

	for i := 0; i < 3; i++ {
		if !collector.publish(Event{EventUUID: fmt.Sprintf("event-%d", i)}) {
			t.Fatalf("collector rejected event %d", i)
		}
	}

	select {
	case <-writer.batches:
		t.Fatal("collector flushed before the configured window elapsed")
	case <-time.After(20 * time.Millisecond):
	}

	select {
	case batch := <-writer.batches:
		if len(batch) != 3 {
			t.Fatalf("batch size = %d, want 3", len(batch))
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("collector did not flush after the configured window")
	}
}
