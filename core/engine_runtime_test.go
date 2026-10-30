// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/0xmattg/go-press/config"
	"github.com/0xmattg/go-press/core/cache"
	"github.com/0xmattg/go-press/core/content"
	"github.com/0xmattg/go-press/core/hook"
	"github.com/0xmattg/go-press/core/plugin"
	"github.com/0xmattg/go-press/core/rewrite"
	"github.com/0xmattg/go-press/core/user"
	"github.com/0xmattg/go-press/core/worker"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func runtimeTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	sqlDB, err := sql.Open("sqlite3", "file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, WithoutReturning: true}), &gorm.Config{
		DisableAutomaticPing: true, SkipDefaultTransaction: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Ping(); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip("runtime test requires CGO-backed SQLite")
		}
		t.Fatal(err)
	}
	return db
}

func readinessStatus(t *testing.T, e *Engine) (int, map[string]string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/readyz", e.serveReadiness)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("readiness body %q: %v", rec.Body.String(), err)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("readiness response must not be cacheable")
	}
	return rec.Code, body
}

func TestReadinessFollowsEngineLifecycle(t *testing.T) {
	e := &Engine{DB: runtimeTestDB(t)}

	if code, body := readinessStatus(t, e); code != http.StatusServiceUnavailable || body["reason"] != "starting" {
		t.Fatalf("before MarkReady: %d %v, want 503 starting", code, body)
	}
	e.MarkReady()
	if code, body := readinessStatus(t, e); code != http.StatusOK || body["status"] != "ready" {
		t.Fatalf("after MarkReady: %d %v, want 200 ready", code, body)
	}
	e.BeginShutdown()
	if code, body := readinessStatus(t, e); code != http.StatusServiceUnavailable || body["reason"] != "shutting_down" {
		t.Fatalf("after BeginShutdown: %d %v, want 503 shutting_down", code, body)
	}
}

func TestReadinessReportsUnavailableDatabase(t *testing.T) {
	db := runtimeTestDB(t)
	sqlDB, _ := db.DB()
	_ = sqlDB.Close()
	e := &Engine{DB: db}
	e.MarkReady()

	if code, body := readinessStatus(t, e); code != http.StatusServiceUnavailable || body["reason"] != "database" {
		t.Fatalf("closed database: %d %v, want 503 database", code, body)
	}
}

type stoppingTestPlugin struct {
	lifecycleTestPlugin
	stops atomic.Int32
}

func (p *stoppingTestPlugin) Name() string { return "stopping-test" }
func (p *stoppingTestPlugin) Stop(context.Context) error {
	p.stops.Add(1)
	return nil
}

type panickingStopPlugin struct{ lifecycleTestPlugin }

func (p *panickingStopPlugin) Name() string               { return "panicking-stop" }
func (p *panickingStopPlugin) Stop(context.Context) error { panic("stop failed") }

func TestShutdownStopsExtensionsAndIsIdempotent(t *testing.T) {
	stopping := &stoppingTestPlugin{}
	panicking := &panickingStopPlugin{}
	manager := plugin.NewManager()
	manager.Register(stopping)
	manager.Register(panicking)
	manager.Activate(stopping.Name(), nil)
	manager.Activate(panicking.Name(), nil)

	pool := worker.NewPool(1)
	sched := worker.NewScheduler(pool)
	sched.Start()
	e := &Engine{
		DB: runtimeTestDB(t), Hooks: hook.New(), PluginManager: manager,
		Workers: pool, Sched: sched,
		Cache: cache.NewManager(cache.NewMemoryCache(10), nil),
	}
	var shutdownHook atomic.Int32
	e.Hooks.AddAction("engine.shutdown", func(context.Context, ...interface{}) { shutdownHook.Add(1) }, 10)

	first := e.Shutdown(context.Background())
	if first == nil || !strings.Contains(first.Error(), "panicking-stop") {
		t.Fatalf("Shutdown() error = %v, want the contained plugin panic reported", first)
	}
	if second := e.Shutdown(context.Background()); second != first {
		t.Fatalf("second Shutdown() = %v, want the first result", second)
	}
	if stopping.stops.Load() != 1 || shutdownHook.Load() != 1 {
		t.Fatalf("stops/hook = %d/%d, want 1/1", stopping.stops.Load(), shutdownHook.Load())
	}
	if err := pool.SubmitFunc("late", func(context.Context) error { return nil }); !errors.Is(err, worker.ErrPoolClosed) {
		t.Fatalf("Submit after Shutdown = %v, want ErrPoolClosed", err)
	}
	if ok, reason := e.Readiness(context.Background()); ok || reason != "shutting_down" {
		t.Fatalf("Readiness after Shutdown = %v %q", ok, reason)
	}
}

func TestRouterRebuildReusesAPILimiter(t *testing.T) {
	registry := content.NewRegistry()
	e := &Engine{
		Config:   &config.Config{},
		Hooks:    hook.New(),
		Cache:    cache.NewNoopManager(),
		Registry: registry,
		Auth:     user.NewAuth("router-test-secret", 1, nil),
		Sitemap:  rewrite.NewSitemapGenerator("https://example.test", registry, content.NewRepository(nil), rewrite.NewEngine(registry)),
	}
	e.SetupRouter()
	first := e.apiLimiter
	e.SetupRouter()
	if first == nil || e.apiLimiter != first {
		t.Fatal("router rebuild replaced the API rate limiter")
	}
	e.apiLimiter.Stop()
}
