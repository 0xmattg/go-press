// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package core

import (
	"regexp"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"github.com/0xmattg/go-press/core/hook"
	coreTheme "github.com/0xmattg/go-press/core/theme"
	"github.com/0xmattg/go-press/pkg/logger"
)

// Content-Security-Policy for theme packages (D1 4.11, GIP-A0001b 4.3.6).
//
// Theme packages may not contain scripts; the package check enforces that.
// While a theme package is active, every front-end response also carries a
// Content-Security-Policy, so a script that slipped past the check would not
// run. Themes built into GoPress get no policy: they ship scripts and do not
// declare what they load.
//
// The policy is built when the theme is activated, from its [[external]]
// declaration, and added by a middleware in front of the page cache, which
// stores bodies but not headers, so cached pages get it too. It uses no
// nonce: a cached body cannot match a nonce that changes per response.
//
//   - script-src allows this origin and what plugins declare (hosts and
//     hashes) through the frontend.csp filter, never inline scripts in
//     general or eval.
//   - style-src and font-src allow this origin and the hosts the theme
//     declares for styles and fonts; inline styles are allowed, since
//     content, embeds and plugin templates use style attributes.
//   - img-src, media-src and frame-src allow any https source: images,
//     videos and embeds in content come from hosts no theme can declare.
//   - connect-src allows this origin and what plugins declare.
//
// Admin, API and Swagger paths are left alone, and a handler that sets its
// own policy (a plugin's sign-in page, a package's static SVG) keeps it.

// CSPSources are the sources plugins add to the theme package policy
// through the hook.FrontendCSP filter. Each list holds CSP source
// expressions: hosts ("https://cdn.example.com", "*.example.com"),
// "'self'", script hashes ("'sha256-…'"), and, outside Script, the schemes
// "https:", "data:" and "blob:". Sources that would weaken the script
// protection ('unsafe-inline', 'unsafe-eval', "*", "https:" for scripts) or
// that are malformed are dropped and logged.
type CSPSources struct {
	Script  []string
	Style   []string
	Font    []string
	Image   []string
	Media   []string
	Frame   []string
	Connect []string
}

// cspDirective is one directive of the policy with its base sources.
type cspDirective struct {
	name    string
	sources []string
	// plugin returns the plugin sources for this directive.
	plugin func(CSPSources) []string
}

// themeCSP is the base policy of the active theme package.
type themeCSP struct {
	directives []cspDirective
}

// themePackageCSP returns the base policy for t, or nil when t is not a
// theme package.
func themePackageCSP(t coreTheme.Theme) *themeCSP {
	if _, ok := coreTheme.PackageInfoOf(t); !ok {
		return nil
	}
	var external []coreTheme.ExternalConfig
	if fp, ok := t.(coreTheme.FileConfigProvider); ok && fp.FileConfig() != nil {
		external = fp.FileConfig().External
	}
	declared := func(kind string) []string {
		var out []string
		for _, e := range external {
			if e.Allows(kind) && e.Host != "" {
				out = append(out, "https://"+e.Host)
			}
		}
		return out
	}
	return &themeCSP{directives: []cspDirective{
		{name: "default-src", sources: []string{"'self'"}},
		{name: "script-src", sources: []string{"'self'"}, plugin: func(s CSPSources) []string { return s.Script }},
		{name: "style-src", sources: append([]string{"'self'", "'unsafe-inline'"}, declared(coreTheme.ExternalStyle)...), plugin: func(s CSPSources) []string { return s.Style }},
		{name: "font-src", sources: append([]string{"'self'", "data:"}, declared(coreTheme.ExternalFont)...), plugin: func(s CSPSources) []string { return s.Font }},
		{name: "img-src", sources: []string{"'self'", "data:", "https:"}, plugin: func(s CSPSources) []string { return s.Image }},
		{name: "media-src", sources: []string{"'self'", "https:"}, plugin: func(s CSPSources) []string { return s.Media }},
		{name: "frame-src", sources: []string{"'self'", "https:"}, plugin: func(s CSPSources) []string { return s.Frame }},
		{name: "connect-src", sources: []string{"'self'"}, plugin: func(s CSPSources) []string { return s.Connect }},
		{name: "object-src", sources: []string{"'none'"}},
		{name: "base-uri", sources: []string{"'self'"}},
	}}
}

