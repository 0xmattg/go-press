# Getting Started with Commerce

This walks through enabling the module, installing the bundled shop theme, importing demo data, and placing a test order end to end.

## 1. Enable the Commerce module

Commerce ships **disabled by default**. There are two ways to enable it:

- **Settings → Modules** — Commerce appears as a toggle card (because it is a `default_inactive` plugin). Turn it on.
- **Activate a shop theme** — a theme that declares `[requires] commerce` triggers a prominent "enable Commerce" banner in the admin; its **Enable now** button links to the module panel.

Enabling the module registers the `product` content type, the storefront routes (`/cart`, `/checkout`, `/order-tracking`, `/my-account/orders`), the admin Orders section, RBAC grants, and the payment settler. Disabling it removes all of that cleanly — the site returns to carrying no e-commerce trace.

> Under the hood, toggling the module calls `RefreshActiveTheme()`, which rebuilds the content registry and the router so the product type and routes appear/disappear immediately, without a restart.

## 2. Install the shop theme (shop-starter)

`shop-starter` is the bundled public reference theme: a compact single-page storefront with a split hero, category shortcuts, product grid, small trust section, and a responsive theme shell for the complete Commerce flow. Activate it from **Appearance → Themes**.

Because `shop-starter` declares `[requires] commerce`, activating it while Commerce is off surfaces the enable banner described above.

## 3. Import demo data

On the **Themes** page, `shop-starter` shows an **Import demo data** button (it implements `DemoDataProvider`). Importing seeds:

- Store + theme settings (identity, announcement, hero, product-section copy, footer, and optional social links).
- Four product categories and two tags.
- Six priced demo products with images downloaded into the media library.

Product **prices** live in the Commerce `product_data` table, not in core content. The seed carries them as `_commerce_*` content meta, and Commerce derives `product_data` from that meta on the generic `seed.completed` hook (and again on activation, so import-then-enable also works). See [Theme Integration](theme-integration.md#demo-data-and-the-seedcompleted-bridge).

> Enable Commerce **before** importing (or re-run activation after), so the price-sync listener is registered when the import fires.

## 4. Add or edit a product manually

Products are a normal content type. Under **Content → Products**, create or edit a product; the Commerce meta box adds:

| Field | Meaning |
|---|---|
| SKU | Stock-keeping unit |
| Price | Regular price (stored as integer minor units) |
| Sale price | Optional discounted price |
| Manage stock + Stock qty | Enables reservation/oversell protection |
| Tax class | For tax calculation |
| Weight | For shipping |

Saving writes to `product_data` (upsert) and refreshes `product_lookup` (the fast-catalog table). Prices are entered as decimals (`19.99`) and stored as minor units (`1999`).

## 5. Configure the store

- **Content → Products** meta boxes — per-product commerce fields.
- **Plugins → Commerce → Settings** — store currency, country, weight unit, flat shipping rate, unpaid-order TTL, and the offline bank-transfer details shown at checkout.
- **Appearance → Themes → shop-starter → Theme settings** — store identity, announcement, hero, product-section copy, footer contact, and optional social links.

## 6. Place a test order (offline gateway)

The built-in **offline bank-transfer** gateway needs no external setup, so you can close the loop immediately:

1. Open a product on the storefront (`/store/<slug>`), add it to the cart.
2. Go to `/cart`, then **Checkout**.
3. Fill the address form, choose **Bank transfer**, and place the order.
4. The order is created (status `on_hold`) and the buyer sees the bank details plus an order-received page.
5. In the admin **Orders → order detail**, click **Mark paid**. This calls the settler, advances the order to `processing`, commits the reserved stock, and queues the confirmation email.

To accept real online payments, configure the **PayPal** satellite — see [Payments](payments.md).

## File map

| Area | Location |
|---|---|
| Contracts | `core/commerce/` |
| Engine plugin | `plugins/commerce/` |
| PayPal satellite | `plugins/commerce-paypal/` |
| Shop theme | `themes/shop-starter/` |
| Demo seed | `themes/shop-starter/demo/data/seed.toml` |
| Design notes | `docs/design/commerce-*.md` |

Next: [Architecture](architecture.md).
