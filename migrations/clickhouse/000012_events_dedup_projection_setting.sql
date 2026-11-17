-- Needed before ReplacingMergeTree projections.
ALTER TABLE flexprice.events MODIFY SETTING deduplicate_merge_projection_mode = 'rebuild';
