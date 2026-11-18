// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package theme

import (
	"html/template"
	"reflect"
	"sync"
	"time"

	"github.com/0xmattg/go-press/core/content"
	"github.com/0xmattg/go-press/core/hook"
	coreI18n "github.com/0xmattg/go-press/core/i18n"
	"github.com/0xmattg/go-press/core/mail"
	coreMedia "github.com/0xmattg/go-press/core/media"
	"github.com/0xmattg/go-press/core/menu"
	"github.com/0xmattg/go-press/core/option"
	"github.com/0xmattg/go-press/core/rewrite"
	"github.com/0xmattg/go-press/core/taxonomy"

	"gorm.io/gorm"
)

var (
	checkFuncsOnce sync.Once
	checkFuncs     template.FuncMap
)

// TemplateCheckFuncs returns a function map for checking templates without an
// engine, as the theme package checker does. It holds every function that
// BaseFuncMap provides on a fully configured engine, with the same names and
// signatures, so templates parse exactly as they would on a site. Every
// function returns zero values and does nothing, so executing a template with
// it has no side effects.
func TemplateCheckFuncs() template.FuncMap {
	checkFuncsOnce.Do(func() {
		b := &BaseTheme{}
		b.InitBase(checkApp{}, "", "", nil)
		checkFuncs = make(template.FuncMap)
		for name, fn := range b.BaseFuncMap() {
			checkFuncs[name] = stubFunc(fn)
		}
	})
	out := make(template.FuncMap, len(checkFuncs))
	for name, fn := range checkFuncs {
		out[name] = fn
	}
	return out
}

// stubFunc returns a function of fn's type that returns zero values.
func stubFunc(fn interface{}) interface{} {
	typ := reflect.TypeOf(fn)
	return reflect.MakeFunc(typ, func([]reflect.Value) []reflect.Value {
		out := make([]reflect.Value, typ.NumOut())
		for i := range out {
			out[i] = reflect.Zero(typ.Out(i))
		}
		return out
	}).Interface()
}

// checkApp has every engine service BaseFuncMap looks for, so the map it
// builds is complete. The services are empty: TemplateCheckFuncs replaces
// every function before any could use them.
type checkApp struct{}

func (checkApp) Database() *gorm.DB                 { return nil }
func (checkApp) ContentRepo() *content.Repository   { return nil }
func (checkApp) TaxonomyRepo() *taxonomy.Repository { return nil }
func (checkApp) ContentRegistry() *content.Registry { return nil }
func (checkApp) OptionsStore() *option.Store        { return nil }
func (checkApp) RewriteEngine() *rewrite.Engine     { return &rewrite.Engine{} }
func (checkApp) SEOBuilder() *rewrite.SEOBuilder    { return &rewrite.SEOBuilder{} }
func (checkApp) MenuStore() *menu.Store             { return &menu.Store{} }
func (checkApp) MediaRepo() *coreMedia.Repository   { return &coreMedia.Repository{} }
func (checkApp) I18nManager() *coreI18n.Manager     { return &coreI18n.Manager{} }
func (checkApp) HookBus() *hook.Bus                 { return &hook.Bus{} }
func (checkApp) MailSender() mail.Sender            { return nil }
func (checkApp) SiteLocation() *time.Location       { return time.UTC }
