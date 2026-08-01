# Catalog, Cart & Orders

This page covers the storefront domain services: the product catalog, the cart, checkout orchestration, inventory concurrency, the order state machine, the order admin, and customer accounts.

## Catalog

`product` is a normal content type (`HasArchive`, rewrite slug `store`, taxonomies `product_cat`/`product_tag`), so it reuses admin CRUD, SEO, sitemap, and the theme's archive/detail templates. Its commerce fields live in two tables:

- **`product_data`** — the authoritative per-product commerce record (SKU, price, sale price, stock, tax class, weight), keyed by content id.
- **`product_lookup`** — a denormalized row (effective price, in-stock) refreshed on every save, used for fast catalog listing and filtering.

The admin meta box is injected on the `admin.content_form.fields` filter (only for the `product` type) and persisted on the `admin.content.saved` action — so Commerce owns its fields without modifying the core save handler. Prices are parsed from decimals (`19.99`) into integer minor units (`1999`).

## Cart

`CartService` is stateless (built per request). It resolves the current cart, reconciling guest and account carts:

- **Guest** — a random token in the `gp_cart` cookie (SameSite=Lax, HttpOnly).
- **Logged in** — keyed by `user_id`.
- **On login** — a guest cart is either adopted as the user's cart or merged into it (quantities summed), then the guest cart and cookie are cleared.

Mutations (`Add`, `SetItemQty`, `RemoveItem`) enforce **ownership**: an item must belong to the current cart, guarding against IDOR. An add/update accepts quantities from 1 through 999 and uses checked integer arithmetic. It accepts a product id only when core content says it is a currently published `product` and its authoritative `product_data` row has a valid positive effective price, matching store currency, and sufficient managed stock. Prices and stock are never trusted from the form or a denormalized cache row.

`View` repeats those authoritative checks and recomputes the current price from `product_data`; invalid legacy lines are not turned into purchasable order lines. `Count` is a read-only badge query that never creates a cart. Cart mutations use row locks where needed, and all three public POST routes enforce same-origin CSRF protection.

Public routes (registered on `routes.register`): `GET /cart`, `POST /cart/add|update|remove`.

## Checkout orchestration

`CheckoutService.PlaceOrder(c, in) (*Order, PaymentAction, error)` captures an authoritative cart snapshot, validates the selected gateway and checked totals, then separates the local transaction from the external payment call:

```text
validate (commerce.checkout.validate filter) → recheck gateway availability →
compute shipping/tax/discount with overflow checks →
BEGIN TX:
  create Order (status=pending, access_key, totals + name/price snapshots)
  create order_items, order_addresses
  InventoryService.Reserve(each line, SELECT … FOR UPDATE)   ← oversell rolls the whole order back
  create payments(pending, idempotency_key = "start:<number>")
COMMIT
→ gateway.StartPayment(order, idempotency_key = "start:<number>") → PaymentAction
→ on success, consume only the quantities in the captured cart snapshot
```

The checkout page displays only `AvailablePaymentGateways`, and submission checks availability again so a disabled/unconfigured method cannot be selected with a stale or forged value. Commerce compensates only a start failure explicitly marked as having no possible remote side effect: it fails the order/payment, releases reservations, and retains the cart. Timeouts, disconnects, or action-persistence failures put the payment in `reconciliation`, retain stock, and consume this checkout snapshot until a webhook or operator resolves it, avoiding a duplicate charge. If a webhook already advanced the locked order, Commerce reuses that result. Successful cleanup subtracts only ordered quantities and preserves concurrent additions or edits from another tab.

The returned `PaymentAction` drives what happens next:

- `DisplayAction` (offline/crypto) → the order moves to `on_hold` (awaiting payment) and the instructions render.
- `RedirectAction` (PayPal) → the buyer is sent off-site; the order stays `pending` until the gateway confirms.
- `CompletedAction` → settle immediately.

Storefront routes: `GET /checkout`, `POST /checkout` (same-origin/CSRF enforced), `GET /checkout/complete/:number`.

## Inventory concurrency

`InventoryService` protects against oversell using row locks inside the checkout transaction:

