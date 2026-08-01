# 目录 · 购物车 · 订单

本页讲店面领域服务：商品目录、购物车、结算编排、库存并发、订单状态机、订单后台与客户账户。

## 目录

`product` 是普通内容类型（`HasArchive`、rewrite slug 为 `store`、taxonomies `product_cat`/`product_tag`），因此复用后台 CRUD、SEO、sitemap 与主题的归档/详情模板。其电商字段存在两张表：

- **`product_data`** —— 权威的逐商品电商记录（SKU、价格、促销价、库存、税类、重量），以 content id 为键。
- **`product_lookup`** —— 冗余行（有效价、是否有货），每次保存刷新，用于目录快速列表与筛选。

后台 meta box 经 `admin.content_form.fields` filter 注入（仅 `product` 类型），经 `admin.content.saved` action 持久化 —— 于是 Commerce 拥有自己的字段而无需改核心保存逻辑。价格从小数（`19.99`）解析为整数最小单位（`1999`）。

## 购物车

`CartService` 无状态（每请求构造）。它解析当前车，并协调游客车与账号车：

- **游客** —— `gp_cart` cookie 里的随机 token（SameSite=Lax、HttpOnly）。
- **登录** —— 以 `user_id` 为键。
- **登录时** —— 游客车要么被收养为用户车，要么并入用户车（数量相加），随后清掉游客车与 cookie。

改动（`Add`、`SetItemQty`、`RemoveItem`）强制**归属校验**：条目必须属于当前车，防 IDOR。新增/更新只接受 1 到 999 的数量，并使用带溢出检查的整数运算。商品 id 只有在 core 内容中确为当前已发布的 `product`，且权威 `product_data` 行具备有效正数成交价、与店铺一致的币种和充足的受管库存时才会被接受；价格与库存从不信任表单或冗余缓存行。

`View` 重复这些权威校验，并从 `product_data` 实时计算当前价格；旧购物车中的无效行不会变成可购买订单行。`Count` 是只读角标查询，从不创建车。购物车改动在需要时使用行锁，三个公开 POST 路由都强制同源 CSRF 防护。

公共路由（经 `routes.register` 注册）：`GET /cart`、`POST /cart/add|update|remove`。

## 结算编排

`CheckoutService.PlaceOrder(c, in) (*Order, PaymentAction, error)` 先捕获权威购物车快照、校验所选网关与带检查的总额，再把本地事务与外部支付调用分开：

```text
校验（commerce.checkout.validate filter）→ 重查网关可用性 →
带溢出检查地计算运费/税/折扣 →
BEGIN TX:
  建 Order（status=pending、access_key、金额 + 名称/价格快照）
  建 order_items、order_addresses
  InventoryService.Reserve(每行, SELECT … FOR UPDATE)   ← 超卖则整单回滚
  建 payments(pending, idempotency_key = "start:<number>")
COMMIT
→ gateway.StartPayment(order, idempotency_key = "start:<number>") → PaymentAction
→ 成功后只消费已捕获购物车快照中的数量
```

结算页只展示 `AvailablePaymentGateways`，提交时还会重新检查可用性，禁用/未配置的支付方式无法通过过期或伪造值选中。只有网关明确标记为「远端尚未产生副作用」的启动失败才会补偿：订单/支付转失败、释放预留并保留购物车。超时、断连或动作落库失败等不确定结果会把支付置为 `reconciliation`、保留库存并消费本次购物车快照，等待 webhook 或人工核对，避免重复扣款。若 webhook 已抢先推进锁定订单，则直接复用其结果。成功后的清理不会整车删除：只扣除已下单数量，并保留另一标签页的并发新增或修改。

返回的 `PaymentAction` 决定下一步：

- `DisplayAction`（离线/加密）→ 订单转 `on_hold`（等待付款），渲染说明。
- `RedirectAction`（PayPal）→ 买家被送往站外；订单保持 `pending` 直到网关确认。
- `CompletedAction` → 立即结算。

店面路由：`GET /checkout`、`POST /checkout`（强制同源/CSRF）、`GET /checkout/complete/:number`。

## 库存并发

`InventoryService` 在结算事务内用行锁防超卖：

