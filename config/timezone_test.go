package config

import (
	"testing"
	"time"
)

func TestLoadTimezoneSupportsLocalFallback(t *testing.T) {
	prevLocal := time.Local
	loc := time.FixedZone("TestLocal", 8*60*60)
	time.Local = loc
	defer func() { time.Local = prevLocal }()

	got, ok := LoadTimezone("")
	if ok {
		t.Fatal("empty timezone should use fallback but report not explicitly configured")
	}
	if got != loc {
		t.Fatalf("empty timezone location = %v, want local", got)
	}

	got, ok = LoadTimezone(LocalTimezoneName)
	if !ok {
		t.Fatal("Local timezone should be valid")
	}
	if got != loc {
		t.Fatalf("Local timezone location = %v, want local", got)
	}
}

func TestLoadTimezoneSupportsIANAName(t *testing.T) {
	got, ok := LoadTimezone("Asia/Shanghai")
	if !ok {
		t.Fatal("Asia/Shanghai should be valid")
	}
	if got.String() != "Asia/Shanghai" {
		t.Fatalf("location = %q, want Asia/Shanghai", got.String())
	}
}
