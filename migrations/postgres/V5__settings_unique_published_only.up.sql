-- Restrict the settings uniqueness constraint to live rows.
--
-- The old index included status, so status was part of the key and at most one
-- archived row could exist per setting. Deleting a key twice — delete,
-- recreate, delete again — archived a row onto the slot the previous tombstone
-- held and failed the constraint: the caller saw a 500 and the setting stayed
-- published. For saml_config that meant a tenant could not turn its own SSO
-- off through the API.
--
-- The replacement applies only to published rows, so deletion history
-- accumulates freely while the guarantee that matters is unchanged: one live
-- configuration per tenant, environment and key.
--
-- Ent creates the new index from the schema but does not drop the old one, so
-- the drop has to be explicit — without it both exist and the collision
-- remains.

CREATE UNIQUE INDEX IF NOT EXISTS settings_tenant_id_environment_id_key
    ON public.settings USING btree (tenant_id, environment_id, key)
    WHERE ((status)::text = 'published'::text);

DROP INDEX IF EXISTS settings_tenant_id_environment_id_status_key;
