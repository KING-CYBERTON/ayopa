-- +goose Up

-- Shop profile fields on tenants.
ALTER TABLE tenants
    ADD COLUMN IF NOT EXISTS description   TEXT NOT NULL DEFAULT ''
        CHECK (length(description)   <= 600),
    ADD COLUMN IF NOT EXISTS tagline       TEXT NOT NULL DEFAULT ''
        CHECK (length(tagline)       <= 140),
    ADD COLUMN IF NOT EXISTS contact_email TEXT NOT NULL DEFAULT ''
        CHECK (length(contact_email) <= 254),
    ADD COLUMN IF NOT EXISTS phone         TEXT NOT NULL DEFAULT ''
        CHECK (length(phone)         <= 30),
    ADD COLUMN IF NOT EXISTS address       TEXT NOT NULL DEFAULT ''
        CHECK (length(address)       <= 300),
    ADD COLUMN IF NOT EXISTS opening_hours TEXT NOT NULL DEFAULT ''
        CHECK (length(opening_hours) <= 300),
    ADD COLUMN IF NOT EXISTS currency      TEXT NOT NULL DEFAULT 'KES'
        CHECK (currency IN ('KES','UGX','TZS','RWF','ETB','NGN','ZAR','USD'));

-- Per-shop product categories.
-- UNIQUE (tenant_id, id) is required by the composite FK on products.
CREATE TABLE IF NOT EXISTS categories (
    id         BIGSERIAL PRIMARY KEY,
    tenant_id  BIGINT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name       TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 60),
    slug       TEXT NOT NULL,
    position   INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, slug),
    UNIQUE (tenant_id, id)
);
CREATE INDEX IF NOT EXISTS categories_tenant_pos_idx
    ON categories (tenant_id, position, id);

-- Products.  The composite FK (tenant_id, category_id) → categories (tenant_id, id)
-- ensures a product can only reference a category in the same shop.
-- ON DELETE is omitted intentionally: deleting a category first NULLs its
-- products in the same transaction (handled in the store).
CREATE TABLE IF NOT EXISTS products (
    id          BIGSERIAL PRIMARY KEY,
    tenant_id   BIGINT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    category_id BIGINT,
    name        TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
    description TEXT NOT NULL DEFAULT ''
        CHECK (length(description) <= 2000),
    price_minor BIGINT NOT NULL DEFAULT 0 CHECK (price_minor >= 0),
    stock       INT    NOT NULL DEFAULT 0 CHECK (stock >= 0),
    status      TEXT   NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft','published','archived')),
    featured    BOOLEAN NOT NULL DEFAULT false,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    CONSTRAINT fk_product_category
        FOREIGN KEY (tenant_id, category_id)
        REFERENCES categories (tenant_id, id)
);
CREATE INDEX IF NOT EXISTS products_tenant_status_created_idx
    ON products (tenant_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS products_tenant_status_price_idx
    ON products (tenant_id, status, price_minor);
CREATE INDEX IF NOT EXISTS products_tenant_category_idx
    ON products (tenant_id, category_id);

-- +goose Down
-- WARNING: destroys all product and category data. For development only.
DROP TABLE IF EXISTS products;
DROP TABLE IF EXISTS categories;
ALTER TABLE tenants
    DROP COLUMN IF EXISTS currency,
    DROP COLUMN IF EXISTS opening_hours,
    DROP COLUMN IF EXISTS address,
    DROP COLUMN IF EXISTS phone,
    DROP COLUMN IF EXISTS contact_email,
    DROP COLUMN IF EXISTS tagline,
    DROP COLUMN IF EXISTS description;
