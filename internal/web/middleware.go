package web

import (
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func newRequestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// logging writes one structured line per request. It logs the path only,
// never query strings, form values or cookies.
func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		id := newRequestID()
		w.Header().Set("X-Request-ID", id)
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		s.log.Info("request",
			"request_id", id,
			"method", r.Method,
			"host", r.Host,
			"path", r.URL.Path,
			"status", sw.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"ip", s.clientIP(r),
		)
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Content-Security-Policy",
			"default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; frame-ancestors 'none'")
		if s.cfg.CookieSecure {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}

func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		next.ServeHTTP(w, r)
	})
}

// csrf rejects state-changing requests that do not come from the same
// origin. It complements the SameSite=Lax session cookie.
func (s *Server) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if !sameOrigin(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func sameOrigin(r *http.Request) bool {
	for _, header := range []string{"Origin", "Referer"} {
		v := r.Header.Get(header)
		if v == "" {
			continue
		}
		u, err := url.Parse(v)
		if err != nil {
			return false
		}
		return strings.EqualFold(u.Host, r.Host)
	}
	return false
}

// clientIP uses X-Forwarded-For only when TRUST_PROXY is set. Caddy
// overwrites that header with the real client address, and the app is not
// reachable except through Caddy.
func (s *Server) clientIP(r *http.Request) string {
	if s.cfg.TrustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if first := strings.TrimSpace(strings.Split(xff, ",")[0]); first != "" {
				return first
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
