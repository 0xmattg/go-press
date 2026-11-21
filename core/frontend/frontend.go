// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

// Package frontend holds the front-end assets core ships for themes: the
// interaction script themes configure with data-gp-* attributes. The files
// are embedded in the binary and served under /static/gopress/, a path core
// reserves the way it reserves /static/admin/.
package frontend

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"html/template"
	"io/fs"
	"path"
	"strings"
	"sync"
)

//go:embed ui.js
var assets embed.FS

// Dir is the directory under /static that core reserves for these assets.
const Dir = "gopress"

// ScriptName is the interaction script's file name.
const ScriptName = "ui.js"

// contentTypes maps the served extensions to their media types. The type is
// set explicitly so it does not depend on the system's MIME database.
var contentTypes = map[string]string{
	".js": "text/javascript; charset=utf-8",
}

// FS returns the embedded assets, named by their path below /static/gopress/.
func FS() fs.FS {
	return assets
}

// ContentType returns the media type for an asset, with ok=false when
// there is no asset of that name.
func ContentType(name string) (string, bool) {
	name = strings.TrimPrefix(path.Clean("/"+name), "/")
	if !fs.ValidPath(name) {
		return "", false
	}
	info, err := fs.Stat(assets, name)
	if err != nil || info.IsDir() {
		return "", false
	}
	ctype, ok := contentTypes[path.Ext(name)]
	return ctype, ok
}

var (
	digestOnce sync.Once
	digest     string
)

// scriptDigest is a short hash of the script, used as its cache-busting
// version: the URL changes exactly when the script does.
func scriptDigest() string {
	digestOnce.Do(func() {
		data, err := assets.ReadFile(ScriptName)
		if err != nil {
			panic("frontend: " + ScriptName + " is not embedded")
		}
		sum := sha256.Sum256(data)
		digest = hex.EncodeToString(sum[:6])
	})
	return digest
}

// ScriptURL returns the site-relative URL of the interaction script.
func ScriptURL() string {
	return "/static/" + Dir + "/" + ScriptName + "?v=" + scriptDigest()
}

// ScriptTag returns the element that loads the interaction script. It is
// deferred, so a theme may place it in <head> or before </body>.
func ScriptTag() template.HTML {
	return template.HTML(`<script src="` + ScriptURL() + `" defer></script>`)
}
