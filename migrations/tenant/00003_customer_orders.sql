-- +goose Up
ALTER TABLE orders ALTER COLUMN created_by DROP NOT NULL;
ALTER TABLE orders ADD COLUMN customer_session_id text REFERENCES table_sessions(id);
ALTER TABLE orders ADD CONSTRAINT orders_actor_check CHECK (
 (created_by IS NOT NULL AND customer_session_id IS NULL) OR
 (created_by IS NULL AND customer_session_id IS NOT NULL AND customer_session_id=session_id)
);
ALTER TABLE orders ADD CONSTRAINT orders_customer_retry UNIQUE(customer_session_id,request_key);
ALTER TABLE consumption ALTER COLUMN created_by DROP NOT NULL;
ALTER TABLE consumption ADD CONSTRAINT consumption_actor_check CHECK (created_by IS NOT NULL OR order_id IS NOT NULL);
ALTER TABLE order_events ALTER COLUMN staff_id DROP NOT NULL;
ALTER TABLE order_events ADD COLUMN customer_session_id text REFERENCES table_sessions(id);
ALTER TABLE order_events ADD CONSTRAINT order_events_actor_check CHECK ((staff_id IS NULL) <> (customer_session_id IS NULL));
-- +goose Down
-- Refuse rollback when customer orders exist; never discard their audit history.
ALTER TABLE consumption ALTER COLUMN created_by SET NOT NULL;
ALTER TABLE consumption DROP CONSTRAINT consumption_actor_check;
ALTER TABLE orders ALTER COLUMN created_by SET NOT NULL;
ALTER TABLE order_events ALTER COLUMN staff_id SET NOT NULL;
ALTER TABLE order_events DROP CONSTRAINT order_events_actor_check;
ALTER TABLE order_events DROP COLUMN customer_session_id;
ALTER TABLE orders DROP CONSTRAINT orders_actor_check;
ALTER TABLE orders DROP CONSTRAINT orders_customer_retry;
ALTER TABLE orders DROP COLUMN customer_session_id;
