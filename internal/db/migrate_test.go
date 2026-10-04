package db_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/KING-CYBERTON/ayopa/internal/db"
	"github.com/KING-CYBERTON/ayopa/migrations"
)

// A database built by hand has the tables but no schema_migrations record.
// The runner must adopt it without failing. This is the situation the
// production database was in when migrations were first automated.
func TestMigrateAdoptsHandBuiltDatabase(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	if !strings.Contains(dsn, "_test") {
		t.Fatal("refusing to run: TEST_DATABASE_URL must point at a database whose name contains _test")
	}
	ctx := context.Background()
	const schema = "migrate_adopt_test"

	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	if _, err := admin.Exec(ctx, `DROP SCHEMA IF EXISTS `+schema+` CASCADE`); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = admin.Exec(ctx, `DROP SCHEMA `+schema+` CASCADE`) }()

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	if _, err := db.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("first run: %v", err)
	}
	// Forget the bookkeeping but keep the tables.
	if _, err := pool.Exec(ctx, `DROP TABLE schema_migrations`); err != nil {
		t.Fatal(err)
	}
	applied, err := db.Migrate(ctx, pool, migrations.FS)
	if err != nil {
		t.Fatalf("adopting a database that already has the tables failed: %v", err)
	}
	if len(applied) == 0 {
		t.Fatal("expected the files to be re-applied and recorded")
	}
}
