// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

// Read-only data functions for templates.
//
// contentList, contentBySlug and taxonomyTerms let a template fetch public
// content without Go code, which is what file themes (installed packages with
// templates only) depend on. They read exactly what a visitor could open: a
// registered content type with a public route, published rows whose publish
// time has passed, and the request's content and taxonomy scopes, so a
// multilingual extension still partitions the results.
//
// Every call is bounded. A call returns at most DataFuncMaxItems rows and runs
// a fixed number of SQL statements whatever the limit; a request makes at most
// DataFuncMaxCalls distinct calls, and a repeated call with the same arguments
// reuses the first result. Bad arguments, an exhausted budget, or a failed
// query return an empty result and log one warning per request, function and
// reason. The functions never return an error, so they cannot abort a render.

package theme

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/0xmattg/go-press/core/content"
	"github.com/0xmattg/go-press/core/taxonomy"
	"github.com/0xmattg/go-press/pkg/logger"

	"github.com/gin-gonic/gin"
)

const (
	// DataFuncMaxItems is the most rows one data function call returns. A
	// larger limit is lowered to it.
	DataFuncMaxItems = 50
	// DataFuncMaxCalls is the most distinct data function calls one request
	// makes. Later calls return an empty result.
	DataFuncMaxCalls = 20
	// dataFuncMaxOffset bounds the contentList "offset" option.
	dataFuncMaxOffset = 50
)

// Data function names. BaseFuncMap sets them after the theme's own functions,
// so a theme cannot replace them.
const (
	funcContentList   = "contentList"
	funcContentBySlug = "contentBySlug"
	funcTaxonomyTerms = "taxonomyTerms"
)

// warnDataFunc logs data function warnings; tests replace it to inspect them.
var warnDataFunc = logger.Warn

// dataBudgetKey stores the request's *dataBudget on the gin.Context.
const dataBudgetKey = "core.theme_data_budget"

// dataBudget tracks one request's data function calls.
type dataBudget struct {
	mu     sync.Mutex
	calls  int
	cache  map[string]interface{}
	warned map[string]bool
}

// requestDataBudget returns the budget stored on c, creating it on first use.
func requestDataBudget(c *gin.Context) *dataBudget {
	if v, ok := c.Get(dataBudgetKey); ok {
		if budget, ok := v.(*dataBudget); ok {
			return budget
		}
	}
	budget := &dataBudget{cache: map[string]interface{}{}, warned: map[string]bool{}}
	c.Set(dataBudgetKey, budget)
	return budget
}

// dataCall is one parsed data function call.
type dataCall struct {
	c      *gin.Context
	budget *dataBudget
	name   string
	theme  string
}

// dataFuncMap returns the data functions bound to b.
func (b *BaseTheme) dataFuncMap() map[string]interface{} {
	return map[string]interface{}{
		funcContentList:   b.contentListFunc,
		funcContentBySlug: b.contentBySlugFunc,
		funcTaxonomyTerms: b.taxonomyTermsFunc,
	}
}

// startDataCall resolves the request from the template argument, which may be
// the *gin.Context (.Ctx) or the page data holding it ($). It returns false
// when there is no request or no engine: without the request the scopes
// cannot be applied, so nothing may be read.
func (b *BaseTheme) startDataCall(name string, ctxArg interface{}) (*dataCall, bool) {
	c := templateGinContext(ctxArg)
	if c == nil {
		warnDataFunc("theme data function needs the request; pass .Ctx or $",
			"theme", b.dataThemeName(), "function", name)
		return nil, false
	}
	call := &dataCall{c: c, budget: requestDataBudget(c), name: name, theme: b.dataThemeName()}
	if b.App == nil || b.App.Database() == nil || b.App.ContentRegistry() == nil {
		call.warn("engine services unavailable")
		return nil, false
	}
	return call, true
}

// warn logs reason once per request and function.
func (call *dataCall) warn(reason string, args ...interface{}) {
	key := call.name + "\x00" + reason
	call.budget.mu.Lock()
	seen := call.budget.warned[key]
	call.budget.warned[key] = true
	call.budget.mu.Unlock()
	if seen {
		return
	}
	path := ""
	if call.c.Request != nil && call.c.Request.URL != nil {
		path = call.c.Request.URL.Path
	}
	attrs := append([]interface{}{"theme", call.theme, "function", call.name, "reason", reason, "path", path}, args...)
	warnDataFunc("theme data function returned an empty result", attrs...)
}

