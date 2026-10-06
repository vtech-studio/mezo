-- +goose Up
CREATE TABLE login_attempts (
 key text PRIMARY KEY, attempts integer NOT NULL CHECK(attempts>0), reset_at timestamptz NOT NULL
);
-- +goose Down
DROP TABLE login_attempts;
