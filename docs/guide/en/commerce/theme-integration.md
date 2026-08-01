# Theme Integration

Commerce is presented by the active theme, but a theme never imports the Commerce plugin. All interaction is core-mediated: render-hook slots for fragments, and the theme-shell renderer for full plugin-owned pages. The bundled `shop-starter` theme is the reference storefront.

## Render-hook slots

A shop theme calls documented slots via `renderHook`; Commerce fills them when active and they degrade to nothing when it is not.

| Slot | Where the theme calls it | What Commerce injects |
|---|---|---|
| `commerce.product.add_to_cart` | product card / detail template, passing the product item | price block (with sale strike-through) + add-to-cart form, or an out-of-stock notice |
| `header.nav.after` | header nav | mini-cart badge with live item count, plus a "my orders" link for logged-in customers |

```gotemplate
{{/* product card */}}
<h3><a href="{{contentURL . "product"}}">{{.Title}}</a></h3>
{{renderHook "commerce.product.add_to_cart" . $.Ctx}}

{{/* header */}}
<ul class="nav-cart">{{renderHook "header.nav.after" .}}</ul>
```

The slot value works whether the theme passes a `gin.H` map (BaseTheme) or a typed struct — Commerce reflects the `ID` out of either.

## Full pages via the theme shell

`/cart`, `/checkout`, `/order-tracking`, `/checkout/complete/:number`, and `/my-account/*` are dynamic, plugin-owned full pages. Commerce renders them with `Engine.RenderNamespacedInActiveTheme(c, "commerce", ...)`, which composes the fragment inside the active theme's `layouts/base.tmpl` + partials using the theme's FuncMap. The core API accepts any safe namespace; Commerce supplies its own. Template resolution follows a WordPress-style override order:

```text
<theme>/templates/commerce/<fragment>.tmpl   ← theme override (wins)
plugins/commerce/templates/commerce/<fragment>.tmpl   ← plugin default
```

So a theme can restyle any storefront page by dropping a file in `templates/commerce/`, without touching Go.

## Building a shop theme

A shop theme:

1. Embeds `BaseTheme` (for product archive/detail rendering from the content registry).
2. Declares the dependency in `theme.toml`:
   ```toml
   [requires]
   plugins = [ { slug = "commerce", version = ">=0.2.1" } ]
   ```
3. Does **not** declare its own `product` content type — Commerce owns it. (A theme that ships its own `product` type will collide; the last registration wins.)
4. Calls the render-hook slots above in its product and header templates.

`shop-starter` is the minimal-yet-complete example. Its only bespoke landing page loads published products and product categories through Core, while price + add-to-cart comes from `commerce.product.add_to_cart`. It presents Core's provider-neutral sign-in choices in a theme-aligned overlay using `loginProviders` + `loginProviderURL`; the normal `/login` link remains the no-JavaScript fallback. `archive-product.tmpl`, `single-product.tmpl`, and the product-taxonomy templates cover catalog browsing; cart, checkout, payment, tracking, and account pages remain plugin-owned and render through the same lightweight theme shell.

## Theme settings

A theme exposes editable settings by implementing `coreTheme.SettingsProvider`:

```go
func (t *MyTheme) SettingsTemplatePath() string {
    return filepath.Join(t.ThemeDir, "templates", "admin", "theme_settings.tmpl")
}
```

The admin renders that template inside the admin chrome with `.Settings` (all options) available; read a value with `{{index .Settings "home_hero_title"}}`. The form posts to `/admin/themes/<slug>/settings`.

**Key-prefix rule:** `ThemeSettingsSave` only persists keys that are registered translatable **or** match a recognized prefix — `site_`, `home_`, `about_`, `company_`, `social_`, `footer_`, `nav_`, `contact_`, `showcase_`, `package_`. Name your settings accordingly (e.g. `home_hero_title`, `site_name`, `company_email`) or they will be silently dropped. `shop-starter` exposes identity, announcement, hero copy + illustration, product-section copy, footer contact, and optional social links this way.

## Images

Follow the split from the [theme-from-UI conventions](../themes/creating-themes.md):

- **Theme-owned art** (hero illustration and logo mark) → `themes/<slug>/static/images/` and `static/logo.svg`, referenced through `/static/images/<file>`. These are not in the media library. `shop-starter` intentionally ships one small SVG hero illustration instead of a photo bundle.
- **CMS content images** (product photos, article covers) → put the URL in the seed's `image_url`; the seeder downloads `http(s)://` images into `/static/uploads/demo/…` and registers them in the media library. Use stable, verified image URLs (Unsplash direct `images.unsplash.com/photo-<id>` links, not `source.unsplash.com`, which is discontinued).

## Demo data and the `seed.completed` bridge

A theme provides one-click demo data by implementing `coreTheme.DemoDataProvider`:

```go
func (t *MyTheme) DemoSeedPath() string {
    return filepath.Join(t.ThemeDir, "demo", "data", "seed.toml")
}
```

The seed is pure core content. Legacy `[[categories]]`/`[[tags]]` remain available for the built-in taxonomies, while generic `[[taxonomies]]` declarations and `[contents.taxonomies]` mappings support extension-registered taxonomy names. It cannot write plugin tables directly. Product category/tag relationships therefore use `product_cat`/`product_tag`, while product **prices** are carried as `_commerce_*` content meta:

```toml
[[taxonomies]]
taxonomy = "product_cat"
name = "Audio"
slug = "audio"

[[contents]]
type = "product"
title = "Aura Headphones"
image_url = "https://images.unsplash.com/photo-1505740420928-5e560c06d30e?w=800&h=800&fit=crop&q=75"
[contents.taxonomies]
product_cat = ["audio"]
[contents.meta]
_commerce_price = "259.00"
_commerce_sale_price = "199.00"
_commerce_sku = "AUD-AURA-01"
_commerce_manage_stock = "true"
_commerce_stock_qty = "120"
```

After the import, the seeder fires the generic `seed.completed` action. Commerce listens and builds `product_data` + `product_lookup` from that meta (and runs the same sync on activation, so enable-after-import also prices products). This keeps seed files free of plugin-table knowledge while still producing priced demo products.

> Set theme setting values with `[[settings]]` using the prefixed keys (`site_name`, `home_hero_image`, …) so they are visible immediately and editable in Theme Settings. Keep the store currency at the Commerce default unless you also handle the option-cache reload timing — the sync reads the currency option at import time.

Back to [Overview](overview.md).
