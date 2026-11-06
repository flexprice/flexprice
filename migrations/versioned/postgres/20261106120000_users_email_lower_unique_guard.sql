-- migrate:up
-- Stops IF NOT EXISTS skipping invalid index.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_index
        WHERE indexrelid = to_regclass('idx_user_email_lower_unique') AND NOT indisvalid
    ) THEN
        RAISE EXCEPTION 'idx_user_email_lower_unique is INVALID: drop it, then retry';
    END IF;
END
$$;

-- migrate:down
-- Guard only; nothing to undo.
