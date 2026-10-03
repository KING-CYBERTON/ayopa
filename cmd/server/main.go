package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ctxKey string

const tenantKey ctxKey = "tenant"

type Tenant struct {
	ID        int64
	Subdomain string
}

var subRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`)

func tenantMiddleware(base string, pool *pgxpool.Pool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := strings.ToLower(r.Host)
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if host == base || host == "www."+base {
			next.ServeHTTP(w, r) // main site, no tenant
			return
		}
		sub, ok := strings.CutSuffix(host, "."+base)
		if !ok || !subRe.MatchString(sub) {
			http.NotFound(w, r)
			return
		}
		t := Tenant{Subdomain: sub}
		err := pool.QueryRow(r.Context(),
			`SELECT id FROM tenants WHERE subdomain = $1`, sub).Scan(&t.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			slog.Error("tenant lookup", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		ctx := context.WithValue(r.Context(), tenantKey, t)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		slog.Error("db pool", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		c, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := pool.Ping(c); err != nil {
			http.Error(w, "db unavailable", http.StatusServiceUnavailable)
			return
		}
		fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		if t, ok := r.Context().Value(tenantKey).(Tenant); ok {
			fmt.Fprintf(w, "Welcome to tenant %q (id %d)\n", t.Subdomain, t.ID)
			return
		}
		fmt.Fprintln(w, "Main site: signup goes here")
	})

	srv := &http.Server{
		Addr:              os.Getenv("ADDR"),
		Handler:           tenantMiddleware(os.Getenv("BASE_DOMAIN"), pool, mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		slog.Info("listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv.Shutdown(shutdownCtx)
}
