// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package themepkg

import (
	"errors"
	"fmt"
	"html/template"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template/parse"

	"github.com/0xmattg/go-press/core/theme"

	"golang.org/x/net/html"
)

const (
	layoutPath   = "templates/layouts/base.tmpl"
	partialsDir  = "templates/partials/"
	pagesDir     = "templates/pages/"
	baseTemplate = "base"

	// Markers stand in for template actions and control structures when
	// the checker looks for names built at run time. They are letters so
	// the tokenizer keeps them inside element and attribute names.
	markAction  = "gpzqactzq"
	markControl = "gpzqctlzq"

	// maxPageText bounds the text of one page with its templates inlined,
	// and maxTotalText the text of all pages.
	maxPageText  = 8 << 20
	maxTotalText = 64 << 20
)

// hasHomePage reports whether pages include a page the home page renders
// with.
func hasHomePage(pages []*file) bool {
	for _, name := range theme.HomePageNames() {
		for _, page := range pages {
			if page.name == pagesDir+name+".tmpl" {
				return true
			}
		}
	}
	return false
}

// checkTemplates checks the page bundle: its layout, that every template
// parses with core's template functions, the script rules on each page as
// it renders, and the html/template escaper.
func (c *checker) checkTemplates() {
	layout := c.byName[layoutPath]
	var partials, pages []*file
	for i := range c.files {
		f := &c.files[i]
		dir, base := path.Split(f.name)
		if path.Ext(base) != ".tmpl" {
			continue
		}
		switch dir {
		case partialsDir:
			partials = append(partials, f)
		case pagesDir:
			pages = append(pages, f)
		}
	}
	if layout == nil {
		c.problem(RuleTemplateLayout, layoutPath, 0, "the package has no templates/layouts/base.tmpl")
	}
	if len(pages) == 0 {
		c.problem(RuleTemplatePages, pagesDir, 0, "the package has no templates/pages/*.tmpl")
	} else if !hasHomePage(pages) {
		names := theme.HomePageNames()
		for i, name := range names {
			names[i] = name + ".tmpl"
		}
		c.problem(RuleTemplateHome, pagesDir, 0, "the package has no home page; add one of %s to templates/pages/", strings.Join(names, ", "))
	}

	funcs := theme.TemplateCheckFuncs()
	sources := map[string]string{} // package path → text
	paths := map[string]string{}   // template file name → package path
	parsed := map[string]bool{}
	all := append(append([]*file{}, partials...), pages...)
	if layout != nil {
		all = append([]*file{layout}, all...)
	}
	for _, f := range all {
		src := string(f.data)
		sources[f.name] = src
		paths[path.Base(f.name)] = f.name
		if _, err := template.New(path.Base(f.name)).Funcs(funcs).Parse(src); err != nil {
			c.problem(RuleTemplateParse, f.name, parseErrorLine(err), "%s", parseErrorMessage(err))
			// Still apply the script rules to the text as written.
			c.scanMarkup(src, 0, fileLocate(f.name, src), markupTemplate, 0)
			continue
		}
		parsed[f.name] = true
	}

	sharedOK := layout != nil && parsed[layout.name]
	for _, f := range partials {
		sharedOK = sharedOK && parsed[f.name]
	}
	if !sharedOK {
		// Pages cannot be put together; check each template on its own.
		for _, f := range all {
			if parsed[f.name] {
				c.scanStandalone(f, funcs, sources, paths)
			}
		}
		return
	}

	shared := template.New("").Funcs(funcs)
	for _, f := range append([]*file{layout}, partials...) {
		if _, err := shared.New(path.Base(f.name)).Parse(sources[f.name]); err != nil {
			c.problem(RuleTemplateParse, f.name, parseErrorLine(err), "%s", parseErrorMessage(err))
			return
		}
	}
	total := 0
	for _, page := range pages {
		if !parsed[page.name] {
			continue
		}
		set, err := shared.Clone()
		if err == nil {
			_, err = set.New(path.Base(page.name)).Parse(sources[page.name])
		}
		if err != nil {
			c.problem(RuleTemplateParse, page.name, parseErrorLine(err), "%s", parseErrorMessage(err))
			continue
		}
		if set.Lookup(baseTemplate) == nil || set.Lookup(baseTemplate).Tree == nil {
			c.problem(RuleTemplateBase, layoutPath, 0, "templates/layouts/base.tmpl must define the %q template", baseTemplate)
			return
		}
		x := newExpander(set, paths, sources, true)
		x.walkTemplate(baseTemplate)
		total += x.a.Len()
		if x.over || total > maxTotalText {
			c.problem(RuleTemplateComplex, page.name, 0, "the page is too large once its templates are put together")
			return
		}
		c.scanExpansion(x)
		c.escapeCheck(set, page.name, paths)
	}
}

