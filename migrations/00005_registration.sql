-- +goose Up
ALTER TABLE users
    ADD COLUMN email TEXT NOT NULL DEFAULT '';

CREATE TABLE tenant_registrations (
    id                 BIGSERIAL PRIMARY KEY,
    slug               TEXT NOT NULL,
    name               TEXT NOT NULL,
    admin_username     TEXT NOT NULL,
    admin_display_name TEXT NOT NULL,
    admin_email        TEXT NOT NULL,
    password_hash      TEXT NOT NULL,
    token_hash         BYTEA NOT NULL,
    expires_at         TIMESTAMPTZ NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT tenant_registrations_slug_not_blank CHECK (length(trim(slug)) > 0),
    CONSTRAINT tenant_registrations_name_not_blank CHECK (length(trim(name)) > 0),
    CONSTRAINT tenant_registrations_email_not_blank CHECK (length(trim(admin_email)) > 0),
    CONSTRAINT tenant_registrations_slug_format CHECK (slug ~ '^[a-z0-9]([a-z0-9-]*[a-z0-9])?$')
);

CREATE UNIQUE INDEX tenant_registrations_slug_uidx ON tenant_registrations (slug);
CREATE UNIQUE INDEX tenant_registrations_token_uidx ON tenant_registrations (token_hash);
CREATE INDEX tenant_registrations_expires_idx ON tenant_registrations (expires_at);

-- +goose Down
DROP INDEX IF EXISTS tenant_registrations_expires_idx;
DROP INDEX IF EXISTS tenant_registrations_token_uidx;
DROP INDEX IF EXISTS tenant_registrations_slug_uidx;
DROP TABLE IF EXISTS tenant_registrations;
ALTER TABLE users DROP COLUMN IF EXISTS email;
