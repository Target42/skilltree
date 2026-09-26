-- +goose Up
ALTER TABLE users
    ADD COLUMN password_hash TEXT NOT NULL DEFAULT '',
    ADD COLUMN is_deletion_officer BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE users
SET password_hash = '$2a$10$dZL.JhUq9oM1/GvnOVcoAunXopwVELJJcQNWL4bFMyxzs8Brqy8xC',
    is_admin = TRUE,
    is_deletion_officer = TRUE
WHERE username = 'demo';

INSERT INTO users (username, display_name, password_hash, is_admin, is_deletion_officer)
VALUES (
    'alice',
    'Alice Beispiel',
    '$2a$10$dZL.JhUq9oM1/GvnOVcoAunXopwVELJJcQNWL4bFMyxzs8Brqy8xC',
    FALSE,
    FALSE
)
ON CONFLICT (username) DO NOTHING;

CREATE TABLE deletion_requests (
    id            BIGSERIAL PRIMARY KEY,
    entity_type   TEXT NOT NULL CHECK (entity_type IN ('group', 'skill')),
    entity_id     BIGINT NOT NULL,
    entity_label  TEXT NOT NULL DEFAULT '',
    requested_by  BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    reason        TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT 'pending'
                  CHECK (status IN ('pending', 'approved', 'rejected')),
    reviewed_by   BIGINT REFERENCES users (id) ON DELETE SET NULL,
    review_note   TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    reviewed_at   TIMESTAMPTZ
);

CREATE UNIQUE INDEX deletion_requests_one_pending
    ON deletion_requests (entity_type, entity_id)
    WHERE status = 'pending';

CREATE INDEX deletion_requests_status_idx ON deletion_requests (status, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS deletion_requests;
ALTER TABLE users
    DROP COLUMN IF EXISTS password_hash,
    DROP COLUMN IF EXISTS is_deletion_officer;
