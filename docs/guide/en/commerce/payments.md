# Payments

Commerce is medium-agnostic about payment. A gateway describes what should happen next at checkout (a `PaymentAction`), and confirms out of band by calling back through the engine's idempotent settler. This page explains the model, the two bundled gateways, and how to write your own satellite gateway.

## The confirmation-inversion model

The engine never asks a gateway "did this pay?" Instead:

```text
checkout → gateway.StartPayment → PaymentAction (storefront renders it)
        ↓
buyer pays by whatever means the gateway uses
        ↓
gateway confirms internally (webhook / manual admin action / on-chain poll)
        ↓
gateway calls commerce.GetSettler(bus).Settle(paid, txn, idempotencyKey)
        ↓
engine advances the order state machine (idempotently) → commits stock, emails
```

All confirmation paths — push (webhook), manual (admin "mark paid"), and pull (a `Reconciler`) — collapse into one idempotent `Settle`. The `payments.idempotency_key` unique index deduplicates, so a webhook retry or a return-then-webhook race never double-advances an order.

### The four next-actions

`StartPayment` returns one of a sealed set:

| Action | Used by | Storefront behavior |
|---|---|---|
| `RedirectAction{URL}` | hosted PSPs (PayPal) | 302 to the approval URL |
| `DisplayAction{Title, Rows, QR, ExpiresAt}` | offline transfer, crypto | render instructions, order → `on_hold` |
| `InlineAction{ClientData}` | tokenized card SDKs | render an inline widget (no PAN stored) |
| `CompletedAction{}` | store credit, comps | settle synchronously |

### Runtime availability and safe payment start

A gateway whose readiness depends on settings implements the optional `GatewayAvailability.Available` contract. Checkout uses `AvailablePaymentGateways` both to build the visible choices and to validate the submitted method. An active but disabled or incompletely configured gateway is therefore neither displayed nor selectable through a forged/stale form value. Gateways without this optional interface remain available for compatibility.

Commerce commits the local order, stock reservations, line/address snapshots, and pending payment first, then calls `StartPayment` with a stable `PaymentRequest.IdempotencyKey` (`start:<order-number>`). A gateway must forward this key to its provider so a retry cannot create a second remote payment.

The cart is not cleared before that external call. Only a `DefinitiveStartFailure`—where the gateway can prove no remote side effect occurred—fails the order/payment, releases reservations, and retains the cart. Ordinary errors, timeouts, invalid actions, and action-persistence failures are ambiguous: the payment enters `reconciliation`, stock stays reserved, and this cart snapshot is consumed until a webhook or operator resolves it; the TTL sweeper skips these orders. If a webhook already advanced the locked order, Commerce reuses that result. A paid event arriving after cancellation/failure moves the order into order-level `reconciliation` instead of silently leaving a charged buyer with a closed order.

### Optional pull-based confirmation

A gateway that cannot push (typical for crypto) implements `Reconciler`. The engine's scheduler periodically hands it the gateway's pending payments (with the opaque context stored at `StartPayment`); the gateway checks its source of truth and returns `SettleRequest`s. The chain choice, confirmation threshold, and indexer stay entirely inside the gateway.

## Built-in offline bank-transfer gateway

Lives inside the Commerce engine (`gateway_offline.go`), so a fresh store can take orders with zero external setup. `StartPayment` returns a `DisplayAction` with the bank details from settings; the order goes `on_hold`. Confirmation is the admin **Mark paid** action on the order detail, which calls `Settle(paid)` like any other gateway. It advertises `Refund: false` (offline refunds are recorded manually).

## Refund safety and cumulative limits

Every admin refund starts by locking the order and creating or resuming a `refunds` row with a stable, unique idempotency key. Its state is `pending`, `succeeded`, or `failed`; pending and failed/ambiguous attempts continue to reserve their amount because a provider may have completed the refund even when its response was lost. Retry such an attempt with the **same key**. A different request can use only `order total − succeeded − reserved`, so concurrent or repeated partial refunds cannot cumulatively exceed the order total. A non-empty remote refund id is unique within its gateway. If equal attempts make an incoming provider event ambiguous, Commerce rejects the event for retry instead of guessing which row to claim.

For a gateway advertising `Refund: true`, Commerce requires the optional `IdempotentRefunder` contract. `RefundWithResult` must send `RefundRequest.IdempotencyKey` to the provider and return a non-empty `RefundResult.TransactionID`. Commerce persists that remote id and uses it to correlate a later webhook rather than recording the refund twice. A registered gateway advertising `Refund: false` remains an explicit manual/offline record; a missing historical gateway is rejected rather than silently downgraded to a local-only refund.

The succeeded remote-money fact is committed before Commerce synchronizes the order status, so a note/status failure cannot erase evidence of money already returned. Partial refunds remain financial records without changing `processing`/`completed`; only when the cumulative succeeded amount reaches the full order total does the order move to `refunded`.

## PayPal satellite (`plugins/commerce-paypal`)

A standalone, opt-in plugin that proves the A-scheme end to end: `go list -deps` confirms it imports only `core/commerce` (plus a narrow core slice for the hook bus, options, and site URL) — never `plugins/commerce`. It uses a narrow `appHost` interface for host capabilities; only `register.go` imports `core` to call `RegisterPlugin`.

**Flow:**

