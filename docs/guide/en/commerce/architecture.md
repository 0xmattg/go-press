# Commerce Architecture

This page covers the layering, the `core/commerce` contracts, how satellites register, the data model, RBAC, and the generic core extension points Commerce relies on.

## Layering and the dependency rule

```text
theme  ──►  core  ◄──  plugin
                 ▲
                 └── core/commerce (contracts)
```

- **Themes** depend on core only. A shop theme declares `[requires] commerce` (a declarative theme→plugin dependency, resolved by core) and calls documented render-hook slots. It never imports the Commerce plugin.
- **The Commerce engine** (`plugins/commerce`) depends on `core` and `core/commerce`.
- **Satellite gateways** (`plugins/commerce-paypal`, …) depend on `core/commerce` (and a narrow slice of core for the hook bus / options / site URL) — **never** on `plugins/commerce`.

The invariant: **no plugin imports another plugin**, and there is no import cycle. `core/commerce` imports only `core/hook`. You can assert it in CI with `go list -deps`.

## Money

All monetary values are exact integers, never floats:

```go
type Money struct {
    Amount   int64  // minor units: 199 with "USD" == $1.99
    Currency string // ISO 4217
}
func New(minorUnits int64, currency string) Money
func (m Money) Add(o Money) (Money, error)   // ErrCurrencyMismatch when currencies differ
func (m Money) Sub(o Money) (Money, error)
func (m Money) MulQty(qty int) Money
```

Display formatting happens in the render layer per request locale; `Money.String()` is debug-only.

## The `core/commerce` contract package

| File | Contents |
|---|---|
| `money.go` | `Money` value type |
| `payment.go` | `PaymentGateway`, optional `GatewayAvailability` / `IdempotentRefunder`, `PaymentAction` (sealed sum), `PaymentSettler`, `Reconciler`, request/settle/refund types |
| `shipping.go` | `ShippingZone`, `ShippingMethod`, `ShippingRate` |
| `tax.go` | `TaxCalculator` |
| `promotion.go` | `PromotionRule`, `Adjustment` |
| `registry.go` | Hook-bus register/read helpers + hook-name constants |
| `types.go` | `Address`, `KV` |

### PaymentGateway and PaymentAction

```go
type PaymentGateway interface {
    ID() string
    Title(c *gin.Context) string
    Icon() string
    Capabilities() Capabilities             // Refund / PartialRefund
    StartPayment(c *gin.Context, req PaymentRequest) (PaymentAction, error)
    Refund(c *gin.Context, req RefundRequest) error
}
```

`PaymentRequest.IdempotencyKey` and `RefundRequest.IdempotencyKey` are stable retry keys that gateways must forward to their provider. Two optional capabilities narrow runtime behavior without coupling Commerce to a specific gateway:

```go
type GatewayAvailability interface {
    Available(c *gin.Context) bool
}
type IdempotentRefunder interface {
    RefundWithResult(c *gin.Context, req RefundRequest) (RefundResult, error)
}
```

Gateways without `GatewayAvailability` remain available for compatibility. Commerce requires `IdempotentRefunder` for an automatic provider refund and persists `RefundResult.TransactionID`; a gateway that advertises `Refund: false` continues to use the explicit manual/offline refund workflow.

`PaymentAction` is a **sealed sum** (an unexported marker method keeps the set closed) so the storefront can render it exhaustively:

| Action | Meaning |
|---|---|
| `RedirectAction{URL}` | Send the buyer to a hosted page (PayPal). |
| `DisplayAction{Title, Rows, QR, ExpiresAt}` | Show instructions and await payment (crypto, offline transfer). |
| `InlineAction{ClientData}` | Render an inline tokenized widget (card SDK; no PAN touches GoPress). |
| `CompletedAction{}` | Payment settled synchronously (rare). |

### Confirmation inversion — PaymentSettler

Commerce does not poll gateways. A gateway confirms by its own means and calls back:

```go
type PaymentSettler interface {
    Settle(ctx context.Context, req SettleRequest) error
}
type SettleRequest struct {
    OrderRef, Gateway, TxnID string
    Amount Money
    Status SettleStatus       // paid/underpaid/overpaid/expired/failed/refunded
    IdempotencyKey string     // unique-indexed → dedup
    Raw map[string]any
}
```

The engine implements `PaymentSettler` and publishes it via `SetSettler`. Gateways fetch it with `GetSettler(bus)`. All settlement funnels through this one idempotent method — see [Payments](payments.md).

### Registration helpers

```go
// Satellite, in Activate:
handle := commerce.RegisterPaymentGateway(bus, myGateway)   // remove in Deactivate
// Engine, at checkout:
gateways := commerce.AvailablePaymentGateways(c, bus)        // lazy + runtime-ready
```

`RegisterPaymentGateway` adds the gateway to a `commerce.payment.gateways` **filter**; `PaymentGateways` applies that filter, while `AvailablePaymentGateways` also evaluates the optional runtime availability contract. Because the read is lazy, activation order never matters. Checkout filters the displayed choices and rechecks availability on submission, so a stale or forged method value cannot select a disabled or unconfigured gateway.

