-- migrate:up
-- subscriptions.line_item_grouping controls whether a charge whose cadence is
-- shorter than the subscription's (e.g. a monthly price on a quarterly sub)
-- bills as one invoice line item per charge period or one per billing period.
--
-- NOT NULL with a constant default: catalog-only in Postgres 11+, no table
-- rewrite regardless of row count. Lane A.
--
-- The default is PER_CHARGE_PERIOD, which is the fan-out behavior that predates
-- this column, so existing subscriptions keep their current invoice shape.
SET lock_timeout = '3s';
SET statement_timeout = '30s';

ALTER TABLE subscriptions
  ADD COLUMN IF NOT EXISTS line_item_grouping character varying(50) NOT NULL DEFAULT 'PER_CHARGE_PERIOD';

-- migrate:down
SET lock_timeout = '3s';
ALTER TABLE subscriptions DROP COLUMN IF EXISTS line_item_grouping;
