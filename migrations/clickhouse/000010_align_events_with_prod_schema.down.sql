-- 000010 down — remove the projection added by the up migration on a fresh env.
-- Does NOT revert column codecs/types (no safe automatic reversal on populated
-- tables). On a fresh CREATE-only env, dropping the projection is sufficient.
ALTER TABLE flexprice.events DROP PROJECTION IF EXISTS proj_by_customer_event;
