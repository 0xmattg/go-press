package commerce

import (
	"bytes"
	"html/template"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/0xmattg/go-press/core"
	"github.com/0xmattg/go-press/core/admin"
	"github.com/0xmattg/go-press/core/content"
	coreI18n "github.com/0xmattg/go-press/core/i18n"
	coreTheme "github.com/0xmattg/go-press/core/theme"

	"github.com/gin-gonic/gin"
)

func TestCommerceStorefrontLocales(t *testing.T) {
	manager := coreI18n.NewManager("en")
	manager.LoadLocalesFS(commerceLocaleFS, "locales")
	p := &Plugin{engine: &core.Engine{I18n: manager}}

	for _, tc := range []struct {
		lang string
		want string
	}{
		{lang: "en", want: "Add to cart"},
		{lang: "zh-CN", want: "加入购物车"},
	} {
		c, _ := gin.CreateTestContext(nil)
		c.Set(coreI18n.CtxKeyLang, tc.lang)
		c.Set(coreI18n.CtxKeyLocalizer, manager.NewLocalizer(tc.lang))
		if got := p.t(c, "commerce.add_to_cart"); got != tc.want {
			t.Fatalf("%s translation = %q, want %q", tc.lang, got, tc.want)
		}
	}
	if got := p.t(nil, "commerce.cart.title"); got != "Cart" {
		t.Fatalf("default translation = %q, want Cart", got)
	}
}

func TestCommerceAdminLocalesAndNavigation(t *testing.T) {
	for _, tc := range []struct {
		lang        string
		wantProduct string
		wantSection string
	}{
		{lang: "en", wantProduct: "Products", wantSection: "Commerce"},
		{lang: "zh-CN", wantProduct: "商品", wantSection: "电商"},
	} {
		if got := commerceAdminCatalog.T(tc.lang, "admin.content_type.product"); got != tc.wantProduct {
			t.Fatalf("%s admin product label = %q, want %q", tc.lang, got, tc.wantProduct)
		}
		p := &Plugin{}
		got, ok := p.contributeNav([]admin.AdminMenuItem{}, "editor", tc.lang).([]admin.AdminMenuItem)
		if !ok || len(got) != 3 || got[0].Section != tc.wantSection {
			t.Fatalf("%s commerce nav = %#v", tc.lang, got)
		}
	}
}

func renderCommerceAdminTemplate(t *testing.T, path, lang string, data gin.H) string {
	t.Helper()
	tmpl, err := template.New("").Funcs(template.FuncMap{
		"X": func(_ interface{}, key, fallback string, args ...interface{}) string {
			if msg := commerceAdminCatalog.Message(lang, key); msg != "" {
				return coreI18n.FormatMessage(msg, args...)
			}
			return coreI18n.FormatMessage(fallback, args...)
		},
	}).ParseFiles(path)
	if err != nil {
		t.Fatal(err)
	}
	if data == nil {
		data = gin.H{}
	}
	data["AdminLanguage"] = lang
	var output bytes.Buffer
	if err := tmpl.ExecuteTemplate(&output, "content", data); err != nil {
		t.Fatal(err)
	}
	return output.String()
}

func TestCommerceAdminTemplatesRenderEnglishWithoutChineseCopy(t *testing.T) {
	settings := renderCommerceAdminTemplate(t, filepath.Join("templates", "admin", "settings.tmpl"), "en", gin.H{
		"PluginName": "Commerce", "PluginSlug": "commerce", "StoreCurrency": "USD",
		"StoreCountry": "US", "WeightUnit": "g", "ReservationTTL": "60",
	})
	orders := renderCommerceAdminTemplate(t, filepath.Join("templates", "admin", "orders.tmpl"), "en", gin.H{})
	detail := renderCommerceAdminTemplate(t, filepath.Join("templates", "admin", "order-detail.tmpl"), "en", gin.H{
		"Order":       Order{ID: 1, Number: "TEST-1", Currency: "USD", PaymentMethod: "offline"},
		"StatusLabel": "Pending payment", "Billing": OrderAddress{}, "Shipping": OrderAddress{},
		"CanMarkPaid": true, "CanShip": true, "CanCancel": true, "CanRefund": true,
		"Refundable": "10.00", "RefundKey": "refund:test",
	})

	for name, output := range map[string]string{"settings": settings, "orders": orders, "detail": detail} {
		if regexp.MustCompile(`\p{Han}`).MatchString(output) {
			t.Fatalf("English %s template leaked Chinese copy: %s", name, output)
		}
	}
	if !strings.Contains(settings, "Store Settings") || !strings.Contains(orders, "No orders yet") || !strings.Contains(detail, "Back to Orders") {
		t.Fatalf("English admin templates missed localized labels")
	}
}

func TestCommerceCatalogTitleSwitchesLanguage(t *testing.T) {
	manager := coreI18n.NewManager("en")
	manager.LoadLocalesFS(commerceLocaleFS, "locales")
	registry := content.NewRegistry()
	registerProductTypes(registry)
	typeDef := registry.GetType("product")

	for _, tc := range []struct {
		lang string
		want string
	}{
		{lang: "en", want: "Products"},
		{lang: "zh-CN", want: "商品"},
	} {
		c, _ := gin.CreateTestContext(nil)
		c.Set(coreI18n.CtxKeyLang, tc.lang)
		c.Set(coreI18n.CtxKeyLocalizer, manager.NewLocalizer(tc.lang))
		if got := coreTheme.LocalizedArchiveTitle(c, manager, typeDef); got != tc.want {
			t.Fatalf("%s catalog title = %q, want %q", tc.lang, got, tc.want)
		}
	}
}

func TestCartTemplateSwitchesLanguageWithRequest(t *testing.T) {
	manager := coreI18n.NewManager("en")
	manager.LoadLocalesFS(commerceLocaleFS, "locales")
	tmpl, err := template.New("").Funcs(template.FuncMap{
		"T": manager.Translate,
	}).ParseFiles(filepath.Join("templates", "commerce", "cart.tmpl"))
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		lang   string
		want   string
		reject string
	}{
		{lang: "en", want: "Your cart is empty.", reject: "购物车是空的。"},
		{lang: "zh-CN", want: "购物车是空的。", reject: "Your cart is empty."},
	} {
		c, _ := gin.CreateTestContext(nil)
		c.Set(coreI18n.CtxKeyLocalizer, manager.NewLocalizer(tc.lang))
		var output bytes.Buffer
		if err := tmpl.ExecuteTemplate(&output, "content", gin.H{
			"Ctx": c, "Cart": CartView{Empty: true},
		}); err != nil {
			t.Fatal(err)
		}
		if got := output.String(); !strings.Contains(got, tc.want) || strings.Contains(got, tc.reject) {
			t.Fatalf("%s cart output did not switch language: %s", tc.lang, got)
		}
	}
}

func TestCommerceStorefrontTemplatesContainNoHardcodedChinese(t *testing.T) {
	han := regexp.MustCompile(`\p{Han}`)
	paths, err := filepath.Glob(filepath.Join("templates", "commerce", "*.tmpl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no commerce storefront templates found")
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if han.Match(data) {
			t.Errorf("%s contains hardcoded Chinese storefront copy", path)
		}
	}
}
