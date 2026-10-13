package tenant

import (
	"errors"
	"regexp"
	"strings"
)

var ErrInvalidDomain = errors.New("invalid mall domain")

var domainPattern = regexp.MustCompile(
	`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)

// NormalizeDomain lowercases, trims spaces and removes a trailing dot.
func NormalizeDomain(s string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(s), "."))
}

// ValidateDomain expects a normalized name with at least two labels.
func ValidateDomain(s string) error {
	if len(s) > 253 || !domainPattern.MatchString(s) {
		return ErrInvalidDomain
	}
	return nil
}

// ValidFormat reports whether s is an acceptable subdomain label,
// ignoring the reserved list.
func ValidFormat(s string) bool {
	return pattern.MatchString(s) && !strings.HasPrefix(s, "xn--")
}
