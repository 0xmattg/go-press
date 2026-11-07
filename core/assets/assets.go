// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

// Package assets resolves where a theme's or plugin's own files are read
// from. A module can ship its templates, static files, locales and demo data
// inside the binary (usually an embed.FS); modules that do not keep reading
// them from their directory in the source tree.
//
// Paths inside a module filesystem are slash-separated and relative to the
// module root, for example "templates/admin/settings.tmpl".
package assets

import (
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"os"
	"path"
)

// Provider is implemented by a plugin that ships its own files, usually an
// embed.FS rooted at the plugin directory. The root follows the module
// layout: templates/, static/, locales/ and so on.
//
// Themes do not implement Provider; they pass their files to
// theme.BaseTheme.InitBaseFS, because BaseTheme parses templates inside the
// theme constructor, before core holds the theme value.
type Provider interface {
	Assets() fs.FS
}

// Resolve returns the filesystem a module's files are read from.
//
//   - builtin == nil: the module ships no files, so diskDir is used, as
//     before modules could embed their files.
//   - preferDisk and diskDir is a directory: diskDir is used, so edits to
//     templates and static files show without rebuilding the binary. Core
//     sets preferDisk outside release mode.
//   - otherwise: builtin.
//
// The disk directory replaces builtin as a whole rather than being merged
// with it, so a debug run sees exactly the files in the source tree.
func Resolve(diskDir string, builtin fs.FS, preferDisk bool) fs.FS {
	if builtin == nil {
		return DirFS(diskDir)
	}
	if preferDisk && diskDir != "" {
		if info, err := os.Stat(diskDir); err == nil && info.IsDir() {
			return os.DirFS(diskDir)
		}
	}
	return builtin
}

// DirFS is os.DirFS except that an empty dir means the working directory,
// matching how filepath.Join("", name) resolved before. os.DirFS("") would
// root the filesystem at "/".
func DirFS(dir string) fs.FS {
	if dir == "" {
		dir = "."
	}
	return os.DirFS(dir)
}

// Sub returns the subtree of fsys rooted at dir, or nil when fsys is nil.
func Sub(fsys fs.FS, dir string) fs.FS {
	if fsys == nil {
		return nil
	}
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		return nil
	}
	return sub
}

// Exists reports whether name is a regular file in fsys.
func Exists(fsys fs.FS, name string) bool {
	if fsys == nil || !fs.ValidPath(name) {
		return false
	}
	info, err := fs.Stat(fsys, name)
	return err == nil && !info.IsDir()
}

// ParseFiles parses the named files from fsys into t with the naming rules of
// html/template's ParseFiles: each file becomes a template named after its
// base name, and a file whose base name equals t's name is parsed into t
// itself. Unlike template.ParseFS, names are taken literally rather than as
// glob patterns.
func ParseFiles(t *template.Template, fsys fs.FS, names ...string) (*template.Template, error) {
	if fsys == nil {
		return nil, fmt.Errorf("assets: no filesystem to parse %v from", names)
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("assets: no files named in call to ParseFiles")
	}
	for _, name := range names {
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, err
		}
		base := path.Base(name)
		if t == nil {
			t = template.New(base)
		}
		tmpl := t
		if base != t.Name() {
			tmpl = t.New(base)
		}
		if _, err := tmpl.Parse(string(data)); err != nil {
			return nil, err
		}
	}
	return t, nil
}

// TemplateNames lists the *.tmpl files directly inside dir in fsys, sorted, as
// paths usable with ParseFiles. A missing dir yields no names and no error.
func TemplateNames(fsys fs.FS, dir string) ([]string, error) {
	if fsys == nil {
		return nil, nil
	}
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || path.Ext(entry.Name()) != ".tmpl" {
			continue
		}
		names = append(names, path.Join(dir, entry.Name()))
	}
	// fs.ReadDir returns entries sorted by file name.
	return names, nil
}
