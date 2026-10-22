package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/KING-CYBERTON/ayopa/internal/provision"
	"github.com/KING-CYBERTON/ayopa/internal/store"
	"github.com/KING-CYBERTON/ayopa/internal/tenant"
	"github.com/KING-CYBERTON/ayopa/internal/web"
)

func runAdmin(name string, args []string) int {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL is required")
		return 1
	}
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "connect:", err)
		return 1
	}
	defer pool.Close()

	st := store.New(pool)
	cfg := web.ConfigFromEnv()

	switch name {
	case "create-mall":
		err = createMall(ctx, st, args)
	case "create-tenant":
		err = createTenant(ctx, st, cfg, args)
	case "create-invite":
		err = createInvite(ctx, st, cfg, args)
	case "set-tenant-status":
		err = setTenantStatus(ctx, st, args)
	case "set-mall-status":
		err = setMallStatus(ctx, st, args)
	case "set-mall-content":
		err = setMallContent(ctx, st, args)
	case "set-mall-style":
		err = setMallStyle(ctx, st, args)
	case "set-mall-locks":
		err = setMallLocks(ctx, st, args)
	case "set-shop-style":
		err = setShopStyle(ctx, st, args)
	case "list":
		err = listAll(ctx, st)
	default:
		err = fmt.Errorf("unknown command %q", name)
	}
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

func newFlags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	return fs
}

func require(pairs ...[2]string) error {
	for _, p := range pairs {
		if strings.TrimSpace(p[1]) == "" {
			return fmt.Errorf("-%s is required", p[0])
		}
	}
	return nil
}

func createMall(ctx context.Context, st *store.Store, args []string) error {
	fs := newFlags("create-mall")
	domain := fs.String("domain", "", "mall domain, for example mall.co.ke")
	name := fs.String("name", "", "display name")
	reserve := fs.String("reserve", "", "comma-separated extra reserved subdomains")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := require([2]string{"domain", *domain}, [2]string{"name", *name}); err != nil {
		return err
	}

	var reserved []string
	if *reserve != "" {
		reserved = strings.Split(*reserve, ",")
	}
	m, err := provision.CreateMall(ctx, st, *domain, *name, reserved)
	if err != nil {
		return err
	}
	fmt.Printf("Created mall %q (%s).\n\n", m.Name, m.Domain)
	fmt.Println("Make sure of the following:")
	fmt.Printf("  1. %s is listed in deploy/malls.txt (certificates)\n", m.Domain)
	fmt.Printf("  2. %s is in mall_zones in infra/terraform.tfvars (DNS)\n", m.Domain)
	fmt.Println("  3. The Caddy Cloudflare token covers the zone")
	fmt.Println("  4. Names with their own DNS records are reserved (-reserve)")
	return nil
}

func createTenant(ctx context.Context, st *store.Store, cfg web.Config, args []string) error {
	fs := newFlags("create-tenant")
	mall := fs.String("mall", "", "mall domain")
	sub := fs.String("subdomain", "", "shop subdomain")
	name := fs.String("name", "", "shop display name")
	owner := fs.String("owner", "", "owner email")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := require([2]string{"mall", *mall}, [2]string{"subdomain", *sub},
		[2]string{"name", *name}, [2]string{"owner", *owner}); err != nil {
		return err
	}

	t, token, err := provision.CreateTenant(ctx, st, *mall, *sub, *name, *owner, cfg.InviteTTL)
	if err != nil {
		return err
	}
	printSetupLink(cfg, tenant.NormalizeDomain(*mall), t.Subdomain, token)
	return nil
}

func createInvite(ctx context.Context, st *store.Store, cfg web.Config, args []string) error {
	fs := newFlags("create-invite")
	mall := fs.String("mall", "", "mall domain")
	sub := fs.String("subdomain", "", "shop subdomain")
	email := fs.String("email", "", "email to invite")
	role := fs.String("role", "owner", "owner or staff")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := require([2]string{"mall", *mall}, [2]string{"subdomain", *sub},
		[2]string{"email", *email}); err != nil {
		return err
	}

	token, err := provision.CreateInvite(ctx, st, *mall, *sub, *email, *role, cfg.InviteTTL)
	if err != nil {
		return err
	}
	printSetupLink(cfg, tenant.NormalizeDomain(*mall), tenant.Normalize(*sub), token)
	return nil
}

func printSetupLink(cfg web.Config, mallDomain, sub, token string) {
	fmt.Println("Shop is ready. Send the owner this one-time setup link:")
	fmt.Println()
	fmt.Println("  " + provision.SetupURL(cfg.Scheme, cfg.Port, mallDomain, sub, token))
	fmt.Println()
	fmt.Printf("It works once and expires in %s. Treat it like a password: anyone with it\n", cfg.InviteTTL)
	fmt.Println("can set the owner's password. It is not stored and cannot be shown again.")
}

func setTenantStatus(ctx context.Context, st *store.Store, args []string) error {
	fs := newFlags("set-tenant-status")
	mall := fs.String("mall", "", "mall domain")
	sub := fs.String("subdomain", "", "shop subdomain")
	status := fs.String("status", "", "active or suspended")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := require([2]string{"mall", *mall}, [2]string{"subdomain", *sub},
		[2]string{"status", *status}); err != nil {
		return err
	}
	if err := provision.SetTenantStatus(ctx, st, *mall, *sub, *status); err != nil {
		return err
	}
	fmt.Printf("%s.%s is now %s.\n", tenant.Normalize(*sub), tenant.NormalizeDomain(*mall), *status)
	return nil
}

