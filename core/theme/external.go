// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package theme

import (
	"fmt"
	"strings"
)

// External resource kinds. Each names what a theme loads from another host
// and maps to a Content-Security-Policy fetch directive (style-src, font-src,
// img-src, media-src, frame-src). There is no script kind: theme packages
// may not run scripts.
const (
	ExternalStyle = "style"
	ExternalFont  = "font"
	ExternalImage = "image"
	ExternalMedia = "media"
	ExternalFrame = "frame"
)

const (
	// MaxExternalHosts is the most [[external]] entries a theme may declare.
	MaxExternalHosts = 20
	// maxExternalPurpose bounds the purpose shown to administrators.
	maxExternalPurpose = 200
)

// ExternalKinds lists the valid kinds in a stable order.
var ExternalKinds = []string{ExternalStyle, ExternalFont, ExternalImage, ExternalMedia, ExternalFrame}

// ExternalConfig maps an [[external]] entry in theme.toml: another host the
// theme loads resources from, what it loads, and why. Administrators see the
// purpose when they install or switch to the theme, and the site's
// Content-Security-Policy allows the host for the declared kinds only.
//
// Host is a lowercase domain name. A "*." prefix covers every subdomain but
// not the domain itself, as in a Content-Security-Policy source.
type ExternalConfig struct {
	Host    string   `toml:"host"`
	Kinds   []string `toml:"kinds"`
	Purpose string   `toml:"purpose"`
}

// Matches reports whether host (a hostname without port) is covered by the
// entry.
func (e ExternalConfig) Matches(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "" {
		return false
	}
	if rest, ok := strings.CutPrefix(e.Host, "*."); ok {
		return strings.HasSuffix(host, "."+rest)
	}
	return host == e.Host
}

// Allows reports whether the entry declares kind.
func (e ExternalConfig) Allows(kind string) bool {
	for _, k := range e.Kinds {
		if k == kind {
			return true
		}
	}
	return false
}

// ExternalHosts returns the entries that cover host.
func (c *FileConfig) ExternalHosts(host string) []ExternalConfig {
	if c == nil {
		return nil
	}
	var out []ExternalConfig
	for _, entry := range c.External {
		if entry.Matches(host) {
			out = append(out, entry)
		}
	}
	return out
}

// ValidateExternal checks the [[external]] entries and returns one error per
// problem, or nil when they are valid.
func (c *FileConfig) ValidateExternal() []error {
	if c == nil {
		return nil
	}
	var errs []error
	if len(c.External) > MaxExternalHosts {
		errs = append(errs, fmt.Errorf("[[external]] has %d entries, at most %d are allowed", len(c.External), MaxExternalHosts))
	}
	seen := map[string]bool{}
	for i, entry := range c.External {
		where := fmt.Sprintf("[[external]] #%d", i+1)
		if err := validExternalHost(entry.Host); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", where, err))
		} else if seen[entry.Host] {
			errs = append(errs, fmt.Errorf("%s: host %q is declared twice", where, entry.Host))
		}
		seen[entry.Host] = true

		if len(entry.Kinds) == 0 {
			errs = append(errs, fmt.Errorf("%s: kinds is empty; use %s", where, strings.Join(ExternalKinds, ", ")))
		}
		kinds := map[string]bool{}
		for _, kind := range entry.Kinds {
			switch {
			case kind == "script":
				errs = append(errs, fmt.Errorf("%s: kind %q is not allowed: themes may not load scripts", where, kind))
			case !validExternalKind(kind):
				errs = append(errs, fmt.Errorf("%s: unknown kind %q; use %s", where, kind, strings.Join(ExternalKinds, ", ")))
			case kinds[kind]:
				errs = append(errs, fmt.Errorf("%s: kind %q is listed twice", where, kind))
			}
			kinds[kind] = true
		}

		purpose := strings.TrimSpace(entry.Purpose)
		switch {
		case purpose == "":
			errs = append(errs, fmt.Errorf("%s: purpose is required", where))
		case len([]rune(purpose)) > maxExternalPurpose:
			errs = append(errs, fmt.Errorf("%s: purpose is longer than %d characters", where, maxExternalPurpose))
		}
	}
	return errs
}

func validExternalKind(kind string) bool {
	for _, k := range ExternalKinds {
		if k == kind {
			return true
		}
	}
	return false
}

// validExternalHost accepts a lowercase domain name of at least two labels,
// optionally prefixed with "*.". Schemes, ports, paths, IP addresses and a
// bare "*" are rejected.
func validExternalHost(host string) error {
	if host == "" {
		return fmt.Errorf("host is required")
	}
	name, _ := strings.CutPrefix(host, "*.")
	if len(host) > 253 || strings.ContainsAny(name, "*/:@?#[] ") {
		return fmt.Errorf("host %q must be a domain name such as fonts.example.com, without scheme, port or path", host)
	}
	labels := strings.Split(name, ".")
	if len(labels) < 2 {
		return fmt.Errorf("host %q must have at least two labels", host)
	}
	for _, label := range labels {
		if !validHostLabel(label) {
			return fmt.Errorf("host %q is not a lowercase domain name", host)
		}
	}
	if strings.Trim(labels[len(labels)-1], "0123456789") == "" {
		return fmt.Errorf("host %q must be a domain name, not an IP address", host)
	}
	return nil
}

func validHostLabel(label string) bool {
	if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
		return false
	}
	for i := 0; i < len(label); i++ {
		ch := label[i]
		if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && ch != '-' {
			return false
		}
	}
	return true
}
