// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

// Package themepkg checks and builds theme packages.
//
// A theme package is a theme without Go code: a zip of theme.toml, page
// bundle templates (templates/layouts, partials, pages), static styles,
// images and fonts, locale files and optional demo data. Packages come from
// third parties, so the checker treats every input as hostile. It enforces:
//
//   - the archive itself: size limits measured on the bytes actually read,
//     no links, encryption, unknown compression or duplicate names;
//   - paths and file types: a fixed layout and an extension allowlist, with
//     binary files matching their extension;
//   - no scripts in any form: script files, script elements, event handler
//     attributes, script URLs, embedded documents, legacy CSS script hooks,
//     and the same rules on templates after their actions are taken out, so
//     an action cannot split a forbidden string;
//   - theme.toml: identity, the core version constraint, the settings
//     declaration and the [[external]] hosts;
//   - templates: they parse with core's template functions and pass the
//     html/template escaper;
//   - external resources: every resource loaded from another host is
//     declared in [[external]] for that kind of resource.
//
// The checker reports every problem it finds, each with a stable rule ID,
// instead of stopping at the first one. Passing the check does not make a
// package trustworthy: installs still verify its signatures.
package themepkg

import (
	"fmt"
	"sort"
	"strings"

	"github.com/0xmattg/go-press/core/theme"
	"github.com/0xmattg/go-press/pkg/sign"
)

// Limits.
const (
	MaxZipSize      = 10 << 20  // size of the .zip file
	MaxUnpackedSize = 25 << 20  // total size of the files, counted as read
	MaxFiles        = 1000      // number of files
	MaxFileSize     = 5 << 20   // size of one file
	MaxTemplateSize = 512 << 10 // size of one template
	MaxPathLength   = 200       // length of a path inside the package
	MaxProblems     = 200       // problems listed in a report
)

// Kind is the package kind theme packages sign with.
const Kind = "theme"

// Rule IDs. They are stable: developer documentation, review tools and
// install errors refer to them.
const (
	RuleZipSize        = "zip.size"
	RuleEntrySpecial   = "entry.special"
	RuleEntryEncrypted = "entry.encrypted"
	RuleEntryMethod    = "entry.method"
	RuleEntryDuplicate = "entry.duplicate"
	RuleEntryCorrupt   = "entry.corrupt"

	RulePathEncoding = "path.encoding"
	RulePathUnsafe   = "path.unsafe"
	RulePathHidden   = "path.hidden"
	RulePathLength   = "path.length"

	RuleSizeTotal    = "size.total"
	RuleSizeFiles    = "size.files"
	RuleSizeFile     = "size.file"
	RuleSizeTemplate = "size.template"

	RuleFileType     = "file.type"
	RuleFilePlace    = "file.place"
	RuleFileContent  = "file.content"
	RuleFileJSON     = "file.json"
	RuleFileTOML     = "file.toml"
	RuleFileShadowed = "file.shadowed"

	RuleScriptFile    = "script.file"
	RuleScriptTag     = "script.tag"
	RuleScriptHandler = "script.handler"
	RuleScriptURL     = "script.url"
	RuleScriptCSS     = "script.css"
	RuleScriptEmbed   = "script.embed"
	RuleScriptBase    = "script.base"
	RuleScriptRefresh = "script.refresh"
	RuleScriptSVG     = "script.svg"
	RuleScriptDynamic = "script.dynamic"

	RuleTemplateAdmin   = "template.admin"
	RuleTemplatePlace   = "template.place"
	RuleTemplateLayout  = "template.layout"
	RuleTemplatePages   = "template.pages"
	RuleTemplateHome    = "template.home"
	RuleTemplateParse   = "template.parse"
	RuleTemplateBase    = "template.base"
	RuleTemplateEscape  = "template.escape"
	RuleTemplateComplex = "template.complex"
	RuleTemplateUnused  = "template.unused"

	RuleManifestMissing      = "manifest.missing"
	RuleManifestSyntax       = "manifest.syntax"
	RuleManifestUnknown      = "manifest.unknown"
	RuleManifestSlug         = "manifest.slug"
	RuleManifestReserved     = "manifest.reserved"
	RuleManifestName         = "manifest.name"
	RuleManifestVersion      = "manifest.version"
	RuleManifestCore         = "manifest.core"
	RuleManifestCoreUnmet    = "manifest.core-unmet"
	RuleManifestRequires     = "manifest.requires"
	RuleManifestSettings     = "manifest.settings"
	RuleManifestPageTemplate = "manifest.page-template"
	RuleManifestScreenshot   = "manifest.screenshot"

	RuleExternalInvalid    = "external.invalid"
	RuleExternalUndeclared = "external.undeclared"
	RuleExternalKind       = "external.kind"
	RuleExternalInsecure   = "external.insecure"

	RuleSourceHidden = "source.hidden"
)

