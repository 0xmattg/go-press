package core

import (
	"errors"
	"fmt"
	"strings"

	"github.com/0xmattg/go-press/core/plugin"
	coreTheme "github.com/0xmattg/go-press/core/theme"
	"github.com/0xmattg/go-press/pkg/logger"
	"github.com/0xmattg/go-press/pkg/semver"
	"github.com/0xmattg/go-press/version"
)

// DepState is the resolved state of one theme→plugin dependency.
type DepState string

const (
	// DepSatisfied: required plugin is present, active, and version-compatible.
	DepSatisfied DepState = "satisfied"
	// DepInactive: present and version-compatible but not activated (auto-activatable).
	DepInactive DepState = "inactive"
	// DepNeedsEnable: present and version-compatible but not activated AND the
	// plugin is a default-inactive opt-in module — so it is NOT auto-activated;
	// the operator must enable it (surfaced as a prominent admin prompt).
	DepNeedsEnable DepState = "needs_enable"
	// DepVersionMismatch: present/active but its version fails the constraint.
	DepVersionMismatch DepState = "version_mismatch"
	// DepMissing: not compiled into this build; only fixable by rebuilding.
	DepMissing DepState = "missing"
)

// DepStatus is the resolution of one [requires].plugins entry.
type DepStatus struct {
	Slug             string
	Constraint       string
	Optional         bool
	State            DepState
	InstalledVersion string // "" when missing
}

// Satisfied reports whether the dependency is fully met.
func (d DepStatus) Satisfied() bool { return d.State == DepSatisfied }

// Blocking reports whether this dependency should block theme activation. An
// Inactive dependency is auto-activatable, so it is not blocking; Missing and
// VersionMismatch block unless the requirement is Optional.
func (d DepStatus) Blocking() bool {
	if d.Optional {
		return false
	}
	return d.State == DepMissing || d.State == DepVersionMismatch
}

// ThemeDepReport is the full dependency resolution for one theme.
type ThemeDepReport struct {
	Slug           string
	CoreConstraint string
	CoreSatisfied  bool
	Plugins        []DepStatus
}

// OK reports whether the theme can run: core constraint satisfied and no
// blocking plugin dependency.
func (r ThemeDepReport) OK() bool {
	if !r.CoreSatisfied {
		return false
	}
	for _, p := range r.Plugins {
		if p.Blocking() {
			return false
		}
	}
	return true
}

// InactiveDeps returns the slugs of present-but-inactive required plugins that
// can be auto-activated to satisfy the theme.
func (r ThemeDepReport) InactiveDeps() []string {
	var out []string
	for _, p := range r.Plugins {
		if p.State == DepInactive {
			out = append(out, p.Slug)
		}
	}
	return out
}

// NeedsEnable returns the slugs of present-but-inactive required plugins that
// are opt-in modules (default-inactive) — they are NOT auto-activated; the admin
// surfaces a prominent "enable" prompt for them.
func (r ThemeDepReport) NeedsEnable() []string {
	var out []string
	for _, p := range r.Plugins {
		if p.State == DepNeedsEnable {
			out = append(out, p.Slug)
		}
	}
	return out
}

// BlockingDeps returns the dependencies that block activation (non-optional
// missing / version-mismatch), for surfacing a precise reason to the operator.
func (r ThemeDepReport) BlockingDeps() []DepStatus {
	var out []DepStatus
	for _, p := range r.Plugins {
		if p.Blocking() {
			out = append(out, p)
		}
	}
	return out
}

// BlockingError returns a human-readable error describing why the theme cannot
// be activated (unmet core constraint or non-optional missing/incompatible
// plugins), or nil when the theme is activatable. Inactive-but-present
// dependencies are not blocking — they are auto-activated on switch.
func (r ThemeDepReport) BlockingError() error {
	if r.OK() {
		return nil
	}
	var parts []string
	if !r.CoreSatisfied {
		parts = append(parts, fmt.Sprintf("requires GoPress core %s (running %s)", r.CoreConstraint, version.String()))
	}
	for _, d := range r.BlockingDeps() {
		switch d.State {
		case DepMissing:
			parts = append(parts, fmt.Sprintf("requires plugin %q which is not in this build (rebuild with it via `gopress build`)", d.Slug))
		case DepVersionMismatch:
			parts = append(parts, fmt.Sprintf("requires plugin %q %s but %s is installed", d.Slug, d.Constraint, d.InstalledVersion))
		}
	}
	return errors.New(strings.Join(parts, "; "))
}

// coreSatisfied reports whether coreVersion meets the constraint. An empty
// constraint is always satisfied; an unparsable constraint/version is treated as
// unsatisfied so a malformed [requires].core surfaces rather than silently passing.
func coreSatisfied(constraint, coreVersion string) bool {
	if constraint == "" {
		return true
	}
	ok, err := semver.Satisfies(coreVersion, constraint)
	return err == nil && ok
}

