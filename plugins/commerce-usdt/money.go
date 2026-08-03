package commerceusdt

import (
	"errors"
	"fmt"
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

// tokenToUSD converts raw token minor units back to USD cents using the exact
// rate snapshot stored on the invoice. It rounds down so an underpayment is
// never overstated to Commerce.
func tokenToUSD(tokenMinor *big.Int, rateScaled int64, decimals int) int64 {
	if tokenMinor == nil || tokenMinor.Sign() <= 0 || rateScaled <= 0 || decimals < 0 {
		return 0
	}
	num := new(big.Int).Mul(new(big.Int).Set(tokenMinor), big.NewInt(100*rateScale))
	den := new(big.Int).Mul(pow10(decimals), big.NewInt(rateScaled))
	result := new(big.Int).Quo(num, den)
	if !result.IsInt64() {
		return int64(^uint64(0) >> 1)
	}
	return result.Int64()
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
	v, err := parseRateScaledStrict(s)
	if err != nil {
		return rateScale
	}
	return v
}

func parseRateScaledStrict(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("USDT rate is required")
	}
	if strings.HasPrefix(s, "+") || strings.HasPrefix(s, "-") || strings.Count(s, ".") > 1 {
		return 0, errors.New("USDT rate must be a positive decimal")
	}
	whole, frac, _ := strings.Cut(s, ".")
	w, okW := parseDecimalBigInt(whole, true)
	if !okW {
		return 0, errors.New("USDT rate must be a positive decimal")
	}
	if len(frac) > 6 {
		return 0, errors.New("USDT rate supports at most 6 decimal places")
	}
	frac = frac + strings.Repeat("0", 6-len(frac))
	f, okF := parseDecimalBigInt(frac, true)
	if !okF {
		return 0, errors.New("USDT rate must be a positive decimal")
	}
	scaled := new(big.Int).Mul(w, big.NewInt(rateScale))
	scaled.Add(scaled, f)
	if scaled.Sign() <= 0 || !scaled.IsInt64() {
		return 0, errors.New("USDT rate is outside the supported range")
	}
	return scaled.Int64(), nil
}

func parseDecimalBigInt(s string, allowEmpty bool) (*big.Int, bool) {
	if s == "" {
		return big.NewInt(0), allowEmpty
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return nil, false
		}
	}
	v, ok := new(big.Int).SetString(s, 10)
	return v, ok
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
	if received == nil || expected == nil || tolerance == nil || received.Sign() < 0 || expected.Sign() <= 0 || tolerance.Sign() < 0 || tolerance.Cmp(expected) >= 0 {
		return false
	}
	min := new(big.Int).Sub(expected, tolerance)
	return received.Cmp(min) >= 0
}

func validateExpectedAmount(expected, dust *big.Int) error {
	if expected == nil || expected.Sign() <= 0 {
		return errors.New("USDT quote rounds to zero; adjust the rate or order amount")
	}
	if dust == nil || dust.Sign() < 0 || dust.Cmp(expected) >= 0 {
		return fmt.Errorf("USDT dust tolerance must be lower than the expected amount")
	}
	return nil
}