// Options controls a check.
type Options struct {
	// CoreVersion, when set, is the GoPress version the package must run
	// on: [requires] core must be satisfied by it. When empty only the
	// constraint's syntax is checked.
	CoreVersion string
	// Reserved reports whether a slug is taken by a theme or plugin built
	// into the binary. Nil checks nothing.
	Reserved func(slug string) bool
}

// Problem is one finding.
type Problem struct {
	Rule    string `json:"rule"`
	Path    string `json:"path,omitempty"`
	Line    int    `json:"line,omitempty"`
	Message string `json:"message"`
}

// String formats the problem as "rule path:line: message".
func (p Problem) String() string {
	where := p.Path
	if where != "" && p.Line > 0 {
		where = fmt.Sprintf("%s:%d", where, p.Line)
	}
	if where == "" {
		return p.Rule + " " + p.Message
	}
	return p.Rule + " " + where + ": " + p.Message
}

// Report is the result of a check.
type Report struct {
	// ID is the package identity from theme.toml. Its fields are empty or
	// invalid when theme.toml is; Problems then says why.
	ID sign.PackageID `json:"id"`
	// Digest is the package's h1 content digest, signatures/ excluded. It
	// is empty when the files could not all be read.
	Digest string `json:"digest,omitempty"`
	// Files and Size count the package's files and their total size.
	Files int   `json:"files"`
	Size  int64 `json:"size"`
	// External is the [[external]] declaration.
	External []theme.ExternalConfig `json:"external,omitempty"`
	// Problems make the package invalid; at most MaxProblems are listed
	// and Omitted counts the rest. Warnings do not.
	Problems []Problem `json:"problems,omitempty"`
	Omitted  int       `json:"omitted,omitempty"`
	Warnings []Problem `json:"warnings,omitempty"`
}

// OK reports whether the package passed the check.
func (r *Report) OK() bool { return r != nil && len(r.Problems) == 0 && r.Omitted == 0 }

// file is one regular file of a package.
type file struct {
	name string
	data []byte
}

// checker collects the findings of one check.
type checker struct {
	opts     Options
	report   *Report
	files    []file
	byName   map[string]*file
	seen     map[Problem]bool
	complete bool // every file was read
	manifest *theme.FileConfig
}

func newChecker(opts Options) *checker {
	return &checker{opts: opts, report: &Report{ID: sign.PackageID{Kind: Kind}}, seen: map[Problem]bool{}}
}

// problem records a problem once.
func (c *checker) problem(rule, path string, line int, format string, args ...interface{}) {
	p := Problem{Rule: rule, Path: path, Line: line, Message: fmt.Sprintf(format, args...)}
	if c.seen[p] {
		return
	}
	c.seen[p] = true
	if len(c.report.Problems) >= MaxProblems {
		c.report.Omitted++
		return
	}
	c.report.Problems = append(c.report.Problems, p)
}

// warn records a warning once.
func (c *checker) warn(rule, path string, line int, format string, args ...interface{}) {
	p := Problem{Rule: rule, Path: path, Line: line, Message: fmt.Sprintf(format, args...)}
	if c.seen[p] || len(c.report.Warnings) >= MaxProblems {
		return
	}
	c.seen[p] = true
	c.report.Warnings = append(c.report.Warnings, p)
}

// addFile records a regular file that was read in full.
func (c *checker) addFile(name string, data []byte) {
	c.files = append(c.files, file{name: name, data: data})
	c.report.Files++
	c.report.Size += int64(len(data))
}

// run checks the collected files.
func (c *checker) run() *Report {
	sort.Slice(c.files, func(i, j int) bool { return c.files[i].name < c.files[j].name })
	c.byName = make(map[string]*file, len(c.files))
	for i := range c.files {
		c.byName[c.files[i].name] = &c.files[i]
	}
	if c.complete && len(c.files) > 0 {
		if digest, err := digestFiles(c.files); err == nil {
			c.report.Digest = digest
		}
	}

	for i := range c.files {
		c.checkFile(&c.files[i])
	}
	c.checkManifest()
	c.checkStaticMarkup()
	c.checkTemplates()
	return c.report
}

// lineAt returns the 1-based line of offset in src.
func lineAt(src string, offset int) int {
	if offset < 0 {
		return 0
	}
	if offset > len(src) {
		offset = len(src)
	}
	return strings.Count(src[:offset], "\n") + 1
}
