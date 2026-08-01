# 支付

Commerce 对支付媒介保持无关。网关在结算时描述「下一步该发生什么」（一个 `PaymentAction`），并通过引擎的幂等 settler 在带外确认。本页讲这个模型、两个内置网关，以及如何编写你自己的卫星网关。

## 确认反转模型

引擎从不问网关「这单付了吗？」，而是：

```text
结算 → gateway.StartPayment → PaymentAction（店面渲染它）
        ↓
买家用网关的任意方式付款
        ↓
网关内部确认（webhook / 后台手动 / 链上轮询）
        ↓
网关调用 commerce.GetSettler(bus).Settle(paid, txn, idempotencyKey)
        ↓
引擎（幂等地）推进订单状态机 → 提交库存、发邮件
```

所有确认路径 —— 推送（webhook）、手动（后台「标记已付」）、拉取（`Reconciler`）—— 都收敛到一个幂等 `Settle`。`payments.idempotency_key` 唯一索引去重，因此 webhook 重投、或「回跳与 webhook 竞争」都不会重复推进订单。

### 四种下一步动作

`StartPayment` 返回密封集合之一：

| Action | 使用者 | 店面行为 |
|---|---|---|
| `RedirectAction{URL}` | 托管 PSP（PayPal） | 302 到审批 URL |
| `DisplayAction{Title, Rows, QR, ExpiresAt}` | 离线转账、加密货币 | 渲染说明，订单 → `on_hold` |
| `InlineAction{ClientData}` | tokenize 卡 SDK | 渲染内联组件（不落卡号） |
| `CompletedAction{}` | 店铺余额、赠单 | 同步结算 |

### 运行时可用性与安全启动支付

就绪状态取决于设置的网关实现可选的 `GatewayAvailability.Available` 契约。结算页通过 `AvailablePaymentGateways` 同时生成可见选项并校验提交方式；因此已激活但被禁用、或配置不完整的网关既不会显示，也无法用伪造/过期表单值选中。未实现该可选接口的旧网关为兼容仍视为可用。

Commerce 先提交本地订单、库存预留、订单行/地址快照和 pending 支付，再用稳定的 `PaymentRequest.IdempotencyKey`（`start:<订单号>`）调用 `StartPayment`。网关必须把此键传给支付服务商，保证重试不会重复创建远端支付。

外部调用前不会清空购物车。只有 `DefinitiveStartFailure`（网关能证明远端尚未产生副作用）才会把订单/支付标为失败、释放预留并保留购物车。普通错误、超时、无效动作或动作落库失败都按结果未知处理：支付进入 `reconciliation`，库存继续保留，本次购物车快照被消费，等待 webhook 或人工核对；TTL 清理器会跳过这类订单。如果 webhook 已抢先推进锁定订单，则复用其结果。即使订单已取消/失败，迟到的已付款事件也会将其转入订单级 `reconciliation`，而不会留下「已扣款但订单关闭」的静默状态。

### 可选的拉取式确认

无法推送的网关（加密货币常见）实现 `Reconciler`。引擎调度器周期性把该网关的待确认支付（连同 `StartPayment` 时存下的不透明上下文）交给它；网关查自己的真值来源并返回 `SettleRequest`。链选型、确认数阈值、indexer 全在网关内部。

## 内置离线银行转账网关

位于 Commerce 引擎内（`gateway_offline.go`），使全新店铺零外部配置即可收单。`StartPayment` 返回带设置里银行信息的 `DisplayAction`，订单转 `on_hold`。确认是订单详情页的后台**标记已付**操作，它像任何网关一样调 `Settle(paid)`。它声明 `Refund: false`（离线退款手工记录）。

## 退款安全与累计限额

每次后台退款先锁定订单，并用稳定、唯一的幂等键新建或恢复一条 `refunds` 记录。记录状态为 `pending`、`succeeded` 或 `failed`；pending 与失败/结果未知的尝试会继续占用对应可退额度，因为即使服务商响应丢失，它也可能已经完成退款。此类尝试必须用**同一个键**重试。其他请求只能使用「订单总额 − 已成功退款 − 已占用额度」，因此并发或反复部分退款的累计值不会超过订单总额。非空网关退款号按「网关 + 远端 id」联合唯一；若两个等额尝试使网关事件无法唯一归属，Commerce 会拒绝本次事件并等待重试，而不会猜测认领某一行。

对于声明 `Refund: true` 的网关，Commerce 要求可选的 `IdempotentRefunder` 契约。`RefundWithResult` 必须把 `RefundRequest.IdempotencyKey` 传给服务商，并返回非空的 `RefundResult.TransactionID`。Commerce 持久化该远端 id，以便后续 webhook 关联同一笔退款而不是重复记录。已注册且声明 `Refund: false` 的网关仍可走明确的手工/离线记录；历史支付网关缺失时会拒绝操作，不会静默降级成本地退款。

远端退款成功这一资金事实会先提交，再同步订单状态；因此备注/状态同步失败无法抹掉已退资金的证据。部分退款只保留为财务记录，不改变 `processing`/`completed`；只有累计成功退款达到订单全额时，订单才进入 `refunded`。

## PayPal 卫星（`plugins/commerce-paypal`）

一个独立、可选的插件，端到端印证 A 方案：`go list -deps` 确认它只 import `core/commerce`（外加一小片 core 用于 hook bus、options、站点 URL）—— 绝不 import `plugins/commerce`。它用一个窄 `appHost` 接口取宿主能力；只有 `register.go` import `core` 去调 `RegisterPlugin`。

**流程：**