// header renders the policy with the plugin sources added.
func (p *themeCSP) header(plugin CSPSources) string {
	parts := make([]string, 0, len(p.directives))
	for _, d := range p.directives {
		sources := append([]string(nil), d.sources...)
		if d.plugin != nil {
			for _, src := range d.plugin(plugin) {
				if validCSPSource(d.name, src) {
					sources = append(sources, src)
				} else {
					warnInvalidCSPSource(d.name, src)
				}
			}
		}
		parts = append(parts, d.name+" "+strings.Join(dedupe(sources), " "))
	}
	return strings.Join(parts, "; ")
}

func dedupe(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := values[:0]
	for _, v := range values {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

var (
	cspHashPattern = regexp.MustCompile(`^'sha(256|384|512)-[A-Za-z0-9+/]+={0,2}'$`)
	// A host source: an optional https:// or http:// scheme, an optional
	// "*." wildcard, a host name, an optional port and an optional path.
	cspHostPattern = regexp.MustCompile(`^(https?://)?(\*\.)?[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*(:[0-9]{1,5})?(/[A-Za-z0-9._~%/-]*)?$`)
)

// validCSPSource reports whether a plugin may add src to directive. Scripts
// accept hosts, 'self' and hashes only; other directives also accept the
// https:, data: and blob: schemes. Keywords that switch protections off
// ('unsafe-inline', 'unsafe-eval', 'unsafe-hashes', 'strict-dynamic',
// 'none'), wildcards and anything with separators are refused.
func validCSPSource(directive, src string) bool {
	switch {
	case src == "'self'":
		return true
	case cspHashPattern.MatchString(src):
		return directive == "script-src" || directive == "style-src"
	case src == "https:" || src == "data:" || src == "blob:":
		return directive != "script-src"
	case strings.HasPrefix(src, "http://"):
		// Plain HTTP is accepted only for a loopback host, for local tools.
		host := strings.TrimPrefix(src, "http://")
		if i := strings.IndexAny(host, ":/"); i >= 0 {
			host = host[:i]
		}
		return (host == "localhost" || host == "127.0.0.1") && cspHostPattern.MatchString(src)
	default:
		return cspHostPattern.MatchString(src)
	}
}

var reportedCSPSources sync.Map

// warnInvalidCSPSource logs a refused plugin source once per process.
func warnInvalidCSPSource(directive, src string) {
	if _, seen := reportedCSPSources.LoadOrStore(directive+" "+src, true); seen {
		return
	}
	logger.Warn("Content-Security-Policy source from a plugin refused", "directive", directive, "source", src)
}

// cspApplies reports whether the theme package policy belongs on a response
// to path: everything but the admin, the JSON API and the API explorer.
func cspApplies(path string) bool {
	switch {
	case path == "/admin" || strings.HasPrefix(path, "/admin/"):
		return false
	case strings.HasPrefix(path, "/api/"), strings.HasPrefix(path, "/swagger/"):
		return false
	}
	return true
}

// setThemeCSP records the policy of the theme just activated: the theme
// package's, or none for a theme built into GoPress.
func (e *Engine) setThemeCSP(t coreTheme.Theme) {
	e.themeCSP.Store(themePackageCSP(t))
}

// ThemeContentSecurityPolicy returns the policy front-end responses get on
// path, with the plugin sources c asks for, or "" when none applies.
func (e *Engine) ThemeContentSecurityPolicy(c *gin.Context) string {
	policy := e.themeCSP.Load()
	if policy == nil || c == nil || c.Request == nil || !cspApplies(c.Request.URL.Path) {
		return ""
	}
	var plugin CSPSources
	if e.Hooks != nil {
		if v, ok := e.Hooks.ApplyFilter(hook.FrontendCSP, CSPSources{}, c).(CSPSources); ok {
			plugin = v
		}
	}
	return policy.header(plugin)
}

// contentSecurityPolicyMiddleware adds the theme package policy. It runs
// before the page cache, so cached responses get it as well; a handler that
// sets its own policy afterwards replaces it.
func (e *Engine) contentSecurityPolicyMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if policy := e.ThemeContentSecurityPolicy(c); policy != "" {
			c.Header("Content-Security-Policy", policy)
		}
		c.Next()
	}
}