- **Reserve** — `tx.Clauses(clause.Locking{Strength:"UPDATE"})` reads `product_data`; only if `stock_qty >= qty` does it decrement and write an `inventory_ledger` `reserve` row. Otherwise it errors and the whole order rolls back. Two requests racing for the last unit → only one wins.
- **Commit** — on `paid`, the reservation becomes a permanent `out`.
- **Release** — on order cancel/expiry or a payment-start failure proven to have no remote side effect, the `product_data` row is locked before stock is returned; checked addition rejects quantity overflow. The TTL sweeper does not release an ambiguous remote result.

Product admin forms carry an optimistic `product_data.version`. Every reserve/release advances it, so a stale form cannot restore stock just reserved by checkout. Demo metadata only creates missing product data; repeated imports or plugin activation never reset live price or inventory.

Abandoned unpaid orders are released by a TTL sweeper. Because core's `Scheduler` starts its tickers at boot (before a default-inactive plugin activates), Commerce runs the sweeper as its **own goroutine**, started on activation and stopped on deactivation, rather than registering with the core scheduler.

## Order state machine

`OrderService.Transition(order, event)` performs a controlled transition and logs an `order_notes` entry inside the caller's transaction. Its status update includes the expected old status, so a concurrent change produces `ErrTransitionConflict` instead of a lost update. It returns an immutable change snapshot but deliberately does **not** fire a hook:

```text
pending ──paid──► processing ──ship──► completed
   ├─cancel─► cancelled          processing/completed ──full cumulative refund──► refunded
   ├─fail───► failed
   └─hold───► on_hold ──paid──► processing
cancelled/failed ──late funds confirmation──► reconciliation ──full refund──► refunded
```

The caller publishes `commerce.order.status_changed` only **after the surrounding transaction commits**, so email/analytics cannot observe a transition that later rolls back. `paid` commits inventory and queues the confirmation email; `cancel`/`fail` release inventory. If funds arrive after an order closed, it moves to `reconciliation` instead of pretending released inventory remains fulfillable. Illegal transitions are rejected, and settlement verifies the full payload behind a duplicate idempotency key. Partial refunds are financial records and leave the fulfillment state as `processing` or `completed`; only a full cumulative refund changes it to `refunded` (legacy `partially_refunded` rows remain operable).

## Order admin

Under the admin **Commerce → Orders** section (contributed via `admin.nav.items`):

- **List** — number, status, total, email, time.
- **Detail** — items, addresses, notes, and actions: **Mark paid**, **Ship**, **Cancel**, **Refund**, add note.

Every route is guarded by `admin.RequirePermission(auth, rbac, "shop_order", "read"|"update"|"refund")`; route integration tests verify an authenticated role without the capability receives 403, while authorized operations pass through the same middleware. The refund action locks the order, enforces the cumulative remaining amount, and creates a stable idempotent attempt before any provider call. Automatic refunds require the registered gateway's `IdempotentRefunder` result (including a non-empty remote refund id); manual/offline refunds remain explicit. Pending and ambiguous failed attempts reserve capacity until retried with the same key. See [Refund safety and cumulative limits](payments.md#refund-safety-and-cumulative-limits).

## Customer accounts & access security

Guest checkout is fully supported — buyers are never forced to register. The module offers two ways to see an order, plus a hardening layer:

- **Order access key** — every order gets a 256-bit `AccessKey` at creation. The order-received/status page (`/checkout/complete/:number`) requires **account ownership OR a matching key** (constant-time compared). This closes enumeration of the short `date-8hex` order number: a guessed number reveals nothing. The post-checkout redirect and the confirmation email carry `?key=…`.
- **Guest order tracking** — `GET|POST /order-tracking` looks an order up by **order number + email** (two-factor). Any mismatch returns the same generic error (no enumeration), the endpoint is same-origin guarded, and a per-IP attempt throttle (in-memory, best-effort) blunts brute force *before* touching the database.
- **My orders** — `GET /my-account/orders(/:number)` for logged-in customers; unauthenticated visitors are bounced to login and returned afterward. The detail query enforces ownership (`WHERE user_id`) as an IDOR guard.

The order-received template is `Heading`-parametrized and shared across checkout completion, guest tracking, and the account detail view.

## Confirmation email

On the `paid` transition, Commerce sends a confirmation email asynchronously via the core `Workers` pool + `Mail` service (mirroring the `notification` package's contact-message notifier). The email link carries the order access key.

Next: [Payments](payments.md).
