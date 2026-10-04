CREATE TABLE IF NOT EXISTS tenants (
    id         BIGSERIAL PRIMARY KEY,
    subdomain  TEXT NOT NULL UNIQUE
               CHECK (subdomain ~ '^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS users (
    id            BIGSERIAL PRIMARY KEY,
    tenant_id     BIGINT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    email         TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, email)
);
