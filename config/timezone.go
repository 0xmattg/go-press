package config

import (
	"strings"
	"time"
)

const LocalTimezoneName = "Local"

// DefaultTimezoneName returns a stable default for new installs. Existing
// sites with no configured timezone still fall back to time.Local at runtime.
func DefaultTimezoneName() string {
	if time.Local != nil {
		if name := strings.TrimSpace(time.Local.String()); name != "" {
			return name
		}
	}
	return "UTC"
}

func LoadTimezone(name string) (*time.Location, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return fallbackLocation(), false
	}
	if name == LocalTimezoneName {
		return fallbackLocation(), true
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return fallbackLocation(), false
	}
	return loc, true
}

func IsValidTimezone(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	_, ok := LoadTimezone(name)
	return ok
}

func fallbackLocation() *time.Location {
	if time.Local != nil {
		return time.Local
	}
	return time.UTC
}
