package web

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/KING-CYBERTON/ayopa/internal/db"
	"github.com/KING-CYBERTON/ayopa/internal/provision"
	"github.com/KING-CYBERTON/ayopa/internal/store"
	"github.com/KING-CYBERTON/ayopa/internal/tenant"
	"github.com/KING-CYBERTON/ayopa/migrations"
)

const testPass = "a-long-password-1"

func testConfig() Config {
	return Config{
		Scheme:       "https",
		CookieSecure: true,
		SessionTTL:   time.Hour,
		InviteTTL:    time.Hour,
		LoginLimit:   1000,
		LoginWindow:  time.Hour,
	}
}

type testEnv struct {
	t    *testing.T
	srv  *Server
	h    http.Handler
	st   *store.Store
	pool *pgxpool.Pool
}

// newEnv connects to TEST_DATABASE_URL, applies the real migrations and
// empties the tables. It refuses to run against a database whose name does
// not contain _test, because it truncates data.
func newEnv(t *testing.T, cfg Config) *testEnv {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	if !strings.Contains(dsn, "_test") {
		t.Fatal("refusing to run: TEST_DATABASE_URL must point at a database whose name contains _test")
	}
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	if _, err := db.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := pool.Exec(ctx, `TRUNCATE malls RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}

	st := store.New(pool)
	srv := New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return &testEnv{t: t, srv: srv, h: srv.Handler(), st: st, pool: pool}
}

func (e *testEnv) do(method, host, path string, form url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	e.t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, "https://"+host+path, body)
	req.Host = host
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if method == http.MethodPost {
		req.Header.Set("Origin", "https://"+host)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

func (e *testEnv) mall(domain string, reserved ...string) store.Mall {
	e.t.Helper()
	m, err := provision.CreateMall(context.Background(), e.st, domain, "Mall "+domain, reserved)
	if err != nil {
		e.t.Fatalf("create mall %s: %v", domain, err)
	}
	return m
}

// shop creates a shop and returns its one-time setup token.
func (e *testEnv) shop(mallDomain, sub, owner string) string {
	e.t.Helper()
	_, token, err := provision.CreateTenant(context.Background(), e.st,
		mallDomain, sub, "Shop "+sub, owner, time.Hour)
	if err != nil {
		e.t.Fatalf("create shop %s.%s: %v", sub, mallDomain, err)
	}
	return token
}

// setup creates a shop, completes the owner setup and returns the session.
func (e *testEnv) setup(mallDomain, sub, owner string) *http.Cookie {
	e.t.Helper()
	token := e.shop(mallDomain, sub, owner)
	rec := e.do(http.MethodPost, sub+"."+mallDomain, "/admin/setup",
		url.Values{"token": {token}, "password": {testPass}, "confirm": {testPass}})
	wantStatus(e.t, rec, http.StatusSeeOther)
	return sessionCookie(e.t, rec)
}

func (e *testEnv) login(host, email string) *http.Cookie {
	e.t.Helper()
	rec := e.do(http.MethodPost, host, "/admin/login",
		url.Values{"email": {email}, "password": {testPass}})
	wantStatus(e.t, rec, http.StatusSeeOther)
	return sessionCookie(e.t, rec)
}

func sessionCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == "__Host-session" && c.Value != "" {
			return c
		}
	}
	t.Fatal("no session cookie in response")
	return nil
}

func wantStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status %d, want %d; body: %.200s", rec.Code, want, rec.Body.String())
	}
}

func TestTemplatesParse(t *testing.T) {
	// Verifies that all embedded templates load without error at server init.
	_ = New(testConfig(), nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestHealthz(t *testing.T) {
	e := newEnv(t, testConfig())
	rec := e.do(http.MethodGet, "anything.example", "/healthz", nil)
	wantStatus(t, rec, http.StatusOK)
	if strings.TrimSpace(rec.Body.String()) != "ok" {
		t.Fatalf("body %q", rec.Body.String())
	}
}

func TestMigrationsAreIdempotent(t *testing.T) {
	e := newEnv(t, testConfig())
	applied, err := db.Migrate(context.Background(), e.pool, migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 0 {
		t.Fatalf("second run applied %v, want nothing", applied)
	}
}

func TestMallDirectory(t *testing.T) {
	e := newEnv(t, testConfig())
	e.mall("mall1.test")
	e.mall("mall2.test")
	e.shop("mall1.test", "alice", "a@example.com")
	e.shop("mall1.test", "bob", "b@example.com")
	e.shop("mall2.test", "carol", "c@example.com")

	rec := e.do(http.MethodGet, "mall1.test", "/", nil)
	wantStatus(t, rec, http.StatusOK)
	body := rec.Body.String()
	for _, want := range []string{"Shop alice", "Shop bob", "alice.mall1.test"} {
		if !strings.Contains(body, want) {
			t.Errorf("directory is missing %q", want)
		}
	}
	if strings.Contains(body, "Shop carol") {
		t.Error("directory leaks a shop from another mall")
	}

	if err := provision.SetTenantStatus(context.Background(), e.st, "mall1.test", "bob", "suspended"); err != nil {
		t.Fatal(err)
	}
	rec = e.do(http.MethodGet, "mall1.test", "/", nil)
	if strings.Contains(rec.Body.String(), "Shop bob") {
		t.Error("suspended shop is still listed")
	}
}

func TestWwwServesDirectory(t *testing.T) {
	e := newEnv(t, testConfig())
	e.mall("mall1.test")
	rec := e.do(http.MethodGet, "www.mall1.test", "/", nil)
	wantStatus(t, rec, http.StatusOK)
	if !strings.Contains(rec.Body.String(), "Mall mall1.test") {
		t.Fatal("www host did not serve the directory")
	}
}

func TestUnknownHostsReturn404(t *testing.T) {
	e := newEnv(t, testConfig())
	e.mall("mall1.test")
	e.shop("mall1.test", "alice", "a@example.com")

	hosts := []string{
		"nobody.mall1.test",
		"evil.com",
		"mall1.test.evil.com",
		"alice.mall1.test.evil.com",
		"a.b.mall1.test",
		"admin.mall1.test",
	}
	for _, host := range hosts {
		rec := e.do(http.MethodGet, host, "/", nil)
		if rec.Code != http.StatusNotFound {
			t.Errorf("host %q: status %d, want 404", host, rec.Code)
		}
	}
}

func TestSetupFlow(t *testing.T) {
	e := newEnv(t, testConfig())
	e.mall("mall1.test")
	token := e.shop("mall1.test", "alice", "owner@example.com")
	host := "alice.mall1.test"
	good := url.Values{"token": {token}, "password": {testPass}, "confirm": {testPass}}

	wantStatus(t, e.do(http.MethodGet, host, "/admin/setup?token=nope", nil), http.StatusBadRequest)

	rec := e.do(http.MethodGet, host, "/admin/setup?token="+token, nil)
	wantStatus(t, rec, http.StatusOK)
	if !strings.Contains(rec.Body.String(), "owner@example.com") {
		t.Fatal("setup page does not show the invited email")
	}

	mismatch := url.Values{"token": {token}, "password": {testPass}, "confirm": {"another-password-2"}}
	wantStatus(t, e.do(http.MethodPost, host, "/admin/setup", mismatch), http.StatusBadRequest)
	short := url.Values{"token": {token}, "password": {"short"}, "confirm": {"short"}}
	wantStatus(t, e.do(http.MethodPost, host, "/admin/setup", short), http.StatusBadRequest)

	rec = e.do(http.MethodPost, host, "/admin/setup", good)
	wantStatus(t, rec, http.StatusSeeOther)
	if loc := rec.Header().Get("Location"); loc != "/admin" {
		t.Fatalf("redirect %q", loc)
	}
	cookie := sessionCookie(t, rec)

	rec = e.do(http.MethodGet, host, "/admin", nil, cookie)
	wantStatus(t, rec, http.StatusOK)
	if !strings.Contains(rec.Body.String(), "owner@example.com") {
		t.Fatal("dashboard does not show the signed-in user")
	}

	// The link works exactly once.
	wantStatus(t, e.do(http.MethodPost, host, "/admin/setup", good), http.StatusBadRequest)

	// The chosen password works for a normal login.
	e.login(host, "owner@example.com")
}

func TestSetupTokenIsTenantBound(t *testing.T) {
	e := newEnv(t, testConfig())
	e.mall("mall1.test")
	aliceToken := e.shop("mall1.test", "alice", "owner@example.com")
	bobToken := e.shop("mall1.test", "bob", "owner@example.com")

	form := url.Values{"token": {aliceToken}, "password": {testPass}, "confirm": {testPass}}
	wantStatus(t, e.do(http.MethodPost, "bob.mall1.test", "/admin/setup", form), http.StatusBadRequest)
	wantStatus(t, e.do(http.MethodGet, "bob.mall1.test", "/admin/setup?token="+aliceToken, nil), http.StatusBadRequest)

	// Bob's own token still works, and Alice's is still unused.
	wantStatus(t, e.do(http.MethodGet, "bob.mall1.test", "/admin/setup?token="+bobToken, nil), http.StatusOK)
	wantStatus(t, e.do(http.MethodGet, "alice.mall1.test", "/admin/setup?token="+aliceToken, nil), http.StatusOK)
}

func TestExpiredInviteIsRejected(t *testing.T) {
	e := newEnv(t, testConfig())
	e.mall("mall1.test")
	token := e.shop("mall1.test", "alice", "owner@example.com")

	if _, err := e.pool.Exec(context.Background(),
		`UPDATE invites SET expires_at = now() - interval '1 minute'`); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, e.do(http.MethodGet, "alice.mall1.test", "/admin/setup?token="+token, nil), http.StatusBadRequest)
}

func TestNewInviteReplacesOldOne(t *testing.T) {
	e := newEnv(t, testConfig())
	e.mall("mall1.test")
	ctx := context.Background()
	oldToken := e.shop("mall1.test", "alice", "owner@example.com")

	newToken, err := provision.CreateInvite(ctx, e.st, "mall1.test", "alice", "owner@example.com", "owner", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	host := "alice.mall1.test"
	wantStatus(t, e.do(http.MethodGet, host, "/admin/setup?token="+oldToken, nil), http.StatusBadRequest)
	wantStatus(t, e.do(http.MethodGet, host, "/admin/setup?token="+newToken, nil), http.StatusOK)

	form := url.Values{"token": {newToken}, "password": {testPass}, "confirm": {testPass}}
	wantStatus(t, e.do(http.MethodPost, host, "/admin/setup", form), http.StatusSeeOther)

	// Once the account exists, inviting the same email again is refused.
	_, err = provision.CreateInvite(ctx, e.st, "mall1.test", "alice", "owner@example.com", "owner", time.Hour)
	if !errors.Is(err, provision.ErrUserExists) {
		t.Fatalf("got %v, want ErrUserExists", err)
	}
}

func TestAdminRequiresLogin(t *testing.T) {
	e := newEnv(t, testConfig())
	e.mall("mall1.test")
	e.shop("mall1.test", "alice", "owner@example.com")
	host := "alice.mall1.test"

	rec := e.do(http.MethodGet, host, "/admin", nil)
	wantStatus(t, rec, http.StatusSeeOther)
	if loc := rec.Header().Get("Location"); loc != "/admin/login" {
		t.Fatalf("redirect %q", loc)
	}
	wantStatus(t, e.do(http.MethodPost, host, "/admin/notes", url.Values{"body": {"x"}}), http.StatusUnauthorized)
	wantStatus(t, e.do(http.MethodGet, host, "/", nil), http.StatusOK) // the storefront is public
}

func TestLoginAndSessionCookie(t *testing.T) {
	e := newEnv(t, testConfig())
	e.mall("mall1.test")
	e.setup("mall1.test", "alice", "owner@example.com")
	host := "alice.mall1.test"

	bad := e.do(http.MethodPost, host, "/admin/login",
		url.Values{"email": {"owner@example.com"}, "password": {"wrong-password-1"}})
	wantStatus(t, bad, http.StatusUnauthorized)
	unknown := e.do(http.MethodPost, host, "/admin/login",
		url.Values{"email": {"nobody@example.com"}, "password": {testPass}})
	wantStatus(t, unknown, http.StatusUnauthorized)

	c := e.login(host, "owner@example.com")
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode {
		t.Errorf("cookie flags: httponly=%v secure=%v samesite=%v", c.HttpOnly, c.Secure, c.SameSite)
	}
	if c.Domain != "" || c.Path != "/" {
		t.Errorf("cookie must be host-only: domain=%q path=%q", c.Domain, c.Path)
	}
}

func TestLogoutInvalidatesSession(t *testing.T) {
	e := newEnv(t, testConfig())
	e.mall("mall1.test")
	cookie := e.setup("mall1.test", "alice", "owner@example.com")
	host := "alice.mall1.test"

	wantStatus(t, e.do(http.MethodPost, host, "/admin/logout", nil, cookie), http.StatusSeeOther)
	wantStatus(t, e.do(http.MethodGet, host, "/admin", nil, cookie), http.StatusSeeOther)
}

func TestExpiredSessionIsRejected(t *testing.T) {
	e := newEnv(t, testConfig())
	e.mall("mall1.test")
	cookie := e.setup("mall1.test", "alice", "owner@example.com")

	if _, err := e.pool.Exec(context.Background(),
		`UPDATE sessions SET expires_at = now() - interval '1 minute'`); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, e.do(http.MethodGet, "alice.mall1.test", "/admin", nil, cookie), http.StatusSeeOther)
}

// Shop A must never see or touch shop B's data, even in the same mall and
// even when both owners use the same email address.
func TestTenantIsolationWithinMall(t *testing.T) {
	e := newEnv(t, testConfig())
	e.mall("mall1.test")
	aliceCookie := e.setup("mall1.test", "alice", "owner@example.com")
	bobCookie := e.setup("mall1.test", "bob", "owner@example.com")
	aliceHost, bobHost := "alice.mall1.test", "bob.mall1.test"

	wantStatus(t, e.do(http.MethodPost, aliceHost, "/admin/notes",
		url.Values{"body": {"alice-secret"}}, aliceCookie), http.StatusSeeOther)

	rec := e.do(http.MethodGet, aliceHost, "/admin", nil, aliceCookie)
	if !strings.Contains(rec.Body.String(), "alice-secret") {
		t.Fatal("alice cannot see her own note")
	}

	rec = e.do(http.MethodGet, bobHost, "/admin", nil, bobCookie)
	wantStatus(t, rec, http.StatusOK)
	if strings.Contains(rec.Body.String(), "alice-secret") {
		t.Fatal("bob's admin leaks alice's data")
	}

	// Alice's session is useless on bob's host.
	rec = e.do(http.MethodGet, bobHost, "/admin", nil, aliceCookie)
	wantStatus(t, rec, http.StatusSeeOther)
	if loc := rec.Header().Get("Location"); loc != "/admin/login" {
		t.Fatalf("alice's cookie was accepted on bob's host (redirect %q)", loc)
	}
	wantStatus(t, e.do(http.MethodPost, bobHost, "/admin/notes",
		url.Values{"body": {"planted"}}, aliceCookie), http.StatusUnauthorized)

	// And the data layer agrees.
	ctx := context.Background()
	mall, err := e.st.MallByDomain(ctx, "mall1.test")
	if err != nil {
		t.Fatal(err)
	}
	bob, err := e.st.TenantBySubdomain(ctx, mall.ID, "bob")
	if err != nil {
		t.Fatal(err)
	}
	notes, err := e.st.ListNotes(ctx, bob.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 0 {
		t.Fatalf("bob has %d notes, want 0", len(notes))
	}
}

// The same subdomain in two malls is two unrelated shops.
func TestSameSubdomainInTwoMalls(t *testing.T) {
	e := newEnv(t, testConfig())
	m1 := e.mall("mall1.test")
	m2 := e.mall("mall2.test")
	c1 := e.setup("mall1.test", "shoes", "owner@example.com")
	c2 := e.setup("mall2.test", "shoes", "owner@example.com")

	wantStatus(t, e.do(http.MethodPost, "shoes.mall1.test", "/admin/notes",
		url.Values{"body": {"mall1-secret"}}, c1), http.StatusSeeOther)

	rec := e.do(http.MethodGet, "shoes.mall2.test", "/admin", nil, c2)
	wantStatus(t, rec, http.StatusOK)
	if strings.Contains(rec.Body.String(), "mall1-secret") {
		t.Fatal("a shop in mall2 sees a note from the same-named shop in mall1")
	}

	// A session from one mall is rejected on the same-named shop in the other.
	rec = e.do(http.MethodGet, "shoes.mall2.test", "/admin", nil, c1)
	wantStatus(t, rec, http.StatusSeeOther)
	if loc := rec.Header().Get("Location"); loc != "/admin/login" {
		t.Fatalf("cross-mall cookie accepted (redirect %q)", loc)
	}

	ctx := context.Background()
	t1, err := e.st.TenantBySubdomain(ctx, m1.ID, "shoes")
	if err != nil {
		t.Fatal(err)
	}
	t2, err := e.st.TenantBySubdomain(ctx, m2.ID, "shoes")
	if err != nil {
		t.Fatal(err)
	}
	if t1.ID == t2.ID {
		t.Fatal("the two shops share one tenant row")
	}
}

func TestMallDomainRules(t *testing.T) {
	e := newEnv(t, testConfig())
	ctx := context.Background()
	e.mall("mall1.test")
	e.mall("a.b-mall.test")

	cases := []struct {
		domain string
		want   error
	}{
		{"shop.mall1.test", store.ErrDomainOverlap},  // inside an existing mall
		{"b-mall.test", store.ErrDomainOverlap},      // contains an existing mall
		{"MALL1.test", store.ErrDomainTaken},         // duplicate after normalizing
		{"Bad_Domain.test", tenant.ErrInvalidDomain}, // bad characters
		{"localhost", tenant.ErrInvalidDomain},       // single label
	}
	for _, c := range cases {
		_, err := provision.CreateMall(ctx, e.st, c.domain, "x", nil)
		if !errors.Is(err, c.want) {
			t.Errorf("CreateMall(%q) = %v, want %v", c.domain, err, c.want)
		}
	}
}

func TestProvisionRules(t *testing.T) {
	e := newEnv(t, testConfig())
	ctx := context.Background()
	e.mall("mall1.test", "promo")
	create := func(mall, sub, email string) error {
		_, _, err := provision.CreateTenant(ctx, e.st, mall, sub, "Shop", email, time.Hour)
		return err
	}

	cases := []struct {
		name string
		err  error
		want error
	}{
		{"dns-backed name", create("mall1.test", "cpanel", "o@example.com"), tenant.ErrReserved},
		{"per-mall reserved", create("mall1.test", "promo", "o@example.com"), tenant.ErrReserved},
		{"too short", create("mall1.test", "ab", "o@example.com"), tenant.ErrInvalid},
		{"bad characters", create("mall1.test", "bad_name", "o@example.com"), tenant.ErrInvalid},
		{"bad email", create("mall1.test", "alice", "not-an-email"), provision.ErrInvalidEmail},
		{"unknown mall", create("nomall.test", "alice", "o@example.com"), store.ErrNotFound},
	}
	for _, c := range cases {
		if !errors.Is(c.err, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, c.err, c.want)
		}
	}

	if err := create("mall1.test", "alice", "o@example.com"); err != nil {
		t.Fatalf("valid shop rejected: %v", err)
	}
	if err := create("mall1.test", "alice", "o@example.com"); !errors.Is(err, store.ErrSubdomainTaken) {
		t.Fatalf("duplicate: got %v, want ErrSubdomainTaken", err)
	}
}

func TestSuspension(t *testing.T) {
	e := newEnv(t, testConfig())
	ctx := context.Background()
	e.mall("mall1.test")
	e.mall("mall2.test")
	cookie := e.setup("mall1.test", "alice", "owner@example.com")
	e.shop("mall2.test", "bob", "b@example.com")

	if err := provision.SetTenantStatus(ctx, e.st, "mall1.test", "alice", "suspended"); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, e.do(http.MethodGet, "alice.mall1.test", "/", nil), http.StatusServiceUnavailable)
	wantStatus(t, e.do(http.MethodGet, "alice.mall1.test", "/admin", nil, cookie), http.StatusServiceUnavailable)

	if err := provision.SetTenantStatus(ctx, e.st, "mall1.test", "alice", "active"); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, e.do(http.MethodGet, "alice.mall1.test", "/", nil), http.StatusOK)

	// Suspending a mall takes down its directory and every shop in it, only.
	if err := provision.SetMallStatus(ctx, e.st, "mall1.test", "suspended"); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, e.do(http.MethodGet, "mall1.test", "/", nil), http.StatusServiceUnavailable)
	wantStatus(t, e.do(http.MethodGet, "alice.mall1.test", "/", nil), http.StatusServiceUnavailable)
	wantStatus(t, e.do(http.MethodGet, "bob.mall2.test", "/", nil), http.StatusOK)

	if err := provision.SetTenantStatus(ctx, e.st, "mall1.test", "alice", "banned"); !errors.Is(err, provision.ErrInvalidStatus) {
		t.Fatalf("got %v, want ErrInvalidStatus", err)
	}
}

func TestCSRFRejectsForeignOrigin(t *testing.T) {
	e := newEnv(t, testConfig())
	e.mall("mall1.test")
	e.shop("mall1.test", "alice", "owner@example.com")
	form := url.Values{"email": {"owner@example.com"}, "password": {testPass}}

	cases := map[string]map[string]string{
		"foreign origin": {"Origin": "https://evil.example"},
		"null origin":    {"Origin": "null"},
		"no headers":     {},
	}
	for name, headers := range cases {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "https://alice.mall1.test/admin/login", strings.NewReader(form.Encode()))
			req.Host = "alice.mall1.test"
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			for k, v := range headers {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			e.h.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status %d, want 403", rec.Code)
			}
		})
	}
}

func TestLandingDefaultRender(t *testing.T) {
	e := newEnv(t, testConfig())
	e.mall("mall1.test")
	e.shop("mall1.test", "alpha", "a@example.com")

	rec := e.do(http.MethodGet, "mall1.test", "/", nil)
	wantStatus(t, rec, http.StatusOK)
	body := rec.Body.String()

	if !strings.Contains(body, "Mall mall1.test") {
		t.Error("landing page missing mall name")
	}
	if !strings.Contains(body, `id="shops"`) {
		t.Error("landing page missing #shops anchor")
	}
	if !strings.Contains(body, "Shop alpha") {
		t.Error("landing page missing shop name")
	}
	if !strings.Contains(body, "alpha.mall1.test") {
		t.Error("landing page missing shop host")
	}
	if strings.Contains(body, "mailto:") {
		t.Error("landing page shows mailto with no contact set")
	}
}

func TestLandingWithContent(t *testing.T) {
	e := newEnv(t, testConfig())
	e.mall("mall1.test")
	ctx := context.Background()

	if err := provision.SetMallContent(ctx, e.st, "mall1.test",
		"Great deals every day", "We bring the best shops together.", "hello@mall1.test"); err != nil {
		t.Fatalf("SetMallContent: %v", err)
	}

	rec := e.do(http.MethodGet, "mall1.test", "/", nil)
	wantStatus(t, rec, http.StatusOK)
	body := rec.Body.String()

	for _, want := range []string{
		"Great deals every day",
		"We bring the best shops together.",
		"mailto:hello@mall1.test",
		"hello@mall1.test",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("landing page missing %q", want)
		}
	}
}

func TestLandingXSSEscaping(t *testing.T) {
	e := newEnv(t, testConfig())
	e.mall("mall1.test")
	ctx := context.Background()

	xss := "<script>alert(1)</script>"
	if err := provision.SetMallContent(ctx, e.st, "mall1.test", xss, xss, ""); err != nil {
		t.Fatalf("SetMallContent: %v", err)
	}

	rec := e.do(http.MethodGet, "mall1.test", "/", nil)
	body := rec.Body.String()
	if strings.Contains(body, "<script>") {
		t.Error("landing page did not escape <script> in tagline/description")
	}
}

func TestLandingEmptyMall(t *testing.T) {
	e := newEnv(t, testConfig())
	e.mall("mall1.test")

	rec := e.do(http.MethodGet, "mall1.test", "/", nil)
	wantStatus(t, rec, http.StatusOK)
	body := rec.Body.String()
	if !strings.Contains(body, "Opening soon") {
		t.Error("empty mall landing page missing opening-soon message")
	}
}

func TestLandingValidation(t *testing.T) {
	e := newEnv(t, testConfig())
	e.mall("mall1.test")
	ctx := context.Background()
	set := func(tagline, description, contact string) error {
		return provision.SetMallContent(ctx, e.st, "mall1.test", tagline, description, contact)
	}

	longTagline := strings.Repeat("a", 141)
	if err := set(longTagline, "", ""); !errors.Is(err, provision.ErrInvalidContent) {
		t.Errorf("tagline too long: got %v, want ErrInvalidContent", err)
	}

	longDesc := strings.Repeat("a", 601)
	if err := set("", longDesc, ""); !errors.Is(err, provision.ErrInvalidContent) {
		t.Errorf("description too long: got %v, want ErrInvalidContent", err)
	}

	if err := set("", "", "not-an-email"); !errors.Is(err, provision.ErrInvalidEmail) {
		t.Errorf("invalid contact: got %v, want ErrInvalidEmail", err)
	}

	if err := provision.SetMallContent(ctx, e.st, "nobody.test", "", "", ""); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown mall: got %v, want ErrNotFound", err)
	}
}

func TestLandingCSPHeader(t *testing.T) {
	e := newEnv(t, testConfig())
	e.mall("mall1.test")

	rec := e.do(http.MethodGet, "mall1.test", "/", nil)
	wantStatus(t, rec, http.StatusOK)
	got := rec.Header().Get("Content-Security-Policy")
	want := "default-src 'none'; style-src 'unsafe-inline' https://fonts.googleapis.com; font-src https://fonts.gstatic.com; base-uri 'none'; frame-ancestors 'none'"
	if got != want {
		t.Errorf("CSP = %q, want %q", got, want)
	}
}

func TestLoginIsRateLimited(t *testing.T) {
	cfg := testConfig()
	cfg.LoginLimit = 2
	e := newEnv(t, cfg)
	e.mall("mall1.test")
	e.shop("mall1.test", "alice", "owner@example.com")

	form := url.Values{"email": {"nobody@example.com"}, "password": {testPass}}
	want := []int{http.StatusUnauthorized, http.StatusUnauthorized, http.StatusTooManyRequests}
	for i, code := range want {
		rec := e.do(http.MethodPost, "alice.mall1.test", "/admin/login", form)
		if rec.Code != code {
			t.Fatalf("attempt %d: status %d, want %d", i+1, rec.Code, code)
		}
	}
}

// ---- catalog / storefront tests ----

func (e *testEnv) createProduct(ctx context.Context, t *testing.T, tenantID int64, name, desc string, price int64, stock int) store.Product {
	t.Helper()
	p, err := e.st.CreateProduct(ctx, tenantID, name, desc, price, stock, nil)
	if err != nil {
		t.Fatalf("create product %q: %v", name, err)
	}
	return p
}

func (e *testEnv) publishProduct(ctx context.Context, t *testing.T, tenantID, productID int64) {
	t.Helper()
	if err := e.st.SetProductStatus(ctx, tenantID, productID, store.ProductPublished); err != nil {
		t.Fatalf("publish product %d: %v", productID, err)
	}
}

func TestStorefrontShowsPublishedProducts(t *testing.T) {
	e := newEnv(t, testConfig())
	e.mall("mall1.test")
	e.setup("mall1.test", "alice", "owner@example.com")
	ctx := context.Background()
	m, _ := e.st.MallByDomain(ctx, "mall1.test")
	alice, _ := e.st.TenantBySubdomain(ctx, m.ID, "alice")

	pub := e.createProduct(ctx, t, alice.ID, "Widget", "a widget", 1000, 5)
	e.publishProduct(ctx, t, alice.ID, pub.ID)
	draft := e.createProduct(ctx, t, alice.ID, "Secret Draft", "hidden", 500, 1)
	_ = draft

	rec := e.do(http.MethodGet, "alice.mall1.test", "/", nil)
	wantStatus(t, rec, http.StatusOK)
	body := rec.Body.String()
	if !strings.Contains(body, "Widget") {
		t.Error("storefront missing published product")
	}
	if strings.Contains(body, "Secret Draft") {
		t.Error("storefront leaked draft product")
	}
}

func TestStorefrontNoindexOnFilter(t *testing.T) {
	e := newEnv(t, testConfig())
	e.mall("mall1.test")
	e.setup("mall1.test", "alice", "owner@example.com")

	rec := e.do(http.MethodGet, "alice.mall1.test", "/?q=test", nil)
	wantStatus(t, rec, http.StatusOK)
	if !strings.Contains(rec.Body.String(), "noindex") {
		t.Error("filtered storefront missing noindex")
	}

	rec = e.do(http.MethodGet, "alice.mall1.test", "/", nil)
	if strings.Contains(rec.Body.String(), "noindex") {
		t.Error("unfiltered storefront should not have noindex")
	}
}

func TestStorefrontTenantIsolation(t *testing.T) {
	e := newEnv(t, testConfig())
	e.mall("mall1.test")
	e.setup("mall1.test", "alice", "owner@example.com")
	e.setup("mall1.test", "bob", "bob@example.com")
	ctx := context.Background()
	m, _ := e.st.MallByDomain(ctx, "mall1.test")
	alice, _ := e.st.TenantBySubdomain(ctx, m.ID, "alice")
	bob, _ := e.st.TenantBySubdomain(ctx, m.ID, "bob")

	aliceProd := e.createProduct(ctx, t, alice.ID, "AliceProduct", "", 1000, 1)
	e.publishProduct(ctx, t, alice.ID, aliceProd.ID)
	bobProd := e.createProduct(ctx, t, bob.ID, "BobProduct", "", 2000, 1)
	e.publishProduct(ctx, t, bob.ID, bobProd.ID)

	rec := e.do(http.MethodGet, "alice.mall1.test", "/", nil)
	body := rec.Body.String()
	if strings.Contains(body, "BobProduct") {
		t.Error("alice's storefront leaked bob's product")
	}
	if !strings.Contains(body, "AliceProduct") {
		t.Error("alice's storefront missing alice's product")
	}
}

func TestProductDetailPublished(t *testing.T) {
	e := newEnv(t, testConfig())
	e.mall("mall1.test")
	e.setup("mall1.test", "alice", "owner@example.com")
	ctx := context.Background()
	m, _ := e.st.MallByDomain(ctx, "mall1.test")
	alice, _ := e.st.TenantBySubdomain(ctx, m.ID, "alice")

	p := e.createProduct(ctx, t, alice.ID, "Blue Widget", "A blue widget description.", 2500, 3)
	e.publishProduct(ctx, t, alice.ID, p.ID)

	rec := e.do(http.MethodGet, "alice.mall1.test", fmt.Sprintf("/shop/%d", p.ID), nil)
	wantStatus(t, rec, http.StatusOK)
	body := rec.Body.String()
	if !strings.Contains(body, "Blue Widget") {
		t.Error("product page missing name")
	}
	if !strings.Contains(body, "blue widget description") {
		t.Error("product page missing description")
	}
}

func TestProductDetailDraftReturns404(t *testing.T) {
	e := newEnv(t, testConfig())
	e.mall("mall1.test")
	e.setup("mall1.test", "alice", "owner@example.com")
	ctx := context.Background()
	m, _ := e.st.MallByDomain(ctx, "mall1.test")
	alice, _ := e.st.TenantBySubdomain(ctx, m.ID, "alice")

	p := e.createProduct(ctx, t, alice.ID, "Hidden Draft", "", 1000, 0)
	// not published
	rec := e.do(http.MethodGet, "alice.mall1.test", fmt.Sprintf("/shop/%d", p.ID), nil)
	wantStatus(t, rec, http.StatusNotFound)
}

func TestProductDetailTenantIsolation(t *testing.T) {
	e := newEnv(t, testConfig())
	e.mall("mall1.test")
	e.setup("mall1.test", "alice", "owner@example.com")
	e.setup("mall1.test", "bob", "bob@example.com")
	ctx := context.Background()
	m, _ := e.st.MallByDomain(ctx, "mall1.test")
	_, _ = e.st.TenantBySubdomain(ctx, m.ID, "alice")
	bob, _ := e.st.TenantBySubdomain(ctx, m.ID, "bob")

	bobProd := e.createProduct(ctx, t, bob.ID, "BobSecret", "", 1000, 1)
	e.publishProduct(ctx, t, bob.ID, bobProd.ID)

	// Alice's shop cannot see Bob's product by ID
	rec := e.do(http.MethodGet, "alice.mall1.test", fmt.Sprintf("/shop/%d", bobProd.ID), nil)
	wantStatus(t, rec, http.StatusNotFound)
}

func TestAdminProductsRequireLogin(t *testing.T) {
	e := newEnv(t, testConfig())
	e.mall("mall1.test")
	e.shop("mall1.test", "alice", "owner@example.com")
	host := "alice.mall1.test"

	for _, path := range []string{
		"/admin/products",
		"/admin/products/new",
		"/admin/categories",
		"/admin/settings",
	} {
		rec := e.do(http.MethodGet, host, path, nil)
		if rec.Code != http.StatusSeeOther {
			t.Errorf("GET %s unauthenticated: status %d, want redirect", path, rec.Code)
		}
	}
}

func TestAdminCategoryCRUD(t *testing.T) {
	e := newEnv(t, testConfig())
	e.mall("mall1.test")
	cookie := e.setup("mall1.test", "alice", "owner@example.com")
	host := "alice.mall1.test"

	// Create
	rec := e.do(http.MethodPost, host, "/admin/categories",
		url.Values{"name": {"Shoes"}}, cookie)
	wantStatus(t, rec, http.StatusSeeOther)

	rec = e.do(http.MethodGet, host, "/admin/categories", nil, cookie)
	if !strings.Contains(rec.Body.String(), "Shoes") {
		t.Fatal("category not visible after creation")
	}

	// Duplicate slug is rejected
	rec = e.do(http.MethodPost, host, "/admin/categories",
		url.Values{"name": {"Shoes"}}, cookie)
	wantStatus(t, rec, http.StatusConflict)

	// Rename and delete via path — need the ID
	ctx := context.Background()
	m, _ := e.st.MallByDomain(ctx, "mall1.test")
	alice, _ := e.st.TenantBySubdomain(ctx, m.ID, "alice")
	cats, _ := e.st.ListCategories(ctx, alice.ID)
	if len(cats) == 0 {
		t.Fatal("no categories found")
	}
	id := cats[0].ID

	rec = e.do(http.MethodPost, host, fmt.Sprintf("/admin/categories/%d/rename", id),
		url.Values{"name": {"Footwear"}}, cookie)
	wantStatus(t, rec, http.StatusSeeOther)

	rec = e.do(http.MethodPost, host, fmt.Sprintf("/admin/categories/%d/delete", id), nil, cookie)
	wantStatus(t, rec, http.StatusSeeOther)

	cats, _ = e.st.ListCategories(ctx, alice.ID)
	if len(cats) != 0 {
		t.Error("category not deleted")
	}
}

func TestAdminProductCRUD(t *testing.T) {
	e := newEnv(t, testConfig())
	e.mall("mall1.test")
	cookie := e.setup("mall1.test", "alice", "owner@example.com")
	host := "alice.mall1.test"

	// Create a product
	rec := e.do(http.MethodPost, host, "/admin/products/new",
		url.Values{
			"name":        {"Test Widget"},
			"description": {"A test widget."},
			"price":       {"29.99"},
			"stock":       {"10"},
			"category_id": {""},
		}, cookie)
	wantStatus(t, rec, http.StatusSeeOther)

	// Find the product
	ctx := context.Background()
	m, _ := e.st.MallByDomain(ctx, "mall1.test")
	alice, _ := e.st.TenantBySubdomain(ctx, m.ID, "alice")
	products, total, _ := e.st.ListProductsAdmin(ctx, alice.ID, "", 1)
	if total != 1 {
		t.Fatalf("expected 1 product, got %d", total)
	}
	p := products[0]
	if p.Name != "Test Widget" {
		t.Errorf("product name = %q, want 'Test Widget'", p.Name)
	}
	if p.PriceMinor != 2999 {
		t.Errorf("product price_minor = %d, want 2999", p.PriceMinor)
	}
	if p.Status != store.ProductDraft {
		t.Errorf("new product status = %q, want draft", p.Status)
	}

	// Publish it
	rec = e.do(http.MethodPost, host, fmt.Sprintf("/admin/products/%d/status", p.ID),
		url.Values{"status": {"published"}}, cookie)
	wantStatus(t, rec, http.StatusSeeOther)

	// Verify it's public on the storefront
	rec = e.do(http.MethodGet, host, "/", nil)
	if !strings.Contains(rec.Body.String(), "Test Widget") {
		t.Error("published product not visible on storefront")
	}

	// Archive it
	rec = e.do(http.MethodPost, host, fmt.Sprintf("/admin/products/%d/status", p.ID),
		url.Values{"status": {"archived"}}, cookie)
	wantStatus(t, rec, http.StatusSeeOther)

	// No longer on storefront
	rec = e.do(http.MethodGet, host, "/", nil)
	if strings.Contains(rec.Body.String(), "Test Widget") {
		t.Error("archived product still visible on storefront")
	}
}

func TestAdminSettingsOwnerOnly(t *testing.T) {
	e := newEnv(t, testConfig())
	e.mall("mall1.test")
	ownerCookie := e.setup("mall1.test", "alice", "owner@example.com")
	host := "alice.mall1.test"

	// Owner can access settings
	rec := e.do(http.MethodGet, host, "/admin/settings", nil, ownerCookie)
	wantStatus(t, rec, http.StatusOK)

	// Unauthenticated is redirected
	rec = e.do(http.MethodGet, host, "/admin/settings", nil)
	wantStatus(t, rec, http.StatusSeeOther)
}

func TestStorefrontXSSEscaping(t *testing.T) {
	e := newEnv(t, testConfig())
	e.mall("mall1.test")
	e.setup("mall1.test", "alice", "owner@example.com")
	ctx := context.Background()
	m, _ := e.st.MallByDomain(ctx, "mall1.test")
	alice, _ := e.st.TenantBySubdomain(ctx, m.ID, "alice")

	xss := `<script>alert(1)</script>`
	p := e.createProduct(ctx, t, alice.ID, xss, xss, 100, 1)
	e.publishProduct(ctx, t, alice.ID, p.ID)

	for _, path := range []string{"/", fmt.Sprintf("/shop/%d", p.ID)} {
		rec := e.do(http.MethodGet, "alice.mall1.test", path, nil)
		if strings.Contains(rec.Body.String(), "<script>") {
			t.Errorf("GET %s: XSS not escaped", path)
		}
	}
}

func TestStorefrontSearchWithSpecialChars(t *testing.T) {
	e := newEnv(t, testConfig())
	e.mall("mall1.test")
	e.setup("mall1.test", "alice", "owner@example.com")

	// ILIKE special chars in search should not cause errors
	rec := e.do(http.MethodGet, "alice.mall1.test", "/?q=100%25+off", nil)
	wantStatus(t, rec, http.StatusOK)
	rec = e.do(http.MethodGet, "alice.mall1.test", "/?q=_sale_", nil)
	wantStatus(t, rec, http.StatusOK)
}

func TestAdminProductInvalidPrice(t *testing.T) {
	e := newEnv(t, testConfig())
	e.mall("mall1.test")
	cookie := e.setup("mall1.test", "alice", "owner@example.com")
	host := "alice.mall1.test"

	bad := url.Values{"name": {"Widget"}, "price": {"not-a-price"}, "stock": {"1"}}
	rec := e.do(http.MethodPost, host, "/admin/products/new", bad, cookie)
	wantStatus(t, rec, http.StatusBadRequest)
}

func TestAdminStatusChangeIsTenantBound(t *testing.T) {
	e := newEnv(t, testConfig())
	e.mall("mall1.test")
	aliceCookie := e.setup("mall1.test", "alice", "owner@example.com")
	bobCookie := e.setup("mall1.test", "bob", "bob@example.com")
	ctx := context.Background()
	m, _ := e.st.MallByDomain(ctx, "mall1.test")
	bob, _ := e.st.TenantBySubdomain(ctx, m.ID, "bob")

	bobProd := e.createProduct(ctx, t, bob.ID, "BobItem", "", 100, 1)

	// Alice cannot change status on Bob's product
	rec := e.do(http.MethodPost, "alice.mall1.test",
		fmt.Sprintf("/admin/products/%d/status", bobProd.ID),
		url.Values{"status": {"published"}}, aliceCookie)
	// 404 because the product doesn't exist in alice's tenant scope
	wantStatus(t, rec, http.StatusNotFound)

	// Bob's product stays draft
	p, _ := e.st.GetProductAdmin(ctx, bob.ID, bobProd.ID)
	if p.Status != store.ProductDraft {
		t.Errorf("bob's product status changed by alice: %q", p.Status)
	}
	_ = bobCookie
}

func TestDashboardStats(t *testing.T) {
	e := newEnv(t, testConfig())
	e.mall("mall1.test")
	cookie := e.setup("mall1.test", "alice", "owner@example.com")
	host := "alice.mall1.test"
	ctx := context.Background()
	m, _ := e.st.MallByDomain(ctx, "mall1.test")
	alice, _ := e.st.TenantBySubdomain(ctx, m.ID, "alice")

	if _, err := e.st.CreateCategory(ctx, alice.ID, "Shoes"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []struct {
		name    string
		stock   int
		publish bool
	}{{"Plenty", 50, true}, {"Few", 3, true}, {"None", 0, true}, {"Hidden", 2, false}} {
		prod, err := e.st.CreateProduct(ctx, alice.ID, p.name, "", 100, p.stock, nil)
		if err != nil {
			t.Fatal(err)
		}
		if p.publish {
			if err := e.st.SetProductStatus(ctx, alice.ID, prod.ID, store.ProductPublished); err != nil {
				t.Fatal(err)
			}
		}
	}

	got, err := e.st.DashboardStats(ctx, alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := store.DashboardStats{Published: 3, LowStock: 1, OutOfStock: 1, Categories: 1}
	if got != want {
		t.Errorf("stats = %+v, want %+v", got, want)
	}

	rec := e.do(http.MethodGet, host, "/admin", nil, cookie)
	wantStatus(t, rec, http.StatusOK)
	if !strings.Contains(rec.Body.String(), `<span class="stat-n">3</span><span class="stat-l">Published products</span>`) {
		t.Error("dashboard does not show the published count")
	}
}

func TestShopAppearance(t *testing.T) {
	e := newEnv(t, testConfig())
	e.mall("mall1.test")
	owner := e.setup("mall1.test", "alice", "owner@example.com")
	host := "alice.mall1.test"
	ctx := context.Background()

	rec := e.do(http.MethodGet, host, "/admin/appearance", nil, owner)
	wantStatus(t, rec, http.StatusOK)
	if body := rec.Body.String(); !strings.Contains(body, `value="classic" checked`) || !strings.Contains(body, `value="forest" checked`) {
		t.Error("appearance page should show the classic forest defaults")
	}
	wantStatus(t, e.do(http.MethodGet, host, "/admin/appearance", nil), http.StatusSeeOther)

	look := func(style, palette, theme string) url.Values {
		return url.Values{"style": {style}, "palette": {palette}, "theme": {theme}}
	}
	wantStatus(t, e.do(http.MethodPost, host, "/admin/appearance", look("boutique", "ocean", "dark")), http.StatusUnauthorized)
	wantStatus(t, e.do(http.MethodPost, host, "/admin/appearance", look("gallery", "ocean", "dark"), owner), http.StatusBadRequest)
	wantStatus(t, e.do(http.MethodPost, host, "/admin/appearance", look("boutique", "ocean", "dark"), owner), http.StatusSeeOther)
	body := e.do(http.MethodGet, host, "/", nil).Body.String()
	if !strings.Contains(body, `data-palette="ocean" data-style="boutique" data-theme="dark"`) {
		t.Error("storefront should use the saved appearance")
	}

	// Locks block new choices but keep the shop's current ones.
	if err := provision.SetMallLocks(ctx, e.st, "mall1.test", []string{"boutique", "market"}, []string{"ocean", "violet"}); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, e.do(http.MethodPost, host, "/admin/appearance", look("market", "ocean", "auto"), owner), http.StatusBadRequest)
	wantStatus(t, e.do(http.MethodPost, host, "/admin/appearance", look("boutique", "violet", "auto"), owner), http.StatusBadRequest)
	wantStatus(t, e.do(http.MethodPost, host, "/admin/appearance", look("boutique", "ocean", "light"), owner), http.StatusSeeOther)
	page := e.do(http.MethodGet, host, "/admin/appearance", nil, owner).Body.String()
	if !strings.Contains(page, `value="market" disabled`) || strings.Contains(page, `value="boutique" checked disabled`) {
		t.Error("only locked options the shop doesn't use should be disabled")
	}
	body = e.do(http.MethodGet, host, "/", nil).Body.String()
	if !strings.Contains(body, `data-palette="ocean" data-style="boutique" data-theme="light"`) {
		t.Error("the shop should keep its locked layout and palette and switch to light mode")
	}
}

func TestMallLocksRules(t *testing.T) {
	e := newEnv(t, testConfig())
	e.mall("mall1.test")
	ctx := context.Background()
	if err := provision.SetMallLocks(ctx, e.st, "mall1.test", []string{"classic"}, nil); !errors.Is(err, provision.ErrInvalidStyle) {
		t.Errorf("locking classic: got %v", err)
	}
	if err := provision.SetMallLocks(ctx, e.st, "mall1.test", nil, provision.Palettes); !errors.Is(err, provision.ErrAllPalettesLocked) {
		t.Errorf("locking every palette: got %v", err)
	}
	if err := provision.SetMallLocks(ctx, e.st, "mall1.test", nil, nil); err != nil {
		t.Errorf("clearing locks: %v", err)
	}
	if err := provision.SetMallLook(ctx, e.st, "mall1.test", "", "ocean"); err != nil {
		t.Fatal(err)
	}
	body := e.do(http.MethodGet, "mall1.test", "/", nil).Body.String()
	if !strings.Contains(body, `data-palette="ocean" data-style="classic"`) {
		t.Error("mall landing should use its palette and keep its layout")
	}
}

func TestShopBlog(t *testing.T) {
	e := newEnv(t, testConfig())
	e.mall("mall1.test")
	owner := e.setup("mall1.test", "alice", "owner@example.com")
	e.setup("mall1.test", "bob", "bob@example.com")
	host := "alice.mall1.test"
	ctx := context.Background()
	m, _ := e.st.MallByDomain(ctx, "mall1.test")
	alice, _ := e.st.TenantBySubdomain(ctx, m.ID, "alice")
	basket := e.createProduct(ctx, t, alice.ID, "Kiondo Basket", "woven", 250000, 3)
	e.publishProduct(ctx, t, alice.ID, basket.ID)

	if strings.Contains(e.do(http.MethodGet, host, "/", nil).Body.String(), `href="/blog"`) {
		t.Error("storefront should not link to an empty blog")
	}
	wantStatus(t, e.do(http.MethodPost, host, "/admin/blog/new", url.Values{"title": {"x"}}), http.StatusUnauthorized)
	wantStatus(t, e.do(http.MethodPost, host, "/admin/blog/new", url.Values{"title": {""}}, owner), http.StatusBadRequest)

	body := fmt.Sprintf("Three days per basket.\n\n## Why it takes time\n\nSee /shop/%d and <b>this</b>.", basket.ID)
	rec := e.do(http.MethodPost, host, "/admin/blog/new", url.Values{
		"title": {"How a kiondo is made"}, "excerpt": {"From sisal to basket."},
		"body": {body}, "category": {"Behind the craft"}, "action": {"draft"},
	}, owner)
	wantStatus(t, rec, http.StatusSeeOther)
	loc := rec.Header().Get("Location")
	if !strings.HasSuffix(loc, "?saved=draft") {
		t.Fatalf("draft redirect = %q", loc)
	}
	path := strings.TrimSuffix(loc, "?saved=draft")
	id := strings.TrimPrefix(path, "/admin/blog/")

	wantStatus(t, e.do(http.MethodGet, host, "/blog/"+id, nil), http.StatusNotFound)
	wantStatus(t, e.do(http.MethodGet, "bob.mall1.test", "/blog/"+id, nil), http.StatusNotFound)

	rec = e.do(http.MethodPost, host, path, url.Values{
		"title": {"How a kiondo is made"}, "excerpt": {"From sisal to basket."},
		"body": {body}, "category": {"Behind the craft"}, "action": {"publish"},
	}, owner)
	wantStatus(t, rec, http.StatusSeeOther)

	rec = e.do(http.MethodGet, host, "/blog/"+id, nil)
	wantStatus(t, rec, http.StatusOK)
	page := rec.Body.String()
	for _, want := range []string{"<h2>Why it takes time</h2>", "&lt;b&gt;this&lt;/b&gt;", "Mentioned in this post", "Kiondo Basket", "Behind the craft"} {
		if !strings.Contains(page, want) {
			t.Errorf("post page missing %q", want)
		}
	}
	if !strings.Contains(e.do(http.MethodGet, host, "/blog", nil).Body.String(), "How a kiondo is made") {
		t.Error("blog index missing the published post")
	}
	if !strings.Contains(e.do(http.MethodGet, host, "/", nil).Body.String(), `href="/blog"`) {
		t.Error("storefront should link to the blog once a post is published")
	}
	if strings.Contains(e.do(http.MethodGet, "bob.mall1.test", "/blog", nil).Body.String(), "kiondo") {
		t.Error("another shop's blog leaked the post")
	}

	wantStatus(t, e.do(http.MethodPost, "bob.mall1.test", path+"/delete", nil, e.login("bob.mall1.test", "bob@example.com")), http.StatusNotFound)
	wantStatus(t, e.do(http.MethodPost, host, path+"/delete", nil, owner), http.StatusSeeOther)
	wantStatus(t, e.do(http.MethodGet, host, "/blog/"+id, nil), http.StatusNotFound)
}

func TestMallLayouts(t *testing.T) {
	e := newEnv(t, testConfig())
	e.mall("mall1.test")
	ctx := context.Background()
	for _, s := range [][2]string{{"alpha", "Alpha Books"}, {"bravo", "Bravo Shoes"}, {"beta", "Beta Tea"}} {
		if _, _, err := provision.CreateTenant(ctx, e.st, "mall1.test", s[0], s[1], s[0]+"@example.com", time.Hour); err != nil {
			t.Fatal(err)
		}
	}

	body := e.do(http.MethodGet, "mall1.test", "/", nil).Body.String()
	if !strings.Contains(body, `data-style="classic"`) {
		t.Error("new mall should use the classic layout")
	}

	if err := provision.SetMallLook(ctx, e.st, "mall1.test", "gallery", ""); !errors.Is(err, provision.ErrInvalidStyle) {
		t.Errorf("unknown style: got %v, want ErrInvalidStyle", err)
	}

	if err := provision.SetMallLook(ctx, e.st, "mall1.test", "editorial", ""); err != nil {
		t.Fatal(err)
	}
	body = e.do(http.MethodGet, "mall1.test", "/", nil).Body.String()
	for _, want := range []string{`data-style="editorial"`, "Every shop", "family=Newsreader", "Bravo Shoes"} {
		if !strings.Contains(body, want) {
			t.Errorf("editorial landing missing %q", want)
		}
	}

	if err := provision.SetMallLook(ctx, e.st, "mall1.test", "directory", ""); err != nil {
		t.Fatal(err)
	}
	body = e.do(http.MethodGet, "mall1.test", "/", nil).Body.String()
	for _, want := range []string{`id="letter-A"`, `id="letter-B"`, "Find a shop in", "3 shops"} {
		if !strings.Contains(body, want) {
			t.Errorf("directory landing missing %q", want)
		}
	}
	if strings.Contains(body, "family=Newsreader") {
		t.Error("directory landing should not load the serif font")
	}

	body = e.do(http.MethodGet, "mall1.test", "/?q=BRAVO", nil).Body.String()
	if !strings.Contains(body, "Bravo Shoes") || strings.Contains(body, "Beta Tea") || strings.Contains(body, "Alpha Books") {
		t.Error("directory search should keep only matching shops")
	}
	if !strings.Contains(body, "noindex") {
		t.Error("directory search results should be noindex")
	}

	body = e.do(http.MethodGet, "mall1.test", "/?q=%3Cscript%3E", nil).Body.String()
	if !strings.Contains(body, "No shops match &ldquo;&lt;script&gt;&rdquo;") {
		t.Error("empty directory search should echo the escaped query")
	}
}

func TestShopLayouts(t *testing.T) {
	e := newEnv(t, testConfig())
	e.mall("mall1.test")
	e.setup("mall1.test", "alice", "owner@example.com")
	ctx := context.Background()
	m, _ := e.st.MallByDomain(ctx, "mall1.test")
	alice, _ := e.st.TenantBySubdomain(ctx, m.ID, "alice")
	p := e.createProduct(ctx, t, alice.ID, "Kiondo Basket", "woven", 250000, 3)
	e.publishProduct(ctx, t, alice.ID, p.ID)

	if err := provision.SetTenantStyle(ctx, e.st, "mall1.test", "alice", "editorial"); !errors.Is(err, provision.ErrInvalidStyle) {
		t.Errorf("mall-only style on a shop: got %v, want ErrInvalidStyle", err)
	}

	cases := []struct {
		style string
		want  []string
	}{
		{"classic", []string{`data-style="classic"`, `class="filter-form"`}},
		{"boutique", []string{`data-style="boutique"`, `class="b-hero"`, `class="b-tile"`, "family=Newsreader"}},
		{"market", []string{`data-style="market"`, `class="m-search"`, `class="m-grid"`}},
	}
	for _, c := range cases {
		if err := provision.SetTenantStyle(ctx, e.st, "mall1.test", "alice", c.style); err != nil {
			t.Fatal(err)
		}
		rec := e.do(http.MethodGet, "alice.mall1.test", "/", nil)
		wantStatus(t, rec, http.StatusOK)
		body := rec.Body.String()
		for _, w := range c.want {
			if !strings.Contains(body, w) {
				t.Errorf("%s storefront missing %q", c.style, w)
			}
		}
		if !strings.Contains(body, "Kiondo Basket") {
			t.Errorf("%s storefront missing the product", c.style)
		}
		rec = e.do(http.MethodGet, "alice.mall1.test", fmt.Sprintf("/shop/%d", p.ID), nil)
		wantStatus(t, rec, http.StatusOK)
		if !strings.Contains(rec.Body.String(), `data-style="`+c.style+`"`) {
			t.Errorf("%s product page missing its layout style", c.style)
		}
	}
}
