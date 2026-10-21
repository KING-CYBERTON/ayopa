package web

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/KING-CYBERTON/ayopa/internal/catalog"
	"github.com/KING-CYBERTON/ayopa/internal/money"
	"github.com/KING-CYBERTON/ayopa/internal/store"
	"github.com/KING-CYBERTON/ayopa/internal/tenant"
)

//go:embed templates
var templateFS embed.FS

type productCard struct {
	P        store.Product
	Currency string
}

var templateFuncs = template.FuncMap{
	"formatMoney": money.FormatCurrency,
	"formatPrice": func(minor int64, currency string) string {
		return money.Format(minor, money.ExponentFor(currency))
	},
	"placeholderColor": func(id int64) string {
		palette := [8]string{
			"#1a5fa8", "#d44000", "#2e7d32", "#6a1b9a",
			"#0277bd", "#c62828", "#37474f", "#558b2f",
		}
		idx := id % 8
		if idx < 0 {
			idx += 8
		}
		return palette[idx]
	},
	"initial": func(name string) string {
		for _, r := range name {
			return string([]rune{r})
		}
		return "•"
	},
	"queryPage": func(q catalog.ListQuery, page int) string {
		qs := q.QueryStringForPage(page)
		if qs == "" {
			return "/"
		}
		return "/?" + qs
	},
	"querySort": func(q catalog.ListQuery, sortKey string) string {
		q2 := q
		q2.Sort = catalog.Sort(sortKey)
		q2.Page = 1
		qs := q2.QueryStringForPage(1)
		if qs == "" {
			return "/"
		}
		return "/?" + qs
	},
	// cardOf passes a product and the shop currency to a card partial.
	"cardOf": func(p store.Product, currency string) productCard {
		return productCard{P: p, Currency: currency}
	},
	"queryCat": func(q catalog.ListQuery, slug string) string {
		q2 := q
		q2.Category = slug
		qs := q2.QueryStringForPage(1)
		if qs == "" {
			return "/"
		}
		return "/?" + qs
	},
	"paginate": func(current, last int) []int {
		if last <= 1 {
			return nil
		}
		if last <= 9 {
			out := make([]int, last)
			for i := range out {
				out[i] = i + 1
			}
			return out
		}
		set := map[int]bool{1: true, last: true}
		for _, p := range []int{current - 2, current - 1, current, current + 1, current + 2} {
			if p >= 1 && p <= last {
				set[p] = true
			}
		}
		var pages []int
		for p := 1; p <= last; p++ {
			if set[p] {
				pages = append(pages, p)
			}
		}
		sort.Ints(pages)
		var out []int
		for i, p := range pages {
			if i > 0 && p > pages[i-1]+1 {
				out = append(out, 0)
			}
			out = append(out, p)
		}
		return out
	},
	"currencies": func() []string {
		return []string{"KES", "UGX", "TZS", "RWF", "ETB", "NGN", "ZAR", "USD"}
	},
	"catSelected": func(p store.Product, catID int64) bool {
		return p.CategoryID != nil && *p.CategoryID == catID
	},
	"adminPageQS": func(status string, page int) string {
		v := ""
		if status != "" {
			v += "status=" + status + "&"
		}
		if page > 1 {
			v += fmt.Sprintf("page=%d", page)
		}
		return strings.TrimRight(v, "&")
	},
	"add": func(a, b int) int { return a + b },
	"sub": func(a, b int) int { return a - b },
}

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
		cfg:   cfg,
		store: st,
		log:   log,
		tpl: template.Must(
			template.New("").Funcs(templateFuncs).ParseFS(templateFS, "templates/*.html"),
		),
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

	// storefront (public)
	s.tenantMux.HandleFunc("GET /{$}", s.showStorefront)
	s.tenantMux.HandleFunc("GET /shop/{id}", s.showProduct)

	// admin auth
	s.tenantMux.HandleFunc("GET /admin", s.showDashboard)
	s.tenantMux.HandleFunc("GET /admin/login", s.showLogin)
	s.tenantMux.HandleFunc("POST /admin/login", s.handleLogin)
	s.tenantMux.HandleFunc("POST /admin/logout", s.handleLogout)
	s.tenantMux.HandleFunc("GET /admin/setup", s.showSetup)
	s.tenantMux.HandleFunc("POST /admin/setup", s.handleSetup)
	s.tenantMux.HandleFunc("POST /admin/notes", s.handleAddNote)

	// admin settings
	s.tenantMux.HandleFunc("GET /admin/settings", s.showSettings)
	s.tenantMux.HandleFunc("POST /admin/settings", s.handleSettings)

	// admin categories
	s.tenantMux.HandleFunc("GET /admin/categories", s.showCategories)
	s.tenantMux.HandleFunc("POST /admin/categories", s.handleCreateCategory)
	s.tenantMux.HandleFunc("POST /admin/categories/{id}/rename", s.handleRenameCategory)
	s.tenantMux.HandleFunc("POST /admin/categories/{id}/delete", s.handleDeleteCategory)

	// admin products
	s.tenantMux.HandleFunc("GET /admin/products", s.showProducts)
	s.tenantMux.HandleFunc("GET /admin/products/new", s.showNewProduct)
	s.tenantMux.HandleFunc("POST /admin/products/new", s.handleCreateProduct)
	s.tenantMux.HandleFunc("GET /admin/products/{id}", s.showEditProduct)
	s.tenantMux.HandleFunc("POST /admin/products/{id}", s.handleUpdateProduct)
	s.tenantMux.HandleFunc("POST /admin/products/{id}/status", s.handleSetProductStatus)
	s.tenantMux.HandleFunc("POST /admin/products/{id}/featured", s.handleSetProductFeatured)
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
