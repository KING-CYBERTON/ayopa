package tenant

import (
	"errors"
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  error
	}{
		{"simple", "alice", nil},
		{"digits", "shop42", nil},
		{"inner hyphen", "my-shop", nil},
		{"min length", "abc", nil},
		{"max length", strings.Repeat("a", 63), nil},
		{"too short", "ab", ErrInvalid},
		{"too long", strings.Repeat("a", 64), ErrInvalid},
		{"empty", "", ErrInvalid},
		{"leading hyphen", "-alice", ErrInvalid},
		{"trailing hyphen", "alice-", ErrInvalid},
		{"uppercase", "Alice", ErrInvalid},
		{"underscore", "al_ice", ErrInvalid},
		{"dot", "alice.bob", ErrInvalid},
		{"space", "alice bob", ErrInvalid},
		{"punycode", "xn--abc", ErrInvalid},
		{"reserved admin", "admin", ErrReserved},
		{"reserved www", "www", ErrReserved},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Validate(tt.input); !errors.Is(got, tt.want) {
				t.Fatalf("Validate(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalize(t *testing.T) {
	if got := Normalize("  AliCE "); got != "alice" {
		t.Fatalf("got %q", got)
	}
}

// If you add a DNS record, add its name to the reserved list. This test
// fails if a name we know about is missing.
func TestReservedCoversExistingDNSNames(t *testing.T) {
	for _, name := range []string{"www", "cpanel", "autodiscover", "test"} {
		if !IsReserved(name) {
			t.Errorf("%q has a DNS record but is not reserved", name)
		}
	}
}
