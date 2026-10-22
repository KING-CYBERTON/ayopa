// Package seed fills a local mall with sample shops, products and blog posts
// so the storefront and admin pages can be previewed. It deletes and rebuilds
// its mall on every run, so it refuses anything but a local database and a
// .localhost or .test mall domain.
package seed

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/KING-CYBERTON/ayopa/internal/auth"
	"github.com/KING-CYBERTON/ayopa/internal/money"
	"github.com/KING-CYBERTON/ayopa/internal/provision"
	"github.com/KING-CYBERTON/ayopa/internal/store"
	"github.com/KING-CYBERTON/ayopa/internal/tenant"
)

const (
	DefaultMall     = "ayopa.localhost"
	DefaultPassword = "ayopa-dev-password"
)

var (
	ErrNotLocalDB      = errors.New("refusing to seed: DATABASE_URL must point at localhost")
	ErrNotDevDomain    = errors.New("refusing to seed: mall domain must end in .localhost or .test")
	ErrShortPassword   = errors.New("password must be at least 10 characters")
	errUnknownCategory = errors.New("seed data names an unknown category")
)

// CheckDatabaseURL accepts only loopback hosts and Unix sockets, so the seed
// can never reach RDS or any other shared database.
func CheckDatabaseURL(dsn string) error {
	cfg, err := pgconn.ParseConfig(dsn)
	if err != nil {
		return err
	}
	hosts := []string{cfg.Host}
	for _, fb := range cfg.Fallbacks {
		hosts = append(hosts, fb.Host)
	}
	for _, h := range hosts {
		if !isLocalHost(h) {
			return fmt.Errorf("%w (host %q)", ErrNotLocalDB, h)
		}
	}
	return nil
}

func isLocalHost(h string) bool {
	if strings.HasPrefix(h, "/") || h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

func checkDomain(domain string) error {
	if !strings.HasSuffix(domain, ".localhost") && !strings.HasSuffix(domain, ".test") {
		return ErrNotDevDomain
	}
	return tenant.ValidateDomain(domain)
}

// Login is one account the seed created.
type Login struct {
	URL   string
	Email string
	Role  string
}

// Result lists what Run created, for the CLI to print.
type Result struct {
	Mall   store.Mall
	Shops  int
	Logins []Login
}

// Run deletes the mall at domain if it exists and creates it again with the
// sample shops. Every owner and staff account gets the same password.
// scheme and port build the login links, as in the setup links.
func Run(ctx context.Context, st *store.Store, domain, password, scheme, port string) (Result, error) {
	domain = tenant.NormalizeDomain(domain)
	if err := checkDomain(domain); err != nil {
		return Result{}, err
	}
	if len(password) < 10 {
		return Result{}, ErrShortPassword
	}
	pwHash, err := auth.HashPassword(password)
	if err != nil {
		return Result{}, err
	}

	if err := st.DeleteMall(ctx, domain); err != nil && !errors.Is(err, store.ErrNotFound) {
		return Result{}, err
	}
	m, err := provision.CreateMall(ctx, st, domain, mallName, nil)
	if err != nil {
		return Result{}, err
	}
	if err := provision.SetMallContent(ctx, st, domain, mallTagline, mallDescription, "hello@"+domain); err != nil {
		return Result{}, err
	}
	if err := provision.SetMallLook(ctx, st, domain, "editorial", "violet"); err != nil {
		return Result{}, err
	}

	res := Result{Mall: m}
	for _, sh := range shops {
		logins, err := seedShop(ctx, st, domain, sh, pwHash, scheme, port)
		if err != nil {
			return Result{}, fmt.Errorf("shop %s: %w", sh.Subdomain, err)
		}
		res.Logins = append(res.Logins, logins...)
		res.Shops++
	}
	return res, nil
}

func seedShop(ctx context.Context, st *store.Store, domain string, sh shop, pwHash, scheme, port string) ([]Login, error) {
	owner := "owner@" + sh.Subdomain + ".test"
	t, token, err := provision.CreateTenant(ctx, st, domain, sh.Subdomain, sh.Name, owner, time.Hour)
	if err != nil {
		return nil, err
	}
	// Claim the owner invite straight away, as the setup page would.
	if _, err := st.AcceptInvite(ctx, auth.HashToken(token), t.ID, pwHash); err != nil {
		return nil, err
	}
	loginURL := fmt.Sprintf("%s://%s.%s%s/admin/login", scheme, sh.Subdomain, domain, port)
	logins := []Login{{URL: loginURL, Email: owner, Role: "owner"}}

	if sh.Staff {
		staff := "staff@" + sh.Subdomain + ".test"
		token, err := provision.CreateInvite(ctx, st, domain, sh.Subdomain, staff, "staff", time.Hour)
		if err != nil {
			return nil, err
		}
		if _, err := st.AcceptInvite(ctx, auth.HashToken(token), t.ID, pwHash); err != nil {
			return nil, err
		}
		logins = append(logins, Login{URL: loginURL, Email: staff, Role: "staff"})
	}

	profile := sh.Profile
	profile.ContactEmail = "hello@" + sh.Subdomain + ".test"
	if err := st.UpdateShopProfile(ctx, t.ID, profile); err != nil {
		return nil, err
	}
	if err := st.SetTenantAppearance(ctx, t.ID, sh.Style, sh.Palette, sh.Theme); err != nil {
		return nil, err
	}

	cats := map[string]int64{}
	for _, name := range sh.Categories {
		c, err := st.CreateCategory(ctx, t.ID, name)
		if err != nil {
			return nil, err
		}
		cats[name] = c.ID
	}

	exp := money.ExponentFor(profile.Currency)
	ids := map[string]int64{}
	for _, p := range sh.Products {
		price, err := money.Parse(p.Price, exp)
		if err != nil {
			return nil, fmt.Errorf("product %q: %w", p.Name, err)
		}
		var catID *int64
		if p.Category != "" {
			id, ok := cats[p.Category]
			if !ok {
				return nil, fmt.Errorf("product %q: %w", p.Name, errUnknownCategory)
			}
			catID = &id
		}
		created, err := st.CreateProduct(ctx, t.ID, p.Name, p.Description, price, p.Stock, catID)
		if err != nil {
			return nil, fmt.Errorf("product %q: %w", p.Name, err)
		}
		ids[p.Name] = created.ID
		if p.Status != store.ProductDraft {
			if err := st.SetProductStatus(ctx, t.ID, created.ID, p.Status); err != nil {
				return nil, err
			}
		}
		if p.Featured {
			if err := st.SetProductFeatured(ctx, t.ID, created.ID, true); err != nil {
				return nil, err
			}
		}
	}

	for _, p := range sh.Posts {
		// Bodies link products as {{Product name}}; swap in the real /shop/ID.
		body := p.Body
		for name, id := range ids {
			body = strings.ReplaceAll(body, "{{"+name+"}}", fmt.Sprintf("/shop/%d", id))
		}
		if strings.Contains(body, "{{") {
			return nil, fmt.Errorf("post %q links an unknown product", p.Title)
		}
		post := store.Post{Title: p.Title, Excerpt: p.Excerpt, Body: body, Category: p.Category, Status: p.Status}
		if _, err := st.CreatePost(ctx, t.ID, post); err != nil {
			return nil, fmt.Errorf("post %q: %w", p.Title, err)
		}
	}
	return logins, nil
}
