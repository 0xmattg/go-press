package commerce

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xmattg/go-press/config"
	"github.com/0xmattg/go-press/core"
	corecommerce "github.com/0xmattg/go-press/core/commerce"
	"github.com/0xmattg/go-press/core/content"
	"github.com/0xmattg/go-press/core/hook"
	"github.com/0xmattg/go-press/pkg/dbprefix"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// commerceTestDB uses SQLite behind GORM's already-shipped SQL callbacks. The
// production code remains PostgreSQL-only; this small test harness gives the
// transaction/compensation tests a real ACID database without a test server.
func commerceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbprefix.Set(dbprefix.DefaultPrefix)
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared&_foreign_keys=1"
	sqlDB, err := sqlOpenSQLite(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	sqlDB.SetMaxOpenConns(1)
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, WithoutReturning: true}), &gorm.Config{
		DisableAutomaticPing:   true,
		SkipDefaultTransaction: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	// SQLite serializes writes itself and has no FOR UPDATE syntax.
	db.ClauseBuilders["FOR"] = func(clause.Clause, clause.Builder) {}
	for _, stmt := range commerceTestSchema {
		if err := db.Exec(stmt).Error; err != nil {
			if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
				t.Skip("real transaction tests require CGO-backed SQLite")
			}
			t.Fatalf("create test schema: %v\n%s", err, stmt)
		}
	}
	return db
}

// Kept behind a helper so the blank driver import and database/sql dependency
// stay confined to this test file.
func sqlOpenSQLite(dsn string) (*sql.DB, error) { return sql.Open("sqlite3", dsn) }

var commerceTestSchema = []string{
	`CREATE TABLE gp_contents (
		id INTEGER PRIMARY KEY AUTOINCREMENT, type TEXT, status TEXT, title TEXT, slug TEXT,
		content TEXT, excerpt TEXT, image_url TEXT, author_id INTEGER, parent_id INTEGER,
		sort_order INTEGER, comment_status TEXT, published_at DATETIME, created_at DATETIME,
		updated_at DATETIME, deleted_at DATETIME
	)`,
	`CREATE TABLE gp_content_meta (
		id INTEGER PRIMARY KEY AUTOINCREMENT, content_id INTEGER, meta_key TEXT, meta_value TEXT
	)`,
	`CREATE TABLE gp_plgn_commerce_product_data (
		content_id INTEGER PRIMARY KEY, version INTEGER NOT NULL DEFAULT 1, type TEXT, sku TEXT, price_amount INTEGER, currency TEXT,
		sale_price_amount INTEGER, tax_class TEXT, manage_stock INTEGER, stock_qty INTEGER,
		stock_status TEXT, weight_grams INTEGER, virtual INTEGER, downloadable INTEGER,
		created_at DATETIME, updated_at DATETIME
	)`,
	`CREATE TABLE gp_plgn_commerce_product_lookup (
		content_id INTEGER PRIMARY KEY, current_price INTEGER, currency TEXT, in_stock INTEGER,
		sales INTEGER DEFAULT 0, rating REAL, updated_at DATETIME
	)`,
	`CREATE TABLE gp_plgn_commerce_orders (
		id INTEGER PRIMARY KEY AUTOINCREMENT, number TEXT UNIQUE, checkout_key TEXT UNIQUE,
		checkout_cart_id INTEGER, access_key TEXT, status TEXT,
		user_id INTEGER, email TEXT, currency TEXT, subtotal INTEGER, discount_total INTEGER,
		shipping_total INTEGER, tax_total INTEGER, grand_total INTEGER, payment_method TEXT,
		created_at DATETIME, updated_at DATETIME, paid_at DATETIME
	)`,
	`CREATE TABLE gp_plgn_commerce_order_items (
		id INTEGER PRIMARY KEY AUTOINCREMENT, order_id INTEGER, product_content_id INTEGER,
		variation_id INTEGER, name_snapshot TEXT, unit_price INTEGER, qty INTEGER,
		line_subtotal INTEGER, line_tax INTEGER, line_total INTEGER
	)`,
	`CREATE TABLE gp_plgn_commerce_order_addresses (
		id INTEGER PRIMARY KEY AUTOINCREMENT, order_id INTEGER, type TEXT, name TEXT, company TEXT,
		line1 TEXT, line2 TEXT, city TEXT, state TEXT, country TEXT, postcode TEXT, phone TEXT, email TEXT
	)`,
	`CREATE TABLE gp_plgn_commerce_payments (
		id INTEGER PRIMARY KEY AUTOINCREMENT, order_id INTEGER, gateway TEXT, txn_id TEXT,
		status TEXT, amount INTEGER, currency TEXT, idempotency_key TEXT UNIQUE, raw TEXT,
		created_at DATETIME
	)`,
	`CREATE TABLE gp_plgn_commerce_carts (
		id INTEGER PRIMARY KEY AUTOINCREMENT, token TEXT, checkout_key TEXT UNIQUE,
		user_id INTEGER, currency TEXT,
		created_at DATETIME, updated_at DATETIME
	)`,
	`CREATE TABLE gp_plgn_commerce_cart_items (
		id INTEGER PRIMARY KEY AUTOINCREMENT, cart_id INTEGER, product_content_id INTEGER,
		variation_id INTEGER, qty INTEGER, unit_price_cache INTEGER
	)`,
	`CREATE TABLE gp_plgn_commerce_order_notes (
		id INTEGER PRIMARY KEY AUTOINCREMENT, order_id INTEGER, author TEXT, note TEXT,
		is_customer_note INTEGER, created_at DATETIME
	)`,
	`CREATE TABLE gp_plgn_commerce_refunds (
		id INTEGER PRIMARY KEY AUTOINCREMENT, order_id INTEGER, payment_id INTEGER, amount INTEGER,
		currency TEXT, reason TEXT, status TEXT, idempotency_key TEXT UNIQUE, gateway TEXT,
		gateway_refund_id TEXT, error TEXT, raw TEXT, created_at DATETIME, updated_at DATETIME
	)`,
	`CREATE UNIQUE INDEX idx_test_refund_gateway_id
		ON gp_plgn_commerce_refunds(gateway, gateway_refund_id) WHERE gateway_refund_id <> ''`,
	`CREATE TABLE gp_plgn_commerce_inventory_ledger (
		id INTEGER PRIMARY KEY AUTOINCREMENT, product_ref INTEGER, delta INTEGER, reason TEXT,
		order_id INTEGER, created_at DATETIME
	)`,
}

