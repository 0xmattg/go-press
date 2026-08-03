package commerce

import (
	"context"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
)

// Capabilities advertises what a gateway supports so commerce can toggle UI
// (e.g. a refund button) accordingly.
type Capabilities struct {
	Refund        bool
	PartialRefund bool
}

// PaymentGateway is the medium-agnostic contract every payment method
// implements — hosted-redirect PSPs (PayPal), display-type methods (crypto,
// offline bank transfer) and inline/tokenized card forms all fit. commerce
// never assumes the confirmation mechanism: gateways confirm by their own means
// and call back through PaymentSettler.
type PaymentGateway interface {
	ID() string
	Title(c *gin.Context) string
	Icon() string
	Capabilities() Capabilities

	// StartPayment prepares payment for an order at checkout and returns a
	// medium-agnostic next action for the storefront to render.
	StartPayment(c *gin.Context, req PaymentRequest) (PaymentAction, error)

	// Refund performs a (partial) refund. Gateways without refund support should
	// report Capabilities.Refund == false; commerce then never calls this.
	Refund(c *gin.Context, req RefundRequest) error
}

// GatewayAvailability is an OPTIONAL capability for gateways whose checkout
// availability depends on runtime configuration (credentials, enabled flag,
// etc.). Gateways that do not implement it are considered available. Commerce
// checks this both while rendering choices and while accepting a submission so
// a stale/forged payment-method value cannot select an unconfigured gateway.
type GatewayAvailability interface {
	Available(c *gin.Context) bool
}

// GatewayCurrencySupport is an OPTIONAL capability for gateways that support
// only a subset of store currencies. Commerce evaluates it both while rendering
// checkout choices and again on submission. Gateways that do not implement it
// remain currency-agnostic for backwards compatibility.
type GatewayCurrencySupport interface {
	SupportsCurrency(currency string) bool
}

// DefinitiveStartFailure marks a StartPayment error that is known to have
// happened before the gateway could create or settle anything remotely (for
// example, missing local configuration). Unmarked errors are intentionally
// treated as ambiguous: a timeout or lost response may hide a successful
// remote operation, so Commerce must not release stock and expose the cart for
// a second charge.
func DefinitiveStartFailure(err error) error {
	if err == nil || IsDefinitiveStartFailure(err) {
		return err
	}
	return definitiveStartFailureError{cause: err}
}

// IsDefinitiveStartFailure reports whether an error carries the optional
// definitive-start marker, including through ordinary %w wrapping.
func IsDefinitiveStartFailure(err error) bool {
	var marked interface{ definitivePaymentStartFailure() }
	return errors.As(err, &marked)
}

type definitiveStartFailureError struct{ cause error }

func (e definitiveStartFailureError) Error() string                { return e.cause.Error() }
func (e definitiveStartFailureError) Unwrap() error                { return e.cause }
func (definitiveStartFailureError) definitivePaymentStartFailure() {}

// PaymentRequest is the input to StartPayment.
type PaymentRequest struct {
	OrderRef       string
	Amount         Money
	Customer       Address
	ReturnURL      string // where the PSP returns the buyer on success
	CancelURL      string
	IdempotencyKey string // stable key for retry-safe remote payment creation
}

// PaymentAction is a sealed sum of the possible next steps after StartPayment.
// The unexported marker keeps the set closed to the four types below so commerce
// can render them exhaustively.
type PaymentAction interface{ isPaymentAction() }

// RedirectAction sends the buyer to a hosted payment page (PayPal, etc.).
type RedirectAction struct{ URL string }

// DisplayAction shows payment instructions and puts the order in an
// awaiting-payment state — crypto (address/QR/amount/chain/expiry) or offline
// bank transfer (account details). commerce renders Rows generically without
// interpreting them.
type DisplayAction struct {
	Title     string
	Rows      []KV
	QR        string     // optional data to encode as a QR image (e.g. a payment URI)
	ExpiresAt *time.Time // optional invoice expiry
}

// InlineAction hands the storefront opaque client data to render an inline,
// tokenized payment widget (no PAN ever touches GoPress).
type InlineAction struct{ ClientData map[string]any }

// CompletedAction means payment settled synchronously (rare, e.g. store credit).
type CompletedAction struct{}

func (RedirectAction) isPaymentAction()  {}
func (DisplayAction) isPaymentAction()   {}
func (InlineAction) isPaymentAction()    {}
func (CompletedAction) isPaymentAction() {}

// SettleStatus is the outcome a gateway reports for a payment.
type SettleStatus string

const (
	SettlePaid      SettleStatus = "paid"
	SettleUnderpaid SettleStatus = "underpaid"
	SettleOverpaid  SettleStatus = "overpaid"
	SettleExpired   SettleStatus = "expired"
	SettleFailed    SettleStatus = "failed"
	SettleRefunded  SettleStatus = "refunded"
)

// SettleRequest is how a gateway tells commerce a payment's state changed — by
// whatever internal means it determined that (webhook, background chain poll,
// manual admin confirmation). commerce advances the order state machine
// idempotently keyed by IdempotencyKey.
type SettleRequest struct {
	OrderRef       string
	Gateway        string
	TxnID          string
	Amount         Money
	Status         SettleStatus
	IdempotencyKey string
	Raw            map[string]any
}

// PaymentSettler is implemented BY the commerce engine and consumed by gateways.
// Retrieve it via GetSettler(bus).
type PaymentSettler interface {
	Settle(ctx context.Context, req SettleRequest) error
}

// PendingPayment is one not-yet-confirmed payment handed to a Reconciler.
// Context carries the opaque per-payment data the gateway stored at
// StartPayment (e.g. a crypto deposit address); commerce treats it as opaque.
type PendingPayment struct {
	OrderRef  string
	Amount    Money
	Context   map[string]any
	CreatedAt time.Time
	ExpiresAt *time.Time
}

// Reconciler is an OPTIONAL gateway capability for pull-based confirmation
// (crypto/USDT chain watching). commerce's scheduler periodically calls it with
// the gateway's pending payments; the gateway checks its source of truth and
// returns settlements to apply. Gateways using push (webhooks) or manual
// confirmation need not implement this.
type Reconciler interface {
	ReconcilePending(ctx context.Context, pending []PendingPayment) []SettleRequest
}

// RefundRequest is the input to PaymentGateway.Refund.
type RefundRequest struct {
	OrderRef       string
	PaymentID      string
	Amount         Money
	Reason         string
	IdempotencyKey string // stable key for retry-safe remote refunds
}

// RefundResult is returned by IdempotentRefunder when a gateway can expose the
// remote refund transaction. Commerce persists TransactionID so a later webhook
// for the same refund can be correlated instead of recorded twice.
type RefundResult struct {
	TransactionID string
	Raw           map[string]any
}

// IdempotentRefunder is an OPTIONAL, backwards-compatible refund capability.
// PaymentGateway.Refund remains the baseline contract for existing gateways;
// Commerce prefers RefundWithResult when present.
type IdempotentRefunder interface {
	RefundWithResult(c *gin.Context, req RefundRequest) (RefundResult, error)
}
