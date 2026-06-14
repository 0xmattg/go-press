package admin

import (
	"path/filepath"
	"testing"

	"go-press/config"
	coreMail "go-press/core/mail"
)

func TestUpdateMailSettingsPreservesAndClearsMailKey(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config.toml")
	cfg := &config.Config{
		Mail: config.MailConfig{
			Enabled:   true,
			Host:      "smtp.old.example",
			Port:      587,
			MailKey:   "existing-secret",
			FromEmail: "old@example.com",
		},
	}
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatal(err)
	}
	mailer := coreMail.NewService(cfg.Mail, nil)
	svc := &Service{
		config:     cfg,
		configPath: configPath,
		mailer:     mailer,
	}

	err := svc.UpdateMailSettings(map[string]string{
		"driver":          "go-mail",
		"enabled":         "1",
		"host":            "smtp.example.com",
		"port":            "587",
		"encryption":      "starttls",
		"username":        "user",
		"from_email":      "no-reply@example.com",
		"timeout_seconds": "15",
	})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Mail.MailKey != "existing-secret" {
		t.Fatalf("mail key = %q, want preserved secret", loaded.Mail.MailKey)
	}
	if loaded.Mail.Host != "smtp.example.com" || loaded.Mail.TimeoutSeconds != 15 {
		t.Fatalf("mail config was not saved correctly: %#v", loaded.Mail)
	}
	if loaded.Mail.Driver != coreMail.DriverGoMail {
		t.Fatalf("mail driver = %q, want %q", loaded.Mail.Driver, coreMail.DriverGoMail)
	}

	err = svc.UpdateMailSettings(map[string]string{
		"driver":         "stdlib",
		"enabled":        "0",
		"host":           "smtp.example.com",
		"port":           "587",
		"encryption":     "starttls",
		"from_email":     "no-reply@example.com",
		"clear_mail_key": "1",
	})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err = config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Mail.MailKey != "" {
		t.Fatalf("mail key = %q, want cleared", loaded.Mail.MailKey)
	}
	if loaded.Mail.Driver != coreMail.DriverStdlib {
		t.Fatalf("mail driver = %q, want %q", loaded.Mail.Driver, coreMail.DriverStdlib)
	}
}