type failingStartGateway struct{ sawKey bool }

func (*failingStartGateway) ID() string                { return "fail-start" }
func (*failingStartGateway) Title(*gin.Context) string { return "Fail" }
func (*failingStartGateway) Icon() string              { return "" }
func (*failingStartGateway) Capabilities() corecommerce.Capabilities {
	return corecommerce.Capabilities{}
}
func (g *failingStartGateway) StartPayment(_ *gin.Context, req corecommerce.PaymentRequest) (corecommerce.PaymentAction, error) {
	g.sawKey = req.IdempotencyKey != ""
	return nil, corecommerce.DefinitiveStartFailure(errors.New("local configuration unavailable"))
}
func (*failingStartGateway) Refund(*gin.Context, corecommerce.RefundRequest) error { return nil }

type settleThenFailGateway struct{ settler corecommerce.PaymentSettler }

func (*settleThenFailGateway) ID() string                { return "settle-then-fail" }
func (*settleThenFailGateway) Title(*gin.Context) string { return "Race" }
func (*settleThenFailGateway) Icon() string              { return "" }
func (*settleThenFailGateway) Capabilities() corecommerce.Capabilities {
	return corecommerce.Capabilities{}
}
func (g *settleThenFailGateway) StartPayment(_ *gin.Context, req corecommerce.PaymentRequest) (corecommerce.PaymentAction, error) {
	if err := g.settler.Settle(context.Background(), corecommerce.SettleRequest{
		OrderRef: req.OrderRef, Gateway: g.ID(), TxnID: "capture-race", Amount: req.Amount,
		Status: corecommerce.SettlePaid, IdempotencyKey: "race:" + req.OrderRef,
	}); err != nil {
		return nil, err
	}
	return nil, errors.New("response lost after remote success")
}
func (*settleThenFailGateway) Refund(*gin.Context, corecommerce.RefundRequest) error { return nil }

type ambiguousStartGateway struct{ calls atomic.Int32 }

func (*ambiguousStartGateway) ID() string                { return "ambiguous-start" }
func (*ambiguousStartGateway) Title(*gin.Context) string { return "Ambiguous" }
func (*ambiguousStartGateway) Icon() string              { return "" }
func (*ambiguousStartGateway) Capabilities() corecommerce.Capabilities {
	return corecommerce.Capabilities{}
}
func (g *ambiguousStartGateway) StartPayment(*gin.Context, corecommerce.PaymentRequest) (corecommerce.PaymentAction, error) {
	g.calls.Add(1)
	return nil, errors.New("response lost after request write")
}
func (*ambiguousStartGateway) Refund(*gin.Context, corecommerce.RefundRequest) error { return nil }

