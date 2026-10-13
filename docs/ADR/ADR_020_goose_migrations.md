# ADR-0020: Use goose as the database migration tool

- **Status:** Accepted
- **Date:** 2026-10-13
- **Deciders:** Project author

## Context

ADR-0013 chose a custom migration runner because CI cannot reach the private RDS
instance (ADR-0004, ADR-0009), ruling out CLI-based migration tools run from
GitHub Actions. The custom runner was intentionally minimal: an advisory lock, a
`schema_migrations` table, one transaction per file.

As the schema grew to three migration files and multiple developers began
contributing, the lack of down migrations made local resets painful and the
absence of a CLI made it hard to inspect migration state without writing SQL
by hand. The constraint that rules out CLI-from-CI does *not* rule out a
migration library embedded inside the app binary, which still runs where the
database is reachable.

## Options considered

### Option 1: Keep the custom runner
- **Pros:** No new dependency; no annotated SQL files.
- **Cons:** No down migrations, no CLI for developers, no maintained locker, a
  custom codebase to maintain indefinitely.

### Option 2: goose as a library inside the app binary (chosen)
- **Pros:** Maintained library with down migrations, a developer-friendly CLI,
  a battle-tested Postgres session locker, and a Provider API with clear
  semantics (returns per-migration results, `PartialError` on partial failure).
  Runs inside the binary so RDS remains unreachable from CI.
- **Cons:** One more dependency; SQL files must carry `-- +goose Up/Down`
  annotations; the legacy `schema_migrations` table must be left in place until
  a follow-up migration drops it.

### Option 3: goose CLI run from CI
- **Pros:** Clean separation of concerns.
- **Cons:** CI cannot reach the private RDS instance without a bastion or NAT
  gateway. Ruled out by the same constraint that led to ADR-0013.

### Option 4: golang-migrate as a library
- **Pros:** Mature, popular, supports many databases.
- **Cons:** Does not have a Postgres session-level advisory locker built in;
  the CLI is separate; the library API is less ergonomic than goose's Provider.
  goose was already the named candidate in ADR-0013's "Revisit when" section.

## Decision

Replace the custom runner with `github.com/pressly/goose/v3` using the Provider
API. The call site in `cmd/server/main.go` and the function signature
`db.Migrate(ctx, pool, fsys) ([]string, error)` are unchanged.

goose derives migration version numbers from the numeric filename prefix
(`0001_init.sql` → version 1) and records applied versions in `goose_db_version`.
The existing `schema_migrations` table is left in place; it is obsolete and can
be dropped by a future migration.

Production adoption is handled by idempotency: the three migration SQL bodies
use `IF NOT EXISTS`, `ON CONFLICT DO NOTHING`, and `IF EXISTS` guards throughout,
so goose can re-apply them on a database that already has the tables. On first
deploy, goose finds no `goose_db_version` table, re-runs all three files
(no-ops), and records versions 1–3.

A Postgres session-level advisory lock (`lock.NewPostgresSessionLocker`) is
passed to the Provider, preserving the concurrent-deploy safety guarantee from
the custom runner.

## Consequences

### Positive
- Down migrations are available for local development, implemented in each
  migration file's `-- +goose Down` section.
- Developers can inspect migration state and create new files with the goose CLI
  without touching production infrastructure.
- goose's `PartialError` type makes it possible to report which migrations
  succeeded before a failure.
- The locker and version-table logic are maintained upstream.

### Negative / trade-offs accepted
- SQL files must carry `-- +goose Up` and `-- +goose Down` annotations. Any file
  missing the annotation is treated as an empty migration.
- The `schema_migrations` table from the old runner is now obsolete; it is left
  in place to avoid a schema change on this branch. A follow-up migration should
  drop it.
- goose adds four indirect runtime dependencies (`mfridman/interpolate`,
  `sethvargo/go-retry`, `uber-go/multierr`, and minor upgrades to existing
  packages).
- Down migrations are for development only. Running them in production would
  destroy data; they are never invoked by `deploy.sh`.

### Risks and mitigations
- **Adoption of existing production database:** Covered by idempotent SQL and a
  dedicated integration test (`TestMigrateAdoptsHandBuiltDatabase`) that
  simulates a database with tables but no `goose_db_version` table.
- **goose API changes:** The Provider API is stable since v3.18. The pinned
  version (v3.29.0) is recorded in `go.mod`.

## Revisit when
- The `schema_migrations` table should be dropped in the next migration that
  touches the schema.
- If zero-downtime requirements arrive, `CREATE INDEX CONCURRENTLY` needs a
  no-transaction migration; goose supports this with `-- +goose NO TRANSACTION`.

## References
- Related: ADR-0004, ADR-0007, ADR-0009, ADR-0013
- goose Provider API: https://pkg.go.dev/github.com/pressly/goose/v3
