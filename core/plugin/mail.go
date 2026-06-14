package plugin

import "go-press/core/mail"

// MailProvider is an optional App capability for plugins that need to send
// notification emails through core's configured mail service.
type MailProvider interface {
	MailSender() mail.Sender
}

// MailSender returns core's mail sender from an App when available.
//
// Plugins should prefer this helper over type asserting *core.Engine directly;
// it keeps the plugin coupled only to the public mail capability.
func MailSender(app App) mail.Sender {
	provider, ok := app.(MailProvider)
	if !ok || provider == nil {
		return nil
	}
	return provider.MailSender()
}
