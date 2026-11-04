package commerce

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/0xmattg/go-press/config"
	"github.com/0xmattg/go-press/core"
	corecommerce "github.com/0xmattg/go-press/core/commerce"
	"github.com/0xmattg/go-press/core/content"
	"github.com/0xmattg/go-press/core/hook"
	coreI18n "github.com/0xmattg/go-press/core/i18n"
	"github.com/0xmattg/go-press/core/option"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var configuredOfflineOptions = map[string]string{
	optOfflineAccountName:   "Test Store LLC",
	optOfflineAccountNumber: "TEST-0000-1234",
}

// stubDisplayGateway is an always-available display-type gateway used to
// prove that an unconfigured offline gateway is filtered individually rather
// than disabling checkout as a whole.
type stubDisplayGateway struct{}

func (stubDisplayGateway) ID() string                { return "stub-display" }
func (stubDisplayGateway) Title(*gin.Context) string { return "Stub" }
func (stubDisplayGateway) Icon() string              { return "" }
func (stubDisplayGateway) Capabilities() corecommerce.Capabilities {
	return corecommerce.Capabilities{}
}
func (stubDisplayGateway) StartPayment(*gin.Context, corecommerce.PaymentRequest) (corecommerce.PaymentAction, error) {
	return corecommerce.DisplayAction{Title: "Stub"}, nil
}
func (stubDisplayGateway) Refund(*gin.Context, corecommerce.RefundRequest) error { return nil }

// offlineCheckoutFixture builds a store whose only built-in gateway is the
// offline bank transfer, configured from opts, with one product (stock 5) and
// a cart holding two of it. extra gateways are registered after offline.
func offlineCheckoutFixture(t *testing.T, opts map[string]string, extra ...corecommerce.PaymentGateway) (*gorm.DB, *Plugin, Cart, string) {
	t.Helper()
	db := commerceTestDB(t)
	bus := hook.New()
	e := &core.Engine{
		DB: db, Hooks: bus, Options: option.NewMemoryStore(opts),
		Config:  &config.Config{Site: config.SiteConfig{URL: "https://shop.example"}},
		Content: content.NewRepository(db),
	}
	p := &Plugin{engine: e, repo: NewRepository(db)}
	corecommerce.RegisterPaymentGateway(bus, offlineGateway{p: p})
	for _, g := range extra {
		corecommerce.RegisterPaymentGateway(bus, g)
	}
	now := time.Now().UTC()
	if err := db.Exec(`INSERT INTO gp_contents (id,type,status,title,slug,published_at,created_at,updated_at)
		VALUES (1,'product','published','Widget','widget',?,?,?)`, now, now, now).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ProductData{
		ContentID: 1, Type: "simple", PriceAmount: 2500, Currency: "USD",
		ManageStock: true, StockQty: 5, StockStatus: "instock",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ProductLookup{ContentID: 1, CurrentPrice: 2500, Currency: "USD", InStock: true}).Error; err != nil {
		t.Fatal(err)
	}
	cart := Cart{Token: "offline-cart", Currency: "USD"}
	if err := db.Create(&cart).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&CartItem{CartID: cart.ID, ProductContentID: 1, Qty: 2, UnitPriceCache: 2500}).Error; err != nil {
		t.Fatal(err)
	}
	key, err := p.cart().checkoutKey(cart.ID)
	if err != nil {
		t.Fatal(err)
	}
	return db, p, cart, key
}

func offlineCheckoutInput(method, key string) CheckoutInput {
	return CheckoutInput{
		Email: "buyer@example.test", Billing: corecommerce.Address{Name: "Buyer", Line1: "1 Test St"},
		ShipToBilling: true, PaymentMethod: method, CheckoutKey: key,
	}
}

// assertNoCheckoutSideEffects proves a refused checkout wrote nothing: no
// order or child rows, no payment intent, no reservation, untouched stock, and
// the cart (items and one-time key) left intact for a later retry.
func assertNoCheckoutSideEffects(t *testing.T, db *gorm.DB, cart Cart, key string) {
	t.Helper()
	for name, model := range map[string]interface{}{
		"orders": &Order{}, "order items": &OrderItem{}, "order addresses": &OrderAddress{},
		"payments": &Payment{}, "inventory ledger": &InventoryLedger{},
	} {
		var n int64
		if err := db.Model(model).Count(&n).Error; err != nil || n != 0 {
			t.Fatalf("%s rows = %d, err=%v; want none after refused checkout", name, n, err)
		}
	}
	var pd ProductData
	if err := db.First(&pd, "content_id = ?", 1).Error; err != nil || pd.StockQty != 5 {
		t.Fatalf("stock = %d, err=%v; want untouched 5", pd.StockQty, err)
	}
	var items int64
	if err := db.Model(&CartItem{}).Where("cart_id = ?", cart.ID).Count(&items).Error; err != nil || items != 1 {
		t.Fatalf("cart items = %d, err=%v; want retained", items, err)
	}
	var saved Cart
	if err := db.First(&saved, cart.ID).Error; err != nil || saved.CheckoutKey == nil || *saved.CheckoutKey != key {
		t.Fatalf("cart checkout key = %v, err=%v; want unchanged so the shopper can retry", saved.CheckoutKey, err)
	}
}

