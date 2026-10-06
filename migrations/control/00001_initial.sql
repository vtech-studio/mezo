-- +goose Up
CREATE TABLE tenants (
 id text PRIMARY KEY, slug text NOT NULL UNIQUE, name text NOT NULL,
 db_name text NOT NULL UNIQUE, db_role text NOT NULL UNIQUE, db_secret text NOT NULL,
 status text NOT NULL CHECK (status IN ('provisioning','active','failed')),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE administrators (email text PRIMARY KEY, password_hash text NOT NULL);
CREATE TABLE auth_sessions (
 token_hash text PRIMARY KEY, kind text NOT NULL CHECK(kind IN ('admin','staff','customer')),
 tenant_id text REFERENCES tenants(id), subject text NOT NULL, csrf text NOT NULL,
 expires_at timestamptz NOT NULL
);
CREATE INDEX auth_sessions_expiry ON auth_sessions(expires_at);
-- +goose Down
DROP TABLE auth_sessions;
DROP TABLE administrators;
DROP TABLE tenants;
