// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package themepkg

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/0xmattg/go-press/pkg/sign"

	"golang.org/x/mod/sumdb/dirhash"
)

// CheckZip checks a package archive of size bytes. Problems with the package
// are reported in the Report; an error means the archive could not be read
// at all.
func CheckZip(r io.ReaderAt, size int64, opts Options) (*Report, error) {
	c, err := checkZip(r, size, opts)
	if err != nil {
		return nil, err
	}
	return c.report, nil
}

// CheckDir checks a theme source directory: the files Build would pack.
// Hidden paths (.git, .DS_Store) and signatures/ are left out, as Build
// leaves them out.
func CheckDir(fsys fs.FS, opts Options) (*Report, error) {
	c, err := checkDir(fsys, opts)
	if err != nil {
		return nil, err
	}
	return c.report, nil
}

// ReadZip checks a package archive like CheckZip and, when it passes,
// returns its files as a Package. When the check fails the Package is nil
// and the Report lists the problems.
func ReadZip(r io.ReaderAt, size int64, opts Options) (*Package, *Report, error) {
	c, err := checkZip(r, size, opts)
	if err != nil {
		return nil, nil, err
	}
	return c.pkg(), c.report, nil
}

// ReadDir checks a theme source directory like CheckDir and, when it
// passes, returns its files as a Package: the files Build would pack, so
// hidden paths and signatures/ are left out.
func ReadDir(fsys fs.FS, opts Options) (*Package, *Report, error) {
	c, err := checkDir(fsys, opts)
	if err != nil {
		return nil, nil, err
	}
	return c.pkg(), c.report, nil
}

func checkZip(r io.ReaderAt, size int64, opts Options) (*checker, error) {
	c := newChecker(opts)
	if size > MaxZipSize {
		c.problem(RuleZipSize, "", 0, "the archive is %s, larger than the %s limit", formatSize(size), formatSize(MaxZipSize))
		return c, nil
	}
	archive, err := zip.NewReader(r, size)
	if err != nil && !errors.Is(err, zip.ErrInsecurePath) {
		return nil, fmt.Errorf("themepkg: open archive: %w", err)
	}
	c.complete = c.readZip(archive)
	c.run()
	return c, nil
}

func checkDir(fsys fs.FS, opts Options) (*checker, error) {
	c := newChecker(opts)
	complete, err := c.readDir(fsys)
	if err != nil {
		return nil, err
	}
	c.complete = complete
	c.run()
	return c, nil
}

// Build checks the theme source directory src like CheckDir and, when it
// passes, writes the package archive to w. Nothing is written when the check
// fails. The archive is reproducible: the same files always give the same
// bytes.
func Build(w io.Writer, src fs.FS, opts Options) (*Report, error) {
	c, err := checkDir(src, opts)
	if err != nil {
		return nil, err
	}
	if !c.report.OK() {
		return c.report, nil
	}
	files := make([]File, len(c.files))
	for i, f := range c.files {
		files[i] = File{Name: f.name, Data: f.data}
	}
	return c.report, WriteZip(w, files)
}

// File is one file of a package archive.
type File struct {
	Name string
	Data []byte
}

// zipTime is the modification time of every archive entry, so archives do
// not depend on when they were built.
var zipTime = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

