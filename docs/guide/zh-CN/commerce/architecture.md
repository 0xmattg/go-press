# Commerce 架构

本页讲分层、`core/commerce` 契约、卫星如何注册、数据模型、RBAC，以及 Commerce 依赖的通用 core 扩展点。

## 分层与依赖规则

```text
theme  ──►  core  ◄──  plugin
                 ▲
                 └── core/commerce（契约）
```

- **主题** 只依赖 core。商城主题在 `theme.toml` 声明 `[requires] commerce`（声明式 theme→plugin 依赖，由 core 解析），并调用文档化的渲染槽位。它从不 import Commerce 插件。
- **Commerce 引擎**（`plugins/commerce`）依赖 `core` 与 `core/commerce`。
- **卫星网关**（`plugins/commerce-paypal` …）依赖 `core/commerce`（外加一小片 core 用于 hook bus / options / 站点 URL）—— **绝不**依赖 `plugins/commerce`。

不变量：**任何插件都不 import 另一个插件**，且无 import 环。`core/commerce` 只 import `core/hook`。可在 CI 里用 `go list -deps` 断言。

## Money

所有金额都是精确整数，绝不用浮点：

```go
type Money struct {
    Amount   int64  // 最小单位：199 + "USD" == $1.99
    Currency string // ISO 4217
}
func New(minorUnits int64, currency string) Money
func (m Money) Add(o Money) (Money, error)   // 币种不同 → ErrCurrencyMismatch
func (m Money) Sub(o Money) (Money, error)
func (m Money) MulQty(qty int) Money
```

展示格式化在渲染层按请求 locale 处理；`Money.String()` 仅供调试。

## `core/commerce` 契约包

| 文件 | 内容 |
|---|---|
| `money.go` | `Money` 值类型 |
| `payment.go` | `PaymentGateway`、可选的 `GatewayAvailability` / `IdempotentRefunder`、`PaymentAction`（密封和类型）、`PaymentSettler`、`Reconciler`、请求/结算/退款类型 |
| `shipping.go` | `ShippingZone`、`ShippingMethod`、`ShippingRate` |
| `tax.go` | `TaxCalculator` |
| `promotion.go` | `PromotionRule`、`Adjustment` |
| `registry.go` | hook-bus 注册/读取 helper + 钩子名常量 |
| `types.go` | `Address`、`KV` |

### PaymentGateway 与 PaymentAction

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

`PaymentRequest.IdempotencyKey` 与 `RefundRequest.IdempotencyKey` 是重试时保持稳定的键，网关必须把它们传给支付服务商。两个可选能力在不让 Commerce 耦合具体网关的前提下收窄运行时行为：

```go
type GatewayAvailability interface {
    Available(c *gin.Context) bool
}
type IdempotentRefunder interface {
    RefundWithResult(c *gin.Context, req RefundRequest) (RefundResult, error)
}
```

未实现 `GatewayAvailability` 的网关为兼容旧实现仍视为可用。Commerce 执行服务商自动退款时要求 `IdempotentRefunder`，并持久化 `RefundResult.TransactionID`；声明 `Refund: false` 的网关继续走明确的手工/离线退款流程。

`PaymentAction` 是**密封和类型**（未导出 marker 方法把集合封闭），使店面能穷举渲染：

| Action | 含义 |
|---|---|
| `RedirectAction{URL}` | 把买家送去托管页（PayPal）。 |
| `DisplayAction{Title, Rows, QR, ExpiresAt}` | 展示说明并等待付款（加密货币、离线转账）。 |
| `InlineAction{ClientData}` | 渲染内联 tokenize 组件（卡 SDK；卡号不落 GoPress）。 |
| `CompletedAction{}` | 同步已结算（少见）。 |

### 确认反转 —— PaymentSettler

Commerce 不轮询网关。网关自行确认后回调：

```go
type PaymentSettler interface {
    Settle(ctx context.Context, req SettleRequest) error
}
type SettleRequest struct {
    OrderRef, Gateway, TxnID string
    Amount Money
    Status SettleStatus       // paid/underpaid/overpaid/expired/failed/refunded
    IdempotencyKey string     // 唯一索引 → 去重
    Raw map[string]any
}
```

引擎实现 `PaymentSettler` 并经 `SetSettler` 发布；网关用 `GetSettler(bus)` 取用。所有结算都汇入这一个幂等方法 —— 见 [支付](payments.md)。

### 注册 helper

```go
// 卫星，在 Activate 中：
handle := commerce.RegisterPaymentGateway(bus, myGateway)   // Deactivate 时移除
// 引擎，在结算时：
gateways := commerce.AvailablePaymentGateways(c, bus)        // 惰性读取 + 运行时就绪
```

`RegisterPaymentGateway` 把网关 append 进 `commerce.payment.gateways` **filter**；`PaymentGateways` 应用该 filter，`AvailablePaymentGateways` 还会计算可选的运行时可用性契约。因为是惰性读取，激活顺序无所谓。结算页会过滤展示选项，并在提交时重新检查，所以过期或伪造的支付方式值无法选中已禁用或未配置的网关。

## 数据模型

表名经 `dbprefix.PluginTable("commerce", <name>)` → `gp_plgn_commerce_*`，天然多站点隔离，并用 `RegisterPluginTable` 登记归属。

