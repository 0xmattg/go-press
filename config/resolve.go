package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ResolveConfigPath returns the config file path based on:
//  1. Explicit flag value (highest priority)
//  2. Auto-discovered first site under sites/
//  3. Default path sites/default/config.toml
//
// The second return value indicates whether the path was auto-discovered.
func ResolveConfigPath(flagValue string) (string, bool) {
	if flagValue != "" {
		return flagValue, false
	}

	if discovered := DiscoverSiteConfig(); discovered != "" {
		return discovered, true
	}

	return filepath.Join("sites", "default", "config.toml"), false
}

// LoadIfPresent loads the config file at path if it exists.
// Returns (nil, false, nil) if the file does not exist.
func LoadIfPresent(path string) (*Config, bool, error) {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}

	cfg, err := Load(path)
	if err != nil {
		return nil, true, err
	}
	return cfg, true, nil
}

// ListenAddr returns the server address string from config,
// falling back to 0.0.0.0:8080 when config is nil or fields are empty.
func ListenAddr(cfg *Config) string {
	host := "0.0.0.0"
	port := 8080

	if cfg != nil {
		if cfg.Server.Host != "" {
			host = cfg.Server.Host
		}
		if cfg.Server.Port > 0 {
			port = cfg.Server.Port
		}
	}

	return fmt.Sprintf("%s:%d", host, port)
}

// DiscoverSiteConfig scans the sites/ directory for the first valid config.toml.
func DiscoverSiteConfig() string {
	entries, err := os.ReadDir("sites")
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		candidate := filepath.Join("sites", entry.Name(), "config.toml")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}
