-- +goose Up
CREATE TABLE IF NOT EXISTS posts (
    id           BIGSERIAL PRIMARY KEY,
    tenant_id    BIGINT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    title        TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 120),
    excerpt      TEXT NOT NULL DEFAULT '' CHECK (length(excerpt) <= 240),
    body         TEXT NOT NULL DEFAULT '' CHECK (length(body) <= 20000),
    category     TEXT NOT NULL DEFAULT '' CHECK (length(category) <= 40),
    status       TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published')),
    published_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS posts_tenant_published_idx ON posts (tenant_id, status, published_at DESC);

-- +goose Down
-- WARNING: deletes every blog post. For development only.
DROP TABLE IF EXISTS posts;
