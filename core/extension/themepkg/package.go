// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package themepkg

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"path"
	"sort"
	"time"

	"github.com/0xmattg/go-press/pkg/sign"
)

// Package is a theme package that passed the check, held in memory. Only
// ReadZip and ReadDir create one, so code that receives a *Package knows its
// files were checked, and they cannot change afterwards: the bytes a site
// serves are the bytes that were checked.
type Package struct {
	report *Report
	fsys   *memFS
}

// pkg returns the checked files as a Package, or nil when the check failed.
func (c *checker) pkg() *Package {
	if !c.report.OK() || c.report.Digest == "" {
		return nil
	}
	return &Package{report: c.report, fsys: newMemFS(c.files, time.Now())}
}

// Report returns the check report: it has no problems, but may have
// warnings.
func (p *Package) Report() *Report { return p.report }

// ID returns the package identity from theme.toml.
func (p *Package) ID() sign.PackageID { return p.report.ID }

// Digest returns the package's h1 content digest, signatures/ excluded.
func (p *Package) Digest() string { return p.report.Digest }

// FS returns the package files as a read-only filesystem rooted at the
// package root. Every file has the time the package was read as its
// modification time.
func (p *Package) FS() fs.FS { return p.fsys }

// memFS is a read-only filesystem over a package's files.
type memFS struct {
	files   map[string][]byte
	dirs    map[string][]fs.DirEntry // directory → entries sorted by name; "." is the root
	modTime time.Time
}

func newMemFS(files []file, modTime time.Time) *memFS {
	m := &memFS{files: make(map[string][]byte, len(files)), modTime: modTime}
	children := map[string]map[string]fs.DirEntry{".": {}}
	for _, f := range files {
		m.files[f.name] = f.data
		var entry fs.DirEntry = memInfo{name: path.Base(f.name), size: int64(len(f.data)), modTime: modTime}
		for name := f.name; name != "."; name = path.Dir(name) {
			dir := path.Dir(name)
			if children[dir] == nil {
				children[dir] = map[string]fs.DirEntry{}
			}
			children[dir][path.Base(name)] = entry
			entry = memInfo{name: path.Base(dir), dir: true, modTime: modTime}
		}
	}
	m.dirs = make(map[string][]fs.DirEntry, len(children))
	for dir, kids := range children {
		list := make([]fs.DirEntry, 0, len(kids))
		for _, kid := range kids {
			list = append(list, kid)
		}
		sort.Slice(list, func(i, j int) bool { return list[i].Name() < list[j].Name() })
		m.dirs[dir] = list
	}
	return m
}

func (m *memFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	if data, ok := m.files[name]; ok {
		info := memInfo{name: path.Base(name), size: int64(len(data)), modTime: m.modTime}
		return &memFile{Reader: bytes.NewReader(data), info: info}, nil
	}
	if entries, ok := m.dirs[name]; ok {
		return &memDir{info: memInfo{name: path.Base(name), dir: true, modTime: m.modTime}, entries: entries}, nil
	}
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

// ReadFile returns a copy of the file, so callers cannot change the package.
func (m *memFS) ReadFile(name string) ([]byte, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readfile", Path: name, Err: fs.ErrInvalid}
	}
	data, ok := m.files[name]
	if !ok {
		return nil, &fs.PathError{Op: "readfile", Path: name, Err: fs.ErrNotExist}
	}
	return bytes.Clone(data), nil
}

func (m *memFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrInvalid}
	}
	entries, ok := m.dirs[name]
	if !ok {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	return append([]fs.DirEntry(nil), entries...), nil
}

func (m *memFS) Stat(name string) (fs.FileInfo, error) {
	f, err := m.Open(name)
	if err != nil {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: errors.Unwrap(err)}
	}
	return f.Stat()
}

// memInfo describes a file or directory; it is both its FileInfo and its
// DirEntry.
type memInfo struct {
	name    string
	size    int64
	dir     bool
	modTime time.Time
}

func (i memInfo) Name() string { return i.name }
func (i memInfo) Size() int64  { return i.size }
func (i memInfo) Mode() fs.FileMode {
	if i.dir {
		return fs.ModeDir | 0o555
	}
	return 0o444
}
func (i memInfo) ModTime() time.Time         { return i.modTime }
func (i memInfo) IsDir() bool                { return i.dir }
func (i memInfo) Sys() any                   { return nil }
func (i memInfo) Type() fs.FileMode          { return i.Mode().Type() }
func (i memInfo) Info() (fs.FileInfo, error) { return i, nil }

// memFile is an open file. Embedding bytes.Reader makes it an io.ReadSeeker
// and io.ReaderAt, so http.ServeContent can serve ranges from it.
type memFile struct {
	*bytes.Reader
	info memInfo
}

func (f *memFile) Stat() (fs.FileInfo, error) { return f.info, nil }
func (f *memFile) Close() error               { return nil }

// memDir is an open directory.
type memDir struct {
	info    memInfo
	entries []fs.DirEntry
	offset  int
}

func (d *memDir) Stat() (fs.FileInfo, error) { return d.info, nil }
func (d *memDir) Close() error               { return nil }
func (d *memDir) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: d.info.name, Err: errors.New("is a directory")}
}

func (d *memDir) ReadDir(n int) ([]fs.DirEntry, error) {
	left := d.entries[d.offset:]
	if n <= 0 {
		d.offset = len(d.entries)
		return append([]fs.DirEntry(nil), left...), nil
	}
	if len(left) == 0 {
		return nil, io.EOF
	}
	if n > len(left) {
		n = len(left)
	}
	d.offset += n
	return append([]fs.DirEntry(nil), left[:n]...), nil
}
