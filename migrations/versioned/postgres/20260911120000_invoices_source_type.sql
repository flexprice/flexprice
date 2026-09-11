-- migrate:up
-- Provenance, not live state: "checkout" means a hosted checkout session created this
-- invoice. The invoice guards read it to decide whether a session lookup is even
-- possible, so every ordinary invoice — including every subscription-cycle invoice in a
-- billing run — skips the query entirely.
--
-- Immutable once set. Nothing clears it when the session goes terminal; whether a session
-- is still active is answered by checkout_sessions, which is the source of truth.
ALTER TABLE invoices ADD COLUMN IF NOT EXISTS source_type varchar NULL;

-- migrate:down
ALTER TABLE invoices DROP COLUMN IF EXISTS source_type;
