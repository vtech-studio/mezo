-- +goose Up
-- Advanced after every change; polling screens reload only when it moves.
ALTER TABLE tenants ADD COLUMN revision bigint NOT NULL DEFAULT 0;
-- +goose Down
ALTER TABLE tenants DROP COLUMN revision;
