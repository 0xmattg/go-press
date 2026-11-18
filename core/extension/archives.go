// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package extension

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/0xmattg/go-press/core/plugin"
	"github.com/0xmattg/go-press/pkg/semver"
)

// Archives stores the original zip files of installed extension packages,
// one file per installed version:
//
//	<dir>/<slug>/<version>-<d8>.zip
//
// d8 is the first 8 hexadecimal digits of the package's content digest, so
// two builds of the same version with different content get different names.
// A file is written once, atomically, and never modified. Archives only
// stores bytes: checking a package is the caller's job, and loading one
// re-checks it.
type Archives struct {
	dir string
}

// archiveTempPrefix starts the name of a file being written.
const archiveTempPrefix = ".tmp-"

// Permissions: only the process user (and its group) may read packages.
const (
	archiveDirMode  = 0o750
	archiveFileMode = 0o640
)

// NewArchives returns the archive store rooted at dir, usually
// <site dir>/packages/archives. Nothing is created until the first Put.
func NewArchives(dir string) *Archives {
	return &Archives{dir: dir}
}

// Dir returns the root directory.
func (a *Archives) Dir() string { return a.dir }

// Path returns where the archive of one installed version is stored. It
// rejects an invalid slug, version or digest instead of building a path from
// it.
func (a *Archives) Path(slug, version, digest string) (string, error) {
	if !plugin.ValidSlug(slug) {
		return "", fmt.Errorf("extension: invalid package slug %q", slug)
	}
	if !semver.Valid(version) || strings.ContainsAny(version, `/\`) || strings.Contains(version, "..") {
		return "", fmt.Errorf("extension: invalid package version %q", version)
	}
	tag, err := digestTag(digest)
	if err != nil {
		return "", err
	}
	return filepath.Join(a.dir, slug, version+"-"+tag+".zip"), nil
}

// digestTag returns the first 8 hexadecimal digits of an h1 digest.
func digestTag(digest string) (string, error) {
	encoded, ok := strings.CutPrefix(digest, "h1:")
	if !ok {
		return "", fmt.Errorf("extension: package digest %q is not an h1 digest", digest)
	}
	sum, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(sum) != 32 {
		return "", fmt.Errorf("extension: package digest %q is not an h1 digest", digest)
	}
	return hex.EncodeToString(sum[:4]), nil
}

// Put stores the archive of one installed version and returns its path and
// size. The file is written to a temporary name, synced and renamed, so a
// crash never leaves a partial archive under the final name. When the
// archive is already stored (the same slug, version and digest), the stored
// file is kept and its size returned: the same content digest means the same
// package, even if the zip bytes differ.
func (a *Archives) Put(slug, version, digest string, data []byte) (string, int64, error) {
	path, err := a.Path(slug, version, digest)
	if err != nil {
		return "", 0, err
	}
	if info, err := os.Stat(path); err == nil {
		if !info.Mode().IsRegular() {
			return "", 0, fmt.Errorf("extension: %s is not a regular file", path)
		}
		return path, info.Size(), nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, archiveDirMode); err != nil {
		return "", 0, err
	}
	tmp, err := os.CreateTemp(dir, archiveTempPrefix+"*")
	if err != nil {
		return "", 0, err
	}
	written := false
	defer func() {
		if !written {
			_ = os.Remove(tmp.Name())
		}
	}()
	if err := tmp.Chmod(archiveFileMode); err != nil {
		_ = tmp.Close()
		return "", 0, err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return "", 0, err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return "", 0, err
	}
	if err := tmp.Close(); err != nil {
		return "", 0, err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return "", 0, err
	}
	written = true
	syncDir(dir)
	return path, int64(len(data)), nil
}

// syncDir flushes a directory entry change to disk where the platform
// supports it.
func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
}

// Remove deletes the archive of one installed version. A missing archive is
// not an error. The slug's directory is removed when it becomes empty.
func (a *Archives) Remove(slug, version, digest string) error {
	path, err := a.Path(slug, version, digest)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	_ = os.Remove(filepath.Dir(path)) // fails while other versions remain
	return nil
}

// Files lists every stored archive, as paths like Path returns. A missing
// root directory means no archives.
func (a *Archives) Files() ([]string, error) {
	slugs, err := os.ReadDir(a.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, slug := range slugs {
		if !slug.IsDir() {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(a.dir, slug.Name()))
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".zip") {
				continue
			}
			out = append(out, filepath.Join(a.dir, slug.Name(), name))
		}
	}
	return out, nil
}

// RemoveStaleTemp deletes temporary files older than age, left behind by
// writes that were interrupted, and returns how many it removed. Younger
// ones may belong to a write in progress in another process.
func (a *Archives) RemoveStaleTemp(age time.Duration) (int, error) {
	slugs, err := os.ReadDir(a.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	cutoff := time.Now().Add(-age)
	removed := 0
	for _, slug := range slugs {
		if !slug.IsDir() {
			continue
		}
		dir := filepath.Join(a.dir, slug.Name())
		entries, err := os.ReadDir(dir)
		if err != nil {
			return removed, err
		}
		for _, entry := range entries {
			if !strings.HasPrefix(entry.Name(), archiveTempPrefix) {
				continue
			}
			info, err := entry.Info()
			if err != nil || info.ModTime().After(cutoff) {
				continue
			}
			if err := os.Remove(filepath.Join(dir, entry.Name())); err == nil {
				removed++
			}
		}
	}
	return removed, nil
}
