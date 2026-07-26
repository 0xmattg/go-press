package core

import (
	"html/template"
	"testing"

	"github.com/gin-gonic/gin"

	"go-press/core/plugin"
	coreTheme "go-press/core/theme"
)

type fakeTheme struct {
	name string
	req  coreTheme.RequiresConfig
}

func (f fakeTheme) Name() string                       { return f.name }
func (f fakeTheme) Version() string                    { return "1.0.0" }
func (f fakeTheme) Description() string                { return "" }
func (f fakeTheme) Author() string                     { return "" }
func (f fakeTheme) Setup(app coreTheme.App)            {}
func (f fakeTheme) ServeHTTP(c *gin.Context)           {}
func (f fakeTheme) TemplateFuncs() template.FuncMap    { return nil }
func (f fakeTheme) TemplateDir() string                { return "" }
func (f fakeTheme) StaticDir() string                  { return "" }
func (f fakeTheme) Requires() coreTheme.RequiresConfig { return f.req }

type fakePlugin struct {
	name, version string
}

func (f fakePlugin) Name() string              { return f.name }
func (f fakePlugin) Version() string           { return f.version }
func (f fakePlugin) Description() string       { return "" }
func (f fakePlugin) Activate(app plugin.App)   {}
func (f fakePlugin) Deactivate(app plugin.App) {}

func newManagerWith(active map[string]string, inactive map[string]string) *plugin.Manager {
	m := plugin.NewManager()
	for name, ver := range active {
		m.Register(fakePlugin{name, ver})
		m.Activate(name, nil)
	}
	for name, ver := range inactive {
		m.Register(fakePlugin{name, ver})
	}
	return m
}

func TestResolvePluginDeps_States(t *testing.T) {
	mgr := newManagerWith(
		map[string]string{"multi-language": "2.0.0", "seo-extras": "1.0.0"}, // active
		map[string]string{"code-snippets": "1.0.0"},                         // registered, inactive
	)
	reqs := []coreTheme.PluginRequirement{
		{Slug: "multi-language", Version: ">=2.0.0"}, // Satisfied
		{Slug: "code-snippets", Version: ">=1.0.0"},  // Inactive (present, ok version, not active)
		{Slug: "seo-extras", Version: ">=2.0.0"},     // VersionMismatch (1.0.0 !>= 2.0.0)
		{Slug: "not-in-build", Version: ">=1.0.0"},   // Missing
	}
	got := resolvePluginDeps(reqs, mgr)
	want := []DepState{DepSatisfied, DepInactive, DepVersionMismatch, DepMissing}
	if len(got) != len(want) {
		t.Fatalf("got %d statuses, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].State != w {
			t.Errorf("[%s] state = %q, want %q", got[i].Slug, got[i].State, w)
		}
	}
	// InstalledVersion populated except for missing.
	if got[0].InstalledVersion != "2.0.0" {
		t.Errorf("installed version = %q", got[0].InstalledVersion)
	}
	if got[3].InstalledVersion != "" {
		t.Errorf("missing dep should have empty installed version, got %q", got[3].InstalledVersion)
	}
}

func TestDepStatusBlocking(t *testing.T) {
	if (DepStatus{State: DepInactive}).Blocking() {
		t.Error("inactive dep must not block (auto-activatable)")
	}
	if !(DepStatus{State: DepMissing}).Blocking() {
		t.Error("missing dep must block")
	}
	if !(DepStatus{State: DepVersionMismatch}).Blocking() {
		t.Error("version mismatch must block")
	}
	if (DepStatus{State: DepMissing, Optional: true}).Blocking() {
		t.Error("optional missing dep must not block")
	}
}

func TestResolvePluginDeps_EmptyConstraintMatchesAny(t *testing.T) {
	mgr := newManagerWith(map[string]string{"seo-extras": "0.1.0"}, nil)
	got := resolvePluginDeps([]coreTheme.PluginRequirement{{Slug: "seo-extras"}}, mgr)
	if got[0].State != DepSatisfied {
		t.Fatalf("empty constraint should match any version, got %q", got[0].State)
	}
}

func TestCoreSatisfied(t *testing.T) {
	if !coreSatisfied("", "0.6.19") {
		t.Error("empty core constraint should always pass")
	}
	if !coreSatisfied(">=0.6.0", "0.6.19") {
		t.Error("0.6.19 should satisfy >=0.6.0")
	}
	if coreSatisfied(">=0.7.0", "0.6.19") {
		t.Error("0.6.19 should NOT satisfy >=0.7.0")
	}
	if coreSatisfied("garbage!!", "0.6.19") {
		t.Error("unparsable constraint should be treated as unsatisfied")
	}
}

func TestEngineThemeDependencies(t *testing.T) {
	mgr := newManagerWith(
		map[string]string{"seo-extras": "1.0.0"},     // active
		map[string]string{"multi-language": "2.0.0"}, // registered, inactive
	)
	e := &Engine{
		PluginManager: mgr,
		themes: map[string]coreTheme.Theme{
			"th": fakeTheme{name: "th", req: coreTheme.RequiresConfig{Plugins: []coreTheme.PluginRequirement{
				{Slug: "seo-extras", Version: ">=1.0.0"},     // satisfied
				{Slug: "multi-language", Version: ">=2.0.0"}, // inactive (auto-activatable)
			}}},
		},
		activeThemeName: "th",
	}
	rep := e.ThemeDependencies("th")
	if !rep.OK() {
		t.Fatalf("report should be OK (only inactive dep): %+v", rep.Plugins)
	}
	if got := rep.InactiveDeps(); len(got) != 1 || got[0] != "multi-language" {
		t.Fatalf("InactiveDeps = %v, want [multi-language]", got)
	}
	if !e.ActiveThemeRequiresPlugin("seo-extras") {
		t.Error("active theme should require seo-extras")
	}
	if e.ActiveThemeRequiresPlugin("code-snippets") {
		t.Error("active theme should not require code-snippets")
	}
}

func TestEngineThemeDependencies_BlockingMissing(t *testing.T) {
	mgr := newManagerWith(nil, nil)
	e := &Engine{
		PluginManager: mgr,
		themes: map[string]coreTheme.Theme{
			"th": fakeTheme{name: "th", req: coreTheme.RequiresConfig{Plugins: []coreTheme.PluginRequirement{
				{Slug: "not-in-build", Version: ">=1.0.0"},
			}}},
		},
		activeThemeName: "th",
	}
	rep := e.ThemeDependencies("th")
	if rep.OK() {
		t.Fatal("missing dep should make report not OK")
	}
	if err := rep.BlockingError(); err == nil {
		t.Fatal("expected a blocking error for missing dependency")
	}
}

func TestThemeDepReportOK(t *testing.T) {
	ok := ThemeDepReport{CoreSatisfied: true, Plugins: []DepStatus{{State: DepSatisfied}, {State: DepInactive}}}
	if !ok.OK() {
		t.Error("satisfied + inactive (auto-activatable) should be OK")
	}
	blocked := ThemeDepReport{CoreSatisfied: true, Plugins: []DepStatus{{State: DepMissing}}}
	if blocked.OK() {
		t.Error("missing dep should make report not OK")
	}
	coreBad := ThemeDepReport{CoreSatisfied: false}
	if coreBad.OK() {
		t.Error("unsatisfied core should make report not OK")
	}
}
