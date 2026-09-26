-- +goose Up
-- Entfernt den Seed-Eintrag „Root“; vorhandene Untergruppen werden Stammgruppen.
UPDATE groups
SET parent_id = NULL
WHERE parent_id IN (
    SELECT id FROM groups
    WHERE name = 'Root'
      AND description = 'Wissenswurzel – Ausgangspunkt des Skillbaums.'
);

DELETE FROM groups
WHERE name = 'Root'
  AND description = 'Wissenswurzel – Ausgangspunkt des Skillbaums.';

-- +goose Down
INSERT INTO groups (name, description, created_by)
VALUES ('Root', 'Wissenswurzel – Ausgangspunkt des Skillbaums.', 1);
