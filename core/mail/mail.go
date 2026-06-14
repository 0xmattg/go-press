package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	stdmail "net/mail"
	"net/smtp"
	"strings"
	"sync"
	"time"

	"go-press/config"
	"go-press/core/hook"

	gomail "github.com/wneessen/go-mail"
)

const (
	EncryptionNone     = "none"
	EncryptionStartTLS = "starttls"
	EncryptionSSL      = "ssl"

	DriverGoMail = "go-mail"
	DriverStdlib = "stdlib"
)

var (
	ErrDisabled      = errors.New("mail delivery is disabled")
	ErrNotConfigured = errors.New("mail delivery is not configured")
)

// Message is the framework-level email object passed through filters and then
// delivered by a transport.
type Message struct {
	From    stdmail.Address
	To      []string
	ReplyTo string
	Subject string
	Text    string
	HTML    string
	Headers map[string]string
}

// Sender is the narrow mail capability exposed to themes and plugins.
//
// Implementations own configuration, hooks, and delivery drivers. Callers only
// need to build a Message and let core decide whether and how to deliver it.
type Sender interface {
	Send(ctx context.Context, msg Message) error
}

// Service sends mail through the configured SMTP transport.
type Service struct {
	mu     sync.RWMutex
	cfg    config.MailConfig
	hooks  *hook.Bus
	now    func() time.Time
	dialer func(ctx context.Context, network, address string) (net.Conn, error)
}

var _ Sender = (*Service)(nil)

// NewService creates a mail service from site config.
func NewService(cfg config.MailConfig, hooks *hook.Bus) *Service {
	s := &Service{
		hooks: hooks,
		now:   time.Now,
	}
	s.SetConfig(cfg)
	return s
}

// Config returns a copy of the current mail config.
func (s *Service) Config() config.MailConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// SetConfig replaces the active mail config.
func (s *Service) SetConfig(cfg config.MailConfig) {
	cfg.Driver = NormalizeDriver(cfg.Driver)
	cfg.Encryption = normalizeEncryption(cfg.Encryption)
	if cfg.Port == 0 {
		if cfg.Encryption == EncryptionSSL {
			cfg.Port = 465
		} else {
			cfg.Port = 587
		}
	}
	if cfg.TimeoutSeconds <= 0 {
		cfg.TimeoutSeconds = 10
	}
	s.mu.Lock()
	s.cfg = cfg
	s.mu.Unlock()
}

// Send applies mail hooks and delivers the message if mail is enabled.
func (s *Service) Send(ctx context.Context, msg Message) error {
	if ctx == nil {
		ctx = context.Background()
	}
	cfg := s.Config()
	if !cfg.Enabled {
		return ErrDisabled
	}
	if err := validateConfig(cfg); err != nil {
		return err
	}
	msg = s.prepareMessage(cfg, msg)
	if s.hooks != nil {
		if filtered := s.hooks.ApplyFilter(hook.MailMessage, msg); filtered != nil {
			if next, ok := filtered.(Message); ok {
				msg = next
			}
		}
		s.hooks.DoAction(ctx, hook.MailBeforeSend, msg)
	}
	err := s.sendSMTP(ctx, cfg, msg)
	if s.hooks != nil {
		if err != nil {
			s.hooks.DoAction(ctx, hook.MailFailed, msg, err)
		} else {
			s.hooks.DoAction(ctx, hook.MailSent, msg)
		}
	}
	return err
}

func (s *Service) prepareMessage(cfg config.MailConfig, msg Message) Message {
	if msg.From.Address == "" {
		msg.From = stdmail.Address{Name: cfg.FromName, Address: cfg.FromEmail}
	}
	if msg.ReplyTo == "" {
		msg.ReplyTo = cfg.ReplyTo
	}
	return msg
}

func (s *Service) sendSMTP(ctx context.Context, cfg config.MailConfig, msg Message) error {
	if cfg.Driver == DriverStdlib {
		return s.sendSMTPStdlib(ctx, cfg, msg)
	}
	return s.sendSMTPGoMail(ctx, cfg, msg)
}

func (s *Service) sendSMTPGoMail(ctx context.Context, cfg config.MailConfig, msg Message) error {
	message, err := s.buildGoMailMessage(msg)
	if err != nil {
		return err
	}
	opts := []gomail.Option{
		gomail.WithPort(cfg.Port),
		gomail.WithTimeout(time.Duration(cfg.TimeoutSeconds) * time.Second),
	}
	switch cfg.Encryption {
	case EncryptionSSL:
		opts = append(opts, gomail.WithSSL())
	case EncryptionNone:
		opts = append(opts, gomail.WithTLSPortPolicy(gomail.NoTLS))
	default:
		opts = append(opts, gomail.WithTLSPortPolicy(gomail.TLSMandatory))
	}
	if cfg.Username != "" || cfg.MailKey != "" {
		authType := gomail.SMTPAuthPlain
		if cfg.Encryption == EncryptionNone {
			authType = gomail.SMTPAuthPlainNoEnc
		}
		opts = append(opts,
			gomail.WithSMTPAuth(authType),
			gomail.WithUsername(cfg.Username),
			gomail.WithPassword(cfg.MailKey),
		)
	}
	client, err := gomail.NewClient(cfg.Host, opts...)
	if err != nil {
		return err
	}
	return client.DialAndSendWithContext(ctx, message)
}

