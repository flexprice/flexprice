-- Add billing_cadence column to subscription_line_items.
-- Mirrors the price's billing_cadence (RECURRING or ONETIME) so invoice
-- classification can identify one-time charges without loading the price.
ALTER TABLE subscription_line_items
    ADD COLUMN IF NOT EXISTS billing_cadence VARCHAR(20) NOT NULL DEFAULT 'RECURRING';

-- Backfill: mark any existing line items whose price is ONETIME.
UPDATE subscription_line_items sli
SET billing_cadence = 'ONETIME'
FROM prices p
WHERE sli.price_id = p.id
  AND p.billing_cadence = 'ONETIME';
