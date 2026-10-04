# ADR-0013: Run database migrations from the app binary during deploy

- **Status:** Accepted
- **Date:** 2026-10-03
- **Deciders:** Project author

## Context
The schema was applied by hand with psql, so a rebuilt environment starts with
an empty database and no schema. RDS sits in private subnets with no public
access (ADR-0004, ADR-0009), so GitHub Actions cannot connect to it directly.
Deploys must be repeatable and must never leave the app running against a
schema it does not match.

## Options considered

### Option 1: Manual psql (current)
- **Pros:** Nothing to build.
- **Cons:** Not repeatable, easy to forget, and a rebuild loses the schema.

### Option 2: Migration runner inside the app binary, run by deploy.sh
- **Pros:** The SQL files are embedded and versioned with the code that needs
  them. It runs where the database is reachable, needs no extra tooling or
  image, and a failure stops the deploy before containers are replaced.
- **Cons:** A small custom runner to maintain, forward-only, and no CLI niceties.

### Option 3: goose or golang-migrate run from CI
- **Pros:** Mature tools with down migrations and drivers.
- **Cons:** CI cannot reach the private RDS instance without a tunnel,
  self-hosted runner or bastion, all of which add cost and exposure.

### Option 4: Separate migration container or one-off ECS task
- **Pros:** Clean separation and works with any tool.
- **Cons:** More infrastructure than a single-instance project needs.

## Decision
Embed `migrations/*.sql` in the binary. `app migrate` applies unapplied files
in filename order, each in its own transaction, recording them in
`schema_migrations`, under a Postgres advisory lock. `deploy.sh` runs
`docker compose run --rm app migrate` after pulling images and before
`docker compose up -d`.

## Consequences

### Positive
- A new environment gets its schema automatically, which makes the
  destroy-and-rebuild demo fully hands-off.
- A failed migration aborts the deploy and the old version keeps serving.
- The advisory lock makes concurrent deploys safe.

### Negative / trade-offs accepted
- Migrations are forward-only. Recovery from a bad migration is a new
  migration or a restore from backup (1-day retention, see ADR-0004).
- Old containers keep running while the new schema is applied, so every
  migration must be backward compatible with the previous app version
  (add columns and tables first, remove them in a later release).
- Each file is a single transaction, so `CREATE INDEX CONCURRENTLY` is not
  possible without changing the runner.
- Applied migrations are never edited. The one exception is 0001, made
  idempotent once so the runner could adopt the hand-built database.

### Risks and mitigations
- **Long-running migration blocks the deploy:** keep migrations small, and
  the SSM deploy step waits up to five minutes.
- **Runner bugs:** CI applies the migrations to a real Postgres on every
  push and asserts a second run applies nothing.

## Revisit when
- Down migrations, dry runs or multiple developers editing the schema justify
  moving to goose or golang-migrate.
- Zero-downtime requirements need online index builds.

## References
- Related: ADR-0004, ADR-0007, ADR-0009