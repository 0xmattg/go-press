// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package themepkg

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"hash/crc32"
	"io/fs"
	"sort"
	"strings"
	"testing"
)

// rawEntry is an archive entry written as is, so tests can build archives
// WriteZip refuses to.
type rawEntry struct {
	name   string
	data   string
	mode   fs.FileMode
	flags  uint16
	method uint16
	// claim, when set, is the uncompressed size written to the header.
	claim uint64
}

// rawZip writes the sample package plus extra entries.
func rawZip(t *testing.T, base map[string]string, extra ...rawEntry) []byte {
	t.Helper()
	var entries []rawEntry
	names := make([]string, 0, len(base))
	for name := range base {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		entries = append(entries, rawEntry{name: name, data: base[name]})
	}
	entries = append(entries, extra...)

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		header := &zip.FileHeader{Name: e.name, Method: zip.Deflate, Flags: e.flags}
		if e.method != 0 {
			header.Method = e.method
		}
		mode := e.mode
		if mode == 0 {
			mode = 0o644
		}
		header.SetMode(mode)
		if e.claim == 0 && e.flags == 0 && header.Method == zip.Deflate {
			w, err := zw.CreateHeader(header)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := w.Write([]byte(e.data)); err != nil {
				t.Fatal(err)
			}
			continue
		}
		// Write the entry raw: compress it ourselves and set the sizes.
		var compressed bytes.Buffer
		if header.Method == zip.Deflate {
			fw, _ := flate.NewWriter(&compressed, flate.BestCompression)
			_, _ = fw.Write([]byte(e.data))
			_ = fw.Close()
		} else {
			compressed.WriteString(e.data)
		}
		header.CRC32 = crc32.ChecksumIEEE([]byte(e.data))
		header.CompressedSize64 = uint64(compressed.Len())
		header.UncompressedSize64 = uint64(len(e.data))
		if e.claim != 0 {
			header.UncompressedSize64 = e.claim
		}
		w, err := zw.CreateRaw(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(compressed.Bytes()); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func checkRaw(t *testing.T, data []byte) *Report {
	t.Helper()
	report, err := CheckZip(bytes.NewReader(data), int64(len(data)), Options{})
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func TestArchivePathRules(t *testing.T) {
	cases := []struct {
		name, rule, path string
	}{
		{"../escape.txt", RulePathUnsafe, ""},
		{"/etc/passwd.txt", RulePathUnsafe, ""},
		{`static\css\x.css`, RulePathUnsafe, ""},
		{"static//x.css", RulePathUnsafe, ""},
		{"static/./x.css", RulePathUnsafe, ""},
		{"C:/x.txt", RulePathUnsafe, ""},
		{".hidden/x.txt", RulePathHidden, ".hidden/x.txt"},
		{"static/.env", RulePathHidden, "static/.env"},
		{"static/\x01x.css", RulePathEncoding, ""},
		{"static/\xff.css", RulePathEncoding, ""},
		{"static/" + strings.Repeat("a", MaxPathLength) + ".css", RulePathLength, "static/aaa"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			report := checkRaw(t, rawZip(t, sample(t, nil), rawEntry{name: tc.name, data: "x"}))
			expectProblem(t, report, tc.rule, tc.path)
			if report.Digest != "" {
				t.Fatal("a package with a rejected entry has a digest")
			}
		})
	}
}

func TestArchiveEntryRules(t *testing.T) {
	cases := []struct {
		desc  string
		entry rawEntry
		rule  string
	}{
		{"duplicate", rawEntry{name: "theme.toml", data: "x"}, RuleEntryDuplicate},
		{"case duplicate", rawEntry{name: "Theme.toml", data: "x"}, RuleEntryDuplicate},
		{"file and directory", rawEntry{name: "theme.toml/x.txt", data: "x"}, RuleEntryDuplicate},
		{"directory and file", rawEntry{name: "locales/en.json/", mode: fs.ModeDir | 0o755}, RuleEntryDuplicate},
		{"symlink", rawEntry{name: "static/link.css", data: "/etc/passwd", mode: fs.ModeSymlink | 0o777}, RuleEntrySpecial},
		{"encrypted", rawEntry{name: "static/x.css", data: "x", flags: 0x1}, RuleEntryEncrypted},
		{"compression method", rawEntry{name: "static/x.css", data: "x", method: 99}, RuleEntryMethod},
		{"lying size", rawEntry{name: "static/x.css", data: strings.Repeat("a", 4096), claim: 10}, RuleEntryCorrupt},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			report := checkRaw(t, rawZip(t, sample(t, nil), tc.entry))
			expectProblem(t, report, tc.rule, "")
		})
	}
}

func TestArchiveDirectoryEntriesAreAllowed(t *testing.T) {
	report := checkRaw(t, rawZip(t, sample(t, nil), rawEntry{name: "static/", mode: fs.ModeDir | 0o755}))
	expectOK(t, report)
}

func TestArchiveSizeLimits(t *testing.T) {
	t.Run("archive", func(t *testing.T) {
		data := zipFiles(t, sample(t, nil))
		report, err := CheckZip(bytes.NewReader(data), MaxZipSize+1, Options{})
		if err != nil {
			t.Fatal(err)
		}
		expectProblem(t, report, RuleZipSize, "")
		if report.Files != 0 {
			t.Fatal("an oversized archive was read")
		}
	})
	t.Run("file", func(t *testing.T) {
		report := checkRaw(t, rawZip(t, sample(t, nil), rawEntry{name: "static/big.css", data: strings.Repeat(" ", MaxFileSize+1)}))
		expectProblem(t, report, RuleSizeFile, "static/big.css")
	})
	t.Run("total", func(t *testing.T) {
		var extra []rawEntry
		for i := 0; i < 7; i++ {
			extra = append(extra, rawEntry{name: "static/big" + string(rune('a'+i)) + ".css", data: strings.Repeat(" ", 4<<20+1)})
		}
		report := checkRaw(t, rawZip(t, sample(t, nil), extra...))
		expectProblem(t, report, RuleSizeTotal, "")
	})
	t.Run("count", func(t *testing.T) {
		var extra []rawEntry
		for i := 0; i < MaxFiles; i++ {
			extra = append(extra, rawEntry{name: "static/f" + strings.Repeat("x", i/100) + string(rune('a'+i%26)) + string(rune('a'+i/26%26)) + ".txt", data: "x"})
		}
		report := checkRaw(t, rawZip(t, sample(t, nil), extra...))
		expectProblem(t, report, RuleSizeFiles, "")
	})
	t.Run("template", func(t *testing.T) {
		big := `{{define "content"}}` + strings.Repeat("<p>x</p>\n", MaxTemplateSize/9+1) + `{{end}}`
		report := check(t, sample(t, map[string]string{"templates/pages/big.tmpl": big}))
		expectProblem(t, report, RuleSizeTemplate, "templates/pages/big.tmpl")
	})
}

func TestCheckZipRejectsAnUnreadableArchive(t *testing.T) {
	if _, err := CheckZip(bytes.NewReader([]byte("not a zip")), 9, Options{}); err == nil {
		t.Fatal("garbage was accepted as an archive")
	}
}
