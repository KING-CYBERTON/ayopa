package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/KING-CYBERTON/ayopa/internal/seed"
)

// The seed must run twice in a row, and every page it exists to preview must
// render with its data.
func TestSeedRendersEveryShop(t *testing.T) {
	e := newEnv(t, testConfig())
	ctx := context.Background()

	if _, err := seed.Run(ctx, e.st, "seed.test", testPass, "https", ""); err != nil {
		t.Fatalf("first run: %v", err)
	}
	res, err := seed.Run(ctx, e.st, "seed.test", testPass, "https", "")
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if res.Shops < 2 || res.Shops > 4 {
		t.Fatalf("seeded %d shops, want 2 to 4", res.Shops)
	}

	wantStatus(t, e.do(http.MethodGet, "seed.test", "/", nil), http.StatusOK)

	shops, err := e.st.ListTenants(ctx, res.Mall.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(shops) != res.Shops {
		t.Fatalf("mall has %d shops after reseeding, want %d", len(shops), res.Shops)
	}
	for _, sh := range shops {
		host := sh.Subdomain + ".seed.test"
		wantStatus(t, e.do(http.MethodGet, host, "/", nil), http.StatusOK)
		wantStatus(t, e.do(http.MethodGet, host, "/blog", nil), http.StatusOK)

		posts, err := e.st.ListPosts(ctx, sh.ID, true)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range posts {
			wantStatus(t, e.do(http.MethodGet, host, fmt.Sprintf("/blog/%d", p.ID), nil), http.StatusOK)
		}

		c := e.login(host, "owner@"+sh.Subdomain+".test")
		for _, path := range []string{"/admin", "/admin/products", "/admin/categories", "/admin/blog", "/admin/appearance", "/admin/settings"} {
			wantStatus(t, e.do(http.MethodGet, host, path, nil, c), http.StatusOK)
		}
	}
}

func TestSeedRefusesSharedTargets(t *testing.T) {
	for _, dsn := range []string{
		"postgres://app:pw@ayopa.abc123.eu-central-1.rds.amazonaws.com:5432/ayopa",
		"postgres://app:pw@10.0.1.5/ayopa",
		"postgres://app:pw@localhost,db.example.com/ayopa",
	} {
		if err := seed.CheckDatabaseURL(dsn); !errors.Is(err, seed.ErrNotLocalDB) {
			t.Errorf("CheckDatabaseURL(%q) = %v, want ErrNotLocalDB", dsn, err)
		}
	}
	for _, dsn := range []string{
		"postgres://app:pw@localhost:5433/ayopa_dev",
		"postgres://app:pw@127.0.0.1/ayopa_dev",
		"host=/var/run/postgresql dbname=ayopa_dev",
	} {
		if err := seed.CheckDatabaseURL(dsn); err != nil {
			t.Errorf("CheckDatabaseURL(%q) = %v, want nil", dsn, err)
		}
	}

	e := newEnv(t, testConfig())
	if _, err := seed.Run(context.Background(), e.st, "ayopa.co.ke", testPass, "https", ""); !errors.Is(err, seed.ErrNotDevDomain) {
		t.Fatalf("seeding a real domain: %v, want ErrNotDevDomain", err)
	}
}