1. `Available` requires the PayPal gateway to be enabled with complete credentials. `StartPayment` then creates a PayPal **Orders v2** order (intent `CAPTURE`, `custom_id` = our order number) and returns `RedirectAction{approvalURL}`.
2. **Buyer-return route** `GET /commerce/paypal/return` → captures the order synchronously and settles, then forwards the buyer to Commerce's return URL. This makes the loop work in a local sandbox without a public webhook. An `ORDER_ALREADY_CAPTURED` response falls back to reading the existing capture, so it is safe to run alongside the webhook.
3. **Webhook** `POST /commerce/paypal/webhook` → verifies the signature via PayPal's `verify-webhook-signature` API, then settles `CHECKOUT.ORDER.APPROVED` (capture), `PAYMENT.CAPTURE.COMPLETED` (paid), and `PAYMENT.CAPTURE.DENIED` (failed). PayPal's `PAYMENT.CAPTURE.REFUNDED` resource describes the aggregate **capture**, not one refund transaction; the handler validates and acknowledges that signal but does not reinterpret the capture id or original capture amount as a new refund. Locally initiated refunds are recorded from `RefundWithResult`, which returns the actual refund id.
4. Both paths settle idempotently with `IdempotencyKey = "paypal:capture:" + captureID`, so return and webhook dedup against each other. PayPal `PayPal-Request-Id` also carries stable keys for order creation, capture, and refund requests.
5. `RefundWithResult` calls the PayPal refund API against the capture id that Commerce passes as `RefundRequest.PaymentID`, and returns the PayPal refund id for persistence/webhook correlation.

Transient signature-verification API failures and settlement/database failures return 5xx from the webhook so PayPal retries; an actually invalid signature remains a 400.

Credentials (client id/secret, sandbox toggle, webhook id) live in the plugin settings page; the secret is a password field preserved on blank save. The plugin is `default_inactive`.

**PayPal object vs. our order:** "Orders v2" is PayPal's REST API version — the created *PayPal order* is a transient payment object on PayPal's side, not a row in our `orders` table. We keep no PayPal order id in our DB; the two are linked only by `custom_id` = our order number.

> Because it is webhook-driven for production confirmation, a full sandbox test needs PayPal sandbox credentials and a publicly reachable webhook URL (e.g. a tunnel). The code path is complete; that live test is the one manual step still outstanding.

## Writing your own satellite gateway

A minimal gateway is a few files. The essential shape:

```go
// gateway.go — implement the core contract
type myGateway struct{ p *Plugin }

func (myGateway) ID() string                { return "mygw" }
func (myGateway) Title(*gin.Context) string { return "My Gateway" }
func (myGateway) Icon() string              { return "💳" }
func (myGateway) Capabilities() corecommerce.Capabilities {
    return corecommerce.Capabilities{Refund: true}
}
func (g myGateway) Available(*gin.Context) bool { return g.p.configReady() }
func (g myGateway) StartPayment(c *gin.Context, req corecommerce.PaymentRequest) (corecommerce.PaymentAction, error) {
    // create a charge/session with your PSP using req.Amount, req.OrderRef,
    // req.ReturnURL, req.CancelURL, and req.IdempotencyKey …
    return corecommerce.RedirectAction{URL: approvalURL}, nil
}
func (g myGateway) Refund(c *gin.Context, req corecommerce.RefundRequest) error {
    _, err := g.RefundWithResult(c, req)
    return err
}
func (g myGateway) RefundWithResult(c *gin.Context, req corecommerce.RefundRequest) (corecommerce.RefundResult, error) {
    // send req.IdempotencyKey to the PSP; return its durable refund id
    return corecommerce.RefundResult{TransactionID: providerRefundID}, nil
}

// plugin.go — register through the hook bus in Activate
func (p *Plugin) Activate(app plugin.App) {
    host := app.(appHost)                 // narrow interface: HookBus(), OptionsStore(), PublicSiteURL()
    p.hooks = host.HookBus()
    p.filters = append(p.filters, corecommerce.RegisterPaymentGateway(p.hooks, myGateway{p: p}))
    // register your webhook/return routes on the routes.register action
}

// on confirmation (webhook/return/poll):
settler := corecommerce.GetSettler(p.hooks)
settler.Settle(ctx, corecommerce.SettleRequest{
    OrderRef:       orderNumber,     // = req.OrderRef you passed through your PSP as metadata
    Gateway:        "mygw",
    TxnID:          chargeID,
    Amount:         corecommerce.New(minorUnits, currency),
    Status:         corecommerce.SettlePaid,
    IdempotencyKey: "mygw:charge:" + chargeID,   // stable across retries
})
```

Checklist:

- Depend on `core/commerce` only; do **not** import `plugins/commerce`.
- Carry the Commerce `OrderRef` through your PSP (metadata / `custom_id`) so confirmation can find the order.
- Implement `GatewayAvailability` when readiness depends on runtime configuration; never rely on UI hiding alone.
- Forward the stable payment/refund request key to the provider, and make each settlement-event `IdempotencyKey` stable and unique.
- If you advertise automatic refunds, implement `IdempotentRefunder` and return a durable, non-empty provider refund id.
- Verify webhook signatures; never store PAN/CVV — use hosted redirect, display, or tokenization only.
- Register in `Activate`, remove the handle in `Deactivate`, ship `default_inactive = true`, and add a blank import to `internal/autoload/autoload_gen.go`.
- Add a `LogoSVG()` (`static/logo.svg`) for the admin plugin card.

Next: [Theme Integration](theme-integration.md).