// scanStandalone applies the script rules to each template in one file
// without inlining the templates it calls.
func (c *checker) scanStandalone(f *file, funcs template.FuncMap, sources, paths map[string]string) {
	set, err := template.New(path.Base(f.name)).Funcs(funcs).Parse(sources[f.name])
	if err != nil {
		return
	}
	var names []string
	for _, t := range set.Templates() {
		names = append(names, t.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		x := newExpander(set, paths, sources, false)
		x.walkTemplate(name)
		if !x.over {
			c.scanExpansion(x)
		}
	}
}

// scanExpansion applies the markup rules to an expanded page: the literal
// text with actions taken out, and the text with markers for actions and
// control structures to find element and attribute names built at run time.
func (c *checker) scanExpansion(x *expander) {
	c.scanMarkup(x.a.String(), 0, x.locate(x.segA), markupTemplate, 0)
	c.scanNames(x.b.String(), 0, x.locate(x.segB), 0)
}

// scanNames reports element and attribute names that template actions or
// control structures help build. html/template treats an action next to a
// name as a separate, harmless name, but the browser joins them: <scr{{"ipt"}}>
// is a script element.
func (c *checker) scanNames(src string, base int, loc locate, depth int) {
	z := html.NewTokenizer(strings.NewReader(src))
	offset := 0
	for {
		tt := z.Next()
		start := offset
		offset += len(z.Raw())
		switch tt {
		case html.ErrorToken:
			return
		case html.CommentToken:
			if depth < maxCommentDepth {
				c.scanNames(z.Token().Data, base+start+4, loc, depth+1)
			}
		case html.StartTagToken, html.SelfClosingTagToken, html.EndTagToken:
			tok := z.Token()
			p, line := loc(base + start)
			if strings.Contains(tok.Data, markAction) || strings.Contains(tok.Data, markControl) {
				c.problem(RuleScriptDynamic, p, line, "template actions and {{if}}/{{range}}/{{with}} must not form part of an element name")
			}
			for _, attr := range tok.Attr {
				if dynamicAttrName(attr.Key) {
					c.problem(RuleScriptDynamic, p, line, "template actions and {{if}}/{{range}}/{{with}} must not form part of an attribute name; write the whole name in each branch")
				}
			}
			if tt == html.StartTagToken && rawTextElements[tok.Data] {
				z.NextIsNotRawText()
			}
		}
	}
}

// dynamicAttrName reports whether an attribute name is put together from
// literal text and actions or from literal pieces in different control
// structures. An action that is the whole name is fine: html/template only
// lets it produce a plain name.
func dynamicAttrName(name string) bool {
	if !strings.Contains(name, markAction) && !strings.Contains(name, markControl) {
		return false
	}
	if strings.HasPrefix(name, "data-") || strings.HasPrefix(name, "aria-") {
		return false
	}
	fields := strings.Fields(strings.ReplaceAll(name, markControl, " "))
	if len(fields) > 1 {
		return true
	}
	return len(fields) == 1 && fields[0] != markAction && strings.Contains(fields[0], markAction)
}

// escapeCheck runs the html/template escaper over a page. Escaping errors,
// such as an action in an ambiguous attribute context, would make the page
// fail on every request.
func (c *checker) escapeCheck(set *template.Template, page string, paths map[string]string) {
	err := runEscaper(set)
	var te *template.Error
	if !errors.As(err, &te) {
		return
	}
	where, line := page, te.Line
	if te.Node != nil {
		// The node knows its file and line; te.Name is often the page's
		// top-level template.
		loc, _ := (*parse.Tree)(nil).ErrorContext(te.Node)
		if parts := strings.Split(loc, ":"); len(parts) >= 2 {
			if p, ok := paths[parts[0]]; ok {
				where = p
				line, _ = strconv.Atoi(parts[1])
			}
		}
	} else if t := set.Lookup(te.Name); t != nil && t.Tree != nil {
		if p, ok := paths[t.Tree.ParseName]; ok {
			where = p
		}
	}
	c.problem(RuleTemplateEscape, where, line, "%s", te.Description)
}

// runEscaper executes the base template into a writer that fails at once.
// html/template escapes the template and everything it calls before it
// writes anything, so escaping errors come back without the page running.
func runEscaper(set *template.Template) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("template panicked: %v", r)
		}
	}()
	return set.ExecuteTemplate(failWriter{}, baseTemplate, nil)
}

