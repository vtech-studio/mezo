-- +goose Up
ALTER TABLE staff DROP CONSTRAINT staff_role_check;
ALTER TABLE staff ADD CONSTRAINT staff_role_check CHECK(role IN ('manager','waiter','kitchen'));
ALTER TABLE staff ADD COLUMN active boolean NOT NULL DEFAULT true;
CREATE TABLE orders (
 id text PRIMARY KEY, session_id text NOT NULL REFERENCES table_sessions(id),
 created_by text NOT NULL REFERENCES staff(id), request_key text NOT NULL, request_hash text NOT NULL,
 status text NOT NULL DEFAULT 'queued' CHECK(status IN ('queued','preparing','ready','delivered','cancelled')),
 note text NOT NULL DEFAULT '' CHECK(char_length(note)<=300), assigned_to text REFERENCES staff(id),
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(created_by,request_key)
);
CREATE TABLE order_items (
 id text PRIMARY KEY, order_id text NOT NULL REFERENCES orders(id), menu_id text NOT NULL REFERENCES menu_items(id),
 name text NOT NULL, price integer NOT NULL CHECK(price>=0), qty integer NOT NULL CHECK(qty BETWEEN 1 AND 100),
 note text NOT NULL DEFAULT '' CHECK(char_length(note)<=200)
);
CREATE INDEX orders_queue ON orders(status,created_at);
CREATE INDEX orders_session ON orders(session_id);
CREATE TABLE order_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, order_id text NOT NULL REFERENCES orders(id),
 action text NOT NULL, staff_id text NOT NULL REFERENCES staff(id), note text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE consumption ADD COLUMN order_id text REFERENCES orders(id);
ALTER TABLE consumption ADD COLUMN cancelled boolean NOT NULL DEFAULT false;
CREATE TABLE settlements (
 session_id text PRIMARY KEY REFERENCES table_sessions(id), closed_by text NOT NULL REFERENCES staff(id),
 method text NOT NULL CHECK(method IN ('cash','pix','card','other','none')),
 subtotal bigint NOT NULL CHECK(subtotal>=0), service bigint NOT NULL CHECK(service>=0),
 total bigint NOT NULL CHECK(total=subtotal+service), created_at timestamptz NOT NULL DEFAULT now()
);
-- +goose Down
DROP TABLE settlements;
ALTER TABLE consumption DROP COLUMN cancelled;
ALTER TABLE consumption DROP COLUMN order_id;
DROP TABLE order_events;
DROP TABLE order_items;
DROP TABLE orders;
ALTER TABLE staff DROP COLUMN active;
ALTER TABLE staff DROP CONSTRAINT staff_role_check;
ALTER TABLE staff ADD CONSTRAINT staff_role_check CHECK(role IN ('manager','waiter'));
