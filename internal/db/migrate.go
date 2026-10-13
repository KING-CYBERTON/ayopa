package db

// Package db contains the migration runner.

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

// Migrate applies every pending *.sql file in fsys using goose, under a
// Postgres session-level advisory lock so concurrent deploys are safe.
// It returns the names of the files it applied (empty slice when nothing
// is pending, never nil).
//
// The legacy schema_migrations table created by the previous custom runner
// is left in place; it is obsolete and a follow-up migration can drop it.
func Migrate(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS) ([]string, error) {
	// stdlib.OpenDBFromPool wraps the pool in a *sql.DB; closing the
	// wrapper does not close the underlying pool.
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()

	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return nil, fmt.Errorf("goose locker: %w", err)
	}

	provider, err := goose.NewProvider(
		goose.DialectPostgres,
		sqlDB,
		fsys,
		goose.WithSessionLocker(locker),
	)
	if err != nil {
		return nil, fmt.Errorf("goose provider: %w", err)
	}

	results, err := provider.Up(ctx)
	if err != nil {
		var partial *goose.PartialError
		if errors.As(err, &partial) {
			return sourceNames(partial.Applied), fmt.Errorf("goose up: %w", err)
		}
		return nil, fmt.Errorf("goose up: %w", err)
	}
	return sourceNames(results), nil
}

func sourceNames(results []*goose.MigrationResult) []string {
	names := make([]string, 0, len(results))
	for _, r := range results {
		names = append(names, r.Source.Path)
	}
	return names
}
