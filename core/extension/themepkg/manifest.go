// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package themepkg

import (
	"path"
	"strings"

	"github.com/0xmattg/go-press/core/plugin"
	"github.com/0xmattg/go-press/core/theme"
	"github.com/0xmattg/go-press/pkg/semver"

	"github.com/BurntSushi/toml"
)

// checkManifest checks theme.toml and fills the report's identity and
// external hosts.
func (c *checker) checkManifest() {
	f := c.byName["theme.toml"]
	if f == nil {
		c.problem(RuleManifestMissing, "theme.toml", 0, "the package has no theme.toml")
		return
	}
	var cfg theme.FileConfig
	meta, err := toml.Decode(string(f.data), &cfg)
	if err != nil {
		c.problem(RuleManifestSyntax, f.name, 0, "invalid TOML: %v", err)
		return
	}
	c.manifest = &cfg
	for _, key := range meta.Undecoded() {
		c.warn(RuleManifestUnknown, f.name, 0, "unknown key %s is ignored", key.String())
	}

	info := cfg.Theme
	c.report.ID.Slug = info.Slug
	c.report.ID.Version = info.Version
	c.report.External = append(c.report.External, cfg.External...)

	switch {
	case info.Slug == "":
		c.problem(RuleManifestSlug, f.name, 0, "[theme] slug is required")
	case !plugin.ValidSlug(info.Slug):
		c.problem(RuleManifestSlug, f.name, 0, "[theme] slug %q must be 2–64 lowercase letters and digits joined by single hyphens", info.Slug)
	case c.opts.Reserved != nil && c.opts.Reserved(info.Slug):
		c.problem(RuleManifestReserved, f.name, 0, "the slug %q is taken by a theme or plugin built into this GoPress", info.Slug)
	}
	if strings.TrimSpace(info.Name) == "" {
		c.problem(RuleManifestName, f.name, 0, "[theme] name is required")
	}
	switch {
	case info.Version == "":
		c.problem(RuleManifestVersion, f.name, 0, "[theme] version is required")
	case !semver.Valid(info.Version) || strings.ContainsAny(info.Version, " \t"):
		c.problem(RuleManifestVersion, f.name, 0, "[theme] version %q is not a semantic version", info.Version)
	}

	core := strings.TrimSpace(cfg.Requires.Core)
	switch {
	case core == "":
		c.problem(RuleManifestCore, f.name, 0, "[requires] core is required, for example \">=0.7.22\"")
	case !semver.ValidConstraint(core):
		c.problem(RuleManifestCore, f.name, 0, "[requires] core %q is not a version constraint", core)
	case c.opts.CoreVersion != "":
		if ok, err := semver.Satisfies(c.opts.CoreVersion, core); err != nil || !ok {
			c.problem(RuleManifestCoreUnmet, f.name, 0, "GoPress %s does not satisfy [requires] core %q", c.opts.CoreVersion, core)
		}
	}
	for _, req := range cfg.Requires.Plugins {
		switch {
		case !plugin.ValidSlug(req.Slug):
			c.problem(RuleManifestRequires, f.name, 0, "[[requires.plugins]] slug %q is not a valid plugin slug", req.Slug)
		case req.Version != "" && !semver.ValidConstraint(req.Version):
			c.problem(RuleManifestRequires, f.name, 0, "[[requires.plugins]] %s version %q is not a version constraint", req.Slug, req.Version)
		}
	}

	schema, err := cfg.SettingsSchema()
	if err != nil {
		c.problem(RuleManifestSettings, f.name, 0, "%v", err)
	}
	for _, err := range cfg.ValidateExternal() {
		c.problem(RuleExternalInvalid, f.name, 0, "%v", err)
	}
	// An image setting's default is loaded by the page like any image.
	if schema != nil {
		for _, field := range schema.Fields {
			if field.Type == theme.SettingImage {
				c.checkExternalURL(field.Default, theme.ExternalImage, f.name, 0)
			}
		}
	}

	for _, pt := range cfg.PageTemplates {
		page := "templates/pages/" + pt.Template + ".tmpl"
		if pt.Template == "" || strings.Contains(pt.Template, "/") || c.byName[page] == nil {
			c.problem(RuleManifestPageTemplate, f.name, 0, "[[page_templates]] %q has no file %s", pt.Template, page)
		}
	}

	if shot := info.Screenshot; shot != "" {
		clean := path.Clean(shot)
		switch {
		case clean != shot || strings.HasPrefix(shot, "/") || c.byName[shot] == nil:
			c.problem(RuleManifestScreenshot, f.name, 0, "[theme] screenshot %q is not a file in the package", shot)
		case !imageExtensions[strings.ToLower(path.Ext(shot))]:
			c.problem(RuleManifestScreenshot, f.name, 0, "[theme] screenshot %q is not an image", shot)
		}
	}
}
