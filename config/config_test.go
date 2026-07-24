package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestJWTSecretInsecure(t *testing.T) {
	t.Parallel()
	cases := []struct {
		secret string
		want   bool
	}{
		{"", true},
		{"   ", true},
		{LegacyPlaceholderJWTSecret, true},
		{"a-real-unique-random-secret-value", false},
	}
	for _, tc := range cases {
		got := CMSConfig{JWTSecret: tc.secret}.JWTSecretInsecure()
		if got != tc.want {
			t.Errorf("JWTSecretInsecure(%q) = %v, want %v", tc.secret, got, tc.want)
		}
	}
}

func TestSaveWritesConfigWithSecurePermissions(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "sites", "example", "config.toml")
	cfg := &Config{
		Site: SiteConfig{
			Name:     "Example",
			URL:      "http://localhost:8080",
			Language: "zh-CN",
			Timezone: "Asia/Shanghai",
			Theme:    "modern-company",
		},
		Server: ServerConfig{
			Host: "0.0.0.0",
			Port: 8080,
			Mode: "debug",
		},
		PG: PGConfig{
			User:     "postgres",
			Password: "secret",
			Hostname: "localhost",
			Port:     "5432",
			Database: "example",
			Schema:   "public",
		},
		CMS: CMSConfig{
			JWTSecret:       "jwt-secret",
			JWTExpireHours:  24,
			UploadDir:       "uploads",
			UploadMaxSizeMB: 10,
		},
		Mail: MailConfig{
			Driver:     "go-mail",
			Enabled:    true,
			Host:       "smtp.example.com",
			Port:       587,
			Encryption: "starttls",
			Username:   "smtp-user",
			MailKey:    "smtp-secret",
			FromEmail:  "no-reply@example.com",
		},
		Install: InstallConfig{
			Completed:   true,
			InstalledAt: "2026-04-09T00:00:00Z",
		},
	}

	if err := Save(configPath, cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("os.Stat() error = %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("config mode = %o, want 600", mode)
	}

	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.PG.Password != cfg.PG.Password {
		t.Fatalf("loaded password = %q, want %q", loaded.PG.Password, cfg.PG.Password)
	}
	if !loaded.Install.Completed {
		t.Fatalf("loaded install completion = false, want true")
	}
	if loaded.Site.Timezone != cfg.Site.Timezone {
		t.Fatalf("loaded timezone = %q, want %q", loaded.Site.Timezone, cfg.Site.Timezone)
	}
	if loaded.Mail.MailKey != cfg.Mail.MailKey {
		t.Fatalf("loaded mail key = %q, want %q", loaded.Mail.MailKey, cfg.Mail.MailKey)
	}
	if loaded.Mail.Driver != cfg.Mail.Driver {
		t.Fatalf("loaded mail driver = %q, want %q", loaded.Mail.Driver, cfg.Mail.Driver)
	}
}
