-- +goose Up
CREATE TABLE restaurant_brand (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    logo_url text NOT NULL DEFAULT '',
    logo_public_id text NOT NULL DEFAULT ''
);
INSERT INTO restaurant_brand(singleton) VALUES(true);
ALTER TABLE menu_items ADD COLUMN image_url text NOT NULL DEFAULT '';
ALTER TABLE menu_items ADD COLUMN image_public_id text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE menu_items DROP COLUMN image_public_id, DROP COLUMN image_url;
DROP TABLE restaurant_brand;
