# 主题接入

Commerce 由当前主题呈现，但主题从不 import Commerce 插件。所有交互都由 core 中介：片段用渲染槽位，插件拥有的整页用主题外壳渲染器。内置的 `shop-starter` 是参考店面主题。

## 渲染槽位

商城主题经 `renderHook` 调用文档化槽位；Commerce 激活时填充，未激活时优雅降级为空。

| 槽位 | 主题调用位置 | Commerce 注入 |
|---|---|---|
| `commerce.product.add_to_cart` | 商品卡/详情模板，传入商品项 | 价格块（含促销划线）+ 加购表单，或缺货提示 |
| `header.nav.after` | 页头导航 | 带实时数量的迷你车角标，及登录客户的「我的订单」链接 |

```gotemplate
{{/* 商品卡 */}}
<h3><a href="{{contentURL . "product"}}">{{.Title}}</a></h3>
{{renderHook "commerce.product.add_to_cart" . $.Ctx}}

{{/* 页头 */}}
<ul class="nav-cart">{{renderHook "header.nav.after" .}}</ul>
```

无论主题传的是 `gin.H` map（BaseTheme）还是类型化 struct，槽位都能工作 —— Commerce 会从两者中反射出 `ID`。

## 整页：主题外壳

`/cart`、`/checkout`、`/order-tracking`、`/checkout/complete/:number`、`/my-account/*` 是动态的、插件拥有的整页。Commerce 用 `Engine.RenderNamespacedInActiveTheme(c, "commerce", ...)` 渲染它们：在当前主题的 `layouts/base.tmpl` + partials 内、用主题的 FuncMap 组合片段。Core API 接受任意安全命名空间，Commerce 只传入它自己的命名空间。模板解析遵循 WordPress 式覆盖顺序：

```text
<theme>/templates/commerce/<fragment>.tmpl   ← 主题覆盖（优先）
plugins/commerce/templates/commerce/<fragment>.tmpl   ← 插件默认
```

于是主题只需在 `templates/commerce/` 放一个文件，就能重设任意店面页样式，无需改 Go。

## 编写商城主题

一个商城主题：

1. 嵌入 `BaseTheme`（据内容注册表渲染商品归档/详情）。
2. 在 `theme.toml` 声明依赖：
   ```toml
   [requires]
   plugins = [ { slug = "commerce", version = ">=0.2.1" } ]
   ```
3. **不**自建 `product` 内容类型 —— Commerce 拥有它。（自带 `product` 类型会冲突，后注册者赢。）
4. 在商品与页头模板里调用上面的渲染槽位。

`shop-starter` 是最小却完整的示例。它只有一个定制落地页，通过 Core 读取已发布商品和商品分类，价格与加购则来自 `commerce.product.add_to_cart`。登录方式通过 `loginProviders` + `loginProviderURL` 呈现在符合主题风格的蒙版弹窗中，普通 `/login` 链接仍作为无 JavaScript 后备入口。`archive-product.tmpl`、`single-product.tmpl` 和商品 taxonomy 模板负责目录浏览；购物车、结算、支付、订单查询与账户页面继续由插件拥有，并统一渲染进这套轻量主题外壳。

## 主题设置

主题通过实现 `coreTheme.SettingsProvider` 暴露可编辑设置：

```go
func (t *MyTheme) SettingsTemplatePath() string {
    return filepath.Join(t.ThemeDir, "templates", "admin", "theme_settings.tmpl")
}
```

后台会把该模板渲染进后台外壳，并提供 `.Settings`（所有选项）；用 `{{index .Settings "home_hero_title"}}` 读值。表单 POST 到 `/admin/themes/<slug>/settings`。

**键前缀规则：** `ThemeSettingsSave` 只持久化「注册为可翻译」**或**匹配已识别前缀的键 —— `site_`、`home_`、`about_`、`company_`、`social_`、`footer_`、`nav_`、`contact_`、`showcase_`、`package_`。请据此命名（如 `home_hero_title`、`site_name`、`company_email`），否则会被静默丢弃。`shop-starter` 以此暴露店铺身份、公告栏、Hero 文案 + 插画、商品区域文案、页脚联系信息与可选社交链接。

## 图片

遵循[从 UI 建主题](../themes/creating-themes.md)的分工：

- **主题自带素材**（Hero 插画和 Logo）→ `themes/<slug>/static/images/` 与 `static/logo.svg`，以 `/static/images/<file>` 引用。它们不进媒体库。`shop-starter` 特意只携带一张小型 SVG Hero 插画，不打包照片素材。
- **CMS 内容图**（商品图、文章封面）→ 把 URL 放进种子的 `image_url`；seeder 会把 `http(s)://` 图片下载到 `/static/uploads/demo/…` 并登记进媒体库。用稳定、验证过的图片 URL（Unsplash 直链 `images.unsplash.com/photo-<id>`，而非已停用的 `source.unsplash.com`）。

## 演示数据与 `seed.completed` 桥接

主题通过实现 `coreTheme.DemoDataProvider` 提供一键演示数据：

```go
func (t *MyTheme) DemoSeedPath() string {
    return filepath.Join(t.ThemeDir, "demo", "data", "seed.toml")
}
```

种子是纯 core 内容。旧的 `[[categories]]`/`[[tags]]` 仍可用于内置 taxonomy；通用的 `[[taxonomies]]` 声明与 `[contents.taxonomies]` 映射可以承载扩展注册的 taxonomy 名称。它不能直接写插件表。因此商品分类/标签关系使用 `product_cat`/`product_tag`，而商品**价格**作为 `_commerce_*` 内容 meta 携带：

```toml
[[taxonomies]]
taxonomy = "product_cat"
name = "影音耳机"
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

导入后，seeder 触发通用的 `seed.completed` action。Commerce 监听它，据该 meta 建 `product_data` + `product_lookup`（激活时也同步一次，因此「先导入后启用」也能补价）。这让种子文件不含插件表知识，却仍能产出含价的演示商品。

> 用 `[[settings]]` 以前缀式键（`site_name`、`home_hero_image`…）设置主题值，使其立即可见、并可在主题设置里编辑。除非你自行处理选项缓存重载时序，否则店铺币种保持 Commerce 默认 —— 同步在导入时读取币种选项。

返回：[总览](overview.md)。