type legacyAutoRefundGateway struct{ failingStartGateway }

func (*legacyAutoRefundGateway) Capabilities() corecommerce.Capabilities {
	return corecommerce.Capabilities{Refund: true, PartialRefund: true}
}

type emptyResultRefundGateway struct{ legacyAutoRefundGateway }

func (*emptyResultRefundGateway) RefundWithResult(*gin.Context, corecommerce.RefundRequest) (corecommerce.RefundResult, error) {
	return corecommerce.RefundResult{}, nil
}

type countingRedirectGateway struct {
	calls   atomic.Int32
	entered chan struct{}
	release chan struct{}
}

func (*countingRedirectGateway) ID() string                { return "counting-redirect" }
func (*countingRedirectGateway) Title(*gin.Context) string { return "Count" }
func (*countingRedirectGateway) Icon() string              { return "" }
func (*countingRedirectGateway) Capabilities() corecommerce.Capabilities {
	return corecommerce.Capabilities{}
}
func (g *countingRedirectGateway) StartPayment(_ *gin.Context, _ corecommerce.PaymentRequest) (corecommerce.PaymentAction, error) {
	g.calls.Add(1)
	if g.entered != nil {
		select {
		case g.entered <- struct{}{}:
		default:
		}
	}
	if g.release != nil {
		<-g.release
	}
	return corecommerce.RedirectAction{URL: "https://pay.example/approve/one"}, nil
}
func (*countingRedirectGateway) Refund(*gin.Context, corecommerce.RefundRequest) error { return nil }

func TestPlaceOrderStartFailureKeepsCartAndCompensatesReservation(t *testing.T) {
	db := commerceTestDB(t)
	bus := hook.New()
	e := &core.Engine{
		DB: db, Hooks: bus, Config: &config.Config{Site: config.SiteConfig{URL: "https://shop.example"}},
		Content: content.NewRepository(db),
	}
	p := &Plugin{engine: e, repo: NewRepository(db)}
	gateway := &failingStartGateway{}
	corecommerce.RegisterPaymentGateway(bus, gateway)

	now := time.Now().UTC()
	if err := db.Exec(`INSERT INTO gp_contents (id,type,status,title,slug,published_at,created_at,updated_at)
		VALUES (1,'product','published','Widget','widget',?,?,?)`, now, now, now).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ProductData{ContentID: 1, Type: "simple", PriceAmount: 2500, Currency: "USD", ManageStock: true, StockQty: 5, StockStatus: "instock"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ProductLookup{ContentID: 1, CurrentPrice: 2500, Currency: "USD", InStock: true}).Error; err != nil {
		t.Fatal(err)
	}
	cart := Cart{Token: "cart-token", Currency: "USD"}
	if err := db.Create(&cart).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&CartItem{CartID: cart.ID, ProductContentID: 1, Qty: 2, UnitPriceCache: 2500}).Error; err != nil {
		t.Fatal(err)
	}
	checkoutKey, err := p.cart().checkoutKey(cart.ID)
	if err != nil {
		t.Fatal(err)
	}

	statusHooks := 0
	bus.AddAction(hookOrderStatusChanged, func(_ context.Context, _ ...interface{}) { statusHooks++ }, 10)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "https://shop.example/checkout", nil)
	c.Request.AddCookie(&http.Cookie{Name: cartCookie, Value: cart.Token})

	order, _, err := p.checkout().PlaceOrder(c, CheckoutInput{
		Email: "buyer@example.test", Billing: corecommerce.Address{Name: "Buyer"}, PaymentMethod: gateway.ID(),
		CheckoutKey: checkoutKey,
	})
	if err == nil || order == nil {
		t.Fatalf("PlaceOrder error/order = %v/%v, want compensated failure", err, order)
	}
	if !gateway.sawKey {
		t.Fatal("StartPayment did not receive an idempotency key")
	}
	var itemCount int64
	if err := db.Model(&CartItem{}).Where("cart_id = ?", cart.ID).Count(&itemCount).Error; err != nil || itemCount != 1 {
		t.Fatalf("cart items after failure = %d, err=%v; want retained", itemCount, err)
	}
	var pd ProductData
	if err := db.First(&pd, "content_id = ?", 1).Error; err != nil || pd.StockQty != 5 {
		t.Fatalf("stock after compensation = %d, err=%v; want 5", pd.StockQty, err)
	}
	var saved Order
	if err := db.First(&saved, order.ID).Error; err != nil || saved.Status != OrderFailed {
		t.Fatalf("order status = %q, err=%v; want failed", saved.Status, err)
	}
	var payment Payment
	if err := db.First(&payment, "order_id = ?", order.ID).Error; err != nil || payment.Status != OrderFailed {
		t.Fatalf("payment status = %q, err=%v; want failed", payment.Status, err)
	}
	var ledgers int64
	if err := db.Model(&InventoryLedger{}).Where("order_id = ?", order.ID).Count(&ledgers).Error; err != nil || ledgers != 2 {
		t.Fatalf("inventory ledger rows = %d, err=%v; want reserve+release", ledgers, err)
	}
	if statusHooks != 1 {
		t.Fatalf("status hooks = %d, want one post-commit failed notification", statusHooks)
	}
	var savedCart Cart
	if err := db.First(&savedCart, cart.ID).Error; err != nil || savedCart.CheckoutKey != nil {
		t.Fatalf("failed checkout key = %v, err=%v; want invalidated", savedCart.CheckoutKey, err)
	}
}

