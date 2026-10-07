-- +goose Up
-- Screens refresh every few seconds; these lookups must not scan the whole history.
CREATE INDEX IF NOT EXISTS order_items_order ON order_items(order_id);
CREATE INDEX IF NOT EXISTS consumption_session ON consumption(session_id);
CREATE INDEX IF NOT EXISTS calls_session ON calls(session_id);
CREATE INDEX IF NOT EXISTS orders_history ON orders(updated_at DESC) WHERE status IN ('delivered','cancelled');
CREATE INDEX IF NOT EXISTS calls_history ON calls(created_at DESC) WHERE status NOT IN ('pending','progress');
-- +goose Down
DROP INDEX IF EXISTS calls_history;
DROP INDEX IF EXISTS orders_history;
DROP INDEX IF EXISTS calls_session;
DROP INDEX IF EXISTS consumption_session;
DROP INDEX IF EXISTS order_items_order;
