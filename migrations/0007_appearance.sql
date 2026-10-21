-- +goose Up
ALTER TABLE malls ADD COLUMN IF NOT EXISTS palette TEXT NOT NULL DEFAULT 'violet'
    CHECK (palette IN ('forest', 'violet', 'ocean', 'terracotta'));
-- Layouts and palettes the mall keeps off its shops' Appearance page.
ALTER TABLE malls ADD COLUMN IF NOT EXISTS locked_styles TEXT[] NOT NULL DEFAULT '{}'
    CHECK (locked_styles <@ ARRAY['boutique', 'market']);
ALTER TABLE malls ADD COLUMN IF NOT EXISTS locked_palettes TEXT[] NOT NULL DEFAULT '{}'
    CHECK (locked_palettes <@ ARRAY['forest', 'violet', 'ocean', 'terracotta']);
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS palette TEXT NOT NULL DEFAULT 'forest'
    CHECK (palette IN ('forest', 'violet', 'ocean', 'terracotta'));
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS theme TEXT NOT NULL DEFAULT 'auto'
    CHECK (theme IN ('auto', 'light', 'dark'));

-- +goose Down
ALTER TABLE tenants DROP COLUMN IF EXISTS theme;
ALTER TABLE tenants DROP COLUMN IF EXISTS palette;
ALTER TABLE malls DROP COLUMN IF EXISTS locked_palettes;
ALTER TABLE malls DROP COLUMN IF EXISTS locked_styles;
ALTER TABLE malls DROP COLUMN IF EXISTS palette;