// run returns the cached result for key, or charges one call to the budget
// and runs fetch. It reports false when the budget is exhausted.
func (call *dataCall) run(key string, fetch func() interface{}) (interface{}, bool) {
	key = call.name + "\x00" + key
	call.budget.mu.Lock()
	if cached, ok := call.budget.cache[key]; ok {
		call.budget.mu.Unlock()
		return cached, true
	}
	if call.budget.calls >= DataFuncMaxCalls {
		call.budget.mu.Unlock()
		call.warn("call budget exhausted", "max_calls", DataFuncMaxCalls)
		return nil, false
	}
	call.budget.calls++
	call.budget.mu.Unlock()

	result := fetch()
	call.budget.mu.Lock()
	call.budget.cache[key] = result
	call.budget.mu.Unlock()
	return result, true
}

func (b *BaseTheme) dataThemeName() string {
	if b == nil {
		return ""
	}
	return b.meta.Name
}

// publicType returns the registered content type when it has a public
// route, which keeps admin-only types such as contact messages and the types
// of a previous theme out of reach.
func (call *dataCall) publicType(b *BaseTheme, nameArg interface{}) (*content.ContentTypeDef, bool) {
	name, _ := stringArg(nameArg)
	typeDef := b.App.ContentRegistry().GetType(name)
	if typeDef == nil || !typeDef.HasPublicRoute() {
		call.warn("content type is not registered or has no public route", "type", name)
		return nil, false
	}
	return typeDef, true
}

// limitArg parses a limit from an integer or a numeric string, lowering it to
// DataFuncMaxItems with a warning.
func (call *dataCall) limitArg(v interface{}) (int, bool) {
	n, ok := intArg(v)
	if !ok || n < 1 {
		call.warn("limit must be a positive integer", "limit", fmt.Sprint(v))
		return 0, false
	}
	if n > DataFuncMaxItems {
		call.warn("limit lowered to the maximum", "limit", n, "max_items", DataFuncMaxItems)
		n = DataFuncMaxItems
	}
	return n, true
}

// optionPairs turns trailing key/value arguments into a map. Keys must be
// strings from allowed and may not repeat.
func (call *dataCall) optionPairs(args []interface{}, allowed ...string) (map[string]interface{}, bool) {
	if len(args)%2 != 0 {
		call.warn("options must be key/value pairs")
		return nil, false
	}
	opts := make(map[string]interface{}, len(args)/2)
	for i := 0; i < len(args); i += 2 {
		key, ok := args[i].(string)
		if !ok || !stringInList(allowed, key) {
			call.warn("unknown option", "option", fmt.Sprint(args[i]))
			return nil, false
		}
		if _, dup := opts[key]; dup {
			call.warn("option given twice", "option", key)
			return nil, false
		}
		opts[key] = args[i+1]
	}
	return opts, true
}