func TestPlaceOrderTreatsStartErrorAsSuccessWhenWebhookAdvancedOrder(t *testing.T) {
	db := commerceTestDB(t)
	bus := hook.New()
	e := &core.Engine{
		DB: db, Hooks: bus, Config: &config.Config{Site: config.SiteConfig{URL: "https://shop.example"}},
		Content: content.NewRepository(db),
	}
	p := &Plugin{engine: e, repo: NewRepository(db)}
	settler := orderSettler{p: p}
	corecommerce.SetSettler(bus, settler)
	gateway := &settleThenFailGateway{settler: settler}
	corecommerce.RegisterPaymentGateway(bus, gateway)

	now := time.Now().UTC()
	if err := db.Exec(`INSERT INTO gp_contents (id,type,status,title,slug,published_at,created_at,updated_at)
		VALUES (1,'product','published','Widget','widget',?,?,?)`, now, now, now).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ProductData{ContentID: 1, Type: "simple", PriceAmount: 2500, Currency: "USD", ManageStock: true, StockQty: 5, StockStatus: "instock"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ProductLookup{ContentID: 1, CurrentPrice: 2500, Currency: "USD", InStock: true}).Error; err != nil {
		t.Fatal(err)
	}
	cart := Cart{Token: "race-cart", Currency: "USD"}
	if err := db.Create(&cart).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&CartItem{CartID: cart.ID, ProductContentID: 1, Qty: 2, UnitPriceCache: 2500}).Error; err != nil {
		t.Fatal(err)
	}
	checkoutKey, err := p.cart().checkoutKey(cart.ID)
	if err != nil {
		t.Fatal(err)
	}
	statusHooks := 0
	bus.AddAction(hookOrderStatusChanged, func(_ context.Context, _ ...interface{}) { statusHooks++ }, 10)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "https://shop.example/checkout", nil)
	c.Request.AddCookie(&http.Cookie{Name: cartCookie, Value: cart.Token})

	order, action, err := p.checkout().PlaceOrder(c, CheckoutInput{
		Email: "buyer@example.test", Billing: corecommerce.Address{Name: "Buyer"}, PaymentMethod: gateway.ID(),
		CheckoutKey: checkoutKey,
	})
	if err != nil || order == nil {
		t.Fatalf("PlaceOrder = order:%v action:%T err:%v; want successful recovered checkout", order, action, err)
	}
	if _, ok := action.(corecommerce.CompletedAction); !ok {
		t.Fatalf("recovered action = %T, want CompletedAction", action)
	}
	if order.Status != OrderProcessing {
		t.Fatalf("recovered order status = %q, want processing", order.Status)
	}
	var itemCount int64
	if err := db.Model(&CartItem{}).Where("cart_id = ?", cart.ID).Count(&itemCount).Error; err != nil || itemCount != 0 {
		t.Fatalf("cart items = %d, err=%v; settled checkout snapshot must be consumed", itemCount, err)
	}
	var pd ProductData
	if err := db.First(&pd, "content_id = ?", 1).Error; err != nil || pd.StockQty != 3 {
		t.Fatalf("stock after settled race = %d, err=%v; want committed reservation at 3", pd.StockQty, err)
	}
	if statusHooks != 1 {
		t.Fatalf("status hooks = %d, want one payment transition", statusHooks)
	}
}

