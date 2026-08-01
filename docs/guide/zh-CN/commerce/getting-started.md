# Commerce 快速上手

本页带你端到端走一遍：启用模块、安装商城主题、导入演示数据、跑通一单。

## 1. 启用 Commerce 模块

Commerce **默认停用**。两种启用方式：

- **系统设置 → 模块** —— Commerce 会作为一张开关卡片出现（因为它是 `default_inactive` 插件）。打开即可。
- **激活商城主题** —— 声明 `[requires] commerce` 的主题会在后台触发醒目的「启用 Commerce」横幅，其中的「立即启用」按钮直达模块面板。

启用模块会注册 `product` 内容类型、店面路由（`/cart`、`/checkout`、`/order-tracking`、`/my-account/orders`）、后台「订单」分区、RBAC 授权与支付 settler。停用则干净移除这一切，站点回到毫无电商痕迹的状态。

> 底层上，开关模块会调用 `RefreshActiveTheme()`，重建内容注册表与路由，使商品类型与路由即时出现/消失，无需重启。

## 2. 安装商城主题（shop-starter）

`shop-starter` 是内置的公开参考主题：紧凑的单页店面包含左右分栏 Hero、分类快捷入口、商品网格和小型信任区域，并提供可承载完整 Commerce 流程的响应式主题外壳。在 **外观 → 主题** 里激活它。

由于 `shop-starter` 声明了 `[requires] commerce`，在 Commerce 未启用时激活它会浮出上面说的启用横幅。

## 3. 导入演示数据

在 **主题** 页，`shop-starter` 会显示「导入演示数据」按钮（它实现了 `DemoDataProvider`）。导入会种下：

- 站点 + 主题设置（店铺身份、公告栏、Hero、商品区域文案、页脚与可选社交链接）。
- 4 个商品分类与 2 个标签。
- 6 个含价演示商品，图片下载入媒体库。

商品**价格**存在 Commerce 的 `product_data` 表，而非 core 内容里。种子把价格作为 `_commerce_*` 内容 meta 携带，Commerce 在通用的 `seed.completed` 钩子上据此派生 `product_data`（激活时也会再同步一次，所以「先导入后启用」也能补价）。见 [主题接入](theme-integration.md#演示数据与-seedcompleted-桥接)。

> 请在导入**之前**启用 Commerce（或导入后重新激活一次），以确保导入触发时价格同步监听器已注册。

## 4. 手动新建/编辑商品

商品是普通内容类型。在 **内容 → 商品** 中新建或编辑，Commerce 的 meta box 增加：

| 字段 | 含义 |
|---|---|
| SKU | 库存单位编号 |
| 价格 | 常规价（存为整数最小单位） |
| 促销价 | 可选折扣价 |
| 管理库存 + 库存数量 | 启用预留 / 防超卖 |
| 税类 | 用于税费计算 |
| 重量 | 用于运费 |

保存时写入 `product_data`（upsert）并刷新 `product_lookup`（目录快查表）。价格以小数录入（`19.99`），存为最小单位（`1999`）。

## 5. 配置店铺

- **内容 → 商品** meta box —— 逐商品的电商字段。
- **插件 → Commerce → 设置** —— 店铺币种、国家、重量单位、统一运费、未支付订单 TTL，以及结算页展示的离线银行转账信息。
- **外观 → 主题 → shop-starter → 主题设置** —— 店铺身份、公告栏、Hero、商品区域文案、页脚联系信息与可选社交链接。

## 6. 跑通一单（离线网关）

内置的**离线银行转账**网关零外部依赖，可立刻打通闭环：

1. 在店面打开某商品（`/store/<slug>`），加入购物车。
2. 进 `/cart`，点**去结算**。
3. 填地址、选**银行转账**、下单。
4. 订单创建（状态 `on_hold`），买家看到银行信息与订单完成页。
5. 后台 **订单 → 订单详情**，点**标记已付款**。这会调用 settler，把订单推进到 `processing`、提交已预留库存，并异步发确认邮件。

要接收真实在线支付，配置 **PayPal** 卫星 —— 见 [支付](payments.md)。

## 文件地图

| 区域 | 位置 |
|---|---|
| 契约 | `core/commerce/` |
| 引擎插件 | `plugins/commerce/` |
| PayPal 卫星 | `plugins/commerce-paypal/` |
| 商城主题 | `themes/shop-starter/` |
| 演示种子 | `themes/shop-starter/demo/data/seed.toml` |
| 设计稿 | `docs/design/commerce-*.md` |

下一页：[架构](architecture.md)。
