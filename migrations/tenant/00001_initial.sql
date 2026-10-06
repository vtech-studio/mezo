-- +goose Up
CREATE TABLE staff (
 id text PRIMARY KEY, name text NOT NULL, email text NOT NULL UNIQUE,
 password_hash text NOT NULL, role text NOT NULL CHECK(role IN ('manager','waiter'))
);
CREATE TABLE dining_tables (number integer PRIMARY KEY CHECK(number BETWEEN 1 AND 500), seats integer NOT NULL DEFAULT 2 CHECK(seats BETWEEN 1 AND 30), qr_token text NOT NULL UNIQUE);
CREATE TABLE table_sessions (
 id text PRIMARY KEY, table_number integer NOT NULL REFERENCES dining_tables(number),
 service boolean NOT NULL DEFAULT true, opened_at timestamptz NOT NULL DEFAULT now(), closed_at timestamptz
);
CREATE UNIQUE INDEX one_open_session ON table_sessions(table_number) WHERE closed_at IS NULL;
CREATE TABLE menu_items (
 id text PRIMARY KEY, name text NOT NULL, description text NOT NULL DEFAULT '',
 price integer NOT NULL CHECK(price BETWEEN 0 AND 10000000), category text NOT NULL,
 art text NOT NULL DEFAULT 'burrata', tags jsonb NOT NULL DEFAULT '[]', active boolean NOT NULL DEFAULT true
);
CREATE TABLE consumption (
 id text PRIMARY KEY, session_id text NOT NULL REFERENCES table_sessions(id), menu_id text NOT NULL REFERENCES menu_items(id),
 name text NOT NULL, price integer NOT NULL CHECK(price >= 0), qty integer NOT NULL CHECK(qty BETWEEN 1 AND 100),
 created_by text NOT NULL REFERENCES staff(id), created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE calls (
 id text PRIMARY KEY, session_id text NOT NULL REFERENCES table_sessions(id), reason text NOT NULL CHECK(reason IN ('help','order','water','bill')),
 note text NOT NULL DEFAULT '' CHECK(char_length(note)<=140), status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','progress','done','cancelled')),
 assigned_to text REFERENCES staff(id), created_at timestamptz NOT NULL DEFAULT now(), accepted_at timestamptz, completed_at timestamptz
);
CREATE UNIQUE INDEX one_active_reason ON calls(session_id,reason) WHERE status IN ('pending','progress');
CREATE INDEX calls_queue ON calls(status,created_at);
CREATE TABLE call_events (id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, call_id text NOT NULL REFERENCES calls(id), action text NOT NULL, staff_id text REFERENCES staff(id), created_at timestamptz NOT NULL DEFAULT now());
-- +goose Down
DROP TABLE call_events;
DROP TABLE calls;
DROP TABLE consumption;
DROP TABLE menu_items;
DROP TABLE table_sessions;
DROP TABLE dining_tables;
DROP TABLE staff;
