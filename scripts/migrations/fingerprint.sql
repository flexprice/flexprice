-- Deterministic schema fingerprint. Reads the catalog rather than pg_dump, whose
-- text is not byte-stable for the same schema.
--
-- character_maximum_length / numeric_precision / numeric_scale are NOT optional:
-- varchar(10) and varchar(20) are both 'character varying' in data_type, so
-- without them this is blind to every column widening and precision change.
\pset tuples_only on
\pset format unaligned

SELECT 'column|'||c.table_name||'|'||c.column_name||'|'||c.data_type
       ||'('||coalesce(c.character_maximum_length::text,
                       c.numeric_precision::text||','||coalesce(c.numeric_scale::text,'0'),
                       c.datetime_precision::text, '-')||')'
       ||'|'||c.is_nullable||'|'||
       -- AutoMigrate writes '0'::numeric where a migration file writes 0.
       -- Same value, different stored text.
       regexp_replace(coalesce(c.column_default,''), '^''([^'']*)''::[a-zA-Z ]+$', '\1')
FROM information_schema.columns c
WHERE c.table_schema = 'public' AND c.table_name NOT LIKE 'schema_migrations%'
UNION ALL
SELECT 'index|'||i.tablename||'|'||i.indexname||'|'||i.indexdef
FROM pg_indexes i
WHERE i.schemaname = 'public' AND i.tablename NOT LIKE 'schema_migrations%'
UNION ALL
SELECT 'constraint|'||rel.relname||'|'||con.conname||'|'||pg_get_constraintdef(con.oid)
FROM pg_constraint con
JOIN pg_class rel ON rel.oid = con.conrelid
JOIN pg_namespace n ON n.oid = rel.relnamespace
WHERE n.nspname = 'public' AND rel.relname NOT LIKE 'schema_migrations%'
ORDER BY 1;
