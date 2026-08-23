package plugin

import (
	"context"
	"errors"
	"testing"

	"github.com/0xmattg/go-press/core/mail"
)

type fakeMailApp struct {
	sender mail.Sender
}

type guardedTestPlugin struct{ err error }

func (p guardedTestPlugin) Name() string        { return "guarded" }
func (p guardedTestPlugin) Version() string     { return "1.0.0" }
func (p guardedTestPlugin) Description() string { return "" }
func (p guardedTestPlugin) Activate(App)        {}
func (p guardedTestPlugin) Deactivate(App)      {}
func (p guardedTestPlugin) CanDeactivate(App) error {
	return p.err
}

func TestManagerConsultsDeactivationGuard(t *testing.T) {
	want := errors.New("owned data remains")
	manager := NewManager()
	manager.Register(guardedTestPlugin{err: want})
	if !manager.Activate("guarded", nil) {
		t.Fatal("guarded plugin did not activate")
	}
	if err := manager.CanDeactivate("guarded", nil); !errors.Is(err, want) {
		t.Fatalf("CanDeactivate error = %v, want %v", err, want)
	}
	if !manager.IsActive("guarded") {
		t.Fatal("guard check changed active state")
	}
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
