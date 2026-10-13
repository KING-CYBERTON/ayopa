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

type ctxKey string

const (
	mallKey   ctxKey = "mall"
	tenantKey ctxKey = "tenant"
)

type hostKind int

const (
	hostUnknown hostKind = iota
	hostMall
	hostShop
)

type Server struct {
	cfg       Config
	store     *store.Store
	log       *slog.Logger
	tpl       *template.Template
	mallMux   *http.ServeMux
	tenantMux *http.ServeMux
	loginRL   *limiter
}

func New(cfg Config, st *store.Store, log *slog.Logger) *Server {
	s := &Server{
		cfg:       cfg,
		store:     st,
		log:       log,
		tpl:       template.Must(template.New("page").Parse(pageTemplates)),
		mallMux:   http.NewServeMux(),
		tenantMux: http.NewServeMux(),
		loginRL:   newLimiter(cfg.LoginLimit, cfg.LoginWindow),
	}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return s.logging(s.securityHeaders(limitBody(s.csrf(http.HandlerFunc(s.route)))))
}

func (s *Server) routes() {
	s.mallMux.HandleFunc("GET /{$}", s.showDirectory)

	s.tenantMux.HandleFunc("GET /{$}", s.showStorefront)
	s.tenantMux.HandleFunc("GET /admin", s.showDashboard)
	s.tenantMux.HandleFunc("GET /admin/login", s.showLogin)
	s.tenantMux.HandleFunc("POST /admin/login", s.handleLogin)
	s.tenantMux.HandleFunc("POST /admin/logout", s.handleLogout)
	s.tenantMux.HandleFunc("GET /admin/setup", s.showSetup)
	s.tenantMux.HandleFunc("POST /admin/setup", s.handleSetup)
	s.tenantMux.HandleFunc("POST /admin/notes", s.handleAddNote)
}

// resolveHost works out which mall and shop a request is for.
//
//	mall.example          -> the mall directory
//	www.mall.example      -> the mall directory
//	shop.mall.example     -> a shop inside that mall
//	anything else         -> unknown
//
// One query tests all candidates. Mall domains never nest (CreateMall
// enforces it), so at most one interpretation can match.
func (s *Server) resolveHost(ctx context.Context, host string) (store.Mall, string, hostKind, error) {
	apex := strings.TrimPrefix(host, "www.")
	candidates := []string{host}
	if apex != host {
		candidates = append(candidates, apex)
	}
	label, parent, hasParent := strings.Cut(host, ".")
	if hasParent {
		candidates = append(candidates, parent)
	}

	malls, err := s.store.MallsByDomains(ctx, candidates)
	if err != nil {
		return store.Mall{}, "", hostUnknown, err
	}
	for _, m := range malls {
		if m.Domain == host || m.Domain == apex {
			return m, "", hostMall, nil
		}
	}
	if hasParent && tenant.ValidFormat(label) {
		for _, m := range malls {
			if m.Domain == parent {
				return m, label, hostShop, nil
			}
		}
	}
	return store.Mall{}, "", hostUnknown, nil
}

// route resolves the mall and shop once. Handlers read them from the request
// context and never take them from user input.
func (s *Server) route(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" {
		s.healthz(w, r)
		return
	}

	mall, label, kind, err := s.resolveHost(r.Context(), hostOnly(r.Host))
	if err != nil {
		s.log.Error("resolve host", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if kind == hostUnknown {
		http.NotFound(w, r)
		return
	}
	if mall.Status != store.StatusActive {
		unavailable(w)
		return
	}

	ctx := context.WithValue(r.Context(), mallKey, mall)
	if kind == hostMall {
		s.mallMux.ServeHTTP(w, r.WithContext(ctx))
		return
	}

	shop, err := s.store.TenantBySubdomain(ctx, mall.ID, label)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.log.Error("tenant lookup", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if shop.Status != store.StatusActive {
		unavailable(w)
		return
	}

	ctx = context.WithValue(ctx, tenantKey, shop)
	s.tenantMux.ServeHTTP(w, r.WithContext(ctx))
}

func unavailable(w http.ResponseWriter) {
	http.Error(w, "This site is temporarily unavailable.", http.StatusServiceUnavailable)
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

func mallFrom(ctx context.Context) store.Mall {
	m, _ := ctx.Value(mallKey).(store.Mall)
	return m
}

func tenantFrom(ctx context.Context) store.Tenant {
	t, _ := ctx.Value(tenantKey).(store.Tenant)
	return t
}

func hostOnly(h string) string {
	h = strings.ToLower(h)
	if host, _, err := net.SplitHostPort(h); err == nil {
		return host
	}
	return h
}

func (s *Server) shopURL(m store.Mall, subdomain, path string) string {
	return fmt.Sprintf("%s://%s.%s%s%s", s.cfg.Scheme, subdomain, m.Domain, s.cfg.Port, path)
}

// The __Host- prefix makes browsers reject the cookie unless it is Secure,
// has Path=/ and no Domain. That stops a sibling subdomain planting it.
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