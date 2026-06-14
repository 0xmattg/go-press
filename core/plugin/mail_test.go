package plugin

import (
	"context"
	"testing"

	"go-press/core/mail"
)

type fakeMailApp struct {
	sender mail.Sender
}

func (a fakeMailApp) MailSender() mail.Sender {
	return a.sender
}

type fakeSender struct{}

func (fakeSender) Send(context.Context, mail.Message) error {
	return nil
}

func TestMailSenderReturnsProviderSender(t *testing.T) {
	t.Parallel()

	sender := fakeSender{}
	got := MailSender(fakeMailApp{sender: sender})
	if got == nil {
		t.Fatal("MailSender returned nil")
	}
}

func TestMailSenderReturnsNilWithoutCapability(t *testing.T) {
	t.Parallel()

	if got := MailSender(struct{}{}); got != nil {
		t.Fatalf("MailSender = %#v, want nil", got)
	}
}
