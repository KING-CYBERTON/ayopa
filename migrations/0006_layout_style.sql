-- +goose Up
ALTER TABLE malls ADD COLUMN IF NOT EXISTS style TEXT NOT NULL DEFAULT 'classic'
    CHECK (style IN ('classic', 'editorial', 'directory'));
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS style TEXT NOT NULL DEFAULT 'classic'
    CHECK (style IN ('classic', 'boutique', 'market'));

-- +goose Down
ALTER TABLE tenants DROP COLUMN IF EXISTS style;
ALTER TABLE malls DROP COLUMN IF EXISTS style;