| 分组 | 表 |
|---|---|
| 目录 | `product_data`（content_id、sku、price、sale_price、stock、tax_class…）、`product_lookup`（冗余的当前价 + 是否有货，加速列表） |
| 购物车 | `carts`（游客 token 或 user_id）、`cart_items` |
| 订单 | `orders`（number、access_key、status、金额快照、payment_method）、`order_items`（名称/价格快照）、`order_addresses`、`payments`（idempotency_key 唯一）、`order_notes`、`refunds`（pending/succeeded/failed、idempotency_key 唯一、服务商退款 id） |
| 库存 | `inventory_ledger`（product_ref、delta、reason：in/out/reserve/release、order_id） |

金额列为 `BIGINT` 最小单位，配独立 `currency` 列。订单行**快照**名称与价格，历史订单不受商品后续改动影响。订单行对内容无外键（软关联），删商品不会级联进订单历史。

## RBAC

Commerce 不新增角色。`Activate` 时把电商能力授予现有 `editor`，`Deactivate` 时回收：

```text
shop_order.{read, update, refund}
coupon.{create, read, update, delete}
commerce_settings.{read, update}
```

`superadmin` 靠 `*.*` 通配自动拥有。每条受保护后台路由都用 `admin.RequirePermission`，客户侧订单查询强制**归属校验**（防 IDOR），即 `WHERE user_id = <当前用户>`。插件可实现 core 的通用 `SettingsAuthorizationProvider` 来声明设置资源：设置 GET 要求 `<resource>.read`，POST 要求 `<resource>.update`；插件贡献的导航项也声明各自 resource/action，使无权限入口不被渲染。Commerce 把它映射为 `commerce_settings`，core 不包含 Commerce 权限特判。见 [目录 · 购物车 · 订单](catalog-orders.md#客户账户与访问安全)。

## 通用 core 扩展点

Commerce 建立在通用 core 能力之上。每个都保持中立 —— core 不硬编码任何电商概念。

### `content.register_types`

`activateTheme()` 会用 core + 当前主题类型重建内容注册表。插件贡献的类型（如 `product`）会在每次切主题时丢失。于是 core 在注册完主题类型后触发 `content.register_types`；Commerce 在该 action 里幂等地（重）注册 `product`，并在 `Activate` 时立即再注册一次（首次主题激活可能早于插件加载）。

### `default_inactive` 模块门控

`plugin.Meta.DefaultInactive`（来自 `plugin.toml` 的 `default_inactive = true`）+ `DefaultInactiveProvider` 接口标记可选模块。无持久化状态时 `LoadPlugin` 只注册不激活。**系统设置 → 模块** 把所有默认停用插件列为开关。

### `DepNeedsEnable` 依赖状态

当主题依赖的插件「已内置、版本兼容、但未启用且默认停用」时，依赖解析返回 `DepNeedsEnable` —— 非阻断，主题照常激活，后台显示强「启用」提示，而非硬失败。缺失或版本不符的依赖仍然阻断。

### `Engine.RenderNamespacedInActiveTheme`

```go
func (e *Engine) RenderNamespacedInActiveTheme(c *gin.Context, namespace, fragment, extensionDefaultDir string, data gin.H) error
```

在当前主题的 `layouts/base.tmpl` + partials 内、用主题自己的 FuncMap 渲染扩展拥有的片段。片段解析是通用的：`<theme>/templates/<namespace>/<fragment>.tmpl` 优先于 `<extensionDefaultDir>/<fragment>.tmpl`，且 namespace/fragment 必须是安全的单路径分量。Commerce 传入 `commerce` 命名空间，core 本身不包含 Commerce 特判。`/cart`、`/checkout`、`/order-tracking`、`/my-account/*` 就是这样共用站点页头/页脚、并允许主题覆盖任意页面。`RenderInActiveTheme` 作为兼容包装保留，会从默认模板目录的 basename 通用推导 namespace。

### `admin.nav.items`

`buildMenuItems()` 在 System 分区之前应用此 filter，让激活插件贡献一个侧栏分区。Commerce 增加「电商」分区与**订单**。商品（内容类型）自动进「内容」分区。

### `seed.completed`

seeder 在首启种子与后台演示导入后触发此 action。Commerce 监听它，据商品内容的 `_commerce_*` meta 派生 `product_data`，使演示种子文件保持纯 core 内容、不碰插件表。见 [主题接入](theme-integration.md#演示数据与-seedcompleted-桥接)。

## Commerce 钩子命名空间

| 钩子 | 类型 | 载荷 |
|---|---|---|
| `commerce.payment.gateways` | filter | `[]PaymentGateway` |
| `commerce.shipping.methods` | filter | `[]ShippingMethod` |
| `commerce.payment.settler` | filter | `PaymentSettler`（引擎提供） |
| `commerce.checkout.validate` | filter | `[]string` 错误（非空则中止） |
| `commerce.order.status_changed` | action | `(order, old, new)`，仅在外围事务提交后发布 |
| `commerce.product.add_to_cart` | filter | 店面价格 + 加购 HTML 槽位 |

下一页：[目录 · 购物车 · 订单](catalog-orders.md)。
