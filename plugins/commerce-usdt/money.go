package commerceusdt

import (
	"math/big"
	"strings"
)

// rateScale is the fixed-point scale for the USD->token rate (6 decimal places).
const rateScale = 1_000_000

// usdToToken converts an order total in USD minor units (cents) into the token's
// raw minor units, applying a USD->token rate (fixed-point, scaled by rateScale)
// and the token's decimals. All integer math (no float); rounds half up.
//
//	token = round( cents/100 * rate * 10^decimals )
func usdToToken(usdMinor int64, rateScaled int64, decimals int) *big.Int {
	num := big.NewInt(usdMinor)
	num.Mul(num, pow10(decimals))        // * 10^decimals
	num.Mul(num, big.NewInt(rateScaled)) // * rate (scaled)
	den := new(big.Int).Mul(big.NewInt(100), big.NewInt(rateScale))
	return divRoundHalfUp(num, den)
}

func pow10(n int) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil)
}

// divRoundHalfUp returns round(num/den) for non-negative num and positive den.
func divRoundHalfUp(num, den *big.Int) *big.Int {
	half := new(big.Int).Rsh(den, 1) // den/2
	n := new(big.Int).Add(num, half)
	return n.Quo(n, den)
}

// formatToken renders raw token minor units to an exact decimal string with the
// token's decimals (e.g. 10_000000 @6 -> "10.000000"). Full precision is kept so
// the buyer sees the exact amount to send.
func formatToken(amount *big.Int, decimals int) string {
	if amount == nil {
		amount = big.NewInt(0)
	}
	neg := amount.Sign() < 0
	abs := new(big.Int).Abs(amount)
	whole := new(big.Int)
	frac := new(big.Int)
	whole.QuoRem(abs, pow10(decimals), frac)
	s := whole.String()
	if decimals > 0 {
		fs := frac.String()
		if len(fs) < decimals {
			fs = strings.Repeat("0", decimals-len(fs)) + fs
		}
		s += "." + fs
	}
	if neg {
		s = "-" + s
	}
	return s
}

// parseRateScaled parses a decimal rate string ("1.00", "0.999") into fixed-point
// scaled by rateScale. Blank/invalid or non-positive yields 1.0 (par).
func parseRateScaled(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return rateScale
	}
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	whole, frac, _ := strings.Cut(s, ".")
	w, okW := parseUint(whole, true)
	if !okW {
		return rateScale
	}
	// Take up to 6 fractional digits, right-padded.
	if len(frac) > 6 {
		frac = frac[:6]
	}
	frac = frac + strings.Repeat("0", 6-len(frac))
	f, okF := parseUint(frac, true)
	if !okF {
		return rateScale
	}
	scaled := w*rateScale + f
	if neg || scaled <= 0 {
		return rateScale
	}
	return scaled
}

// parseUint parses a decimal integer string; allowEmpty treats "" as 0.
func parseUint(s string, allowEmpty bool) (int64, bool) {
	if s == "" {
		return 0, allowEmpty
	}
	var v int64
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
		v = v*10 + int64(r-'0')
	}
	return v, true
}

// formatRate renders a fixed-point USD->token rate back to a trimmed decimal
// string ("1000000" -> "1", "999000" -> "0.999") for the settings form.
func formatRate(scaled int64) string {
	s := formatToken(big.NewInt(scaled), 6)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	if s == "" {
		s = "0"
	}
	return s
}

// withinTolerance reports whether received >= expected - tolerance (all raw
// token minor units, non-negative).
func withinTolerance(received, expected, tolerance *big.Int) bool {
	min := new(big.Int).Sub(expected, tolerance)
	if min.Sign() < 0 {
		min.SetInt64(0)
	}
	return received.Cmp(min) >= 0
}
