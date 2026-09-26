-- +goose Up
CREATE TABLE tenants (
    id          BIGSERIAL PRIMARY KEY,
    slug        TEXT NOT NULL UNIQUE,
    name        TEXT NOT NULL,
    is_active   BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT tenants_slug_not_blank CHECK (length(trim(slug)) > 0),
    CONSTRAINT tenants_name_not_blank CHECK (length(trim(name)) > 0),
    CONSTRAINT tenants_slug_format CHECK (slug ~ '^[a-z0-9]([a-z0-9-]*[a-z0-9])?$')
);

INSERT INTO tenants (slug, name)
VALUES ('demo', 'Demo Firma');

ALTER TABLE users
    ADD COLUMN tenant_id BIGINT REFERENCES tenants (id),
    ADD COLUMN is_platform_admin BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN is_active BOOLEAN NOT NULL DEFAULT TRUE;

UPDATE users
SET tenant_id = (SELECT id FROM tenants WHERE slug = 'demo'),
    is_platform_admin = TRUE
WHERE username = 'demo';

UPDATE users
SET tenant_id = (SELECT id FROM tenants WHERE slug = 'demo')
WHERE tenant_id IS NULL;

ALTER TABLE users
    ALTER COLUMN tenant_id SET NOT NULL;

ALTER TABLE users
    DROP CONSTRAINT IF EXISTS users_username_key;

ALTER TABLE users
    ADD CONSTRAINT users_tenant_username_key UNIQUE (tenant_id, username);

CREATE INDEX users_tenant_id_idx ON users (tenant_id);

ALTER TABLE groups
    ADD COLUMN tenant_id BIGINT REFERENCES tenants (id);

UPDATE groups
SET tenant_id = (SELECT id FROM tenants WHERE slug = 'demo')
WHERE tenant_id IS NULL;

ALTER TABLE groups
    ALTER COLUMN tenant_id SET NOT NULL;

CREATE INDEX groups_tenant_id_idx ON groups (tenant_id);

ALTER TABLE skills
    ADD COLUMN tenant_id BIGINT REFERENCES tenants (id);

UPDATE skills
SET tenant_id = (SELECT id FROM tenants WHERE slug = 'demo')
WHERE tenant_id IS NULL;

ALTER TABLE skills
    ALTER COLUMN tenant_id SET NOT NULL;

CREATE INDEX skills_tenant_id_idx ON skills (tenant_id);

ALTER TABLE deletion_requests
    ADD COLUMN tenant_id BIGINT REFERENCES tenants (id);

UPDATE deletion_requests
SET tenant_id = (SELECT id FROM tenants WHERE slug = 'demo')
WHERE tenant_id IS NULL;

ALTER TABLE deletion_requests
    ALTER COLUMN tenant_id SET NOT NULL;

CREATE INDEX deletion_requests_tenant_id_idx ON deletion_requests (tenant_id);

-- +goose Down
DROP INDEX IF EXISTS deletion_requests_tenant_id_idx;
ALTER TABLE deletion_requests DROP COLUMN IF EXISTS tenant_id;

DROP INDEX IF EXISTS skills_tenant_id_idx;
ALTER TABLE skills DROP COLUMN IF EXISTS tenant_id;

DROP INDEX IF EXISTS groups_tenant_id_idx;
ALTER TABLE groups DROP COLUMN IF EXISTS tenant_id;

DROP INDEX IF EXISTS users_tenant_id_idx;
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_tenant_username_key;
ALTER TABLE users
    DROP COLUMN IF EXISTS tenant_id,
    DROP COLUMN IF EXISTS is_platform_admin,
    DROP COLUMN IF EXISTS is_active;
ALTER TABLE users ADD CONSTRAINT users_username_key UNIQUE (username);

DROP TABLE IF EXISTS tenants;