func TestPlaceOrderAmbiguousStartKeepsReservationForLatePaidWebhook(t *testing.T) {
	db := commerceTestDB(t)
	bus := hook.New()
	e := &core.Engine{
		DB: db, Hooks: bus, Config: &config.Config{Site: config.SiteConfig{URL: "https://shop.example"}},
		Content: content.NewRepository(db),
	}
	p := &Plugin{engine: e, repo: NewRepository(db)}
	settler := orderSettler{p: p}
	corecommerce.SetSettler(bus, settler)
	gateway := &ambiguousStartGateway{}
	corecommerce.RegisterPaymentGateway(bus, gateway)

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
	cart := Cart{Token: "late-webhook-cart", Currency: "USD"}
	if err := db.Create(&cart).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&CartItem{CartID: cart.ID, ProductContentID: 1, Qty: 2, UnitPriceCache: 2500}).Error; err != nil {
		t.Fatal(err)
	}
	checkoutKey, err := p.cart().checkoutKey(cart.ID)
	if err != nil {
		t.Fatal(err)
	}

	order, action, err := p.checkout().PlaceOrder(checkoutTestContext(cart.Token), CheckoutInput{
		Email: "buyer@example.test", Billing: corecommerce.Address{Name: "Buyer"},
		PaymentMethod: gateway.ID(), CheckoutKey: checkoutKey,
	})
	if err != nil || order == nil {
		t.Fatalf("ambiguous PlaceOrder = order:%v action:%T err:%v; want reconciliation handoff", order, action, err)
	}
	if _, ok := action.(corecommerce.CompletedAction); !ok {
		t.Fatalf("ambiguous action = %T, want CompletedAction handoff", action)
	}
	if order.Status != OrderPending {
		t.Fatalf("ambiguous order status = %q, want pending", order.Status)
	}
	var payment Payment
	if err := db.First(&payment, "order_id = ?", order.ID).Error; err != nil || payment.Status != paymentReconciliationState {
		t.Fatalf("ambiguous payment status = %q, err=%v; want reconciliation", payment.Status, err)
	}
	var pd ProductData
	if err := db.First(&pd, "content_id = ?", 1).Error; err != nil || pd.StockQty != 3 {
		t.Fatalf("stock before late webhook = %d, err=%v; want reserved at 3", pd.StockQty, err)
	}
	var cartItems int64
	if err := db.Model(&CartItem{}).Where("cart_id = ?", cart.ID).Count(&cartItems).Error; err != nil || cartItems != 0 {
		t.Fatalf("cart items after ambiguous start = %d, err=%v; want consumed", cartItems, err)
	}

	err = settler.Settle(context.Background(), corecommerce.SettleRequest{
		OrderRef: order.Number, Gateway: gateway.ID(), TxnID: "late-capture",
		Status: corecommerce.SettlePaid, Amount: corecommerce.New(order.GrandTotal, order.Currency),
		IdempotencyKey: "late-paid:" + order.Number,
	})
	if err != nil {
		t.Fatalf("late paid webhook: %v", err)
	}
	var saved Order
	if err := db.First(&saved, order.ID).Error; err != nil || saved.Status != OrderProcessing {
		t.Fatalf("order after late webhook = %q, err=%v; want processing", saved.Status, err)
	}
	if err := db.First(&payment, payment.ID).Error; err != nil || payment.Status != string(corecommerce.SettlePaid) || payment.TxnID != "late-capture" {
		t.Fatalf("payment after late webhook = status:%q txn:%q err:%v", payment.Status, payment.TxnID, err)
	}
	var releases int64
	if err := db.Model(&InventoryLedger{}).Where("order_id = ? AND reason = ?", order.ID, "release").Count(&releases).Error; err != nil || releases != 0 {
		t.Fatalf("release ledgers after late payment = %d, err=%v; want 0", releases, err)
	}
}

