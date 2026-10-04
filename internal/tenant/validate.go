package tenant

// Package tenant holds the naming rules for tenant subdomains.

import (
	"errors"
	"regexp"
	"sort"
	"strings"
)

var (
	ErrInvalid  = errors.New("invalid subdomain")
	ErrReserved = errors.New("reserved subdomain")
)

var pattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`)

// reservedNames MUST include every name that has its own DNS record,
// because an explicit record overrides the wildcard. Add a name here
// whenever you add a record in Cloudflare.
var reservedNames = []string{
	// names with real DNS records
	"www", "cpanel", "autodiscover", "test",
	// mail and infrastructure
	"autoconfig", "mail", "webmail", "smtp", "imap", "pop", "mx", "ns1", "ns2", "dns", "ftp",
	// platform surfaces
	"api", "app", "admin", "administrator", "root", "dashboard", "billing", "account",
	"accounts", "auth", "login", "logout", "signup", "register", "status", "support",
	"help", "docs", "blog", "static", "assets", "cdn", "staging", "dev", "demo",
	// trust and abuse
	"security", "abuse", "postmaster", "hostmaster", "webmaster", "info", "noreply",
	"localhost", "ayopa",
}

var reserved = func() map[string]struct{} {
	m := make(map[string]struct{}, len(reservedNames))
	for _, n := range reservedNames {
		m[n] = struct{}{}
	}
	return m
}()

// Normalize lowercases and trims user input.
func Normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// Validate expects a normalized name. It rejects bad formats, punycode
// labels (lookalike names) and reserved names.
func Validate(s string) error {
	if !pattern.MatchString(s) || strings.HasPrefix(s, "xn--") {
		return ErrInvalid
	}
	if IsReserved(s) {
		return ErrReserved
	}
	return nil
}

func IsReserved(s string) bool {
	_, ok := reserved[s]
	return ok
}

func ReservedNames() []string {
	out := append([]string(nil), reservedNames...)
	sort.Strings(out)
	return out
}
