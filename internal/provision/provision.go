// Package provision holds the operator actions: creating malls and shops,
// issuing invites and suspending. The CLI and the tests both use it.
package provision

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/KING-CYBERTON/ayopa/internal/auth"
	"github.com/KING-CYBERTON/ayopa/internal/store"
	"github.com/KING-CYBERTON/ayopa/internal/tenant"
)

var (
	ErrInvalidEmail  = errors.New("invalid email address")
	ErrInvalidName   = errors.New("name must be 1 to 80 characters")
	ErrInvalidRole   = errors.New("role must be owner or staff")
	ErrInvalidStatus = errors.New("status must be active or suspended")
	ErrUserExists    = errors.New("a user with that email already exists in this shop")
)

func ValidEmail(e string) bool {
	if len(e) > 254 {
		return false
	}
	a, err := mail.ParseAddress(e)
	return err == nil && a.Address == e
}

func normalizeEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func cleanName(s string) (string, error) {
	s = strings.TrimSpace(s)
	if n := utf8.RuneCountInString(s); n < 1 || n > 80 {
		return "", ErrInvalidName
	}
	return s, nil
}

func validStatus(s string) bool {
	return s == store.StatusActive || s == store.StatusSuspended
}

func CreateMall(ctx context.Context, st *store.Store, domain, name string, reserved []string) (store.Mall, error) {
	domain = tenant.NormalizeDomain(domain)
	if err := tenant.ValidateDomain(domain); err != nil {
		return store.Mall{}, err
	}
	name, err := cleanName(name)
	if err != nil {
		return store.Mall{}, err
	}
	var cleaned []string
	for _, r := range reserved {
		if r = tenant.Normalize(r); r != "" && !slices.Contains(cleaned, r) {
			cleaned = append(cleaned, r)
		}
	}
	return st.CreateMall(ctx, domain, name, cleaned)
}

// CreateTenant creates a shop and an owner invite, and returns the one-time
// token. The token is the only credential: it is never stored, only its hash.
func CreateTenant(ctx context.Context, st *store.Store, mallDomain, subdomain, name, ownerEmail string, ttl time.Duration) (store.Tenant, string, error) {
	sub := tenant.Normalize(subdomain)
	if err := tenant.Validate(sub); err != nil {
		return store.Tenant{}, "", err
	}
	email := normalizeEmail(ownerEmail)
	if !ValidEmail(email) {
		return store.Tenant{}, "", ErrInvalidEmail
	}
	name, err := cleanName(name)
	if err != nil {
		return store.Tenant{}, "", err
	}

	domain := tenant.NormalizeDomain(mallDomain)
	mall, err := st.MallByDomain(ctx, domain)
	if err != nil {
		return store.Tenant{}, "", fmt.Errorf("mall %q: %w", domain, err)
	}
	if slices.Contains(mall.Reserved, sub) {
		return store.Tenant{}, "", tenant.ErrReserved
	}

	token, hash, err := auth.NewToken()
	if err != nil {
		return store.Tenant{}, "", err
	}
	t, err := st.CreateTenantWithInvite(ctx, mall.ID, sub, name, email, "owner", hash, time.Now().Add(ttl))
	if err != nil {
		return store.Tenant{}, "", err
	}
	return t, token, nil
}

// CreateInvite issues a fresh link for an existing shop, replacing any
// earlier unused link for the same email.
func CreateInvite(ctx context.Context, st *store.Store, mallDomain, subdomain, email, role string, ttl time.Duration) (string, error) {
	if role != "owner" && role != "staff" {
		return "", ErrInvalidRole
	}
	email = normalizeEmail(email)
	if !ValidEmail(email) {
		return "", ErrInvalidEmail
	}
	domain := tenant.NormalizeDomain(mallDomain)
	mall, err := st.MallByDomain(ctx, domain)
	if err != nil {
		return "", fmt.Errorf("mall %q: %w", domain, err)
	}
	t, err := st.TenantBySubdomain(ctx, mall.ID, tenant.Normalize(subdomain))
	if err != nil {
		return "", fmt.Errorf("shop %q: %w", subdomain, err)
	}

	if _, err := st.UserByEmail(ctx, t.ID, email); err == nil {
		return "", ErrUserExists
	} else if !errors.Is(err, store.ErrNotFound) {
		return "", err
	}

	token, hash, err := auth.NewToken()
	if err != nil {
		return "", err
	}
	if err := st.CreateInvite(ctx, t.ID, email, role, hash, time.Now().Add(ttl)); err != nil {
		return "", err
	}
	return token, nil
}

func SetTenantStatus(ctx context.Context, st *store.Store, mallDomain, subdomain, status string) error {
	if !validStatus(status) {
		return ErrInvalidStatus
	}
	domain := tenant.NormalizeDomain(mallDomain)
	mall, err := st.MallByDomain(ctx, domain)
	if err != nil {
		return fmt.Errorf("mall %q: %w", domain, err)
	}
	return st.SetTenantStatus(ctx, mall.ID, tenant.Normalize(subdomain), status)
}

func SetMallStatus(ctx context.Context, st *store.Store, mallDomain, status string) error {
	if !validStatus(status) {
		return ErrInvalidStatus
	}
	return st.SetMallStatus(ctx, tenant.NormalizeDomain(mallDomain), status)
}

func SetupURL(scheme, port, mallDomain, subdomain, token string) string {
	return fmt.Sprintf("%s://%s.%s%s/admin/setup?token=%s", scheme, subdomain, mallDomain, port, token)
}