func TestPlaceOrderSequentialCheckoutReplayCreatesAndStartsOnce(t *testing.T) {
	db, p, gateway, cart := checkoutIdempotencyFixture(t, nil)
	_, key, err := p.cart().checkoutSnapshot(checkoutTestContext(cart.Token))
	if err != nil {
		t.Fatal(err)
	}
	_, secondViewKey, err := p.cart().checkoutSnapshot(checkoutTestContext(cart.Token))
	if err != nil {
		t.Fatal(err)
	}
	if secondViewKey != key {
		t.Fatalf("unchanged cart checkout keys = %q/%q; want same key", key, secondViewKey)
	}
	in := CheckoutInput{
		Email: "buyer@example.test", Billing: corecommerce.Address{Name: "Buyer"},
		PaymentMethod: gateway.ID(), CheckoutKey: key,
	}

	first, firstAction, err := p.checkout().PlaceOrder(checkoutTestContext(cart.Token), in)
	if err != nil || first == nil {
		t.Fatalf("first PlaceOrder = order:%v action:%T err:%v", first, firstAction, err)
	}
	firstRedirect, ok := firstAction.(corecommerce.RedirectAction)
	if !ok {
		t.Fatalf("first action = %T, want RedirectAction", firstAction)
	}
	second, secondAction, err := p.checkout().PlaceOrder(checkoutTestContext(cart.Token), in)
	if err != nil || second == nil || second.ID != first.ID {
		t.Fatalf("replay PlaceOrder = order:%v action:%T err:%v; want order %d", second, secondAction, err, first.ID)
	}
	secondRedirect, ok := secondAction.(corecommerce.RedirectAction)
	if !ok || secondRedirect.URL != firstRedirect.URL {
		t.Fatalf("replay action = %#v, want persisted redirect %#v", secondAction, firstRedirect)
	}
	assertSingleCheckoutEffects(t, db, gateway, first.ID)

	var savedCart Cart
	if err := db.First(&savedCart, cart.ID).Error; err != nil || savedCart.CheckoutKey != nil {
		t.Fatalf("successful checkout key = %v, err=%v; want invalidated", savedCart.CheckoutKey, err)
	}
	if err := p.cart().Add(checkoutTestContext(cart.Token), 1, 1); err != nil {
		t.Fatalf("add after checkout: %v", err)
	}
	nextKey, err := p.cart().checkoutKey(cart.ID)
	if err != nil {
		t.Fatal(err)
	}
	if nextKey == key {
		t.Fatal("new shopping cart reused the completed checkout key")
	}
}

func TestCheckoutKeyConcurrentInitializationUsesOneCartKey(t *testing.T) {
	db, p, _, cart := checkoutIdempotencyFixture(t, nil)
	type result struct {
		key string
		err error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			key, err := p.cart().checkoutKey(cart.ID)
			results <- result{key: key, err: err}
		}()
	}
	close(start)
	first, second := <-results, <-results
	if first.err != nil || second.err != nil || first.key == "" || first.key != second.key {
		t.Fatalf("concurrent keys = %#v / %#v; want one shared key", first, second)
	}
	var saved Cart
	if err := db.First(&saved, cart.ID).Error; err != nil || saved.CheckoutKey == nil || *saved.CheckoutKey != first.key {
		t.Fatalf("persisted cart key = %v, err=%v; want %q", saved.CheckoutKey, err, first.key)
	}
}

func TestPlaceOrderConcurrentCheckoutReplayStartsGatewayOnce(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	db, p, gateway, cart := checkoutIdempotencyFixture(t, &countingRedirectGateway{
		entered: entered,
		release: release,
	})
	key, err := p.cart().checkoutKey(cart.ID)
	if err != nil {
		t.Fatal(err)
	}
	in := CheckoutInput{
		Email: "buyer@example.test", Billing: corecommerce.Address{Name: "Buyer"},
		PaymentMethod: gateway.ID(), CheckoutKey: key,
	}
	type result struct {
		order  *Order
		action corecommerce.PaymentAction
		err    error
	}
	firstResult := make(chan result, 1)
	go func() {
		order, action, err := p.checkout().PlaceOrder(checkoutTestContext(cart.Token), in)
		firstResult <- result{order: order, action: action, err: err}
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first checkout did not reach StartPayment")
	}

	secondResult := make(chan result, 1)
	go func() {
		order, action, err := p.checkout().PlaceOrder(checkoutTestContext(cart.Token), in)
		secondResult <- result{order: order, action: action, err: err}
	}()
	var second result
	select {
	case second = <-secondResult:
	case <-time.After(2 * time.Second):
		t.Fatal("concurrent replay waited for or invoked the gateway")
	}
	if !errors.Is(second.err, ErrCheckoutInProgress) || second.order == nil || second.action != nil {
		t.Fatalf("concurrent replay = order:%v action:%T err:%v; want in-progress", second.order, second.action, second.err)
	}

	close(release)
	var first result
	select {
	case first = <-firstResult:
	case <-time.After(2 * time.Second):
		t.Fatal("first checkout did not complete")
	}
	if first.err != nil || first.order == nil || first.order.ID != second.order.ID {
		t.Fatalf("first/replay orders = %v/%v, first err=%v", first.order, second.order, first.err)
	}
	firstRedirect, ok := first.action.(corecommerce.RedirectAction)
	if !ok {
		t.Fatalf("first action = %T, want RedirectAction", first.action)
	}
	replayed, replayAction, err := p.checkout().PlaceOrder(checkoutTestContext(cart.Token), in)
	if err != nil || replayed == nil || replayed.ID != first.order.ID {
		t.Fatalf("persisted replay = order:%v action:%T err:%v", replayed, replayAction, err)
	}
	replayRedirect, ok := replayAction.(corecommerce.RedirectAction)
	if !ok || replayRedirect.URL != firstRedirect.URL {
		t.Fatalf("persisted replay action = %#v, want %#v", replayAction, firstRedirect)
	}
	assertSingleCheckoutEffects(t, db, gateway, first.order.ID)
}