// intArg accepts the integer kinds templates produce and numeric strings, so
// a limit can come straight from a setting.
func intArg(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int8:
		return int(n), true
	case int16:
		return int(n), true
	case int32:
		return int(n), true
	case int64:
		if n > math.MaxInt32 || n < math.MinInt32 {
			return 0, false
		}
		return int(n), true
	case uint:
		if n > math.MaxInt32 {
			return 0, false
		}
		return int(n), true
	case uint8:
		return int(n), true
	case uint16:
		return int(n), true
	case uint32:
		if n > math.MaxInt32 {
			return 0, false
		}
		return int(n), true
	case uint64:
		if n > math.MaxInt32 {
			return 0, false
		}
		return int(n), true
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(n))
		if err != nil || parsed > math.MaxInt32 || parsed < math.MinInt32 {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

// boolArg accepts a bool or the strings true/false/1/0.
func boolArg(v interface{}) (bool, bool) {
	switch b := v.(type) {
	case bool:
		return b, true
	case string:
		switch strings.ToLower(strings.TrimSpace(b)) {
		case "true", "1":
			return true, true
		case "false", "0":
			return false, true
		}
	}
	return false, false
}

func stringArg(v interface{}) (string, bool) {
	s, ok := v.(string)
	return strings.TrimSpace(s), ok
}

func stringInList(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

// contentListOptions is a validated contentList call.
type contentListOptions struct {
	Type      string
	Limit     int
	Field     string
	Dir       string
	Taxonomy  string
	Term      string
	Exclude   uint
	Offset    int
	MetaKey   string
	MetaValue string
}

// contentListFunc implements
//
//	contentList CTX TYPE LIMIT [KEY VALUE]...
//
// with the options order ("published_at desc"), taxonomy and term, exclude
// (a content ID), offset (0–50), and meta_key and meta_value. It returns the
// same item views as archive pages.
func (b *BaseTheme) contentListFunc(ctxArg, typeName, limit interface{}, args ...interface{}) []map[string]interface{} {
	call, ok := b.startDataCall(funcContentList, ctxArg)
	if !ok {
		return nil
	}
	opts, ok := b.parseContentList(call, typeName, limit, args)
	if !ok {
		return nil
	}
	result, ok := call.run(fmt.Sprintf("%+v", opts), func() interface{} {
		return b.fetchContentList(call, opts)
	})
	if !ok {
		return nil
	}
	views, _ := result.([]map[string]interface{})
	return views
}

func (b *BaseTheme) parseContentList(call *dataCall, typeName, limit interface{}, args []interface{}) (contentListOptions, bool) {
	var opts contentListOptions
	typeDef, ok := call.publicType(b, typeName)
	if !ok {
		return opts, false
	}
	opts.Type = typeDef.Name
	if opts.Limit, ok = call.limitArg(limit); !ok {
		return opts, false
	}
	pairs, ok := call.optionPairs(args, "order", "taxonomy", "term", "exclude", "offset", "meta_key", "meta_value")
	if !ok {
		return opts, false
	}

	opts.Field, opts.Dir = archiveOrderField(typeDef), archiveOrderDir(typeDef)
	if v, set := pairs["order"]; set {
		order, _ := stringArg(v)
		parts := strings.Fields(strings.ToLower(order))
		if len(parts) == 0 || len(parts) > 2 || !content.IsPublicOrderField(parts[0]) {
			call.warn("order must be a sortable field with an optional asc or desc", "order", fmt.Sprint(v))
			return opts, false
		}
		opts.Field, opts.Dir = parts[0], "DESC"
		if len(parts) == 2 {
			switch parts[1] {
			case "asc":
				opts.Dir = "ASC"
			case "desc":
			default:
				call.warn("order must be a sortable field with an optional asc or desc", "order", fmt.Sprint(v))
				return opts, false
			}
		}
	}

	_, hasTax := pairs["taxonomy"]
	_, hasTerm := pairs["term"]
	if hasTax || hasTerm {
		opts.Taxonomy, _ = stringArg(pairs["taxonomy"])
		opts.Term, _ = stringArg(pairs["term"])
		if opts.Taxonomy == "" || opts.Term == "" {
			call.warn("taxonomy and term must be given together")
			return opts, false
		}
		if b.App.ContentRegistry().GetTaxonomy(opts.Taxonomy) == nil || !stringInList(typeDef.Taxonomies, opts.Taxonomy) {
			call.warn("taxonomy is not declared on the content type", "type", opts.Type, "taxonomy", opts.Taxonomy)
			return opts, false
		}
	}

	if v, set := pairs["exclude"]; set {
		id, ok := intArg(v)
		if !ok || id < 1 {
			call.warn("exclude must be a content ID", "exclude", fmt.Sprint(v))
			return opts, false
		}
		opts.Exclude = uint(id)
	}

	if v, set := pairs["offset"]; set {
		offset, ok := intArg(v)
		if !ok || offset < 0 || offset > dataFuncMaxOffset {
			call.warn("offset must be between 0 and 50", "offset", fmt.Sprint(v))
			return opts, false
		}
		opts.Offset = offset
	}

	_, hasKey := pairs["meta_key"]
	_, hasValue := pairs["meta_value"]
	if hasKey || hasValue {
		var keyOK, valueOK bool
		opts.MetaKey, keyOK = stringArg(pairs["meta_key"])
		opts.MetaValue, valueOK = pairs["meta_value"].(string)
		if !keyOK || !valueOK || opts.MetaKey == "" {
			call.warn("meta_key and meta_value must be given together")
			return opts, false
		}
	}
	return opts, true
}

func (b *BaseTheme) fetchContentList(call *dataCall, opts contentListOptions) interface{} {
	empty := []map[string]interface{}{}
	q := content.NewQuery(content.ScopedDB(call.c, b.App.Database())).Type(opts.Type).Published()
	if opts.Taxonomy != "" {
		repo := b.App.TaxonomyRepo()
		if repo == nil {
			call.warn("taxonomy repository unavailable")
			return empty
		}
		term, err := repo.FindByTypeAndSlugContext(taxonomy.RequestContext(call.c), opts.Taxonomy, opts.Term)
		if err != nil || term == nil || term.ID == 0 {
			// An unknown term in the current scope is an empty list, not a
			// template mistake.
			return empty
		}
		q = q.TaxonomyID(term.ID)
	}
	if opts.Exclude != 0 {
		q = q.ExcludeID(opts.Exclude)
	}
	if opts.MetaKey != "" {
		q = q.Meta(opts.MetaKey, opts.MetaValue)
	}
	items, err := q.OrderStable(opts.Field, opts.Dir).Offset(opts.Offset).Limit(opts.Limit).Get()
	if err != nil {
		call.warn("query failed", "error", err)
		return empty
	}
	return b.contentViews(call.c, items)
}

// contentBySlugFunc implements
//
//	contentBySlug CTX TYPE SLUG
//
// and returns the item view, or nil when no public row matches in the current
// scope.
func (b *BaseTheme) contentBySlugFunc(ctxArg, typeName, slugArg interface{}) map[string]interface{} {
	call, ok := b.startDataCall(funcContentBySlug, ctxArg)
	if !ok {
		return nil
	}
	typeDef, ok := call.publicType(b, typeName)
	if !ok {
		return nil
	}
	slug, _ := stringArg(slugArg)
	if slug == "" {
		call.warn("slug must not be empty", "type", typeDef.Name)
		return nil
	}
	result, ok := call.run(typeDef.Name+"\x00"+slug, func() interface{} {
		items, err := content.NewQuery(content.ScopedDB(call.c, b.App.Database())).
			Type(typeDef.Name).Slug(slug).Published().
			OrderStable("published_at", "DESC").Limit(1).Get()
		if err != nil {
			call.warn("query failed", "error", err)
			return map[string]interface{}(nil)
		}
		if len(items) == 0 {
			return map[string]interface{}(nil)
		}
		return b.contentViews(call.c, items)[0]
	})
	if !ok {
		return nil
	}
	view, _ := result.(map[string]interface{})
	return view
}

// taxonomyTermsOptions is a validated taxonomyTerms call.
type taxonomyTermsOptions struct {
	Taxonomy  string
	Limit     int
	Order     string
	HideEmpty bool
}

// taxonomyTermsFunc implements
//
//	taxonomyTerms CTX TAXONOMY [KEY VALUE]...
//
// with the options limit (1–50, default 50), order ("count" or "name") and
// hide_empty. It returns term views with Count, Description and ParentID.
func (b *BaseTheme) taxonomyTermsFunc(ctxArg, nameArg interface{}, args ...interface{}) []map[string]interface{} {
	call, ok := b.startDataCall(funcTaxonomyTerms, ctxArg)
	if !ok {
		return nil
	}
	name, _ := stringArg(nameArg)
	opts := taxonomyTermsOptions{Taxonomy: name, Limit: DataFuncMaxItems, Order: taxonomy.PublicOrderCount}
	if b.App.ContentRegistry().GetTaxonomy(opts.Taxonomy) == nil {
		call.warn("taxonomy is not registered", "taxonomy", opts.Taxonomy)
		return nil
	}
	pairs, ok := call.optionPairs(args, "limit", "order", "hide_empty")
	if !ok {
		return nil
	}
	if v, set := pairs["limit"]; set {
		if opts.Limit, ok = call.limitArg(v); !ok {
			return nil
		}
	}
	if v, set := pairs["order"]; set {
		order, _ := stringArg(v)
		order = strings.ToLower(order)
		if order != taxonomy.PublicOrderCount && order != taxonomy.PublicOrderName {
			call.warn("order must be count or name", "order", fmt.Sprint(v))
			return nil
		}
		opts.Order = order
	}
	if v, set := pairs["hide_empty"]; set {
		if opts.HideEmpty, ok = boolArg(v); !ok {
			call.warn("hide_empty must be true or false", "hide_empty", fmt.Sprint(v))
			return nil
		}
	}
	result, ok := call.run(fmt.Sprintf("%+v", opts), func() interface{} {
		empty := []map[string]interface{}{}
		repo := b.App.TaxonomyRepo()
		if repo == nil {
			call.warn("taxonomy repository unavailable")
			return empty
		}
		items, err := repo.ListPublicContext(taxonomy.RequestContext(call.c), opts.Taxonomy, opts.Order, opts.Limit, opts.HideEmpty)
		if err != nil {
			call.warn("query failed", "error", err)
			return empty
		}
		views := make([]map[string]interface{}, len(items))
		for i, item := range items {
			view := taxonomyView(call.c, b.App, item)
			view["Count"] = item.Count
			view["Description"] = item.Description
			view["ParentID"] = item.ParentID
			views[i] = view
		}
		return views
	})
	if !ok {
		return nil
	}
	views, _ := result.([]map[string]interface{})
	return views
}

// dataFuncStubs returns empty-result data functions for template parsing
// without an engine, such as package checks.
func dataFuncStubs() map[string]interface{} {
	return map[string]interface{}{
		funcContentList: func(_, _, _ interface{}, _ ...interface{}) []map[string]interface{} {
			return nil
		},
		funcContentBySlug: func(_, _, _ interface{}) map[string]interface{} {
			return nil
		},
		funcTaxonomyTerms: func(_, _ interface{}, _ ...interface{}) []map[string]interface{} {
			return nil
		},
	}
}
