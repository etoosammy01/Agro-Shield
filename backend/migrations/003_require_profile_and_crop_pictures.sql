-- Before applying this migration to an existing database, populate every
-- NULL photo_url and image_url. PostgreSQL intentionally rejects the
-- constraint while incomplete legacy rows remain.
ALTER TABLE farmers
    ALTER COLUMN photo_url SET NOT NULL;

ALTER TABLE crops
    ALTER COLUMN image_url SET NOT NULL;
