package money_test

import (
	"strings"
	"testing"

	"github.com/KING-CYBERTON/ayopa/internal/money"
)

func TestParse(t *testing.T) {
	cases := []struct {
		in   string
		exp  int
		want int64
		err  bool
	}{
		{"99.95", 2, 9995, false},
		{"1000", 2, 100000, false},
		{"0.50", 2, 50, false},
		{"0.5", 2, 50, false},
		{"100", 0, 100, false},
		{"0", 2, 0, false},
		{"0.00", 2, 0, false},
		// errors
		{"", 2, 0, true},
		{"-1", 2, 0, true},
		{"99.", 2, 0, true},
		{"abc", 2, 0, true},
		{"1.234", 2, 0, true},  // too many dp
		{"100.5", 0, 0, true},  // currency has no dp
	}
	for _, c := range cases {
		got, err := money.Parse(c.in, c.exp)
		if c.err {
			if err == nil {
				t.Errorf("Parse(%q, %d) = %d, want error", c.in, c.exp, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("Parse(%q, %d) error: %v", c.in, c.exp, err)
			continue
		}
		if got != c.want {
			t.Errorf("Parse(%q, %d) = %d, want %d", c.in, c.exp, got, c.want)
		}
	}
}

func TestFormat(t *testing.T) {
	cases := []struct {
		minor int64
		exp   int
		want  string
	}{
		{9995, 2, "99.95"},
		{100, 0, "100"},
		{50, 2, "0.50"},
		{0, 2, "0.00"},
		{100000, 2, "1000.00"},
		{1, 2, "0.01"},
	}
	for _, c := range cases {
		got := money.Format(c.minor, c.exp)
		if got != c.want {
			t.Errorf("Format(%d, %d) = %q, want %q", c.minor, c.exp, got, c.want)
		}
	}
}

func TestFormatCurrency(t *testing.T) {
	// FormatCurrency uses a narrow no-break or non-breaking space as separator;
	// verify the currency code and amount are both present in the result.
	cases := []struct {
		minor    int64
		currency string
		amount   string // what Format returns
	}{
		{9995, "KES", "99.95"},
		{500, "UGX", "500"},
		{10050, "TZS", "100.50"},
	}
	for _, c := range cases {
		got := money.FormatCurrency(c.minor, c.currency)
		if !strings.HasPrefix(got, c.currency) {
			t.Errorf("FormatCurrency(%d, %q) = %q: does not start with currency code", c.minor, c.currency, got)
		}
		if !strings.HasSuffix(got, c.amount) {
			t.Errorf("FormatCurrency(%d, %q) = %q: does not end with %q", c.minor, c.currency, got, c.amount)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	cases := []struct {
		s        string
		currency string
	}{
		{"99.95", "KES"},
		{"1000.00", "KES"},
		{"0.01", "KES"},
		{"500", "UGX"},
		{"100.50", "TZS"},
	}
	for _, c := range cases {
		exp := money.ExponentFor(c.currency)
		minor, err := money.Parse(c.s, exp)
		if err != nil {
			t.Errorf("Parse(%q, %d): %v", c.s, exp, err)
			continue
		}
		got := money.Format(minor, exp)
		if got != c.s {
			t.Errorf("Format(Parse(%q)) = %q", c.s, got)
		}
	}
}

func TestExponentFor(t *testing.T) {
	if money.ExponentFor("KES") != 2 {
		t.Error("KES exp should be 2")
	}
	if money.ExponentFor("UGX") != 0 {
		t.Error("UGX exp should be 0")
	}
	if money.ExponentFor("UNKNOWN") != 2 {
		t.Error("unknown currency should default to 2")
	}
}
