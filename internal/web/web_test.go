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
	"github.com/KING-CYBERTON/ayopa/internal/store"
	"github.com/KING-CYBERTON/ayopa/migrations"
)

const (
	testBase = "example.test"
	testPass = "a-long-password-1"
)

func testConfig() Config {
	return Config{
		BaseDomain:   testBase,
		Scheme:       "https",
		CookieSecure: true,
		SessionTTL:   time.Hour,
		SignupLimit:  1000,
		SignupWindow: time.Hour,
		LoginLimit:   1000,
		LoginWindow:  time.Hour,
	}
}

// newTestServer connects to TEST_DATABASE_URL, applies the real migrations
// and empties the tables, so every test starts from a clean database.
func newTestServer(t *testing.T, cfg Config) (*Server, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
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
	if _, err := pool.Exec(ctx, `TRUNCATE tenants RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(cfg, store.New(pool), log), pool
}

func request(t *testing.T, h http.Handler, method, host, path string, form url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
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
	h.ServeHTTP(rec, req)
	return rec
}

func signupForm(sub, email, pw string) url.Values {
	return url.Values{"subdomain": {sub}, "email": {email}, "password": {pw}}
}

func mustSignup(t *testing.T, h http.Handler, sub, email string) {
	t.Helper()
	rec := request(t, h, http.MethodPost, testBase, "/signup", signupForm(sub, email, testPass))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("signup %s: status %d, body: %s", sub, rec.Code, rec.Body.String())
	}
}

func mustLogin(t *testing.T, h http.Handler, sub, email string) *http.Cookie {
	t.Helper()
	rec := request(t, h, http.MethodPost, sub+"."+testBase, "/login",
		url.Values{"email": {email}, "password": {testPass}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("login %s: status %d", sub, rec.Code)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == "__Host-session" {
			return c
		}
	}
	t.Fatalf("login %s: no session cookie", sub)
	return nil
}

func TestHealthz(t *testing.T) {
	srv, _ := newTestServer(t, testConfig())
	rec := request(t, srv.Handler(), http.MethodGet, testBase, "/healthz", nil)
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "ok" {
		t.Fatalf("status %d body %q", rec.Code, rec.Body.String())
	}
}

func TestMigrationsAreIdempotent(t *testing.T) {
	_, pool := newTestServer(t, testConfig())
	applied, err := db.Migrate(context.Background(), pool, migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 0 {
		t.Fatalf("second run applied %v, want nothing", applied)
	}
}

func TestSignupCreatesTenant(t *testing.T) {
	srv, _ := newTestServer(t, testConfig())
	h := srv.Handler()

	rec := request(t, h, http.MethodPost, testBase, "/signup", signupForm("alice", "owner@example.com", testPass))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "https://alice."+testBase+"/login") {
		t.Fatalf("redirect %q", loc)
	}
	if _, err := srv.store.TenantBySubdomain(context.Background(), "alice"); err != nil {
		t.Fatalf("tenant not created: %v", err)
	}
	if rec := request(t, h, http.MethodGet, "alice."+testBase, "/login", nil); rec.Code != http.StatusOK {
		t.Fatalf("login page status %d", rec.Code)
	}
}

func TestSignupRejectsBadInput(t *testing.T) {
	srv, _ := newTestServer(t, testConfig())
	h := srv.Handler()

	tests := []struct {
		name string
		sub  string
		mail string
		pw   string
	}{
		{"reserved cpanel", "cpanel", "a@example.com", testPass},
		{"reserved autodiscover", "autodiscover", "a@example.com", testPass},
		{"reserved www", "www", "a@example.com", testPass},
		{"reserved test", "test", "a@example.com", testPass},
		{"too short", "ab", "a@example.com", testPass},
		{"leading hyphen", "-alice", "a@example.com", testPass},
		{"bad email", "carol", "not-an-email", testPass},
		{"display name email", "carol", "Carol <c@example.com>", testPass},
		{"short password", "carol", "c@example.com", "short"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := request(t, h, http.MethodPost, testBase, "/signup", signupForm(tt.sub, tt.mail, tt.pw))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status %d, want 400", rec.Code)
			}
		})
	}
	if _, err := srv.store.TenantBySubdomain(context.Background(), "carol"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("a rejected signup created a tenant: %v", err)
	}
}

func TestSignupDuplicateSubdomain(t *testing.T) {
	srv, _ := newTestServer(t, testConfig())
	h := srv.Handler()

	mustSignup(t, h, "alice", "one@example.com")
	rec := request(t, h, http.MethodPost, testBase, "/signup", signupForm("alice", "two@example.com", testPass))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status %d, want 409", rec.Code)
	}
}

func TestLoginAndSessionCookie(t *testing.T) {
	srv, _ := newTestServer(t, testConfig())
	h := srv.Handler()
	host := "alice." + testBase
	mustSignup(t, h, "alice", "owner@example.com")

	bad := request(t, h, http.MethodPost, host, "/login",
		url.Values{"email": {"owner@example.com"}, "password": {"wrong-password-1"}})
	if bad.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password: status %d", bad.Code)
	}
	unknown := request(t, h, http.MethodPost, host, "/login",
		url.Values{"email": {"nobody@example.com"}, "password": {testPass}})
	if unknown.Code != http.StatusUnauthorized {
		t.Fatalf("unknown email: status %d", unknown.Code)
	}

	cookie := mustLogin(t, h, "alice", "owner@example.com")
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("cookie flags: httponly=%v secure=%v samesite=%v", cookie.HttpOnly, cookie.Secure, cookie.SameSite)
	}
	if cookie.Domain != "" || cookie.Path != "/" {
		t.Errorf("cookie must be host-only: domain=%q path=%q", cookie.Domain, cookie.Path)
	}

	if rec := request(t, h, http.MethodGet, host, "/dashboard", nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("dashboard without session: status %d", rec.Code)
	}
	rec := request(t, h, http.MethodGet, host, "/dashboard", nil, cookie)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "owner@example.com") {
		t.Fatalf("dashboard with session: status %d", rec.Code)
	}
}

func TestLogoutInvalidatesSession(t *testing.T) {
	srv, _ := newTestServer(t, testConfig())
	h := srv.Handler()
	host := "alice." + testBase
	mustSignup(t, h, "alice", "owner@example.com")
	cookie := mustLogin(t, h, "alice", "owner@example.com")

	if rec := request(t, h, http.MethodPost, host, "/logout", nil, cookie); rec.Code != http.StatusSeeOther {
		t.Fatalf("logout status %d", rec.Code)
	}
	if rec := request(t, h, http.MethodGet, host, "/dashboard", nil, cookie); rec.Code != http.StatusSeeOther {
		t.Fatalf("old cookie still works after logout: status %d", rec.Code)
	}
}

func TestExpiredSessionIsRejected(t *testing.T) {
	srv, pool := newTestServer(t, testConfig())
	h := srv.Handler()
	mustSignup(t, h, "alice", "owner@example.com")
	cookie := mustLogin(t, h, "alice", "owner@example.com")

	if _, err := pool.Exec(context.Background(),
		`UPDATE sessions SET expires_at = now() - interval '1 minute'`); err != nil {
		t.Fatal(err)
	}
	if rec := request(t, h, http.MethodGet, "alice."+testBase, "/dashboard", nil, cookie); rec.Code != http.StatusSeeOther {
		t.Fatalf("expired session accepted: status %d", rec.Code)
	}
}

// The important one: tenant A must never see or touch tenant B's data.
func TestTenantIsolation(t *testing.T) {
	srv, _ := newTestServer(t, testConfig())
	h := srv.Handler()
	aliceHost, bobHost := "alice."+testBase, "bob."+testBase

	// The same email in two tenants is two independent accounts.
	mustSignup(t, h, "alice", "owner@example.com")
	mustSignup(t, h, "bob", "owner@example.com")
	aliceCookie := mustLogin(t, h, "alice", "owner@example.com")
	bobCookie := mustLogin(t, h, "bob", "owner@example.com")

	rec := request(t, h, http.MethodPost, aliceHost, "/notes", url.Values{"body": {"alice-secret"}}, aliceCookie)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("alice add note: status %d", rec.Code)
	}

	// Alice sees her note.
	rec = request(t, h, http.MethodGet, aliceHost, "/dashboard", nil, aliceCookie)
	if !strings.Contains(rec.Body.String(), "alice-secret") {
		t.Fatal("alice cannot see her own note")
	}

	// Bob does not.
	rec = request(t, h, http.MethodGet, bobHost, "/dashboard", nil, bobCookie)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "alice-secret") {
		t.Fatalf("bob's dashboard leaks alice's data (status %d)", rec.Code)
	}

	// Alice's session cookie is useless on bob's host.
	rec = request(t, h, http.MethodGet, bobHost, "/dashboard", nil, aliceCookie)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Fatalf("alice's cookie was accepted on bob's host: status %d", rec.Code)
	}
	rec = request(t, h, http.MethodPost, bobHost, "/notes", url.Values{"body": {"planted"}}, aliceCookie)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("alice's cookie wrote to bob's tenant: status %d", rec.Code)
	}

	// Check the data layer directly as well.
	ctx := context.Background()
	bob, err := srv.store.TenantBySubdomain(ctx, "bob")
	if err != nil {
		t.Fatal(err)
	}
	notes, err := srv.store.ListNotes(ctx, bob.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 0 {
		t.Fatalf("bob has %d notes, want 0", len(notes))
	}

	// Alice's password does not unlock bob's account.
	rec = request(t, h, http.MethodPost, bobHost, "/login",
		url.Values{"email": {"nobody@example.com"}, "password": {testPass}})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unknown user on bob's host: status %d", rec.Code)
	}
}

func TestUnknownHostsReturn404(t *testing.T) {
	srv, _ := newTestServer(t, testConfig())
	h := srv.Handler()
	for _, host := range []string{"nobody." + testBase, "admin." + testBase, "evil.com"} {
		if rec := request(t, h, http.MethodGet, host, "/", nil); rec.Code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", host, rec.Code)
		}
	}
}

func TestCSRFRejectsForeignOrigin(t *testing.T) {
	srv, _ := newTestServer(t, testConfig())
	h := srv.Handler()
	form := signupForm("mallory", "m@example.com", testPass)

	cases := map[string]map[string]string{
		"foreign origin": {"Origin": "https://evil.example"},
		"null origin":    {"Origin": "null"},
		"no headers":     {},
	}
	for name, headers := range cases {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "https://"+testBase+"/signup", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			for k, v := range headers {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status %d, want 403", rec.Code)
			}
		})
	}
	if _, err := srv.store.TenantBySubdomain(context.Background(), "mallory"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("forged request created a tenant: %v", err)
	}
}

func TestSignupIsRateLimited(t *testing.T) {
	cfg := testConfig()
	cfg.SignupLimit = 2
	srv, _ := newTestServer(t, cfg)
	h := srv.Handler()

	want := []int{http.StatusBadRequest, http.StatusBadRequest, http.StatusTooManyRequests}
	for i, code := range want {
		rec := request(t, h, http.MethodPost, testBase, "/signup", signupForm("www", "a@example.com", testPass))
		if rec.Code != code {
			t.Fatalf("attempt %d: status %d, want %d", i+1, rec.Code, code)
		}
	}
}