// resolvePluginDeps evaluates a theme's [requires].plugins against the plugin
// manager. It is generic: it knows no specific plugin and resolves purely by
// slug + semver against the manager snapshot.
func resolvePluginDeps(reqs []coreTheme.PluginRequirement, mgr *plugin.Manager) []DepStatus {
	out := make([]DepStatus, 0, len(reqs))
	for _, r := range reqs {
		st := DepStatus{Slug: r.Slug, Constraint: r.Version, Optional: r.Optional}
		p, found := mgr.FindBySlug(r.Slug)
		if !found {
			st.State = DepMissing
			out = append(out, st)
			continue
		}
		st.InstalledVersion = p.Version()
		if r.Version != "" {
			ok, err := semver.Satisfies(p.Version(), r.Version)
			if err != nil || !ok {
				st.State = DepVersionMismatch
				out = append(out, st)
				continue
			}
		}
		switch {
		case mgr.IsActive(p.Name()):
			st.State = DepSatisfied
		case mgr.IsDefaultInactive(p.Name()):
			// Opt-in module: don't auto-activate; prompt the operator to enable.
			st.State = DepNeedsEnable
		default:
			st.State = DepInactive
		}
		out = append(out, st)
	}
	return out
}

// themeRequires returns the declared [requires] block of the theme registered
// under slug, or an empty block when the theme declares none.
func (e *Engine) themeRequires(slug string) coreTheme.RequiresConfig {
	t, ok := e.themes[slug]
	if !ok {
		return coreTheme.RequiresConfig{}
	}
	if rp, ok := t.(coreTheme.RequirementsProvider); ok {
		return rp.Requires()
	}
	return coreTheme.RequiresConfig{}
}

// ThemeDependencies resolves the dependency status of the theme registered under
// slug against the current plugin manager and the running core version.
func (e *Engine) ThemeDependencies(slug string) ThemeDepReport {
	req := e.themeRequires(slug)
	return ThemeDepReport{
		Slug:           slug,
		CoreConstraint: req.Core,
		CoreSatisfied:  coreSatisfied(req.Core, version.String()),
		Plugins:        resolvePluginDeps(req.Plugins, e.PluginManager),
	}
}

// ActiveThemeDependencies resolves dependencies for the currently active theme.
func (e *Engine) ActiveThemeDependencies() ThemeDepReport {
	return e.ThemeDependencies(e.ActiveThemeName())
}

// ActiveThemeRequiresPlugin reports whether the active theme declares a
// non-optional dependency on the given plugin slug — such a plugin must not be
// deactivated while that theme is active.
func (e *Engine) ActiveThemeRequiresPlugin(slug string) bool {
	for _, r := range e.themeRequires(e.ActiveThemeName()).Plugins {
		if r.Slug == slug && !r.Optional {
			return true
		}
	}
	return false
}

// ActivateThemeDependencies activates every present-but-inactive plugin the
// theme requires and persists the choice, so the theme's declared needs are met.
// Returns the slugs that were activated. Missing/version-mismatch dependencies
// cannot be resolved at runtime and are left for the caller to report.
func (e *Engine) ActivateThemeDependencies(report ThemeDepReport) []string {
	var activated []string
	for _, slug := range report.InactiveDeps() {
		p, ok := e.PluginManager.FindBySlug(slug)
		if !ok {
			continue
		}
		if e.PluginManager.Activate(p.Name(), e) {
			if e.Options != nil {
				_ = e.Options.Set("plugin_active_"+p.Name(), "true")
			}
			activated = append(activated, p.Name())
		}
	}
	return activated
}

// ReconcileActiveThemeDependencies runs at boot: it auto-activates the active
// theme's inactive required plugins (respecting the theme's declared need) and
// logs any dependency that cannot be resolved at runtime (missing from build or
// version-incompatible) — the site still boots and the admin surfaces a warning.
func (e *Engine) ReconcileActiveThemeDependencies() {
	report := e.ActiveThemeDependencies()
	for _, name := range e.ActivateThemeDependencies(report) {
		logger.Info("Auto-activated plugin required by active theme", "plugin", name, "theme", report.Slug)
	}
	if !report.CoreSatisfied {
		logger.Error("Active theme requires an incompatible core version",
			"theme", report.Slug, "constraint", report.CoreConstraint, "core", version.String())
	}
	for _, d := range report.BlockingDeps() {
		logger.Error("Active theme has an unmet plugin dependency — rebuild with the plugin or fix the version",
			"theme", report.Slug, "plugin", d.Slug, "state", string(d.State),
			"constraint", d.Constraint, "installed", d.InstalledVersion)
	}
}
