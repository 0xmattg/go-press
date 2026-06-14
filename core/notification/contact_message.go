package notification

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go-press/core/content"
	"go-press/core/hook"
	"go-press/core/mail"
	"go-press/core/option"
	"go-press/core/worker"
	"go-press/pkg/logger"
)

const (
	OptionContactMessageEnabled    = "mail.notify_contact_message"
	OptionContactMessageRecipients = "mail.notify_contact_message_recipients"
)

// ContactMessageNotifier sends default email notifications for the core
// contact_message content type. It listens to generic content events so themes
// only save content and never depend on mail delivery.
type ContactMessageNotifier struct {
	hooks    *hook.Bus
	options  *option.Store
	mailer   mail.Sender
	workers  *worker.Pool
	siteName string
	siteURL  string
}

func NewContactMessageNotifier(hooks *hook.Bus, options *option.Store, mailer mail.Sender, workers *worker.Pool, siteName, siteURL string) *ContactMessageNotifier {
	return &ContactMessageNotifier{
		hooks:    hooks,
		options:  options,
		mailer:   mailer,
		workers:  workers,
		siteName: strings.TrimSpace(siteName),
		siteURL:  strings.TrimRight(strings.TrimSpace(siteURL), "/"),
	}
}

func (n *ContactMessageNotifier) Register() {
	if n == nil || n.hooks == nil {
		return
	}
	n.hooks.AddAction(hook.ContentCreated, func(ctx context.Context, args ...interface{}) {
		n.handleContentCreated(ctx, args...)
	}, 10)
}

func (n *ContactMessageNotifier) handleContentCreated(ctx context.Context, args ...interface{}) {
	if !n.enabled() || n.mailer == nil || len(args) == 0 {
		return
	}
	item, ok := args[0].(*content.Content)
	if !ok || item == nil || item.Type != "contact_message" {
		return
	}
	meta := map[string]string{}
	if len(args) > 1 {
		if m, ok := args[1].(map[string]string); ok {
			meta = m
		}
	}
	msg := n.buildMessage(item, meta)
	if len(msg.To) == 0 {
		return
	}
	task := func(taskCtx context.Context) error {
		if err := n.mailer.Send(taskCtx, msg); err != nil {
			if errors.Is(err, mail.ErrDisabled) {
				return nil
			}
			logger.Error("contact message notification failed", "content_id", item.ID, "error", err)
			return nil
		}
		return nil
	}
	if n.workers != nil {
		n.workers.SubmitFunc("mail:contact_message", task)
		return
	}
	_ = task(ctx)
}

func (n *ContactMessageNotifier) buildMessage(item *content.Content, meta map[string]string) mail.Message {
	recipients := splitRecipients(n.optionDefault(OptionContactMessageRecipients, n.optionDefault("admin_email", "")))
	if n.hooks != nil {
		if filtered := n.hooks.ApplyFilter(hook.NotificationContactMessageRecipients, recipients, item, meta); filtered != nil {
			if next, ok := filtered.([]string); ok {
				recipients = next
			}
		}
	}
	subject := fmt.Sprintf("[%s] New contact message from %s", n.displaySiteName(), item.Title)
	if n.hooks != nil {
		if filtered := n.hooks.ApplyFilter(hook.NotificationContactMessageSubject, subject, item, meta); filtered != nil {
			if next, ok := filtered.(string); ok {
				subject = next
			}
		}
	}
	body := n.defaultBody(item, meta)
	if n.hooks != nil {
		if filtered := n.hooks.ApplyFilter(hook.NotificationContactMessageBody, body, item, meta); filtered != nil {
			if next, ok := filtered.(string); ok {
				body = next
			}
		}
	}
	return mail.Message{
		To:      recipients,
		ReplyTo: meta["email"],
		Subject: subject,
		Text:    body,
	}
}

func (n *ContactMessageNotifier) defaultBody(item *content.Content, meta map[string]string) string {
	var b strings.Builder
	b.WriteString("A new contact message was submitted.\n\n")
	b.WriteString("Name: ")
	b.WriteString(item.Title)
	b.WriteString("\n")
	if email := strings.TrimSpace(meta["email"]); email != "" {
		b.WriteString("Email: ")
		b.WriteString(email)
		b.WriteString("\n")
	}
	if phone := strings.TrimSpace(meta["phone"]); phone != "" {
		b.WriteString("Phone: ")
		b.WriteString(phone)
		b.WriteString("\n")
	}
	b.WriteString("\nMessage:\n")
	b.WriteString(item.Content)
	if n.siteURL != "" {
		b.WriteString("\n\nAdmin: ")
		b.WriteString(n.siteURL)
		b.WriteString("/admin/")
	}
	return b.String()
}

func (n *ContactMessageNotifier) enabled() bool {
	return n.optionDefault(OptionContactMessageEnabled, "1") == "1"
}

func (n *ContactMessageNotifier) optionDefault(key, fallback string) string {
	if n.options == nil {
		return fallback
	}
	value := strings.TrimSpace(n.options.Get(key))
	if value == "" {
		return fallback
	}
	return value
}

func (n *ContactMessageNotifier) displaySiteName() string {
	if n.siteName != "" {
		return n.siteName
	}
	return "GoPress"
}

func splitRecipients(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r'
	})
	recipients := make([]string, 0, len(parts))
	seen := make(map[string]bool, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || seen[strings.ToLower(part)] {
			continue
		}
		seen[strings.ToLower(part)] = true
		recipients = append(recipients, part)
	}
	return recipients
}
