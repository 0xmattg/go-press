package admin

import (
	"errors"

	coreI18n "go-press/core/i18n"
)

// This file defines the small, per-domain interfaces the admin Handler depends
// on instead of coupling to concrete func-field bags. The engine still injects
// the existing *ThemeManager / *CacheCallbacks / ... closure structs, which now
// double as the default implementations of these interfaces. Splitting the
// God-object wiring into focused capabilities keeps handlers readable and makes
// the admin surface trivially mockable in tests.
//
// Every method is nil-safe on both the receiver and the underlying func field,
// so a partially wired struct degrades gracefully (no-op / zero value / the
// unavailable sentinel) instead of panicking. Handlers still guard the
// domain-level interface for "capability not wired at all" before use.

// errAdminCapabilityUnavailable is returned by controller methods whose backing
// function was not provided. It surfaces as a generic failure in the handler's
// existing error branch; in the real engine every function is wired, so this is
// only reachable under partial (e.g. test) construction.
var errAdminCapabilityUnavailable = errors.New("admin capability unavailable")

// Compile-time guarantees that the closure structs the engine injects satisfy
// the domain interfaces the handler depends on.
var (
	_ ThemeController    = (*ThemeManager)(nil)
	_ CacheController    = (*CacheCallbacks)(nil)
	_ RedirectController = (*RedirectCallbacks)(nil)
	_ PluginController   = (*PluginCallbacks)(nil)
	_ SitemapController  = (*SitemapCallbacks)(nil)
	_ MenuController     = (*MenuCallbacks)(nil)
)

// ThemeController exposes theme management to admin handlers.
type ThemeController interface {
	Switch(name string) error
	Active() string
	Available() []ThemeDisplayInfo
	ImportDemo(slug string) error
	SettingsTemplate(slug string) string
	LocaleCatalog(slug string) *coreI18n.Catalog
}

func (m *ThemeManager) Switch(name string) error {
	if m == nil || m.SwitchFn == nil {
		return errAdminCapabilityUnavailable
	}
	return m.SwitchFn(name)
}

func (m *ThemeManager) Active() string {
	if m == nil || m.ActiveFn == nil {
		return ""
	}
	return m.ActiveFn()
}

func (m *ThemeManager) Available() []ThemeDisplayInfo {
	if m == nil || m.AvailableFn == nil {
		return nil
	}
	return m.AvailableFn()
}

func (m *ThemeManager) ImportDemo(slug string) error {
	if m == nil || m.ImportDemoFn == nil {
		return errAdminCapabilityUnavailable
	}
	return m.ImportDemoFn(slug)
}

func (m *ThemeManager) SettingsTemplate(slug string) string {
	if m == nil || m.SettingsTemplateFn == nil {
		return ""
	}
	return m.SettingsTemplateFn(slug)
}

func (m *ThemeManager) LocaleCatalog(slug string) *coreI18n.Catalog {
	if m == nil || m.LocaleCatalogFn == nil {
		return nil
	}
	return m.LocaleCatalogFn(slug)
}

// CacheController exposes cache management to admin handlers.
type CacheController interface {
	Status() CacheManagerInfo
	FlushAll()
	FlushPage()
	FlushFrag()
}

func (c *CacheCallbacks) Status() CacheManagerInfo {
	if c == nil || c.StatusFn == nil {
		return CacheManagerInfo{}
	}
	return c.StatusFn()
}

func (c *CacheCallbacks) FlushAll() {
	if c == nil || c.FlushAllFn == nil {
		return
	}
	c.FlushAllFn()
}

func (c *CacheCallbacks) FlushPage() {
	if c == nil || c.FlushPageFn == nil {
		return
	}
	c.FlushPageFn()
}

func (c *CacheCallbacks) FlushFrag() {
	if c == nil || c.FlushFragFn == nil {
		return
	}
	c.FlushFragFn()
}

// RedirectController exposes redirect management to admin handlers.
type RedirectController interface {
	All() []RedirectInfo
	Add(source, target string, code int) error
	Remove(source string) error
}

func (c *RedirectCallbacks) All() []RedirectInfo {
	if c == nil || c.AllFn == nil {
		return nil
	}
	return c.AllFn()
}

func (c *RedirectCallbacks) Add(source, target string, code int) error {
	if c == nil || c.AddFn == nil {
		return errAdminCapabilityUnavailable
	}
	return c.AddFn(source, target, code)
}

