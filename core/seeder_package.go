// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package core

import (
	"fmt"
	"io/fs"
	"net/url"
	"strings"

	"github.com/0xmattg/go-press/core/assets"
	coreTheme "github.com/0xmattg/go-press/core/theme"
)

// maxSeedProblems bounds the problems a rejected package seed lists.
const maxSeedProblems = 20

// packageSeedPolicy restricts the demo data of a theme package. A package
// comes from a third party, and a demo import replaces the site's options,
// so its seed is untrusted:
//
//   - it may only set the settings the theme declares in [[settings]], each
//     validated against its declaration; any other key could change how the
//     site works;
//   - the server never downloads its images: an image_url is a file of the
//     package's static/ directory or an https URL on a host the theme
//     declares for images in [[external]], kept as it is;
//   - content bodies are sanitized as when an editor saves them.
//
// The whole seed is checked before anything is deleted, and every problem
// is reported.
type packageSeedPolicy struct {
	slug     string
	settings *coreTheme.SettingsSchema
	config   *coreTheme.FileConfig
	static   fs.FS
}

// packageSeedPolicyFor returns the policy for the demo data of package
// theme t.
func (e *Engine) packageSeedPolicyFor(slug string, t coreTheme.Theme) (*packageSeedPolicy, error) {
	cfg, err := e.themeFileConfig(slug, t)
	if err != nil {
		return nil, fmt.Errorf("theme %q: %w", slug, err)
	}
	schema, err := cfg.SettingsSchema()
	if err != nil {
		return nil, fmt.Errorf("invalid settings declaration in theme %q: %w", slug, err)
	}
	return &packageSeedPolicy{slug: slug, settings: schema, config: cfg, static: coreTheme.StaticFS(t)}, nil
}

// check validates prepared against the policy, stores the validated setting
// values, and marks it untrusted.
func (p *packageSeedPolicy) check(prepared *preparedSeedData) error {
	var problems []string
	add := func(format string, args ...interface{}) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}
	for i, s := range prepared.data.Settings {
		field, ok := p.settings.Field(s.Key)
		if !ok {
			add("setting %q is not declared in the theme's [[settings]]", s.Key)
			continue
		}
		value, err := field.Validate(s.Value)
		if err != nil {
			add("%v", err)
			continue
		}
		prepared.data.Settings[i].Value = value
	}
	for _, c := range prepared.data.Contents {
		if err := p.checkImage(c.ImageURL); err != nil {
			add("content %q: %v", c.Title, err)
		}
	}
	if len(problems) == 0 {
		prepared.untrusted = true
		return nil
	}
	shown := problems
	if len(shown) > maxSeedProblems {
		shown = append(shown[:maxSeedProblems:maxSeedProblems], fmt.Sprintf("and %d more", len(problems)-maxSeedProblems))
	}
	return fmt.Errorf("demo data of theme package %q is not allowed: %s", p.slug, strings.Join(shown, "; "))
}

// checkImage accepts an empty image_url, a /static/ path to a file in the
// package, or an https URL on a host declared for images.
func (p *packageSeedPolicy) checkImage(raw string) error {
	switch {
	case raw == "":
		return nil
	case strings.HasPrefix(raw, "/static/"):
		name := strings.TrimPrefix(raw, "/static/")
		// uploads/ and admin/ are served from elsewhere, never from the theme.
		if strings.HasPrefix(name, "uploads/") || strings.HasPrefix(name, "admin/") || !assets.Exists(p.static, name) {
			return fmt.Errorf("image %q is not a file in the theme's static directory", raw)
		}
		return nil
	case strings.HasPrefix(raw, "https://"):
		u, err := url.Parse(raw)
		if err != nil || u.Hostname() == "" || u.User != nil || u.Port() != "" {
			return fmt.Errorf("image %q is not a valid https URL", raw)
		}
		for _, entry := range p.config.ExternalHosts(u.Hostname()) {
			if entry.Allows(coreTheme.ExternalImage) {
				return nil
			}
		}
		return fmt.Errorf("image %q: host %s is not declared in [[external]] for images", raw, u.Hostname())
	default:
		return fmt.Errorf("image %q must be a /static/ path in the theme or an https URL", raw)
	}
}
