--
-- Plan-price sync sequence.
--
-- Source of monotonic numbers stamped on `prices.sequence` whenever a
-- plan-price's state changes in a way subscriptions need to react to:
-- create, end_date set, or compatibility-affecting edit (currency,
-- billing_period, billing_period_count, type).
--
-- The `prices.sequence` column is declared and managed by the Ent
-- schema (ent/schema/price.go) with `DEFAULT nextval('prices_sequence_seq')`,
-- which is why the sequence must exist before `make migrate-ent` runs.
-- `migrate-postgres` runs first per the init-db ordering in the Makefile.
--

CREATE SEQUENCE IF NOT EXISTS prices_sequence_seq;
