-- +goose Up
CREATE TABLE IF NOT EXISTS malls (
    id         BIGSERIAL PRIMARY KEY,
    domain     TEXT NOT NULL UNIQUE
               CHECK (domain ~ '^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$'),
    name       TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
    status     TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended')),
    reserved   TEXT[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Adopt tenants created before malls existed into the original domain.
-- A fresh database has no tenants, so it gets no mall until create-mall runs.
INSERT INTO malls (domain, name)
SELECT 'ayopa.co.ke', 'Ayopa'
WHERE EXISTS (SELECT 1 FROM tenants)
ON CONFLICT (domain) DO NOTHING;

ALTER TABLE tenants ADD COLUMN IF NOT EXISTS mall_id BIGINT REFERENCES malls(id) ON DELETE CASCADE;
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS name TEXT;
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'active'
    CHECK (status IN ('active', 'suspended'));

UPDATE tenants SET mall_id = (SELECT id FROM malls WHERE domain = 'ayopa.co.ke') WHERE mall_id IS NULL;
UPDATE tenants SET name = subdomain WHERE name IS NULL;

ALTER TABLE tenants ALTER COLUMN mall_id SET NOT NULL;
ALTER TABLE tenants ALTER COLUMN name SET NOT NULL;

-- A subdomain is unique per mall, not globally.
ALTER TABLE tenants DROP CONSTRAINT IF EXISTS tenants_subdomain_key;
CREATE UNIQUE INDEX IF NOT EXISTS tenants_mall_subdomain_idx ON tenants (mall_id, subdomain);

ALTER TABLE users ADD COLUMN IF NOT EXISTS role TEXT NOT NULL DEFAULT 'owner'
    CHECK (role IN ('owner', 'staff'));

CREATE TABLE IF NOT EXISTS invites (
    id         BIGSERIAL PRIMARY KEY,
    tenant_id  BIGINT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    email      TEXT NOT NULL,
    role       TEXT NOT NULL DEFAULT 'owner' CHECK (role IN ('owner', 'staff')),
    token_hash BYTEA NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS invites_tenant_idx ON invites (tenant_id);

-- +goose Down
-- WARNING: destroys all invite data and reverts tenants to single-mall structure.
-- Restoring tenants_subdomain_key may fail if tenants from different malls share
-- a subdomain. For development only; never run down migrations in production.
DROP TABLE IF EXISTS invites;
ALTER TABLE users DROP COLUMN IF EXISTS role;
DROP INDEX IF EXISTS tenants_mall_subdomain_idx;
ALTER TABLE tenants ADD CONSTRAINT tenants_subdomain_key UNIQUE (subdomain);
ALTER TABLE tenants DROP COLUMN IF EXISTS status;
ALTER TABLE tenants DROP COLUMN IF EXISTS name;
ALTER TABLE tenants DROP COLUMN IF EXISTS mall_id;
DROP TABLE IF EXISTS malls;