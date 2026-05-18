ALTER TABLE invoice_line_items
    ADD COLUMN IF NOT EXISTS sub_line_item_id VARCHAR(50),
    ADD COLUMN IF NOT EXISTS adjusted_from_entitlement_quantity NUMERIC(20,8);
