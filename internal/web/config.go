// Package web contains the HTTP layer: routing, tenant resolution,
// signup, login, sessions and the security middleware.
package web

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	BaseDomain   string
	Scheme       string // used to build links to tenant subdomains
	Port         string // e.g. ":8080" for local development, empty in production
	CookieSecure bool
	TrustProxy   bool // trust X-Forwarded-For (only safe behind Caddy)
	SessionTTL   time.Duration

	SignupLimit  int
	SignupWindow time.Duration
	LoginLimit   int
	LoginWindow  time.Duration
}

func ConfigFromEnv() Config {
	return Config{
		BaseDomain:   os.Getenv("BASE_DOMAIN"),
		Scheme:       envOr("PUBLIC_SCHEME", "https"),
		Port:         os.Getenv("PUBLIC_PORT"),
		CookieSecure: envBool("COOKIE_SECURE", true),
		TrustProxy:   envBool("TRUST_PROXY", false),
		SessionTTL:   7 * 24 * time.Hour,
		SignupLimit:  5,
		SignupWindow: time.Hour,
		LoginLimit:   10,
		LoginWindow:  15 * time.Minute,
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}
