// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package themepkg

import (
	"bytes"
	"encoding/json"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/0xmattg/go-press/core/frontend"
	"github.com/0xmattg/go-press/pkg/sign"

	"github.com/BurntSushi/toml"
)

// Allowed file extensions. Text files must be UTF-8; binary files must start
// with their format's signature.
var (
	textExtensions = map[string]bool{
		".tmpl": true, ".html": true, ".css": true, ".toml": true, ".json": true,
		".svg": true, ".txt": true, ".md": true,
	}
	binaryExtensions = map[string]bool{
		".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
		".avif": true, ".ico": true, ".woff": true, ".woff2": true, ".ttf": true, ".otf": true,
	}
	imageExtensions = map[string]bool{
		".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
		".avif": true, ".svg": true, ".ico": true,
	}
	scriptExtensions = map[string]bool{".js": true, ".mjs": true, ".cjs": true}
)

// Top-level directories and root files a package may have.
var (
	topDirs   = map[string]bool{"templates": true, "static": true, "locales": true, "demo": true, sign.SignaturesDir: true}
	rootFiles = map[string]bool{"theme.toml": true, "README.md": true, "LICENSE": true, "CHANGELOG.md": true}
)

// coreStaticDirs are the directories under /static/ that core serves itself
// (admin assets, uploads, front-end assets): a theme's files there can never
// be requested.
var coreStaticDirs = []string{"admin/", "uploads/", frontend.Dir + "/"}

// checkFile applies the type, placement and content rules to one file.
func (c *checker) checkFile(f *file) {
	name := f.name
	ext := strings.ToLower(path.Ext(name))
	top, rest, nested := strings.Cut(name, "/")

	switch {
	case scriptExtensions[ext]:
		c.problem(RuleScriptFile, name, 0, "script files are not allowed in theme packages")
		return
	case name == "LICENSE":
	case !textExtensions[ext] && !binaryExtensions[ext]:
		c.problem(RuleFileType, name, 0, "files of type %q are not allowed", ext)
		return
	}

	switch {
	case !nested:
		if !rootFiles[name] && !imageExtensions[ext] {
			c.problem(RuleFilePlace, name, 0, "only theme.toml, README.md, CHANGELOG.md, LICENSE and a screenshot may be at the top level")
			return
		}
	case !topDirs[top]:
		c.problem(RuleFilePlace, name, 0, "%s/ is not a package directory; use templates/, static/, locales/ or demo/", top)
		return
	case top == "templates":
		c.checkTemplatePlace(f, rest, ext)
	case ext == ".tmpl":
		c.problem(RuleFilePlace, name, 0, "templates belong under templates/")
		return
	case top == "locales":
		if !isLocaleFile(rest) {
			c.problem(RuleFilePlace, name, 0, "locales/ holds <lang>.json and admin/<lang>.json files only")
			return
		}
		c.checkLocale(f)
	case top == sign.SignaturesDir:
		if name != sign.PublisherSignatureFile && name != sign.MarketSignatureFile {
			c.problem(RuleFilePlace, name, 0, "signatures/ holds publisher.json and market.json only")
			return
		}
	case ext == ".toml" && top != "demo":
		c.problem(RuleFilePlace, name, 0, ".toml files are allowed only as theme.toml and under demo/")
		return
	case ext == ".toml":
		var v map[string]interface{}
		if _, err := toml.Decode(string(f.data), &v); err != nil {
			c.problem(RuleFileTOML, name, 0, "invalid TOML: %v", err)
		}
	}

	if top == "static" {
		c.checkShadowed(rest)
	}
	c.checkContent(f, ext)
}

// checkShadowed warns, once per directory, about files under a static/
// directory core serves itself. It is a warning: such a file is unreachable,
// not unsafe, and an installed package must not turn broken over it.
func (c *checker) checkShadowed(rest string) {
	for _, dir := range coreStaticDirs {
		if strings.HasPrefix(rest, dir) {
			c.warn(RuleFileShadowed, "static/"+dir, 0, "/static/%s is served by GoPress itself; files here can never be requested", dir)
			return
		}
	}
}

// checkTemplatePlace checks a file under templates/; rest is its path below
// templates/.
func (c *checker) checkTemplatePlace(f *file, rest, ext string) {
	dir, base := path.Split(rest)
	switch {
	case dir == "admin/" || strings.HasPrefix(rest, "admin/"):
		c.problem(RuleTemplateAdmin, f.name, 0, "theme packages cannot provide admin templates; declare settings in theme.toml instead")
	case ext != ".tmpl":
		c.problem(RuleTemplatePlace, f.name, 0, "only .tmpl files belong under templates/")
	case dir != "layouts/" && dir != "partials/" && dir != "pages/":
		c.problem(RuleTemplatePlace, f.name, 0, "templates go directly in templates/layouts/, templates/partials/ or templates/pages/")
	case len(f.data) > MaxTemplateSize:
		c.problem(RuleSizeTemplate, f.name, 0, "the template is larger than %s", formatSize(MaxTemplateSize))
	case dir == "layouts/" && base != "base.tmpl":
		c.warn(RuleTemplateUnused, f.name, 0, "only templates/layouts/base.tmpl is loaded")
	}
}

func isLocaleFile(rest string) bool {
	rest = strings.TrimPrefix(rest, "admin/")
	return !strings.Contains(rest, "/") && strings.HasSuffix(rest, ".json") && len(rest) > len(".json")
}

// checkLocale requires a locale file to be a JSON object of strings.
func (c *checker) checkLocale(f *file) {
	var messages map[string]string
	if err := json.Unmarshal(f.data, &messages); err != nil {
		c.problem(RuleFileJSON, f.name, 0, "a locale file must be a JSON object of strings: %v", err)
	}
}

// checkContent checks that text files are UTF-8 and binary files match
// their extension.
func (c *checker) checkContent(f *file, ext string) {
	if textExtensions[ext] || f.name == "LICENSE" {
		if !utf8.Valid(f.data) || bytes.IndexByte(f.data, 0) >= 0 {
			c.problem(RuleFileContent, f.name, 0, "a %s file must be UTF-8 text", strings.TrimPrefix(ext, "."))
		}
		return
	}
	if !matchesSignature(ext, f.data) {
		c.problem(RuleFileContent, f.name, 0, "the content is not a %s file", strings.TrimPrefix(ext, "."))
	}
}

// matchesSignature reports whether data starts with the signature of the
// binary format ext names.
func matchesSignature(ext string, data []byte) bool {
	has := func(prefix string) bool { return bytes.HasPrefix(data, []byte(prefix)) }
	switch ext {
	case ".png":
		return has("\x89PNG\r\n\x1a\n")
	case ".jpg", ".jpeg":
		return has("\xff\xd8\xff")
	case ".gif":
		return has("GIF87a") || has("GIF89a")
	case ".webp":
		return len(data) >= 12 && has("RIFF") && string(data[8:12]) == "WEBP"
	case ".avif":
		if len(data) < 12 || string(data[4:8]) != "ftyp" {
			return false
		}
		switch string(data[8:12]) {
		case "avif", "avis", "mif1", "msf1":
			return true
		}
		return false
	case ".ico":
		return has("\x00\x00\x01\x00")
	case ".woff":
		return has("wOFF")
	case ".woff2":
		return has("wOF2")
	case ".ttf":
		return has("\x00\x01\x00\x00") || has("true") || has("ttcf")
	case ".otf":
		return has("OTTO")
	}
	return false
}
