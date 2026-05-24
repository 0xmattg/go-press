package config

import (
	"os"
	"path/filepath"
	"testing"
)

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
}
