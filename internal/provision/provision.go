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
	ErrInvalidEmail      = errors.New("invalid email address")
	ErrInvalidName       = errors.New("name must be 1 to 80 characters")
	ErrInvalidRole       = errors.New("role must be owner or staff")
	ErrInvalidStatus     = errors.New("status must be active or suspended")
	ErrInvalidContent    = errors.New("content too long")
	ErrInvalidStyle      = errors.New("unknown layout style")
	ErrInvalidPalette    = errors.New("unknown palette")
	ErrInvalidTheme      = errors.New("theme must be auto, light or dark")
	ErrLockedByMall      = errors.New("reserved by the mall")
	ErrAllPalettesLocked = errors.New("leave at least one palette open to shops")
	ErrUserExists        = errors.New("a user with that email already exists in this shop")
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

func SetMallContent(ctx context.Context, st *store.Store, mallDomain, tagline, description, contact string) error {
	tagline = strings.TrimSpace(tagline)
	description = strings.TrimSpace(description)
	contact = normalizeEmail(contact)
	if utf8.RuneCountInString(tagline) > 140 {
		return ErrInvalidContent
	}
	if utf8.RuneCountInString(description) > 600 {
		return ErrInvalidContent
	}
	if contact != "" && !ValidEmail(contact) {
		return ErrInvalidEmail
	}
	return st.SetMallContent(ctx, tenant.NormalizeDomain(mallDomain), tagline, description, contact)
}

// The layouts, palettes and colour modes the migrations allow.
var (
	MallStyles = []string{"classic", "editorial", "directory"}
	ShopStyles = []string{"classic", "boutique", "market"}
	Palettes   = []string{"forest", "violet", "ocean", "terracotta"}
	Themes     = []string{"auto", "light", "dark"}
)

// SetMallLook changes the mall landing layout and palette. An empty value
// keeps the current one.
func SetMallLook(ctx context.Context, st *store.Store, mallDomain, style, palette string) error {
	domain := tenant.NormalizeDomain(mallDomain)
	m, err := st.MallByDomain(ctx, domain)
	if err != nil {
		return fmt.Errorf("mall %q: %w", domain, err)
	}
	if style == "" {
		style = m.Style
	}
	if palette == "" {
		palette = m.Palette
	}
	if !slices.Contains(MallStyles, style) {
		return ErrInvalidStyle
	}
	if !slices.Contains(Palettes, palette) {
		return ErrInvalidPalette
	}
	return st.SetMallLook(ctx, domain, style, palette)
}

// SetMallLocks reserves shop layouts and palettes. Classic can't be locked
// and at least one palette stays open, so every shop always has a choice.
func SetMallLocks(ctx context.Context, st *store.Store, mallDomain string, styles, palettes []string) error {
	for _, v := range styles {
		if v == "classic" || !slices.Contains(ShopStyles, v) {
			return ErrInvalidStyle
		}
	}
	for _, v := range palettes {
		if !slices.Contains(Palettes, v) {
			return ErrInvalidPalette
		}
	}
	styles, palettes = uniq(styles), uniq(palettes)
	if len(palettes) == len(Palettes) {
		return ErrAllPalettesLocked
	}
	return st.SetMallLocks(ctx, tenant.NormalizeDomain(mallDomain), styles, palettes)
}

func uniq(v []string) []string {
	out := []string{}
	for _, s := range v {
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

// CheckShopAppearance reports whether a shop may switch to the given look.
// A value the mall has locked is still accepted when the shop already uses
// it, so locking never breaks a shop's saved settings.
func CheckShopAppearance(m store.Mall, cur store.Tenant, style, palette, theme string) error {
	if !slices.Contains(ShopStyles, style) {
		return ErrInvalidStyle
	}
	if !slices.Contains(Palettes, palette) {
		return ErrInvalidPalette
	}
	if !slices.Contains(Themes, theme) {
		return ErrInvalidTheme
	}
	if style != cur.Style && slices.Contains(m.LockedStyles, style) {
		return ErrLockedByMall
	}
	if palette != cur.Palette && slices.Contains(m.LockedPalettes, palette) {
		return ErrLockedByMall
	}
	return nil
}

func SetTenantStyle(ctx context.Context, st *store.Store, mallDomain, subdomain, style string) error {
	if !slices.Contains(ShopStyles, style) {
		return ErrInvalidStyle
	}
	domain := tenant.NormalizeDomain(mallDomain)
	mall, err := st.MallByDomain(ctx, domain)
	if err != nil {
		return fmt.Errorf("mall %q: %w", domain, err)
	}
	return st.SetTenantStyle(ctx, mall.ID, tenant.Normalize(subdomain), style)
}

func SetupURL(scheme, port, mallDomain, subdomain, token string) string {
	return fmt.Sprintf("%s://%s.%s%s/admin/setup?token=%s", scheme, subdomain, mallDomain, port, token)
}
