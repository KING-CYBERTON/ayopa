package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/KING-CYBERTON/ayopa/internal/db"
	"github.com/KING-CYBERTON/ayopa/internal/store"
	"github.com/KING-CYBERTON/ayopa/internal/web"
	"github.com/KING-CYBERTON/ayopa/migrations"
)

const usage = `usage: server [command] [flags]

  serve               run the web server (default)
  migrate             apply database migrations
  create-mall         -domain D -name N [-reserve a,b,c]
  create-tenant       -mall D -subdomain S -name N -owner EMAIL
  create-invite       -mall D -subdomain S -email EMAIL [-role owner|staff]
  set-tenant-status   -mall D -subdomain S -status active|suspended
  set-mall-status     -mall D -status active|suspended
  set-mall-content    -mall D [-tagline T] [-description D] [-contact EMAIL]
  set-mall-style      -mall D [-style classic|editorial|directory] [-palette forest|violet|ocean|terracotta]
  set-mall-locks      -mall D [-styles boutique,market] [-palettes P,...]
  set-shop-style      -mall D -subdomain S -style classic|boutique|market
  list                show malls and shops
  seed                [-mall ayopa.localhost] [-password P]   rebuild a local mall of sample shops
`

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cmd, args := "serve", os.Args[1:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}

	switch cmd {
	case "serve":
		os.Exit(runServer(logger))
	case "migrate":
		os.Exit(runMigrate(logger))
	case "create-mall", "create-tenant", "create-invite",
		"set-tenant-status", "set-mall-status", "set-mall-content",
		"set-mall-style", "set-mall-locks", "set-shop-style", "list", "seed":
		os.Exit(runAdmin(cmd, args))
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
}

func runMigrate(log *slog.Logger) int {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Error("connect", "err", err)
		return 1
	}
	defer pool.Close()

	applied, err := db.Migrate(ctx, pool, migrations.FS)
	if err != nil {
		log.Error("migrate", "err", err)
		return 1
	}
	log.Info("migrations complete", "applied", applied)
	return 0
}

func runServer(log *slog.Logger) int {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Error("DATABASE_URL is required")
		return 1
	}
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Error("db pool", "err", err)
		return 1
	}
	defer pool.Close()
	st := store.New(pool)

	go cleanup(ctx, log, st)

	srv := &http.Server{
		Addr:              addr,
		Handler:           web.New(web.ConfigFromEnv(), st, log).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", addr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Error("server", "err", err)
			return 1
		}
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("shutdown", "err", err)
		return 1
	}
	return 0
}

func cleanup(ctx context.Context, log *slog.Logger, st *store.Store) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if n, err := st.DeleteExpiredSessions(ctx); err != nil {
				log.Error("cleanup sessions", "err", err)
			} else if n > 0 {
				log.Info("expired sessions removed", "count", n)
			}
			if n, err := st.DeleteStaleInvites(ctx); err != nil {
				log.Error("cleanup invites", "err", err)
			} else if n > 0 {
				log.Info("stale invites removed", "count", n)
			}
		}
	}
}
