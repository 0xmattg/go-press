package notification

import (
	"context"
	"reflect"
	"testing"

	"go-press/core/content"
	"go-press/core/hook"
	"go-press/core/mail"
)

func TestSplitRecipientsNormalizesAndDeduplicates(t *testing.T) {
	t.Parallel()

	got := splitRecipients(" admin@example.com, sales@example.com;ADMIN@example.com\nops@example.com ")
	want := []string{"admin@example.com", "sales@example.com", "ops@example.com"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitRecipients() = %#v, want %#v", got, want)
	}
}

type recordingSender struct {
	calls int
	last  mail.Message
	err   error
}

func (s *recordingSender) Send(_ context.Context, msg mail.Message) error {
	s.calls++
	s.last = msg
	return s.err
}

func TestContactMessageNotifierUsesMailSenderInterface(t *testing.T) {
	t.Parallel()

	sender := &recordingSender{}
	hooks := hook.New()
	hooks.AddFilter(hook.NotificationContactMessageRecipients, func(value interface{}, args ...interface{}) interface{} {
		return []string{"admin@example.com"}
	}, 10)
	n := NewContactMessageNotifier(hooks, nil, sender, nil, "Site", "https://example.com")
	n.handleContentCreated(context.Background(), &content.Content{
		Type:    "contact_message",
		Title:   "Jane",
		Content: "Hello",
	}, map[string]string{"email": "jane@example.com"})

	if sender.calls != 1 {
		t.Fatalf("sender calls = %d, want 1", sender.calls)
	}
	if sender.last.ReplyTo != "jane@example.com" {
		t.Fatalf("reply-to = %q, want submitter email", sender.last.ReplyTo)
	}
}

func TestContactMessageNotifierIgnoresDisabledMail(t *testing.T) {
	t.Parallel()

	sender := &recordingSender{err: mail.ErrDisabled}
	hooks := hook.New()
	hooks.AddFilter(hook.NotificationContactMessageRecipients, func(value interface{}, args ...interface{}) interface{} {
		return []string{"admin@example.com"}
	}, 10)
	n := NewContactMessageNotifier(hooks, nil, sender, nil, "Site", "https://example.com")
	n.handleContentCreated(context.Background(), &content.Content{
		Type:    "contact_message",
		Title:   "Jane",
		Content: "Hello",
	}, nil)

	if sender.calls != 1 {
		t.Fatalf("sender calls = %d, want 1", sender.calls)
	}
}