// WriteZip writes files as a reproducible zip archive: sorted by name,
// deflated, with a fixed time and mode 0644, and no directory entries.
func WriteZip(w io.Writer, files []File) error {
	sorted := append([]File(nil), files...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	zw := zip.NewWriter(w)
	for i, f := range sorted {
		if i > 0 && f.Name == sorted[i-1].Name {
			return fmt.Errorf("themepkg: duplicate file %s", f.Name)
		}
		header := &zip.FileHeader{Name: f.Name, Method: zip.Deflate, Modified: zipTime}
		header.SetMode(0o644)
		out, err := zw.CreateHeader(header)
		if err != nil {
			return err
		}
		if _, err := out.Write(f.Data); err != nil {
			return err
		}
	}
	return zw.Close()
}

// readZip reads every entry of archive and reports whether all files were
// read.
func (c *checker) readZip(archive *zip.Reader) bool {
	complete := true
	names := map[string]string{} // lower-case path → path
	var total int64
	for _, entry := range archive.File {
		name := strings.TrimSuffix(entry.Name, "/")
		isDir := strings.HasSuffix(entry.Name, "/") || entry.Mode().IsDir()
		if !c.checkPath(entry.Name, name, true) {
			complete = false
			continue
		}
		key := strings.ToLower(name)
		if prev, ok := names[key]; ok {
			if prev == name {
				c.problem(RuleEntryDuplicate, name, 0, "the archive has two entries named %s", name)
			} else {
				c.problem(RuleEntryDuplicate, name, 0, "%s and %s differ only in case", prev, name)
			}
			complete = false
			continue
		}
		names[key] = name
		if isDir {
			continue
		}
		switch {
		case !entry.Mode().IsRegular():
			c.problem(RuleEntrySpecial, name, 0, "only regular files are allowed, not %s", fileModeKind(entry.Mode()))
			complete = false
			continue
		case entry.Flags&0x1 != 0:
			c.problem(RuleEntryEncrypted, name, 0, "encrypted entries are not allowed")
			complete = false
			continue
		case entry.Method != zip.Store && entry.Method != zip.Deflate:
			c.problem(RuleEntryMethod, name, 0, "compression method %d is not allowed; use store or deflate", entry.Method)
			complete = false
			continue
		}
		if c.report.Files >= MaxFiles {
			c.problem(RuleSizeFiles, "", 0, "the package has more than %d files", MaxFiles)
			return false
		}
		data, ok := c.readEntry(name, entry, total)
		if data == nil && !ok {
			return false
		}
		if !ok {
			complete = false
			continue
		}
		total += int64(len(data))
		c.addFile(name, data)
	}
	// A file and a directory of the same name would collide on extraction.
	parents := map[string]bool{}
	for key := range names {
		for parent := path.Dir(key); parent != "."; parent = path.Dir(parent) {
			parents[parent] = true
		}
	}
	for _, f := range c.files {
		if parents[strings.ToLower(f.name)] {
			c.problem(RuleEntryDuplicate, f.name, 0, "%s is both a file and a directory", f.name)
			complete = false
		}
	}
	return complete
}

// readEntry reads one archive entry within the size limits. It returns the
// data and true on success; nil and false when the total limit is exceeded
// and reading must stop; or non-nil-or-nil data and false when only this
// entry is bad.
func (c *checker) readEntry(name string, entry *zip.File, total int64) ([]byte, bool) {
	rc, err := entry.Open()
	if err != nil {
		c.problem(RuleEntryCorrupt, name, 0, "cannot read the entry: %v", err)
		return []byte{}, false
	}
	defer rc.Close()
	return c.readLimited(name, rc, total)
}

// readLimited reads r for file name, enforcing the per-file and total size
// limits on the bytes actually read.
func (c *checker) readLimited(name string, r io.Reader, total int64) ([]byte, bool) {
	limit := int64(MaxFileSize)
	if left := MaxUnpackedSize - total; left < limit {
		limit = left
	}
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		c.problem(RuleEntryCorrupt, name, 0, "cannot read the file: %v", err)
		return []byte{}, false
	}
	if int64(len(data)) > limit {
		if limit < MaxFileSize {
			c.problem(RuleSizeTotal, "", 0, "the files add up to more than %s", formatSize(MaxUnpackedSize))
			return nil, false
		}
		c.problem(RuleSizeFile, name, 0, "the file is larger than %s", formatSize(MaxFileSize))
		return []byte{}, false
	}
	return data, true
}

// junkNames are system files left out of source directories without a
// warning.
var junkNames = map[string]bool{".DS_Store": true, "Thumbs.db": true, "desktop.ini": true, "__MACOSX": true}

