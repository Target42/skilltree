-- +goose Up
CREATE TABLE users (
    id          BIGSERIAL PRIMARY KEY,
    username    TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL DEFAULT '',
    is_admin    BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE groups (
    id          BIGSERIAL PRIMARY KEY,
    parent_id   BIGINT REFERENCES groups (id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_by  BIGINT REFERENCES users (id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT groups_name_not_blank CHECK (length(trim(name)) > 0)
);

CREATE INDEX groups_parent_id_idx ON groups (parent_id);

CREATE TABLE skills (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_by  BIGINT REFERENCES users (id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT skills_name_not_blank CHECK (length(trim(name)) > 0)
);

CREATE TABLE group_skills (
    group_id    BIGINT NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
    skill_id    BIGINT NOT NULL REFERENCES skills (id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, skill_id)
);

CREATE INDEX group_skills_skill_id_idx ON group_skills (skill_id);

CREATE TABLE skill_ratings (
    user_id     BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    skill_id    BIGINT NOT NULL REFERENCES skills (id) ON DELETE CASCADE,
    level       SMALLINT NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, skill_id),
    CONSTRAINT skill_ratings_level_range CHECK (level >= 0 AND level <= 5)
);

CREATE TABLE group_watches (
    user_id     BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    group_id    BIGINT NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, group_id)
);

CREATE TABLE notifications (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    group_id    BIGINT REFERENCES groups (id) ON DELETE SET NULL,
    skill_id    BIGINT REFERENCES skills (id) ON DELETE SET NULL,
    message     TEXT NOT NULL,
    read_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX notifications_user_unread_idx ON notifications (user_id) WHERE read_at IS NULL;

-- Seed demo user for local development (login later).
INSERT INTO users (username, display_name, is_admin)
VALUES ('demo', 'Demo User', TRUE);

-- +goose Down
DROP TABLE IF EXISTS notifications;
DROP TABLE IF EXISTS group_watches;
DROP TABLE IF EXISTS skill_ratings;
DROP TABLE IF EXISTS group_skills;
DROP TABLE IF EXISTS skills;
DROP TABLE IF EXISTS groups;
DROP TABLE IF EXISTS users;
