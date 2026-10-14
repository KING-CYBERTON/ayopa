-- +goose Up
ALTER TABLE malls ADD COLUMN IF NOT EXISTS tagline TEXT NOT NULL DEFAULT ''
    CHECK (length(tagline) <= 140);
ALTER TABLE malls ADD COLUMN IF NOT EXISTS description TEXT NOT NULL DEFAULT ''
    CHECK (length(description) <= 600);
ALTER TABLE malls ADD COLUMN IF NOT EXISTS contact_email TEXT NOT NULL DEFAULT ''
    CHECK (length(contact_email) <= 254);

-- +goose Down
-- WARNING: removes all landing page copy from every mall. For development only.
ALTER TABLE malls DROP COLUMN IF EXISTS contact_email;
ALTER TABLE malls DROP COLUMN IF EXISTS description;
ALTER TABLE malls DROP COLUMN IF EXISTS tagline;