func (c *RedirectCallbacks) Remove(source string) error {
	if c == nil || c.RemoveFn == nil {
		return errAdminCapabilityUnavailable
	}
	return c.RemoveFn(source)
}

// PluginController exposes plugin management to admin handlers.
type PluginController interface {
	All() []PluginInfo
	Activate(name string) error
	Deactivate(name string) error
	SettingsTemplate(slug string) string
	SettingsData(slug string) map[string]interface{}
	SettingsSave(slug string, settings map[string]string)
	LocaleCatalog(slug string) *coreI18n.Catalog
}

func (c *PluginCallbacks) All() []PluginInfo {
	if c == nil || c.AllFn == nil {
		return nil
	}
	return c.AllFn()
}

func (c *PluginCallbacks) Activate(name string) error {
	if c == nil || c.ActivateFn == nil {
		return errAdminCapabilityUnavailable
	}
	return c.ActivateFn(name)
}

func (c *PluginCallbacks) Deactivate(name string) error {
	if c == nil || c.DeactivateFn == nil {
		return errAdminCapabilityUnavailable
	}
	return c.DeactivateFn(name)
}

func (c *PluginCallbacks) SettingsTemplate(slug string) string {
	if c == nil || c.SettingsTemplateFn == nil {
		return ""
	}
	return c.SettingsTemplateFn(slug)
}

func (c *PluginCallbacks) SettingsData(slug string) map[string]interface{} {
	if c == nil || c.SettingsDataFn == nil {
		return nil
	}
	return c.SettingsDataFn(slug)
}

func (c *PluginCallbacks) SettingsSave(slug string, settings map[string]string) {
	if c == nil || c.SettingsSaveFn == nil {
		return
	}
	c.SettingsSaveFn(slug, settings)
}

func (c *PluginCallbacks) LocaleCatalog(slug string) *coreI18n.Catalog {
	if c == nil || c.LocaleCatalogFn == nil {
		return nil
	}
	return c.LocaleCatalogFn(slug)
}

// SitemapController exposes sitemap generation to admin handlers.
type SitemapController interface {
	Generate() (int, error)
}

func (c *SitemapCallbacks) Generate() (int, error) {
	if c == nil || c.GenerateFn == nil {
		return 0, errAdminCapabilityUnavailable
	}
	return c.GenerateFn()
}

// MenuController exposes menu management to admin handlers.
type MenuController interface {
	All() ([]MenuInfo, error)
	GetByID(id uint) (*MenuInfo, error)
	Create(name, location string) error
	Update(id uint, name, location string) error
	Delete(id uint) error
	SaveItems(menuID uint, items []MenuItemInfo) error
	Locations() []MenuLocationInfo
	Reload()
}

func (c *MenuCallbacks) All() ([]MenuInfo, error) {
	if c == nil || c.AllFn == nil {
		return nil, errAdminCapabilityUnavailable
	}
	return c.AllFn()
}

func (c *MenuCallbacks) GetByID(id uint) (*MenuInfo, error) {
	if c == nil || c.GetByIDFn == nil {
		return nil, errAdminCapabilityUnavailable
	}
	return c.GetByIDFn(id)
}

func (c *MenuCallbacks) Create(name, location string) error {
	if c == nil || c.CreateFn == nil {
		return errAdminCapabilityUnavailable
	}
	return c.CreateFn(name, location)
}

func (c *MenuCallbacks) Update(id uint, name, location string) error {
	if c == nil || c.UpdateFn == nil {
		return errAdminCapabilityUnavailable
	}
	return c.UpdateFn(id, name, location)
}

func (c *MenuCallbacks) Delete(id uint) error {
	if c == nil || c.DeleteFn == nil {
		return errAdminCapabilityUnavailable
	}
	return c.DeleteFn(id)
}

func (c *MenuCallbacks) SaveItems(menuID uint, items []MenuItemInfo) error {
	if c == nil || c.SaveItemsFn == nil {
		return errAdminCapabilityUnavailable
	}
	return c.SaveItemsFn(menuID, items)
}

func (c *MenuCallbacks) Locations() []MenuLocationInfo {
	if c == nil || c.LocationsFn == nil {
		return nil
	}
	return c.LocationsFn()
}

func (c *MenuCallbacks) Reload() {
	if c == nil || c.ReloadFn == nil {
		return
	}
	c.ReloadFn()
}
