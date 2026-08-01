# Commerce Overview

Commerce is GoPress's opt-in e-commerce module — a production-oriented storefront foundation (products, cart, checkout, orders, inventory, payments) plus the registries that satellite payment and shipping plugins plug into. It occupies the same position in the GoPress ecosystem that WooCommerce holds in WordPress, but it is built around Go interfaces, a compiled service, and a strict extension boundary.

It ships **disabled by default**. Nothing about Commerce touches a site until an operator enables it.

## What it delivers

- **Catalog** — a `product` content type with an admin meta box (SKU, price, sale price, stock, tax class, weight) and a denormalized lookup table for fast listing.
- **Cart** — guest (cookie) and logged-in carts, with guest→account merge on login and IDOR-safe mutations.
- **Checkout** — a single-transaction order placement that snapshots prices, reserves stock under row locks, and hands off to a payment gateway.
- **Orders** — a controlled state machine (`pending → processing → completed`, plus `cancelled/failed/on_hold/refunded/partially_refunded`), an order admin (list, detail, mark-paid, ship, cancel, refund, notes), and confirmation email.
- **Inventory** — reserve / commit / release with `SELECT … FOR UPDATE` locking and a TTL sweeper that releases abandoned orders.
- **Payments** — a medium-agnostic `PaymentGateway` contract, a built-in offline bank-transfer gateway, and a **PayPal** satellite plugin (Orders v2, sandbox/live, webhook verification, refunds).
- **Customer accounts** — guest order tracking (order number + email), a logged-in "my orders" area, and a high-entropy access key that closes order-number enumeration.
- **Storefront theming** — render-hook slots and a theme-shell renderer so any theme can present the shop without importing the plugin, demonstrated by the bundled `shop-starter` theme.

## The "A-scheme": contracts sink into core

The defining architectural decision is that the payment/shipping/tax **contracts live in core**, in a tiny `core/commerce` package that depends only on `core/hook`. The Commerce engine and every satellite plugin depend on those contracts — never on each other.

```text
core/commerce         contracts only: Money, PaymentGateway, PaymentAction,
   ▲          ▲        PaymentSettler, ShippingMethod, TaxCalculator, registry helpers
   │          │
plugins/commerce     plugins/commerce-paypal, -stripe, -usdt …
(the engine)          (satellite gateways)
   │                          │
   └────────► core ◄──────────┘   both depend on core; neither imports the other
```

This yields three properties the rest of the module relies on:

1. **No plugin→plugin dependency, no import cycle.** A satellite imports `core/commerce` and registers itself through the core hook bus. `go list -deps` on `plugins/commerce-paypal` shows no dependency on `plugins/commerce`.
2. **Registration order is irrelevant.** Satellites add themselves to a filter during `Activate`; the engine reads that filter lazily at checkout. Whoever activates first is fine.
3. **The engine never learns a gateway's confirmation mechanism.** A gateway confirms payment by any internal means (webhook, manual admin action, on-chain polling) and calls back through the engine's idempotent `PaymentSettler`. See [Payments](payments.md).

## Module gating

Commerce is a `default_inactive` plugin (a generic core capability, not an e-commerce special case). A shop theme declares `[requires] commerce` in `theme.toml`; activating the theme does **not** auto-enable Commerce. Instead the admin shows a prominent "enable Commerce" banner, and the module can be toggled from **Settings → Modules**. When disabled, Commerce registers no content type, routes, admin pages, or hooks — the site carries no e-commerce trace.

See [Getting Started](getting-started.md) for the enable flow.

## Generic core capabilities introduced for Commerce

Building Commerce required several *generic* core extension points — deliberately not e-commerce-specific, so they benefit any plugin:

| Capability | Purpose |
|---|---|
| `content.register_types` action | Lets a plugin (re)register content types idempotently so they survive theme switches, which rebuild the registry. |
| `default_inactive` + `DefaultInactiveProvider` | Marks an opt-in module that ships disabled and is toggled from Settings → Modules. |
| `DepNeedsEnable` dependency state | A theme dependency that is present-but-disabled produces a non-blocking "enable" prompt instead of a hard block. |
| `Engine.RenderNamespacedInActiveTheme` | Renders an extension-owned full page inside the active theme's layout, with namespace-scoped theme overrides. |
| `admin.nav.items` filter | Lets an active plugin contribute an admin sidebar section (Commerce's "Orders" lives here). |
| `seed.completed` action | Fires after demo/seed import so a plugin can derive satellite rows from seeded content (Commerce builds `product_data` from product meta). |

These are documented in [Architecture](architecture.md) and the general [Hook System](../architecture/hooks.md).

## Phased delivery

| Phase | Scope | Status |
|---|---|---|
| P0 — Foundation | Core extension points, `core/commerce` contracts, `Money`, the default-inactive plugin skeleton, settings page | Done |
| P1 — Catalog + Cart | `product` type + data tables, admin meta box, catalog, guest/user cart with merge | Done |
| P2 — Checkout + Orders | Address/shipping/tax, order state machine, inventory reservation, order admin, confirmation email, offline gateway, PayPal satellite | Done (live-sandbox payment testing pending) |
| P3+ | Coupons, variable products, richer tax/shipping zones, Store REST API, my-account expansion | Future |

## Scope boundaries (v1)

Physical goods, single currency, single merchant. Product type is `simple` only. Explicitly out of scope for now: multi-vendor/marketplace, subscriptions, multi-currency settlement, digital/downloadable goods, complex promotion stacking, B2B quoting.

## Where to go next

- [Getting Started](getting-started.md) — enable the module, install the shop theme, import demo data, place a test order.
- [Architecture](architecture.md) — the contracts, hooks, tables, RBAC, and dependency rules.
- [Catalog, Cart & Orders](catalog-orders.md) — products, cart, checkout orchestration, inventory, the order state machine, and customer accounts.
- [Payments](payments.md) — the gateway contract and how to write a satellite gateway.
- [Theme Integration](theme-integration.md) — render slots, the theme shell, building a shop theme, and demo data.