func setMallStatus(ctx context.Context, st *store.Store, args []string) error {
	fs := newFlags("set-mall-status")
	mall := fs.String("mall", "", "mall domain")
	status := fs.String("status", "", "active or suspended")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := require([2]string{"mall", *mall}, [2]string{"status", *status}); err != nil {
		return err
	}
	if err := provision.SetMallStatus(ctx, st, *mall, *status); err != nil {
		return err
	}
	fmt.Printf("%s is now %s.\n", tenant.NormalizeDomain(*mall), *status)
	return nil
}

func setMallStyle(ctx context.Context, st *store.Store, args []string) error {
	fs := newFlags("set-mall-style")
	mall := fs.String("mall", "", "mall domain")
	style := fs.String("style", "", "landing layout: "+strings.Join(provision.MallStyles, ", "))
	palette := fs.String("palette", "", "landing palette: "+strings.Join(provision.Palettes, ", "))
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := require([2]string{"mall", *mall}); err != nil {
		return err
	}
	if *style == "" && *palette == "" {
		return errors.New("-style or -palette is required")
	}
	if err := provision.SetMallLook(ctx, st, *mall, *style, *palette); err != nil {
		return err
	}
	fmt.Printf("Landing page look updated for %s.\n", tenant.NormalizeDomain(*mall))
	return nil
}

func setMallLocks(ctx context.Context, st *store.Store, args []string) error {
	fs := newFlags("set-mall-locks")
	mall := fs.String("mall", "", "mall domain")
	styles := fs.String("styles", "", "comma-separated shop layouts to reserve (boutique, market); empty clears")
	palettes := fs.String("palettes", "", "comma-separated palettes to reserve; empty clears")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := require([2]string{"mall", *mall}); err != nil {
		return err
	}
	domain := tenant.NormalizeDomain(*mall)
	m, err := st.MallByDomain(ctx, domain)
	if err != nil {
		return fmt.Errorf("mall %q: %w", domain, err)
	}
	newStyles, newPalettes := m.LockedStyles, m.LockedPalettes
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "styles":
			newStyles = splitList(*styles)
		case "palettes":
			newPalettes = splitList(*palettes)
		}
	})
	if err := provision.SetMallLocks(ctx, st, domain, newStyles, newPalettes); err != nil {
		return err
	}
	fmt.Printf("Reserved for %s: layouts [%s], palettes [%s].\n", domain,
		strings.Join(newStyles, ", "), strings.Join(newPalettes, ", "))
	return nil
}

func splitList(s string) []string {
	out := []string{}
	for _, v := range strings.Split(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func setShopStyle(ctx context.Context, st *store.Store, args []string) error {
	fs := newFlags("set-shop-style")
	mall := fs.String("mall", "", "mall domain")
	sub := fs.String("subdomain", "", "shop subdomain")
	style := fs.String("style", "", strings.Join(provision.ShopStyles, ", "))
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := require([2]string{"mall", *mall}, [2]string{"subdomain", *sub},
		[2]string{"style", *style}); err != nil {
		return err
	}
	if err := provision.SetTenantStyle(ctx, st, *mall, *sub, *style); err != nil {
		return err
	}
	fmt.Printf("%s.%s now uses the %s layout.\n", tenant.Normalize(*sub), tenant.NormalizeDomain(*mall), *style)
	return nil
}

func setMallContent(ctx context.Context, st *store.Store, args []string) error {
	fs := newFlags("set-mall-content")
	mall := fs.String("mall", "", "mall domain")
	tagline := fs.String("tagline", "", "short tagline, up to 140 characters")
	description := fs.String("description", "", "body copy, up to 600 characters")
	contact := fs.String("contact", "", "contact email shown on landing page (leave empty to clear)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := require([2]string{"mall", *mall}); err != nil {
		return err
	}

	// Read current values so unset flags keep existing content.
	domain := tenant.NormalizeDomain(*mall)
	m, err := st.MallByDomain(ctx, domain)
	if err != nil {
		return fmt.Errorf("mall %q: %w", domain, err)
	}

	newTagline := m.Tagline
	newDesc := m.Description
	newContact := m.ContactEmail

	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "tagline":
			newTagline = *tagline
		case "description":
			newDesc = *description
		case "contact":
			newContact = *contact
		}
	})

	if err := provision.SetMallContent(ctx, st, domain, newTagline, newDesc, newContact); err != nil {
		return err
	}
	fmt.Printf("Landing page content updated for %s.\n", domain)
	return nil
}

func listAll(ctx context.Context, st *store.Store) error {
	malls, err := st.ListMalls(ctx)
	if err != nil {
		return err
	}
	if len(malls) == 0 {
		fmt.Println("No malls yet. Create one with: create-mall -domain D -name N")
		return nil
	}
	for _, m := range malls {
		fmt.Printf("%s  (%q, %s)\n", m.Domain, m.Name, m.Status)
		shops, err := st.ListTenants(ctx, m.ID, true)
		if err != nil {
			return err
		}
		for _, t := range shops {
			fmt.Printf("  %s.%s  %q  [%s]\n", t.Subdomain, m.Domain, t.Name, t.Status)
		}
	}
	return nil
}
