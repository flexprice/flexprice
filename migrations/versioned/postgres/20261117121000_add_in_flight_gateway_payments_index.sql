-- migrate:up transaction:false
-- Cross-tenant settlement crons list in-flight payments by gateway.
-- statement_timeout must be 0; a killed build leaves an INVALID index.
-- On "relation already exists": DROP INDEX CONCURRENTLY IF EXISTS idx_in_flight_gateway_payments;
CREATE INDEX CONCURRENTLY idx_in_flight_gateway_payments
  ON payments (payment_gateway, payment_status)
  WHERE (((status)::text = 'published'::text) AND ((payment_status)::text = ANY (ARRAY[('PENDING'::character varying)::text, ('PROCESSING'::character varying)::text])));

-- migrate:down transaction:false
DROP INDEX CONCURRENTLY IF EXISTS idx_in_flight_gateway_payments;