func TestCheckoutKeyValidationIsStrict(t *testing.T) {
	key, err := newCheckoutKey()
	if err != nil {
		t.Fatal(err)
	}
	if !validCheckoutKey(key) || len(key) != 64 {
		t.Fatalf("generated checkout key %q is invalid", key)
	}
	for _, invalid := range []string{"", strings.Repeat("a", 63), strings.Repeat("a", 65), strings.Repeat("z", 64), " " + key} {
		if validCheckoutKey(invalid) {
			t.Fatalf("validCheckoutKey(%q) = true", invalid)
		}
		if _, _, err := (&CheckoutService{}).PlaceOrder(nil, CheckoutInput{CheckoutKey: invalid}); !errors.Is(err, ErrInvalidCheckoutKey) {
			t.Fatalf("PlaceOrder malformed key error = %v, want ErrInvalidCheckoutKey", err)
		}
	}
}

func TestPlaceOrderRejectsKeyFromAnotherCart(t *testing.T) {
	db, p, gateway, cart := checkoutIdempotencyFixture(t, nil)
	key, err := p.cart().checkoutKey(cart.ID)
	if err != nil {
		t.Fatal(err)
	}
	other := Cart{Token: "other-cart", Currency: "USD"}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&CartItem{CartID: other.ID, ProductContentID: 1, Qty: 1, UnitPriceCache: 2500}).Error; err != nil {
		t.Fatal(err)
	}
	_, _, err = p.checkout().PlaceOrder(checkoutTestContext(other.Token), CheckoutInput{
		Email: "buyer@example.test", Billing: corecommerce.Address{Name: "Buyer"},
		PaymentMethod: gateway.ID(), CheckoutKey: key,
	})
	if !errors.Is(err, ErrInvalidCheckoutKey) {
		t.Fatalf("foreign cart checkout error = %v, want ErrInvalidCheckoutKey", err)
	}
	if gateway.calls.Load() != 0 {
		t.Fatalf("foreign cart invoked StartPayment %d times", gateway.calls.Load())
	}
}

func TestPlaceOrderRevalidatesProductRowsInsideTransaction(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*gorm.DB) error
		want   error
	}{
		{
			name: "price changed",
			mutate: func(db *gorm.DB) error {
				return db.Model(&ProductData{}).Where("content_id = ?", 1).Update("price_amount", 2600).Error
			},
			want: ErrCheckoutCartChanged,
		},
		{
			name: "product unpublished",
			mutate: func(db *gorm.DB) error {
				return db.Exec("UPDATE gp_contents SET status = ? WHERE id = ?", "draft", 1).Error
			},
			want: ErrProductUnavailable,
		},
		{
			name: "product data deleted",
			mutate: func(db *gorm.DB) error {
				return db.Where("content_id = ?", 1).Delete(&ProductData{}).Error
			},
			want: ErrProductDataMissing,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, p, gateway, cart := checkoutIdempotencyFixture(t, nil)
			key, err := p.cart().checkoutKey(cart.ID)
			if err != nil {
				t.Fatal(err)
			}
			var mutationErr error
			p.engine.Hooks.AddFilter(hookCheckoutValidate, func(value interface{}, _ ...interface{}) interface{} {
				mutationErr = tt.mutate(db)
				return value
			}, 10)
			_, _, err = p.checkout().PlaceOrder(checkoutTestContext(cart.Token), CheckoutInput{
				Email: "buyer@example.test", Billing: corecommerce.Address{Name: "Buyer"},
				PaymentMethod: gateway.ID(), CheckoutKey: key,
			})
			if mutationErr != nil {
				t.Fatalf("fixture mutation: %v", mutationErr)
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("PlaceOrder error = %v, want %v", err, tt.want)
			}
			if gateway.calls.Load() != 0 {
				t.Fatalf("StartPayment calls = %d, want 0", gateway.calls.Load())
			}
			var orders, payments, ledgers, cartItems int64
			for model, count := range map[interface{}]*int64{
				&Order{}: &orders, &Payment{}: &payments, &InventoryLedger{}: &ledgers,
			} {
				if countErr := db.Model(model).Count(count).Error; countErr != nil {
					t.Fatal(countErr)
				}
			}
			if orders != 0 || payments != 0 || ledgers != 0 {
				t.Fatalf("rolled-back counts orders/payments/ledgers = %d/%d/%d", orders, payments, ledgers)
			}
			if err := db.Model(&CartItem{}).Where("cart_id = ?", cart.ID).Count(&cartItems).Error; err != nil || cartItems != 1 {
				t.Fatalf("cart items after rejected checkout = %d, err=%v; want retained", cartItems, err)
			}
		})
	}
}

