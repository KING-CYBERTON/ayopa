package db

// Package db contains the migration runner.

import (
	"context"
	"fmt"
	"io/fs"
	"sort"

	"github.com/jackc/pgx/v5/pgxpool"
)

// lockID is an arbitrary key for a Postgres advisory lock, so two deploys
// can never run migrations at the same time.
const lockID int64 = 7263541

// Migrate applies every *.sql file in fsys that is not yet recorded in
// schema_migrations, in filename order, each inside its own transaction.
// It returns the names of the files it applied.
func Migrate(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS) ([]string, error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, lockID); err != nil {
		return nil, fmt.Errorf("take migration lock: %w", err)
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, lockID)
	}()

	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		return nil, err
	}

	names, err := fs.Glob(fsys, "*.sql")
	if err != nil {
		return nil, err
	}
	sort.Strings(names)

	applied := []string{}
	for _, name := range names {
		var done bool
		err := conn.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, name,
		).Scan(&done)
		if err != nil {
			return applied, err
		}
		if done {
			continue
		}

		body, err := fs.ReadFile(fsys, name)
		if err != nil {
			return applied, err
		}

		tx, err := conn.Begin(ctx)
		if err != nil {
			return applied, err
		}
		if _, err := tx.Exec(ctx, string(body)); err != nil {
			_ = tx.Rollback(ctx)
			return applied, fmt.Errorf("apply %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, name); err != nil {
			_ = tx.Rollback(ctx)
			return applied, fmt.Errorf("record %s: %w", name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return applied, fmt.Errorf("commit %s: %w", name, err)
		}
		applied = append(applied, name)
	}
	return applied, nil
}
