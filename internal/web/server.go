package web

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/KING-CYBERTON/ayopa/internal/store"
	"github.com/KING-CYBERTON/ayopa/internal/tenant"
)

type ctxKey struct{}

type Server struct {
	cfg       Config
	store     *store.Store
	log       *slog.Logger
	tpl       *template.Template
	apexMux   *http.ServeMux
	tenantMux *http.ServeMux
	signupRL  *limiter
	loginRL   *limiter
}

func New(cfg Config, st *store.Store, log *slog.Logger) *Server {
	cfg.BaseDomain = strings.ToLower(cfg.BaseDomain)
	s := &Server{
		cfg:       cfg,
		store:     st,
		log:       log,
		tpl:       template.Must(template.New("page").Parse(pageTemplates)),
		apexMux:   http.NewServeMux(),
		tenantMux: http.NewServeMux(),
		signupRL:  newLimiter(cfg.SignupLimit, cfg.SignupWindow),
		loginRL:   newLimiter(cfg.LoginLimit, cfg.LoginWindow),
	}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return s.logging(s.securityHeaders(limitBody(s.csrf(http.HandlerFunc(s.route)))))
}

func (s *Server) routes() {
	s.apexMux.HandleFunc("GET /{$}", s.showSignup)
	s.apexMux.HandleFunc("POST /signup", s.handleSignup)

	s.tenantMux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
	})
	s.tenantMux.HandleFunc("GET /login", s.showLogin)
	s.tenantMux.HandleFunc("POST /login", s.handleLogin)
	s.tenantMux.HandleFunc("POST /logout", s.handleLogout)
	s.tenantMux.HandleFunc("GET /dashboard", s.showDashboard)
	s.tenantMux.HandleFunc("POST /notes", s.handleAddNote)
}

// route decides whether a request is for the main site or for a tenant.
// The tenant is resolved once, here, and every tenant handler reads it
// from the request context. Handlers never take a tenant from user input.
func (s *Server) route(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" {
		s.healthz(w, r)
		return
	}

	host := hostOnly(r.Host)
	base := s.cfg.BaseDomain
	if host == base || host == "www."+base {
		s.apexMux.ServeHTTP(w, r)
		return
	}

	sub, ok := strings.CutSuffix(host, "."+base)
	if !ok || tenant.Validate(sub) != nil {
		http.NotFound(w, r)
		return
	}

	t, err := s.store.TenantBySubdomain(r.Context(), sub)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.log.Error("tenant lookup", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	ctx := context.WithValue(r.Context(), ctxKey{}, t)
	s.tenantMux.ServeHTTP(w, r.WithContext(ctx))
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.store.Ping(ctx); err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}

func tenantFrom(ctx context.Context) store.Tenant {
	t, _ := ctx.Value(ctxKey{}).(store.Tenant)
	return t
}

func hostOnly(h string) string {
	h = strings.ToLower(h)
	if host, _, err := net.SplitHostPort(h); err == nil {
		return host
	}
	return h
}

func (s *Server) tenantURL(sub, path string) string {
	return fmt.Sprintf("%s://%s.%s%s%s", s.cfg.Scheme, sub, s.cfg.BaseDomain, s.cfg.Port, path)
}

// The __Host- prefix makes browsers reject the cookie unless it is Secure,
// has Path=/ and no Domain. That stops a malicious sibling subdomain from
// planting a cookie for this one ("cookie tossing").
func (s *Server) cookieName() string {
	if s.cfg.CookieSecure {
		return "__Host-session"
	}
	return "session"
}

func (s *Server) sessionCookie(value string, ttl time.Duration) *http.Cookie {
	c := &http.Cookie{
		Name:     s.cookieName(),
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	}
	if ttl > 0 {
		c.MaxAge = int(ttl.Seconds())
		c.Expires = time.Now().Add(ttl)
	} else {
		c.MaxAge = -1
	}
	return c
}
