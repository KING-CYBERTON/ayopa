// Package money converts between decimal strings and integer minor units.
// All monetary values in the database are stored as BIGINT minor units
// (e.g. KES 100.00 is stored as 10000). This package handles the conversion
// and formatting; no floating-point arithmetic is used anywhere.
package money

import (
	"fmt"
	"strings"
)

// Exponents maps ISO 4217 currency codes to their minor-unit decimal places.
var Exponents = map[string]int{
	"KES": 2, "UGX": 0, "TZS": 2, "RWF": 0,
	"ETB": 2, "NGN": 2, "ZAR": 2, "USD": 2,
}

// ExponentFor returns the number of decimal places for the given currency,
// defaulting to 2 for unknown currencies.
func ExponentFor(currency string) int {
	if e, ok := Exponents[currency]; ok {
		return e
	}
	return 2
}

// Parse converts a decimal string (e.g. "99.95" or "1000") to minor units.
// exp is the number of decimal places for the currency (e.g. 2 for KES).
// Returns an error for negative values, non-numeric input, too many decimal
// places, or trailing decimal points.
func Parse(s string, exp int) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty amount")
	}
	if strings.HasPrefix(s, "-") {
		return 0, fmt.Errorf("negative amounts are not allowed")
	}

	intStr, fracStr, hasDot := strings.Cut(s, ".")
	if hasDot && fracStr == "" {
		return 0, fmt.Errorf("invalid amount: trailing decimal point")
	}
	if !isDigits(intStr) {
		return 0, fmt.Errorf("invalid amount %q", s)
	}
	if hasDot {
		if !isDigits(fracStr) {
			return 0, fmt.Errorf("invalid amount %q", s)
		}
		if len(fracStr) > exp {
			return 0, fmt.Errorf("amount has %d decimal places, currency allows %d", len(fracStr), exp)
		}
	}
	if exp == 0 && hasDot {
		return 0, fmt.Errorf("currency has no decimal places")
	}

	var total int64
	for _, c := range intStr {
		next := total*10 + int64(c-'0')
		if next < total {
			return 0, fmt.Errorf("amount too large")
		}
		total = next
	}

	factor := pow10(exp)
	total, overflow := mulCheck(total, factor)
	if overflow {
		return 0, fmt.Errorf("amount too large")
	}

	if hasDot {
		pad := fracStr
		for len(pad) < exp {
			pad += "0"
		}
		var frac int64
		for _, c := range pad {
			frac = frac*10 + int64(c-'0')
		}
		total += frac
	}

	return total, nil
}

// Format converts minor units to a decimal string suitable for display.
// With exp=2: 9995 → "99.95". With exp=0: 100 → "100".
func Format(minor int64, exp int) string {
	if exp == 0 {
		return fmt.Sprintf("%d", minor)
	}
	factor := pow10(exp)
	intPart := minor / factor
	fracPart := minor % factor
	if fracPart < 0 {
		fracPart = -fracPart
	}
	return fmt.Sprintf("%d.%0*d", intPart, exp, fracPart)
}

// FormatCurrency returns "KES 99.95" style strings.
func FormatCurrency(minor int64, currency string) string {
	return currency + " " + Format(minor, ExponentFor(currency))
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func pow10(n int) int64 {
	v := int64(1)
	for i := 0; i < n; i++ {
		v *= 10
	}
	return v
}

func mulCheck(a, b int64) (int64, bool) {
	if a == 0 || b == 0 {
		return 0, false
	}
	result := a * b
	if result/b != a {
		return 0, true
	}
	return result, false
}
