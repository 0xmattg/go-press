package commerce

import (
	"strings"

	"go-press/core/hook"

	"github.com/gin-gonic/gin"
)

// Hook names on the core bus through which satellite plugins register their
// commerce capabilities. Satellites depend on this package only; the commerce
// engine reads these lazily so registration order never matters.
const (
	HookPaymentGateways = "commerce.payment.gateways" // filter value: []PaymentGateway
	HookShippingMethods = "commerce.shipping.methods" // filter value: []ShippingMethod
	hookSettler         = "commerce.payment.settler"  // filter value: PaymentSettler (engine-provided)
)

// RegisterPaymentGateway is called by a satellite gateway plugin (in Activate)
// to add itself to the available set. Remove the returned handle in Deactivate.
func RegisterPaymentGateway(bus *hook.Bus, g PaymentGateway) hook.Handle {
	return bus.AddFilter(HookPaymentGateways, func(value interface{}, _ ...interface{}) interface{} {
		list, _ := value.([]PaymentGateway)
		return append(list, g)
	}, 10)
}

// PaymentGateways returns all currently registered gateways. The commerce engine
// calls this at checkout, so the result reflects whatever satellites are active.
func PaymentGateways(bus *hook.Bus) []PaymentGateway {
	v := bus.ApplyFilter(HookPaymentGateways, []PaymentGateway(nil))
	list, _ := v.([]PaymentGateway)
	return list
}

// GatewayAvailable evaluates the optional runtime availability contract. A
// plain PaymentGateway remains available for backwards compatibility.
func GatewayAvailable(c *gin.Context, g PaymentGateway) bool {
	if g == nil {
		return false
	}
	available, ok := g.(GatewayAvailability)
	return !ok || available.Available(c)
}

// GatewaySupportsCurrency evaluates the optional currency capability. An empty
// currency means the caller has no order context yet, so it preserves the
// historical availability-only behavior.
func GatewaySupportsCurrency(g PaymentGateway, currency string) bool {
	if g == nil {
		return false
	}
	currency = strings.TrimSpace(currency)
	if currency == "" {
		return true
	}
	support, ok := g.(GatewayCurrencySupport)
	return !ok || support.SupportsCurrency(currency)
}

// AvailablePaymentGateways returns only gateways that can currently start a
// checkout payment. Callers must still re-evaluate availability on submission,
// because configuration may change after the checkout page was rendered.
func AvailablePaymentGateways(c *gin.Context, bus *hook.Bus) []PaymentGateway {
	return AvailablePaymentGatewaysForCurrency(c, bus, "")
}

// AvailablePaymentGatewaysForCurrency returns gateways that are both runtime
// available and compatible with the order currency.
func AvailablePaymentGatewaysForCurrency(c *gin.Context, bus *hook.Bus, currency string) []PaymentGateway {
	registered := PaymentGateways(bus)
	out := make([]PaymentGateway, 0, len(registered))
	for _, g := range registered {
		if GatewayAvailable(c, g) && GatewaySupportsCurrency(g, currency) {
			out = append(out, g)
		}
	}
	return out
}

// RegisterShippingMethod adds a shipping method to the available set.
func RegisterShippingMethod(bus *hook.Bus, m ShippingMethod) hook.Handle {
	return bus.AddFilter(HookShippingMethods, func(value interface{}, _ ...interface{}) interface{} {
		list, _ := value.([]ShippingMethod)
		return append(list, m)
	}, 10)
}

// ShippingMethods returns all currently registered shipping methods.
func ShippingMethods(bus *hook.Bus) []ShippingMethod {
	v := bus.ApplyFilter(HookShippingMethods, []ShippingMethod(nil))
	list, _ := v.([]ShippingMethod)
	return list
}

// SetSettler is called by the commerce engine to publish its PaymentSettler so
// gateways can confirm payments (push/manual paths). Remove the handle when the
// engine deactivates.
func SetSettler(bus *hook.Bus, s PaymentSettler) hook.Handle {
	return bus.AddFilter(hookSettler, func(_ interface{}, _ ...interface{}) interface{} { return s }, 10)
}

// GetSettler returns the commerce-provided settler, or nil when commerce is not
// active. Gateways use it from their webhook / manual-confirm paths.
func GetSettler(bus *hook.Bus) PaymentSettler {
	v := bus.ApplyFilter(hookSettler, PaymentSettler(nil))
	s, _ := v.(PaymentSettler)
	return s
}
