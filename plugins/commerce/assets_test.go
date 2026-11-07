package commerce

import (
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xmattg/go-press/core/assets"
	"github.com/0xmattg/go-press/core/plugin"
)

// TestEmbeddedFilesServeEveryFragment is the S3a pilot check for this plugin:
// the storefront fragments, the order back-office fragments, the settings page
// and the locales all come from the embedded files.
func TestEmbeddedFilesServeEveryFragment(t *testing.T) {
	p := New()
	fsys := p.files()
	if fsys == nil {
		t.Fatal("plugin files must fall back to the embedded copy before Activate")
	}

	storefront := assets.Sub(fsys, "templates/commerce")
	for _, fragment := range []string{"cart", "checkout", "payment-inline", "order-received", "order-tracking", "account-orders"} {
		if !assets.Exists(storefront, fragment+".tmpl") {
			t.Errorf("storefront fragment %s is not embedded", fragment)
		}
	}
	for _, fragment := range []string{"orders", "order-detail"} {
		ref := p.adminTemplate(fragment)
		if !ref.Valid() || !assets.Exists(ref.FS, ref.Name) {
			t.Errorf("admin fragment %s is not embedded at %s", fragment, ref.Name)
		}
	}
	for _, name := range []string{plugin.SettingsTemplateName, "locales/en.json", "locales/admin/en.json", "locales/admin/zh-CN.json", "static/logo.svg"} {
		if !assets.Exists(fsys, name) {
			t.Errorf("%s is not embedded", name)
		}
	}
	if got := commerceAdminCatalog.T("en", "plugin.commerce.orders.title"); got == "plugin.commerce.orders.title" {
		t.Error("admin catalog must load from the embedded locales")
	}
}

// TestEmbeddedFilesMatchPluginDirectory guards the go:embed list: every
// runtime file under templates/, locales/ and static/ must be embedded.
func TestEmbeddedFilesMatchPluginDirectory(t *testing.T) {
	for _, dir := range []string{"templates", "locales", "static"} {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if strings.HasPrefix(d.Name(), ".") {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			name := filepath.ToSlash(path)
			if strings.HasPrefix(d.Name(), "_") {
				t.Errorf("%s starts with _ and is not embedded without the all: prefix", name)
				return nil
			}
			if _, err := fs.Stat(commerceFiles, name); err != nil {
				t.Errorf("%s is not embedded: %v", name, err)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
