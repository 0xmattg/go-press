package mail

import (
	stdmail "net/mail"
	"strings"
	"testing"

	"go-press/config"
)

func TestServiceSetConfigNormalizesDefaults(t *testing.T) {
	t.Parallel()

	svc := NewService(config.MailConfig{Encryption: "tls"}, nil)
	cfg := svc.Config()
	if cfg.Encryption != EncryptionSSL {
		t.Fatalf("encryption = %q, want %q", cfg.Encryption, EncryptionSSL)
	}
	if cfg.Driver != DriverGoMail {
		t.Fatalf("driver = %q, want %q", cfg.Driver, DriverGoMail)
	}
	if cfg.Port != 465 {
		t.Fatalf("port = %d, want 465", cfg.Port)
	}
	if cfg.TimeoutSeconds != 10 {
		t.Fatalf("timeout = %d, want 10", cfg.TimeoutSeconds)
	}
}

func TestNormalizeDriverKeepsStdlibFallback(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"stdlib", "net-smtp", "net/smtp", "standard"} {
		if got := NormalizeDriver(value); got != DriverStdlib {
			t.Fatalf("NormalizeDriver(%q) = %q, want %q", value, got, DriverStdlib)
		}
	}
	if got := NormalizeDriver(""); got != DriverGoMail {
		t.Fatalf("NormalizeDriver(\"\") = %q, want %q", got, DriverGoMail)
	}
}

func TestRenderMessageUsesConfiguredHeaders(t *testing.T) {
	t.Parallel()

	svc := NewService(config.MailConfig{}, nil)
	msg := Message{
		From:    mustAddress(t, "GoPress <no-reply@example.com>"),
		To:      []string{"admin@example.com"},
		ReplyTo: "sender@example.com",
		Subject: "测试邮件",
		Text:    "hello",
	}
	raw := svc.renderMessage(msg)
	for _, want := range []string{
		"From: \"GoPress\" <no-reply@example.com>",
		"To: admin@example.com",
		"Reply-To: sender@example.com",
		"Content-Type: text/plain; charset=\"UTF-8\"",
		"\r\n\r\nhello",
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("rendered message missing %q:\n%s", want, raw)
		}
	}
	if strings.Contains(raw, "测试邮件") {
		t.Fatalf("subject should be RFC 2047 encoded, got raw message:\n%s", raw)
	}
}

func mustAddress(t *testing.T, value string) stdmail.Address {
	t.Helper()
	parsed, err := stdmail.ParseAddress(value)
	if err != nil {
		t.Fatal(err)
	}
	return *parsed
}