// readDir reads a theme source directory. It reports whether all files were
// read; the error is for I/O failures.
func (c *checker) readDir(fsys fs.FS) (bool, error) {
	complete := true
	var hidden []string
	var total int64
	stop := errors.New("stop")
	err := fs.WalkDir(fsys, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if name == "." {
			return nil
		}
		base := path.Base(name)
		skip := junkNames[base] || strings.HasPrefix(base, ".") || name == sign.SignaturesDir
		if skip {
			if !junkNames[base] && name != sign.SignaturesDir {
				hidden = append(hidden, name)
			}
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !c.checkPath(name, name, false) {
			complete = false
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			c.problem(RuleEntrySpecial, name, 0, "only regular files are allowed, not %s", fileModeKind(d.Type()))
			complete = false
			return nil
		}
		if c.report.Files >= MaxFiles {
			c.problem(RuleSizeFiles, "", 0, "the package has more than %d files", MaxFiles)
			complete = false
			return stop
		}
		f, err := fsys.Open(name)
		if err != nil {
			return err
		}
		data, ok := c.readLimited(name, f, total)
		_ = f.Close()
		if data == nil && !ok {
			complete = false
			return stop
		}
		if !ok {
			complete = false
			return nil
		}
		total += int64(len(data))
		c.addFile(name, data)
		return nil
	})
	if err != nil && !errors.Is(err, stop) {
		return false, err
	}
	if len(hidden) > 0 {
		shown := hidden
		if len(shown) > 5 {
			shown = append(shown[:5:5], "…")
		}
		c.warn(RuleSourceHidden, "", 0, "left out %d hidden path(s): %s", len(hidden), strings.Join(shown, ", "))
	}
	return complete, nil
}

// checkPath applies the path rules to an entry. raw is the name as stored,
// name the name without a trailing slash. strict also rejects hidden
// segments, which source directories leave out instead.
func (c *checker) checkPath(raw, name string, strict bool) bool {
	if !utf8.ValidString(raw) {
		c.problem(RulePathEncoding, "", 0, "the path %q is not valid UTF-8", raw)
		return false
	}
	for _, r := range raw {
		if r < 0x20 || r == 0x7f {
			c.problem(RulePathEncoding, "", 0, "the path %q contains a control character", raw)
			return false
		}
	}
	if len(name) > MaxPathLength {
		c.problem(RulePathLength, name, 0, "the path is longer than %d bytes", MaxPathLength)
		return false
	}
	if name == "" || strings.HasPrefix(raw, "/") || strings.Contains(raw, "\\") || strings.Contains(raw, ":") {
		c.problem(RulePathUnsafe, "", 0, "the path %q must be relative and use / as separator", raw)
		return false
	}
	for _, seg := range strings.Split(name, "/") {
		switch {
		case seg == "" || seg == "." || seg == "..":
			c.problem(RulePathUnsafe, "", 0, "the path %q has an empty, . or .. segment", raw)
			return false
		case strict && strings.HasPrefix(seg, "."):
			c.problem(RulePathHidden, name, 0, "hidden files and directories are not allowed")
			return false
		}
	}
	return true
}

// digestFiles returns the h1 digest of files, signatures/ excluded, the
// same value sign.PackageDigest computes for the unpacked package.
func digestFiles(files []file) (string, error) {
	data := make(map[string][]byte, len(files))
	var names []string
	for _, f := range files {
		if f.name == sign.SignaturesDir || strings.HasPrefix(f.name, sign.SignaturesDir+"/") {
			continue
		}
		data[f.name] = f.data
		names = append(names, f.name)
	}
	if len(names) == 0 {
		return "", errors.New("themepkg: nothing to digest")
	}
	return dirhash.Hash1(names, func(name string) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(data[name])), nil
	})
}

func fileModeKind(mode fs.FileMode) string {
	switch {
	case mode&fs.ModeSymlink != 0:
		return "a symbolic link"
	case mode&fs.ModeDevice != 0:
		return "a device"
	case mode&fs.ModeNamedPipe != 0:
		return "a named pipe"
	case mode&fs.ModeSocket != 0:
		return "a socket"
	default:
		return "a special file"
	}
}

func formatSize(n int64) string {
	switch {
	case n >= 1<<20 && n%(1<<20) == 0:
		return fmt.Sprintf("%d MB", n>>20)
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	default:
		return fmt.Sprintf("%d bytes", n)
	}
}