func TestOfflineGatewayAvailabilityRequiresAccountNameAndNumber(t *testing.T) {
	for name, tc := range map[string]struct {
		opts map[string]string
		want bool
	}{
		"empty":           {opts: map[string]string{}, want: false},
		"name only":       {opts: map[string]string{optOfflineAccountName: "Store"}, want: false},
		"number only":     {opts: map[string]string{optOfflineAccountNumber: "123"}, want: false},
		"whitespace only": {opts: map[string]string{optOfflineAccountName: "  ", optOfflineAccountNumber: "\t"}, want: false},
		"bank and note without account": {opts: map[string]string{
			optOfflineBankName: "Bank", optOfflineInstructions: "Use the order number",
		}, want: false},
		"name and number, no bank name": {opts: configuredOfflineOptions, want: true},
	} {
		p := &Plugin{engine: &core.Engine{Options: option.NewMemoryStore(tc.opts)}}
		if got := (offlineGateway{p: p}).Available(nil); got != tc.want {
			t.Errorf("%s: Available = %v, want %v", name, got, tc.want)
		}
		if got := p.SettingsData()["OfflineReady"]; got != tc.want {
			t.Errorf("%s: SettingsData OfflineReady = %v, want %v", name, got, tc.want)
		}
	}
	if (offlineGateway{}).Available(nil) {
		t.Fatal("offline gateway without a plugin must not be available")
	}
}

func TestCheckoutGatewayListExcludesUnconfiguredOffline(t *testing.T) {
	ids := func(views []gatewayView) []string {
		out := make([]string, 0, len(views))
		for _, v := range views {
			out = append(out, v.ID)
		}
		return out
	}
	c := checkoutTestContext("offline-cart")

	_, unconfigured, _, _ := offlineCheckoutFixture(t, map[string]string{})
	if got := unconfigured.gatewayViews(c, "USD"); len(got) != 0 {
		t.Fatalf("unconfigured gateway views = %v, want none", ids(got))
	}

	t.Run("configured", func(t *testing.T) {
		_, configured, _, _ := offlineCheckoutFixture(t, configuredOfflineOptions)
		if got := ids(configured.gatewayViews(c, "USD")); len(got) != 1 || got[0] != offlineGatewayID {
			t.Fatalf("configured gateway views = %v, want [offline]", got)
		}
	})
	t.Run("mixed", func(t *testing.T) {
		_, mixed, _, _ := offlineCheckoutFixture(t, map[string]string{}, stubDisplayGateway{})
		if got := ids(mixed.gatewayViews(c, "USD")); len(got) != 1 || got[0] != "stub-display" {
			t.Fatalf("mixed gateway views = %v, want only the available stub", got)
		}
	})
}

func TestPlaceOrderRefusedWhenOfflineGatewayUnconfigured(t *testing.T) {
	db, p, cart, key := offlineCheckoutFixture(t, map[string]string{
		optOfflineAccountName: "   ", optOfflineBankName: "Bank", optOfflineInstructions: "note",
	})
	// Both an explicit (stale or forged) offline choice and an empty choice
	// must be refused: the button being disabled is not the safeguard.
	for _, method := range []string{offlineGatewayID, ""} {
		order, action, err := p.checkout().PlaceOrder(checkoutTestContext(cart.Token), offlineCheckoutInput(method, key))
		if !errors.Is(err, ErrCheckoutUnavailable) || order != nil || action != nil {
			t.Fatalf("PlaceOrder(%q) = order:%v action:%T err:%v; want ErrCheckoutUnavailable", method, order, action, err)
		}
		assertNoCheckoutSideEffects(t, db, cart, key)
	}

	// Once the admin fills in the required details, the very same form and
	// cart check out normally with the bank details shown to the buyer.
	p.engine.Options = option.NewMemoryStore(configuredOfflineOptions)
	order, action, err := p.checkout().PlaceOrder(checkoutTestContext(cart.Token), offlineCheckoutInput(offlineGatewayID, key))
	if err != nil || order == nil {
		t.Fatalf("configured PlaceOrder = order:%v err:%v; want success", order, err)
	}
	display, ok := action.(corecommerce.DisplayAction)
	if !ok {
		t.Fatalf("configured action = %T, want DisplayAction", action)
	}
	values := map[string]bool{}
	for _, row := range display.Rows {
		values[row.Value] = true
	}
	if !values["Test Store LLC"] || !values["TEST-0000-1234"] {
		t.Fatalf("display rows = %+v, want account name and number", display.Rows)
	}
	if order.Status != OrderOnHold || order.PaymentMethod != offlineGatewayID {
		t.Fatalf("order = status %q method %q, want on_hold via offline", order.Status, order.PaymentMethod)
	}
	var reservations int64
	if err := db.Model(&InventoryLedger{}).Where("order_id = ? AND reason = ?", order.ID, "reserve").Count(&reservations).Error; err != nil || reservations != 1 {
		t.Fatalf("reservations = %d, err=%v; want 1", reservations, err)
	}
	var pd ProductData
	if err := db.First(&pd, "content_id = ?", 1).Error; err != nil || pd.StockQty != 3 {
		t.Fatalf("stock = %d, err=%v; want 3 reserved from 5", pd.StockQty, err)
	}
}

