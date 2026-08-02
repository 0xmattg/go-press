package commerceusdt

import (
	"math/big"
	"testing"
)

func TestUsdToToken(t *testing.T) {
	// $10.00 (1000 cents) at rate 1.0, 6 decimals -> 10.000000 USDT = 10_000000.
	if got := usdToToken(1000, rateScale, 6); got.String() != "10000000" {
		t.Errorf("usdToToken 10usd@6 = %s, want 10000000", got)
	}
	// Same at 18 decimals (BSC USDT) -> 10 * 1e18.
	want18, _ := new(big.Int).SetString("10000000000000000000", 10)
	if got := usdToToken(1000, rateScale, 18); got.Cmp(want18) != 0 {
		t.Errorf("usdToToken 10usd@18 = %s, want %s", got, want18)
	}
	// $19.99 at rate 1.0, 6dp -> 19.990000.
	if got := usdToToken(1999, rateScale, 6); got.String() != "19990000" {
		t.Errorf("usdToToken 19.99 = %s, want 19990000", got)
	}
	// Rate 0.999 applied: 10usd -> 9.99 USDT = 9990000 (6dp).
	if got := usdToToken(1000, parseRateScaled("0.999"), 6); got.String() != "9990000" {
		t.Errorf("usdToToken 10usd@0.999 = %s, want 9990000", got)
	}
}

func TestFormatToken(t *testing.T) {
	cases := map[string]struct {
		amt string
		dec int
		out string
	}{
		"ten":   {"10000000", 6, "10.000000"},
		"cents": {"19990000", 6, "19.990000"},
		"sub":   {"50000", 6, "0.050000"},
		"zero":  {"0", 6, "0.000000"},
		"bsc":   {"10000000000000000000", 18, "10.000000000000000000"},
	}
	for name, c := range cases {
		amt, _ := new(big.Int).SetString(c.amt, 10)
		if got := formatToken(amt, c.dec); got != c.out {
			t.Errorf("%s: formatToken(%s,%d) = %q, want %q", name, c.amt, c.dec, got, c.out)
		}
	}
}

func TestParseRateScaled(t *testing.T) {
	cases := map[string]int64{
		"1.00": 1_000_000, "1": 1_000_000, "": 1_000_000,
		"0.999": 999_000, "1.5": 1_500_000, "0": 1_000_000, // non-positive -> par
		"-2": 1_000_000, "1.234567": 1_234_567,
	}
	for in, want := range cases {
		if got := parseRateScaled(in); got != want {
			t.Errorf("parseRateScaled(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestWithinTolerance(t *testing.T) {
	exp := big.NewInt(1000)
	if !withinTolerance(big.NewInt(1000), exp, big.NewInt(0)) {
		t.Error("exact should be within tolerance")
	}
	if !withinTolerance(big.NewInt(1200), exp, big.NewInt(0)) {
		t.Error("overpay should be within tolerance")
	}
	if withinTolerance(big.NewInt(999), exp, big.NewInt(0)) {
		t.Error("underpay beyond tolerance should fail")
	}
	if !withinTolerance(big.NewInt(998), exp, big.NewInt(2)) {
		t.Error("underpay within dust tolerance should pass")
	}
}

func TestFormatRate(t *testing.T) {
	if got := formatRate(1_000_000); got != "1" {
		t.Errorf("formatRate(1e6) = %q, want 1", got)
	}
	if got := formatRate(999_000); got != "0.999" {
		t.Errorf("formatRate(999000) = %q, want 0.999", got)
	}
}
