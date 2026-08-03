# Commerce 总览

Commerce 是 GoPress 的可选电商模块 —— 面向生产的店面底座（商品、购物车、结算、订单、库存、支付），以及供支付/物流卫星插件接入的注册表。它在 GoPress 生态中的定位，等同于 WooCommerce 之于 WordPress，但建立在 Go 接口、编译型服务与严格的扩展边界之上。

它**默认停用**。在运营者显式启用之前，Commerce 不会给站点带来任何痕迹。

## 提供的能力

- **目录** —— `product` 内容类型，后台带商品数据 meta box（SKU、价格、促销价、库存、税类、重量），并有一张冗余查询表加速列表。
- **购物车** —— 游客（cookie）车与登录车，登录时游客车并入账号车，改动做归属校验（防 IDOR）。
- **结算** —— 单事务下单：快照价格、行锁预留库存，再交给支付网关。
- **订单** —— 受控状态机（`pending → processing → completed`，旁支 `cancelled/failed/on_hold/refunded/partially_refunded`）、订单后台（列表、详情、标记已付、发货、取消、退款、备注）、确认邮件。
- **库存** —— 预留 / 提交 / 释放，`SELECT … FOR UPDATE` 行锁，加上释放弃单的 TTL 清理任务。
- **支付** —— 媒介无关的 `PaymentGateway` 契约、内置离线银行转账网关、**PayPal** 重定向/webhook 卫星，以及可选的**以太坊 USDT** 展示/拉取式卫星（RPC 验证扫链与幂等结算）。
- **客户账户** —— 游客订单查询（订单号 + 邮箱）、登录用户的「我的订单」，以及一把高熵访问密钥堵住订单号枚举。
- **主题接入** —— 渲染槽位与主题外壳渲染器，让任意主题无需 import 插件即可呈现店面，示例主题为 `shop-starter`。

## 「A 方案」：契约下沉 core

最关键的架构决策是把支付/物流/税费**契约放进 core** —— 一个极小的 `core/commerce` 包，只依赖 `core/hook`。Commerce 引擎与所有卫星插件都依赖这些契约，而**彼此不依赖**。

```text
core/commerce         仅契约：Money、PaymentGateway、PaymentAction、
   ▲          ▲        PaymentSettler、ShippingMethod、TaxCalculator、注册 helper
   │          │
plugins/commerce     plugins/commerce-paypal、-stripe、-usdt …
（引擎）               （卫星网关）
   │                          │
   └────────► core ◄──────────┘   两者都依赖 core；彼此不 import
```

由此得到三条模块其余部分赖以成立的性质：

1. **无 plugin→plugin 依赖、无 import 环。** 卫星只 import `core/commerce`，经 core hook bus 注册自身。对 `plugins/commerce-paypal` 跑 `go list -deps` 不会出现对 `plugins/commerce` 的依赖。
2. **注册顺序无关。** 卫星在 `Activate` 时把自己 append 进一个 filter；引擎在结算时才惰性读取该 filter。谁先激活都行。
3. **引擎永不知晓网关的确认机制。** 网关用任意内部手段（webhook、后台手动、链上轮询）确认后，回调引擎的**幂等** `PaymentSettler`。见 [支付](payments.md)。

## 模块门控

Commerce 是 `default_inactive` 插件（一个通用 core 能力，而非电商特判）。商城主题在 `theme.toml` 里声明 `[requires] commerce`；激活该主题**不会**自动启用 Commerce，而是后台显示醒目的「启用 Commerce」横幅，模块也可在 **系统设置 → 模块** 里开关。停用时，Commerce 不注册内容类型、路由、后台页或钩子 —— 站点毫无电商痕迹。

启用流程见 [快速上手](getting-started.md)。

## 为 Commerce 引入的通用 core 能力

实现 Commerce 需要几个**通用**的 core 扩展点 —— 有意做成非电商特定，因此惠及任何插件：

| 能力 | 用途 |
|---|---|
| `content.register_types` action | 让插件幂等地（重）注册内容类型，从而在切主题（会重建注册表）后依然存在。 |
| `default_inactive` + `DefaultInactiveProvider` | 标记默认停用、可在「设置 → 模块」开关的可选模块。 |
| `DepNeedsEnable` 依赖状态 | 主题依赖「已内置但未启用」时，产生非阻断的「立即启用」强提示，而非硬阻断。 |
| `Engine.RenderNamespacedInActiveTheme` | 在当前主题布局内渲染扩展拥有的整页，支持按命名空间隔离的主题覆盖。 |
| `admin.nav.items` filter | 让激活插件贡献一个后台侧栏分区（Commerce 的「订单」在此）。 |
| `seed.completed` action | 演示/种子导入后触发，让插件从种子内容派生卫星数据（Commerce 据商品 meta 建 `product_data`）。 |

这些在 [架构](architecture.md) 与通用的 [Hook 系统](../architecture/hooks.md) 中有说明。

## 分阶段交付

| 阶段 | 范围 | 状态 |
|---|---|---|
| P0 地基 | core 扩展点、`core/commerce` 契约、`Money`、默认停用插件骨架、设置页 | 已完成 |
| P1 目录 + 购物车 | `product` 类型 + 数据表、后台 meta box、目录、游客/登录车与合并 | 已完成 |
| P2 结算 + 订单 | 地址/物流/税、订单状态机、库存预留、订单后台、确认邮件、离线网关、PayPal 卫星 | 已完成（仅剩线上沙盒支付实测） |
| P3+ | 优惠券、变体商品、更完整的税/物流分区、Store REST API、my-account 扩展 | 后续 |

## 范围边界（v1）

实体商品、单币种、单商户。商品类型仅 `simple`。当前明确不做：多商户/marketplace、订阅、多币种结算、数字/下载类商品、复杂促销叠加、B2B 报价。

## 下一步

- [快速上手](getting-started.md) —— 启用模块、装商城主题、导入演示数据、跑一单。
- [架构](architecture.md) —— 契约、钩子、表、RBAC 与依赖规则。
- [目录 · 购物车 · 订单](catalog-orders.md) —— 商品、购物车、结算编排、库存、订单状态机与客户账户。
- [支付](payments.md) —— 网关契约与如何编写卫星网关。
- [主题接入](theme-integration.md) —— 渲染槽位、主题外壳、商城主题与演示数据。
