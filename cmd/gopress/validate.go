package main

import (
	"fmt"
	"os"
	"path/filepath"

	corePlugin "github.com/0xmattg/go-press/core/plugin"
	coreTheme "github.com/0xmattg/go-press/core/theme"
	"github.com/0xmattg/go-press/pkg/semver"
)

// validateExtensionDeps checks, at codegen time, that every theme's declared
// [requires] can be satisfied by the plugins in this build and that all
// versions/constraints are valid semver. It returns human-readable problems; the
// caller prints them as warnings. The authoritative gate is at runtime (boot and
// theme switch) — this just surfaces problems early, before you ship a build
// whose active theme can't work.
func validateExtensionDeps(root string, themes, plugins []string) []string {
	// Map the plugin's runtime slug (plugin.toml [plugin].slug — which equals its
	// Name()) to its version. Themes reference plugins by slug, not by directory
	// name, and the two can differ (dir "multilang" vs slug "multi-language").
	pluginVer := make(map[string]string, len(plugins))
	var problems []string
	for _, dir := range plugins {
		data, err := os.ReadFile(filepath.Join(root, pluginsRelDir, dir, pluginMarker))
		if err != nil {
			continue
		}
		meta := corePlugin.ParseMetaString(string(data))
		slug := meta.Slug
		if slug == "" {
			problems = append(problems, fmt.Sprintf("plugin dir %q is missing [plugin].slug in plugin.toml (needed for theme dependencies)", dir))
			slug = dir
		}
		pluginVer[slug] = meta.Version
		if meta.Version != "" && !semver.Valid(meta.Version) {
			problems = append(problems, fmt.Sprintf("plugin %q has invalid semver version %q", slug, meta.Version))
		}
	}
	for _, t := range themes {
		cfg, err := coreTheme.LoadFileConfig(filepath.Join(root, themesRelDir, t))
		if err != nil {
			continue
		}
		if cfg.Theme.Version != "" && !semver.Valid(cfg.Theme.Version) {
			problems = append(problems, fmt.Sprintf("theme %q has invalid semver version %q", t, cfg.Theme.Version))
		}
		req := cfg.Requires
		if req.Core != "" && !semver.ValidConstraint(req.Core) {
			problems = append(problems, fmt.Sprintf("theme %q has an invalid [requires].core constraint %q", t, req.Core))
		}
		for _, r := range req.Plugins {
			if r.Version != "" && !semver.ValidConstraint(r.Version) {
				problems = append(problems, fmt.Sprintf("theme %q requires plugin %q with an invalid version constraint %q", t, r.Slug, r.Version))
			}
			ver, present := pluginVer[r.Slug]
			if !present {
				problems = append(problems, fmt.Sprintf("theme %q requires plugin %q which is not in this build (add it under plugins/ and rebuild)", t, r.Slug))
				continue
			}
			if r.Version != "" && ver != "" {
				if ok, err := semver.Satisfies(ver, r.Version); err == nil && !ok {
					problems = append(problems, fmt.Sprintf("theme %q requires plugin %q %s but %s is present", t, r.Slug, r.Version, ver))
				}
			}
		}
	}
	return problems
}

// warnExtensionDeps prints dependency problems to stderr as warnings.
func warnExtensionDeps(root string, themes, plugins []string) {
	for _, p := range validateExtensionDeps(root, themes, plugins) {
		fmt.Fprintf(os.Stderr, "gopress: warning: %s\n", p)
	}
}
