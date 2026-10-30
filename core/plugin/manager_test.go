// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package plugin

import (
	"sync"
	"sync/atomic"
	"testing"
)

type countingPlugin struct {
	name        string
	activations atomic.Int32
	stops       atomic.Int32
	manager     *Manager
}

func (p *countingPlugin) Name() string        { return p.name }
func (p *countingPlugin) Version() string     { return "1.0.0" }
func (p *countingPlugin) Description() string { return "" }
func (p *countingPlugin) Activate(App) {
	p.activations.Add(1)
	if p.manager != nil {
		// Read-only queries from inside a lifecycle callback must not deadlock.
		_ = p.manager.IsActive(p.name)
		_ = p.manager.ActivePlugins()
	}
}
func (p *countingPlugin) Deactivate(App) { p.stops.Add(1) }

func TestManagerActivateIsIdempotent(t *testing.T) {
	m := NewManager()
	p := &countingPlugin{name: "alpha", manager: m}
	m.Register(p)

	if !m.Activate("alpha", nil) || !m.Activate("alpha", nil) {
		t.Fatal("Activate() = false for a registered plugin")
	}
	if got := p.activations.Load(); got != 1 {
		t.Fatalf("Activate callback ran %d times, want 1", got)
	}
	if !m.Deactivate("alpha", nil) {
		t.Fatal("Deactivate() = false for an active plugin")
	}
	if m.Deactivate("alpha", nil) {
		t.Fatal("second Deactivate() = true, want false")
	}
	if got := p.stops.Load(); got != 1 {
		t.Fatalf("Deactivate callback ran %d times, want 1", got)
	}
}

func TestManagerUnknownPlugin(t *testing.T) {
	m := NewManager()
	if m.Activate("missing", nil) {
		t.Fatal("Activate() = true for an unregistered plugin")
	}
	if m.IsActive("missing") {
		t.Fatal("IsActive() = true for an unregistered plugin")
	}
}

func TestManagerZeroValueIsUsable(t *testing.T) {
	var m Manager
	m.Register(&countingPlugin{name: "alpha"})
	if m.IsActive("alpha") || len(m.ActivePlugins()) != 0 {
		t.Fatal("zero-value manager reports an active plugin before activation")
	}
	if !m.Activate("alpha", nil) || !m.IsActive("alpha") {
		t.Fatal("zero-value manager failed to activate")
	}
}

func TestManagerActivePluginsFollowsRegistrationOrder(t *testing.T) {
	m := NewManager()
	for _, name := range []string{"c", "a", "b"} {
		m.Register(&countingPlugin{name: name})
		m.Activate(name, nil)
	}
	got := m.ActivePlugins()
	want := []string{"c", "a", "b"}
	if len(got) != len(want) {
		t.Fatalf("ActivePlugins() len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Name() != want[i] {
			t.Fatalf("ActivePlugins()[%d] = %q, want %q", i, got[i].Name(), want[i])
		}
	}
}

// TestManagerConcurrentLifecycleAndReads reproduces the audit's race between
// admin toggles and request-path IsActive checks. Run with -race.
func TestManagerConcurrentLifecycleAndReads(t *testing.T) {
	m := NewManager()
	plugins := []*countingPlugin{{name: "alpha"}, {name: "beta"}}
	for _, p := range plugins {
		p.manager = m
		m.Register(p)
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = m.IsActive("alpha")
					_ = m.ActivePlugins()
					_ = m.RegisteredPlugins()
					_ = m.CanDeactivate("beta", nil)
				}
			}
		}()
	}
	var toggles sync.WaitGroup
	for _, p := range plugins {
		toggles.Add(1)
		go func(name string) {
			defer toggles.Done()
			for i := 0; i < 200; i++ {
				m.Activate(name, nil)
				m.Activate(name, nil)
				m.Deactivate(name, nil)
			}
		}(p.name)
	}
	toggles.Wait()
	close(stop)
	wg.Wait()

	for _, p := range plugins {
		if a, s := p.activations.Load(), p.stops.Load(); a != s || a != 200 {
			t.Fatalf("%s activations/stops = %d/%d, want 200/200", p.name, a, s)
		}
	}
}
