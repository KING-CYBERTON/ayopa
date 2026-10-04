package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/KING-CYBERTON/ayopa/internal/db"
	"github.com/KING-CYBERTON/ayopa/internal/store"
	"github.com/KING-CYBERTON/ayopa/internal/web"
	"github.com/KING-CYBERTON/ayopa/migrations"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		os.Exit(runMigrate(logger))
	}
	os.Exit(runServer(logger))
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
	cfg := web.ConfigFromEnv()
	dbURL := os.Getenv("DATABASE_URL")
	if cfg.BaseDomain == "" || dbURL == "" {
		log.Error("BASE_DOMAIN and DATABASE_URL are required")
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

	go cleanupSessions(ctx, log, st)

	srv := &http.Server{
		Addr:              addr,
		Handler:           web.New(cfg, st, log).Handler(),
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

func cleanupSessions(ctx context.Context, log *slog.Logger, st *store.Store) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := st.DeleteExpiredSessions(ctx)
			if err != nil {
				log.Error("cleanup sessions", "err", err)
			} else if n > 0 {
				log.Info("expired sessions removed", "count", n)
			}
		}
	}
}
