package core

import (
	"testing"

	"go-press/core/plugin"
	coreTheme "go-press/core/theme"
)

// fakeModule is a registered-but-inactive default-inactive plugin.
type fakeModule struct{}

func (fakeModule) Name() string          { return "commerce" }
func (fakeModule) Version() string       { return "1.0.0" }
func (fakeModule) Description() string   { return "" }
func (fakeModule) Activate(plugin.App)   {}
func (fakeModule) Deactivate(plugin.App) {}
func (fakeModule) DefaultInactive() bool { return true }

// TestDefaultInactiveDependencyNeedsEnable verifies C2+C3: a theme that requires
// a default-inactive module resolves to DepNeedsEnable — not auto-activated, not
// blocking — so the admin can prompt the operator to enable it.
func TestDefaultInactiveDependencyNeedsEnable(t *testing.T) {
	mgr := plugin.NewManager()
	mgr.Register(fakeModule{})

	deps := resolvePluginDeps([]coreTheme.PluginRequirement{{Slug: "commerce"}}, mgr)
	if len(deps) != 1 || deps[0].State != DepNeedsEnable {
		t.Fatalf("state = %+v, want DepNeedsEnable", deps)
	}
	if deps[0].Blocking() {
		t.Fatal("a needs-enable module must not block theme activation")
	}

	report := ThemeDepReport{CoreSatisfied: true, Plugins: deps}
	if !report.OK() {
		t.Fatal("report should be OK (non-blocking) so the theme activates")
	}
	if got := report.NeedsEnable(); len(got) != 1 || got[0] != "commerce" {
		t.Fatalf("NeedsEnable = %v, want [commerce]", got)
	}
	if len(report.InactiveDeps()) != 0 {
		t.Fatal("default-inactive module must be excluded from auto-activation (InactiveDeps)")
	}
}

// TestManagerIsDefaultInactive checks the Manager helper both ways.
func TestManagerIsDefaultInactive(t *testing.T) {
	mgr := plugin.NewManager()
	mgr.Register(fakeModule{})
	if !mgr.IsDefaultInactive("commerce") {
		t.Fatal("fakeModule should be default-inactive")
	}
	if mgr.IsDefaultInactive("nonexistent") {
		t.Fatal("absent plugin is not default-inactive")
	}
}