type failWriter struct{}

var errStopWrite = errors.New("themepkg: output discarded")

func (failWriter) Write([]byte) (int, error) { return 0, errStopWrite }

// segment records where a piece of expanded text came from.
type segment struct {
	start int    // offset in the expanded text
	path  string // package path of the template file
	pos   int    // offset in that file
}

// expander inlines a page's templates the way they render, collecting the
// text of every branch in order. a holds the literal text alone; b has a
// marker for each action and around each control structure.
type expander struct {
	set        *template.Template
	paths      map[string]string
	sources    map[string]string
	inline     bool
	a, b       strings.Builder
	segA, segB []segment
	stack      map[string]bool
	over       bool
}

func newExpander(set *template.Template, paths, sources map[string]string, inline bool) *expander {
	return &expander{set: set, paths: paths, sources: sources, inline: inline, stack: map[string]bool{}}
}

func (x *expander) walkTemplate(name string) {
	t := x.set.Lookup(name)
	if t == nil || t.Tree == nil || x.stack[name] {
		x.b.WriteString(markAction)
		return
	}
	x.stack[name] = true
	x.walk(t.Tree.Root, t.Tree)
	delete(x.stack, name)
}

func (x *expander) walk(n parse.Node, tree *parse.Tree) {
	if x.over || n == nil {
		return
	}
	switch n := n.(type) {
	case *parse.ListNode:
		if n == nil {
			return
		}
		for _, child := range n.Nodes {
			x.walk(child, tree)
		}
	case *parse.TextNode:
		p := x.paths[tree.ParseName]
		x.segA = append(x.segA, segment{start: x.a.Len(), path: p, pos: int(n.Pos)})
		x.segB = append(x.segB, segment{start: x.b.Len(), path: p, pos: int(n.Pos)})
		x.a.Write(n.Text)
		x.b.Write(n.Text)
		if x.a.Len() > maxPageText {
			x.over = true
		}
	case *parse.ActionNode:
		x.b.WriteString(markAction)
	case *parse.IfNode:
		x.branch(&n.BranchNode, tree)
	case *parse.RangeNode:
		x.branch(&n.BranchNode, tree)
	case *parse.WithNode:
		x.branch(&n.BranchNode, tree)
	case *parse.TemplateNode:
		if x.inline {
			x.walkTemplate(n.Name)
		} else {
			x.b.WriteString(markAction)
		}
	}
}

func (x *expander) branch(n *parse.BranchNode, tree *parse.Tree) {
	x.b.WriteString(markControl)
	x.walk(n.List, tree)
	x.b.WriteString(markControl)
	if n.ElseList != nil {
		x.walk(n.ElseList, tree)
		x.b.WriteString(markControl)
	}
}

// locate maps offsets in the expanded text back to template files.
func (x *expander) locate(segs []segment) locate {
	return func(offset int) (string, int) {
		i := sort.Search(len(segs), func(i int) bool { return segs[i].start > offset }) - 1
		if i < 0 {
			return "", 0
		}
		s := segs[i]
		return s.path, lineAt(x.sources[s.path], s.pos+offset-s.start)
	}
}

var parseErrorPos = regexp.MustCompile(`^template: [^:]+:(\d+):`)

// parseErrorLine extracts the line from a template parse error.
func parseErrorLine(err error) int {
	if m := parseErrorPos.FindStringSubmatch(err.Error()); m != nil {
		line, _ := strconv.Atoi(m[1])
		return line
	}
	return 0
}

// parseErrorMessage strips the "template: name:line:" prefix.
func parseErrorMessage(err error) string {
	msg := err.Error()
	if m := parseErrorPos.FindStringIndex(msg); m != nil {
		msg = strings.TrimSpace(msg[m[1]:])
	}
	return msg
}