func TestInventoryReserveRejectsMissingProductData(t *testing.T) {
	db := commerceTestDB(t)
	p := &Plugin{engine: &core.Engine{DB: db}}
	err := db.Transaction(func(tx *gorm.DB) error {
		return p.inventory().Reserve(tx, 404, 1, 9)
	})
	if !errors.Is(err, ErrProductDataMissing) {
		t.Fatalf("Reserve missing product error = %v, want ErrProductDataMissing", err)
	}
	var ledgers int64
	if err := db.Model(&InventoryLedger{}).Count(&ledgers).Error; err != nil || ledgers != 0 {
		t.Fatalf("ledger rows after missing reserve = %d, err=%v", ledgers, err)
	}
}

func checkoutIdempotencyFixture(t *testing.T, gateway *countingRedirectGateway) (*gorm.DB, *Plugin, *countingRedirectGateway, Cart) {
	t.Helper()
	db := commerceTestDB(t)
	bus := hook.New()
	e := &core.Engine{
		DB: db, Hooks: bus, Config: &config.Config{Site: config.SiteConfig{URL: "https://shop.example"}},
		Content: content.NewRepository(db),
	}
	p := &Plugin{engine: e, repo: NewRepository(db)}
	if gateway == nil {
		gateway = &countingRedirectGateway{}
	}
	corecommerce.RegisterPaymentGateway(bus, gateway)
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
	cart := Cart{Token: "idempotency-cart", Currency: "USD"}
	if err := db.Create(&cart).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&CartItem{CartID: cart.ID, ProductContentID: 1, Qty: 2, UnitPriceCache: 2500}).Error; err != nil {
		t.Fatal(err)
	}
	return db, p, gateway, cart
}

func checkoutTestContext(cartToken string) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "https://shop.example/checkout", nil)
	c.Request.AddCookie(&http.Cookie{Name: cartCookie, Value: cartToken})
	return c
}

func assertSingleCheckoutEffects(t *testing.T, db *gorm.DB, gateway *countingRedirectGateway, orderID uint) {
	t.Helper()
	if got := gateway.calls.Load(); got != 1 {
		t.Fatalf("StartPayment calls = %d, want 1", got)
	}
	var orders, payments, reservations int64
	if err := db.Model(&Order{}).Count(&orders).Error; err != nil || orders != 1 {
		t.Fatalf("order count = %d, err=%v; want 1", orders, err)
	}
	if err := db.Model(&Payment{}).Where("order_id = ?", orderID).Count(&payments).Error; err != nil || payments != 1 {
		t.Fatalf("payment count = %d, err=%v; want 1", payments, err)
	}
	if err := db.Model(&InventoryLedger{}).Where("order_id = ? AND reason = ?", orderID, "reserve").Count(&reservations).Error; err != nil || reservations != 1 {
		t.Fatalf("reservation count = %d, err=%v; want 1", reservations, err)
	}
	var pd ProductData
	if err := db.First(&pd, "content_id = ?", 1).Error; err != nil || pd.StockQty != 3 {
		t.Fatalf("stock after replay = %d, err=%v; want 3", pd.StockQty, err)
	}
}

func TestInventoryReleaseRejectsQuantityOverflow(t *testing.T) {
	db := commerceTestDB(t)
	p := &Plugin{engine: &core.Engine{DB: db}}
	if err := db.Create(&ProductData{
		ContentID: 77, Type: "simple", PriceAmount: 100, Currency: "USD",
		ManageStock: true, StockQty: math.MaxInt, StockStatus: "instock",
	}).Error; err != nil {
		t.Fatal(err)
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		return p.inventory().Release(tx, 77, 1, 9)
	})
	if !errors.Is(err, ErrInventoryOverflow) {
		t.Fatalf("Release overflow error = %v, want ErrInventoryOverflow", err)
	}
	var pd ProductData
	if err := db.First(&pd, "content_id = ?", 77).Error; err != nil || pd.StockQty != math.MaxInt {
		t.Fatalf("stock after rejected release = %d, err=%v", pd.StockQty, err)
	}
	var ledgers int64
	if err := db.Model(&InventoryLedger{}).Where("order_id = ?", 9).Count(&ledgers).Error; err != nil || ledgers != 0 {
		t.Fatalf("ledger rows after rejected release = %d, err=%v", ledgers, err)
	}
}