func TestPlaceOrderRejectsUnconfiguredOfflineWhenAnotherGatewayIsAvailable(t *testing.T) {
	db, p, cart, key := offlineCheckoutFixture(t, map[string]string{}, stubDisplayGateway{})
	order, _, err := p.checkout().PlaceOrder(checkoutTestContext(cart.Token), offlineCheckoutInput(offlineGatewayID, key))
	if !errors.Is(err, ErrNoGateway) || order != nil {
		t.Fatalf("PlaceOrder(offline) = order:%v err:%v; want ErrNoGateway", order, err)
	}
	assertNoCheckoutSideEffects(t, db, cart, key)
}

func TestOfflineStartPaymentFailsDefinitivelyWhenUnconfigured(t *testing.T) {
	p := &Plugin{engine: &core.Engine{Options: option.NewMemoryStore(map[string]string{})}}
	action, err := offlineGateway{p: p}.StartPayment(nil, corecommerce.PaymentRequest{OrderRef: "R-1"})
	if action != nil || !corecommerce.IsDefinitiveStartFailure(err) {
		t.Fatalf("StartPayment = %T, %v; want definitive start failure", action, err)
	}
}

func TestCheckoutUnavailableMessageIsTranslated(t *testing.T) {
	manager := coreI18n.NewManager("en")
	manager.LoadLocalesFS(commerceLocaleFS, "locales")
	p := &Plugin{engine: &core.Engine{I18n: manager}}
	for _, tc := range []struct{ lang, want string }{
		{lang: "en", want: "Orders cannot be processed right now. Please try again later."},
		{lang: "zh-CN", want: "当前暂时无法处理订单，请稍后再试。"},
	} {
		c, _ := gin.CreateTestContext(nil)
		c.Set(coreI18n.CtxKeyLang, tc.lang)
		c.Set(coreI18n.CtxKeyLocalizer, manager.NewLocalizer(tc.lang))
		wrapped := fmt.Errorf("checkout: %w", ErrCheckoutUnavailable)
		if got := p.checkoutErrorMessage(c, wrapped); got != tc.want {
			t.Fatalf("%s message = %q, want %q", tc.lang, got, tc.want)
		}
	}
}

func TestCheckoutTemplateDisablesPlaceOrderWithoutGateways(t *testing.T) {
	tmpl, err := template.New("").Funcs(template.FuncMap{
		"T": func(_ interface{}, key string) string { return key },
	}).ParseFiles(filepath.Join("templates", "commerce", "checkout.tmpl"))
	if err != nil {
		t.Fatal(err)
	}
	render := func(data map[string]interface{}) string {
		var out bytes.Buffer
		data["Cart"] = CartView{Currency: "USD"}
		if err := tmpl.ExecuteTemplate(&out, "content", data); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}

	unavailable := render(map[string]interface{}{"Notice": "orders-unavailable"})
	for _, want := range []string{
		`id="commerce-checkout-notice"`, "orders-unavailable",
		`disabled aria-describedby="commerce-checkout-notice"`,
	} {
		if !strings.Contains(unavailable, want) {
			t.Fatalf("unavailable checkout missing %q:\n%s", want, unavailable)
		}
	}
	if strings.Contains(unavailable, `name="payment_method"`) || strings.Contains(unavailable, "commerce.checkout.payment_method") {
		t.Fatalf("unavailable checkout must not offer a payment method:\n%s", unavailable)
	}

	available := render(map[string]interface{}{
		"Gateways": []gatewayView{{ID: offlineGatewayID, Title: "Bank transfer"}},
	})
	if strings.Contains(available, "disabled") || strings.Contains(available, "commerce-checkout-notice") {
		t.Fatalf("available checkout must keep Place order enabled without a notice:\n%s", available)
	}
	if !strings.Contains(available, `value="offline"`) {
		t.Fatalf("available checkout missing the offline option:\n%s", available)
	}
}
