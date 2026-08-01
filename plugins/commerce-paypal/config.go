package commercepaypal

import "strings"

// Option keys (namespaced plugin_<slug>_*, matching the admin settings pipeline).
const (
	optEnabled   = "plugin_commerce-paypal_enabled"
	optClientID  = "plugin_commerce-paypal_client_id"
	optSecret    = "plugin_commerce-paypal_client_secret"
	optSandbox   = "plugin_commerce-paypal_sandbox"
	optWebhookID = "plugin_commerce-paypal_webhook_id"
)

// config is the resolved PayPal gateway configuration from stored options.
type config struct {
	Enabled   bool
	ClientID  string
	Secret    string
	Sandbox   bool
	WebhookID string
}

func (p *Plugin) loadConfig() config {
	c := config{Sandbox: true}
	if p == nil || p.options == nil {
		return c
	}
	c.Enabled = p.options.GetDefault(optEnabled, "0") == "1"
	c.ClientID = strings.TrimSpace(p.options.Get(optClientID))
	c.Secret = strings.TrimSpace(p.options.Get(optSecret))
	c.Sandbox = p.options.GetDefault(optSandbox, "1") == "1"
	c.WebhookID = strings.TrimSpace(p.options.Get(optWebhookID))
	return c
}

// ready reports whether checkout can start a PayPal payment.
func (c config) ready() bool {
	return c.Enabled && c.ClientID != "" && c.Secret != ""
}

// apiBase is the PayPal REST host for the selected environment.
func (c config) apiBase() string {
	if c.Sandbox {
		return "https://api-m.sandbox.paypal.com"
	}
	return "https://api-m.paypal.com"
}