1. `Available` 要求 PayPal 网关已启用且凭据完整；随后 `StartPayment` 创建 PayPal **Orders v2** 订单（intent `CAPTURE`，`custom_id` = 我方订单号）并返回 `RedirectAction{审批URL}`。
2. **买家回跳路由** `GET /commerce/paypal/return` → 同步 capture 并 settle，再把买家转发到 Commerce 的 return URL。这让本地沙盒无需公网 webhook 也能闭环。若返回 `ORDER_ALREADY_CAPTURED`，回退去读已有 capture，因此可与 webhook 并存。
3. **Webhook** `POST /commerce/paypal/webhook` → 经 PayPal `verify-webhook-signature` API **验签**，再处理 `CHECKOUT.ORDER.APPROVED`（capture）、`PAYMENT.CAPTURE.COMPLETED`（paid）和 `PAYMENT.CAPTURE.DENIED`（failed）。PayPal 的 `PAYMENT.CAPTURE.REFUNDED` 资源描述的是汇总后的 **capture**，并非某一笔退款交易；处理器会校验并确认该信号，但不会把 capture ID 或原始 capture 金额误当成一笔新退款。本系统发起的退款以 `RefundWithResult` 返回的真实退款号为权威记录。
4. 两条路径都以 `IdempotencyKey = "paypal:capture:" + captureID` 幂等 settle，因此回跳与 webhook 互相去重。创建订单、capture 与退款请求还会通过 PayPal `PayPal-Request-Id` 携带稳定键。
5. `RefundWithResult` 用 Commerce 作为 `RefundRequest.PaymentID` 传入的 capture id 调 PayPal 退款 API，并返回 PayPal 退款 id 供持久化与 webhook 关联。

验签 API 的临时故障、以及结算/数据库失败会让 webhook 返回 5xx，要求 PayPal 重试；确实无效的签名仍返回 400。

凭据（client id/secret、sandbox 开关、webhook id）存插件设置页；secret 是 password 字段、留空保存时保持不变。插件为 `default_inactive`。

**PayPal 订单 vs 我方订单：**「Orders v2」是 PayPal 的 REST API 版本 —— 创建出的 *PayPal 订单* 是 PayPal 那边的临时支付对象，不是我方 `orders` 表里的行。我方库里不存 PayPal 订单 id；二者仅靠 `custom_id` = 我方订单号 关联。

> 由于生产确认是 webhook 驱动，完整沙盒测试需要 PayPal 沙盒凭据 + 一个公网可达的 webhook URL（如内网穿透）。代码路径已完整；这项线上实测是唯一尚未跑过的手工步骤。

## 编写你自己的卫星网关

一个最小网关只需几个文件。核心骨架：

```go
// gateway.go —— 实现 core 契约
type myGateway struct{ p *Plugin }

func (myGateway) ID() string                { return "mygw" }
func (myGateway) Title(*gin.Context) string { return "My Gateway" }
func (myGateway) Icon() string              { return "💳" }
func (myGateway) Capabilities() corecommerce.Capabilities {
    return corecommerce.Capabilities{Refund: true}
}
func (g myGateway) Available(*gin.Context) bool { return g.p.configReady() }
func (g myGateway) StartPayment(c *gin.Context, req corecommerce.PaymentRequest) (corecommerce.PaymentAction, error) {
    // 用 req.Amount、req.OrderRef、req.ReturnURL、req.CancelURL、req.IdempotencyKey … 在你的 PSP 建单/会话
    return corecommerce.RedirectAction{URL: approvalURL}, nil
}
func (g myGateway) Refund(c *gin.Context, req corecommerce.RefundRequest) error {
    _, err := g.RefundWithResult(c, req)
    return err
}
func (g myGateway) RefundWithResult(c *gin.Context, req corecommerce.RefundRequest) (corecommerce.RefundResult, error) {
    // 把 req.IdempotencyKey 传给 PSP，并返回其持久退款 id
    return corecommerce.RefundResult{TransactionID: providerRefundID}, nil
}

// plugin.go —— 在 Activate 中经 hook bus 注册
func (p *Plugin) Activate(app plugin.App) {
    host := app.(appHost)                 // 窄接口：HookBus()、OptionsStore()、PublicSiteURL()
    p.hooks = host.HookBus()
    p.filters = append(p.filters, corecommerce.RegisterPaymentGateway(p.hooks, myGateway{p: p}))
    // 在 routes.register action 上注册你的 webhook/回跳路由
}

// 确认时（webhook/回跳/轮询）：
settler := corecommerce.GetSettler(p.hooks)
settler.Settle(ctx, corecommerce.SettleRequest{
    OrderRef:       orderNumber,     // = 你透传给 PSP 作为元数据的 req.OrderRef
    Gateway:        "mygw",
    TxnID:          chargeID,
    Amount:         corecommerce.New(minorUnits, currency),
    Status:         corecommerce.SettlePaid,
    IdempotencyKey: "mygw:charge:" + chargeID,   // 重投时保持稳定
})
```

清单：

- 只依赖 `core/commerce`；**不要** import `plugins/commerce`。
- 把 Commerce 的 `OrderRef` 透传给你的 PSP（metadata / `custom_id`），确认时才能找回订单。
- 就绪状态依赖运行时配置时实现 `GatewayAvailability`；不要只依赖前端隐藏。
- 把稳定的支付/退款请求键传给服务商；每个结算事件的 `IdempotencyKey` 也要稳定且唯一。
- 如声明支持自动退款，实现 `IdempotentRefunder` 并返回持久、非空的服务商退款 id。
- 验 webhook 签名；绝不存 PAN/CVV —— 只走托管重定向、展示型或 tokenization。
- 在 `Activate` 注册、`Deactivate` 移除句柄，发布 `default_inactive = true`，并在 `internal/autoload/autoload_gen.go` 加一条空白 import。
- 加 `LogoSVG()`（`static/logo.svg`）用于后台插件卡片。

下一页：[主题接入](theme-integration.md)。