## Data model

Tables use `dbprefix.PluginTable("commerce", <name>)` → `gp_plgn_commerce_*`, so they are multi-site isolated and registered for ownership with `RegisterPluginTable`.

| Group | Tables |
|---|---|
| Catalog | `product_data` (content_id, sku, price, sale_price, stock, tax_class, …), `product_lookup` (denormalized current price + in-stock for fast listing) |
| Cart | `carts` (guest token or user_id), `cart_items` |
| Orders | `orders` (number, access_key, status, totals snapshot, payment_method), `order_items` (name/price snapshot), `order_addresses`, `payments` (idempotency_key unique), `order_notes`, `refunds` (pending/succeeded/failed, idempotency_key unique, provider refund id) |
| Inventory | `inventory_ledger` (product_ref, delta, reason: in/out/reserve/release, order_id) |

Money columns are `BIGINT` minor units with a separate `currency` column. Order rows **snapshot** name and price, so historical orders are unaffected by later product edits. Order line items carry no FK to content (soft association), so deleting a product never cascades into order history.

## RBAC

Commerce does not add a new role. On `Activate` it grants commerce capabilities to the existing `editor` role and revokes them on `Deactivate`:

```text
shop_order.{read, update, refund}
coupon.{create, read, update, delete}
commerce_settings.{read, update}
```

`superadmin` has them via the `*.*` wildcard. Every protected admin route uses `admin.RequirePermission`, and customer-facing order lookups enforce **ownership** (IDOR guard) via `WHERE user_id = <current>`. A plugin can implement core's generic `SettingsAuthorizationProvider` to declare its settings resource: settings GET requires `<resource>.read`, POST requires `<resource>.update`, and contributed navigation items declare their own resource/action so unauthorized entries are not rendered. Commerce maps this to `commerce_settings`; core contains no Commerce-specific permission branch. See [Catalog, Cart & Orders](catalog-orders.md#customer-accounts--access-security).

## Generic core extension points

Commerce is built on generic core capabilities. Each is neutral — core never hardcodes an e-commerce concept.

### `content.register_types`

`activateTheme()` rebuilds the content registry from core + active-theme types. Plugin-contributed types (like `product`) would be lost on every theme switch. So core fires `content.register_types` after theme types are registered; Commerce (re)registers `product` idempotently in that action, and once more immediately on `Activate` (the first theme activation may precede the plugin load).

### `default_inactive` module gating

`plugin.Meta.DefaultInactive` (from `default_inactive = true` in `plugin.toml`) + the `DefaultInactiveProvider` interface mark an opt-in module. `LoadPlugin` registers it without activating when there is no persisted state. **Settings → Modules** lists every default-inactive plugin as a toggle.

### `DepNeedsEnable` dependency state

When a theme requires a plugin that is present, version-compatible, but disabled *and* default-inactive, dependency resolution returns `DepNeedsEnable` — non-blocking, so the theme still activates, and the admin shows a strong "enable" prompt rather than a hard failure. Absent or version-mismatched dependencies still block.

### `Engine.RenderNamespacedInActiveTheme`

```go
func (e *Engine) RenderNamespacedInActiveTheme(c *gin.Context, namespace, fragment, extensionDefaultDir string, data gin.H) error
```

Renders an extension-owned fragment inside the active theme's `layouts/base.tmpl` + partials, using the theme's own FuncMap. Fragment resolution is generic: `<theme>/templates/<namespace>/<fragment>.tmpl` wins over `<extensionDefaultDir>/<fragment>.tmpl`. Namespace and fragment are validated as single safe path components. Commerce passes the `commerce` namespace; core itself has no Commerce-specific branch. This is how `/cart`, `/checkout`, `/order-tracking`, and `/my-account/*` share the site's header/footer and let a theme override any page. `RenderInActiveTheme` remains as a compatibility wrapper that derives the namespace from the default directory's basename.

### `admin.nav.items`

`buildMenuItems()` applies this filter before the System section, so an active plugin can contribute a sidebar section. Commerce adds an "电商 / Commerce" section with **Orders**. Products appear under Content automatically (they are a content type).

### `seed.completed`

The seeder fires this action after first-run seeding and admin demo import. Commerce listens and derives `product_data` from the `_commerce_*` meta on product content, so demo seed files stay pure core content and never reference plugin tables. See [Theme Integration](theme-integration.md#demo-data-and-the-seedcompleted-bridge).

## Commerce hook namespace

| Hook | Kind | Payload |
|---|---|---|
| `commerce.payment.gateways` | filter | `[]PaymentGateway` |
| `commerce.shipping.methods` | filter | `[]ShippingMethod` |
| `commerce.payment.settler` | filter | `PaymentSettler` (engine-provided) |
| `commerce.checkout.validate` | filter | `[]string` errors (non-empty aborts) |
| `commerce.order.status_changed` | action | `(order, old, new)`, published only after the surrounding transaction commits |
| `commerce.product.add_to_cart` | filter | storefront price + add-to-cart HTML slot |

Next: [Catalog, Cart & Orders](catalog-orders.md).
