package tenant

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateDomain(t *testing.T) {
	good := []string{"ayopa.co.ke", "mall1.test", "a-b.example.com", "mall.localhost"}
	for _, d := range good {
		if err := ValidateDomain(d); err != nil {
			t.Errorf("%q rejected: %v", d, err)
		}
	}
	bad := []string{
		"", "localhost", "-a.com", "a-.com", "a..com", "Mall.com",
		"a_b.com", "a.com.", "has space.com", strings.Repeat("a", 64) + ".com",
	}
	for _, d := range bad {
		if err := ValidateDomain(d); !errors.Is(err, ErrInvalidDomain) {
			t.Errorf("%q accepted, want ErrInvalidDomain (got %v)", d, err)
		}
	}
}

func TestNormalizeDomain(t *testing.T) {
	if got := NormalizeDomain("  Mall.COM. "); got != "mall.com" {
		t.Fatalf("got %q", got)
	}
}

func TestValidFormat(t *testing.T) {
	if !ValidFormat("cpanel") {
		t.Error("format check must not apply the reserved list")
	}
	if ValidFormat("xn--abc") || ValidFormat("ab") || ValidFormat("-ab") {
		t.Error("bad format accepted")
	}
}