func (s *Service) buildGoMailMessage(msg Message) (*gomail.Msg, error) {
	message := gomail.NewMsg()
	message.FromMailAddress(&msg.From)
	if err := message.To(msg.To...); err != nil {
		return nil, err
	}
	if msg.ReplyTo != "" {
		if err := message.ReplyTo(msg.ReplyTo); err != nil {
			return nil, err
		}
	}
	if msg.Subject != "" {
		message.Subject(msg.Subject)
	}
	for key, value := range msg.Headers {
		if key != "" && value != "" {
			if isManagedHeader(key) {
				continue
			}
			message.SetGenHeader(gomail.Header(key), value)
		}
	}
	if msg.HTML != "" {
		message.SetBodyString(gomail.TypeTextHTML, msg.HTML)
		return message, nil
	}
	message.SetBodyString(gomail.TypeTextPlain, msg.Text)
	return message, nil
}

func (s *Service) sendSMTPStdlib(ctx context.Context, cfg config.MailConfig, msg Message) error {
	addr := net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", cfg.Port))
	conn, err := s.openConnection(ctx, cfg, addr)
	if err != nil {
		return err
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		return err
	}
	defer client.Close()

	if cfg.Encryption == EncryptionStartTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("smtp server does not advertise STARTTLS")
		}
		if err := client.StartTLS(&tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return err
		}
	}
	if cfg.Username != "" || cfg.MailKey != "" {
		auth := smtp.PlainAuth("", cfg.Username, cfg.MailKey, cfg.Host)
		if err := client.Auth(auth); err != nil {
			return err
		}
	}
	from := msg.From.Address
	if err := client.Mail(from); err != nil {
		return err
	}
	for _, recipient := range msg.To {
		if err := client.Rcpt(strings.TrimSpace(recipient)); err != nil {
			return err
		}
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write([]byte(s.renderMessage(msg))); err != nil {
		_ = writer.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}

func (s *Service) openConnection(ctx context.Context, cfg config.MailConfig, addr string) (net.Conn, error) {
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	dial := s.dialer
	if dial == nil {
		d := &net.Dialer{Timeout: timeout}
		dial = d.DialContext
	}
	if cfg.Encryption == EncryptionSSL {
		raw, err := dial(ctx, "tcp", addr)
		if err != nil {
			return nil, err
		}
		return tls.Client(raw, &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12}), nil
	}
	return dial(ctx, "tcp", addr)
}

func (s *Service) renderMessage(msg Message) string {
	headers := map[string]string{
		"From":         msg.From.String(),
		"To":           strings.Join(msg.To, ", "),
		"Subject":      mime.QEncoding.Encode("utf-8", msg.Subject),
		"Date":         s.now().Format(time.RFC1123Z),
		"MIME-Version": "1.0",
	}
	if msg.ReplyTo != "" {
		headers["Reply-To"] = msg.ReplyTo
	}
	for key, value := range msg.Headers {
		if key != "" && value != "" {
			if isManagedHeader(key) {
				continue
			}
			headers[key] = value
		}
	}
	body := msg.Text
	if msg.HTML != "" {
		headers["Content-Type"] = `text/html; charset="UTF-8"`
		body = msg.HTML
	} else {
		headers["Content-Type"] = `text/plain; charset="UTF-8"`
	}
	headers["Content-Transfer-Encoding"] = "8bit"

	order := []string{"From", "To", "Reply-To", "Subject", "Date", "MIME-Version", "Content-Type", "Content-Transfer-Encoding"}
	var b strings.Builder
	written := make(map[string]bool, len(order))
	for _, key := range order {
		if value := headers[key]; value != "" {
			b.WriteString(key)
			b.WriteString(": ")
			b.WriteString(value)
			b.WriteString("\r\n")
			written[key] = true
		}
	}
	for key, value := range headers {
		if !written[key] {
			b.WriteString(key)
			b.WriteString(": ")
			b.WriteString(value)
			b.WriteString("\r\n")
		}
	}
	b.WriteString("\r\n")
	b.WriteString(body)
	return b.String()
}

func isManagedHeader(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "from", "to", "reply-to", "subject", "date", "mime-version", "content-type", "content-transfer-encoding":
		return true
	default:
		return false
	}
}

func validateConfig(cfg config.MailConfig) error {
	if strings.TrimSpace(cfg.Host) == "" || cfg.Port <= 0 || strings.TrimSpace(cfg.FromEmail) == "" {
		return ErrNotConfigured
	}
	if _, err := stdmail.ParseAddress(cfg.FromEmail); err != nil {
		return fmt.Errorf("invalid from email: %w", err)
	}
	return nil
}

func normalizeEncryption(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case EncryptionNone:
		return EncryptionNone
	case EncryptionSSL, "tls":
		return EncryptionSSL
	default:
		return EncryptionStartTLS
	}
}

// NormalizeDriver returns the supported mail transport driver. The go-mail
// driver is the default; stdlib keeps the Go standard-library SMTP branch.
func NormalizeDriver(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case DriverStdlib, "net-smtp", "net/smtp", "standard":
		return DriverStdlib
	default:
		return DriverGoMail
	}
}
