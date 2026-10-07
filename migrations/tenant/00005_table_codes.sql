-- +goose Up
-- Renewed whenever the table opens; tables opened before this change have none.
ALTER TABLE table_sessions ADD COLUMN code text CHECK (code ~ '^[0-9]{4}$');
-- +goose Down
ALTER TABLE table_sessions DROP COLUMN code;
