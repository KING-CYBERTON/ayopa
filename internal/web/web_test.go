package web

import (
	"context"
	"errors"
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
		"",
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