- **Reserve** —— `tx.Clauses(clause.Locking{Strength:"UPDATE"})` 读 `product_data`；仅当 `stock_qty >= qty` 才扣减并写一条 `inventory_ledger` `reserve`。否则报错、整单回滚。两个请求抢最后一件 → 只有一个成功。
- **Commit** —— `paid` 时把预留落实为永久 `out`。
- **Release** —— 取消/超时订单或明确无远端副作用的支付启动失败时，先锁定 `product_data` 行再退回库存；带检查的加法会拒绝数量溢出。不确定的远端结果不会由 TTL 清理器释放。

商品后台表单携带 `product_data.version` 乐观锁；每次库存预留/释放都会递增版本，因此打开已久的表单不能把结算刚扣除的库存写回。演示 Meta 也只补建不存在的商品数据，重复导入或插件重新激活不会重置真实价格和库存。

弃单由 TTL 清理任务释放。由于 core 的 `Scheduler` 在启动时（早于默认停用插件激活）就启动 ticker，Commerce 把清理器作为**自己的 goroutine** 运行：激活时启动、停用时停止，而不注册进 core 调度器。

## 订单状态机

`OrderService.Transition(order, event)` 在调用者事务内做受控转移并写一条 `order_notes`。状态更新带预期旧状态，因此并发变化会返回 `ErrTransitionConflict`，而不会覆盖更新。它返回不可变的变化快照，但刻意**不触发** Hook：

```text
pending ──paid──► processing ──ship──► completed
   ├─cancel─► cancelled          processing/completed ──累计全额退款──► refunded
   ├─fail───► failed
   └─hold───► on_hold ──paid──► processing
cancelled/failed ──迟到的资金确认──► reconciliation ──全额退款──► refunded
```

调用者只在外围事务**提交后**发布 `commerce.order.status_changed`，使邮件/分析 Hook 不会看到随后回滚的状态。`paid` 提交库存并排队确认邮件；`cancel`/`fail` 释放库存。关闭订单若随后收到资金确认，会进入 `reconciliation` 而不会假装库存仍可履约。非法转移会被拒，幂等 settlement 还会核对重复键的完整载荷，防止不同事件冒用同一键。部分退款只是财务记录，履约状态仍为 `processing` 或 `completed`；只有累计全额退款才转为 `refunded`（旧的 `partially_refunded` 行仍可继续操作）。

## 订单后台

在后台 **电商 → 订单** 分区（经 `admin.nav.items` 贡献）：

- **列表** —— 订单号、状态、金额、邮箱、时间。
- **详情** —— 商品、地址、备注，以及操作：**标记已付**、**发货**、**取消**、**退款**、加备注。

每条路由由 `admin.RequirePermission(auth, rbac, "shop_order", "read"|"update"|"refund")` 守护；路由集成测试验证已认证但无相应能力的角色得到 403，有权限操作则通过同一中间件。退款操作先锁定订单、检查累计剩余额度，并在调用服务商前创建带稳定幂等键的尝试记录。自动退款要求已注册网关返回 `IdempotentRefunder` 结果（含非空远端退款 id）；手工/离线退款保持明确。pending 与结果未知的 failed 尝试会占用额度，直到使用同一键重试。见 [退款安全与累计限额](payments.md#退款安全与累计限额)。

## 客户账户与访问安全

完全支持游客结算 —— 从不强制注册。模块提供两种看订单的方式，外加一层加固：

- **订单访问密钥** —— 每单在创建时得到一把 256bit 的 `AccessKey`。订单完成/状态页（`/checkout/complete/:number`）要求**账号归属 或 密钥匹配**（常量时间比较）。这堵住了对短订单号（`日期-8hex`）的枚举：猜到号也看不到任何东西。下单后重定向与确认邮件都带 `?key=…`。
- **游客订单查询** —— `GET|POST /order-tracking` 按**订单号 + 邮箱**双因子查单。任何不匹配都返回同一句通用错误（不泄露），端点做同源校验，并有按 IP 的尝试限频（内存、best-effort）在**触库之前**掐断爆破。
- **我的订单** —— 登录客户的 `GET /my-account/orders(/:number)`；未登录访客被引导去登录并回跳。详情查询以 `WHERE user_id` 强制归属（防 IDOR）。

订单完成模板以 `Heading` 参数化，被结算完成、游客查询、账户详情三处共用。

## 确认邮件

在 `paid` 转移时，Commerce 经 core `Workers` 池 + `Mail` 服务异步发确认邮件（仿 `notification` 包的联系留言通知器）。邮件链接携带订单访问密钥。

下一页：[支付](payments.md)。
