package db_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/KING-CYBERTON/ayopa/internal/db"
	"github.com/KING-CYBERTON/ayopa/migrations"
)

// mustPool creates a pgxpool with the given search_path set in RuntimeParams.
func mustPool(t *testing.T, ctx context.Context, dsn, schema string) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// mustSchema creates an isolated schema and drops it when the test ends.
func mustSchema(t *testing.T, ctx context.Context, admin *pgxpool.Pool, name string) {
	t.Helper()
	if _, err := admin.Exec(ctx, `DROP SCHEMA IF EXISTS `+name+` CASCADE`); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `CREATE SCHEMA `+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(ctx, `DROP SCHEMA `+name+` CASCADE`) })
}

// tableExists reports whether a table is visible in the pool's search_path.
func tableExists(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table string) bool {
	t.Helper()
	var exists bool
	err := pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = current_schema() AND table_name = $1
		)`, table,
	).Scan(&exists)
	if err != nil {
		t.Fatalf("tableExists(%s): %v", table, err)
	}
	return exists
}

// gooseVersions returns the applied goose version numbers from the
// goose_db_version table, excluding the zero baseline row goose always inserts.
// Returns nil if the table does not exist.
func gooseVersions(t *testing.T, ctx context.Context, pool *pgxpool.Pool) []int64 {
	t.Helper()
	rows, err := pool.Query(ctx,
		`SELECT version_id FROM goose_db_version WHERE is_applied AND version_id > 0 ORDER BY version_id`)
	if err != nil {
		// goose_db_version might not exist yet; treat that as empty.
		return nil
	}
	defer rows.Close()
	var versions []int64
	for rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		versions = append(versions, v)
	}
	return versions
}

// TestMigrateAdoptsHandBuiltDatabase simulates a production database that was
// built by the old custom runner: the tables exist, a schema_migrations table
// is present, but no goose_db_version table. Migrate must succeed, re-apply
// all migrations idempotently, and record versions 1–3 in goose_db_version.
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
	mustSchema(t, ctx, admin, schema)

	pool := mustPool(t, ctx, dsn, schema)

	// First run: apply everything through goose, which creates goose_db_version.
	if _, err := db.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("first run: %v", err)
	}

	// Simulate the production state: drop goose's version table and leave a
	// legacy schema_migrations table in its place.
	if _, err := pool.Exec(ctx, `DROP TABLE goose_db_version`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations (
		version    TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		t.Fatal(err)
	}

	// Second run: goose sees no version table, so it re-applies all three
	// migrations. Because the SQL is idempotent the statements are no-ops.
	applied, err := db.Migrate(ctx, pool, migrations.FS)
	if err != nil {
		t.Fatalf("adopting a database that already has the tables failed: %v", err)
	}
	if len(applied) != 3 {
		t.Fatalf("expected 3 files re-applied, got %d: %v", len(applied), applied)
	}

	// goose_db_version should now record versions 1, 2, 3.
	versions := gooseVersions(t, ctx, pool)
	if fmt.Sprint(versions) != "[1 2 3]" {
		t.Fatalf("goose versions after adoption = %v, want [1 2 3]", versions)
	}
}

// TestMigrateUpDownUp verifies that migrations can be fully rolled back and
// then re-applied, and that the tables are present at the end.
func TestMigrateUpDownUp(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	if !strings.Contains(dsn, "_test") {
		t.Fatal("refusing to run: TEST_DATABASE_URL must point at a database whose name contains _test")
	}
	ctx := context.Background()
	const schema = "migrate_updownup_test"

	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	mustSchema(t, ctx, admin, schema)

	pool := mustPool(t, ctx, dsn, schema)

	// Up.
	applied, err := db.Migrate(ctx, pool, migrations.FS)
	if err != nil {
		t.Fatalf("up: %v", err)
	}
	if len(applied) != 3 {
		t.Fatalf("expected 3 applied, got %d: %v", len(applied), applied)
	}
	if !tableExists(t, ctx, pool, "malls") {
		t.Fatal("malls table missing after up")
	}

	// Down to 0 using goose directly.
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations.FS)
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	if _, err := provider.DownTo(ctx, 0); err != nil {
		t.Fatalf("down: %v", err)
	}
	if tableExists(t, ctx, pool, "malls") {
		t.Fatal("malls table still present after down")
	}
	if tableExists(t, ctx, pool, "tenants") {
		t.Fatal("tenants table still present after down")
	}

	// Up again.
	applied, err = db.Migrate(ctx, pool, migrations.FS)
	if err != nil {
		t.Fatalf("second up: %v", err)
	}
	if len(applied) != 3 {
		t.Fatalf("expected 3 applied on second up, got %d: %v", len(applied), applied)
	}
	if !tableExists(t, ctx, pool, "malls") {
		t.Fatal("malls table missing after second up")
	}
	if !tableExists(t, ctx, pool, "users") {
		t.Fatal("users table missing after second up")
	}
}

// TestMigrationsAreIdempotent verifies that a second Migrate call records
// nothing when all migrations are already applied.
func TestMigrationsAreIdempotent(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	if !strings.Contains(dsn, "_test") {
		t.Fatal("refusing to run: TEST_DATABASE_URL must point at a database whose name contains _test")
	}
	ctx := context.Background()
	const schema = "migrate_idempotent_test"

	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	mustSchema(t, ctx, admin, schema)

	pool := mustPool(t, ctx, dsn, schema)

	if _, err := db.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("first run: %v", err)
	}
	applied, err := db.Migrate(ctx, pool, migrations.FS)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if len(applied) != 0 {
		t.Fatalf("second run applied %v, want nothing", applied)
	}
}

// TestMigrateFailureIsolated verifies that when a migration fails, earlier
// migrations are committed and recorded, and the broken one is not.
func TestMigrateFailureIsolated(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	if !strings.Contains(dsn, "_test") {
		t.Fatal("refusing to run: TEST_DATABASE_URL must point at a database whose name contains _test")
	}
	ctx := context.Background()
	const schema = "migrate_failure_test"

	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	mustSchema(t, ctx, admin, schema)

	pool := mustPool(t, ctx, dsn, schema)

	badFS := fstest.MapFS{
		"0001_ok.sql": &fstest.MapFile{
			Data: []byte("-- +goose Up\nCREATE TABLE good_table (id int);\n-- +goose Down\nDROP TABLE IF EXISTS good_table;\n"),
		},
		"0002_broken.sql": &fstest.MapFile{
			Data: []byte("-- +goose Up\nTHIS IS NOT VALID SQL;\n-- +goose Down\n"),
		},
	}

	applied, err := db.Migrate(ctx, pool, badFS)
	if err == nil {
		t.Fatal("expected error from broken migration, got nil")
	}
	if len(applied) != 1 {
		t.Fatalf("expected 1 migration applied before failure, got %d: %v", len(applied), applied)
	}
	if applied[0] != "0001_ok.sql" {
		t.Fatalf("expected 0001_ok.sql to be applied, got %q", applied[0])
	}

	// Version 1 must be recorded; version 2 must not be.
	versions := gooseVersions(t, ctx, pool)
	if len(versions) != 1 || versions[0] != 1 {
		t.Fatalf("goose versions after partial failure = %v, want [1]", versions)
	}

	// The table from migration 1 must still exist.
	if !tableExists(t, ctx, pool, "good_table") {
		t.Fatal("good_table missing after partial failure — migration 1 was not committed")
	}

	// The error message must mention the failing migration.
	if !strings.Contains(err.Error(), "goose up") {
		t.Errorf("error %q does not mention goose up", err)
	}

	// A second run must fail again on the same broken file and still not
	// leave version 2 recorded.
	_, err2 := db.Migrate(ctx, pool, badFS)
	if err2 == nil {
		t.Fatal("expected second run to fail again")
	}
	versions2 := gooseVersions(t, ctx, pool)
	if len(versions2) != 1 || versions2[0] != 1 {
		t.Fatalf("after second failure, goose versions = %v, want [1]", versions2)
	}
}
