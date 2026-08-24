# Versioned migrations

Every schema change ships as a reviewed SQL file. `dbmate` applies what a database
has not seen yet and records it in `schema_migrations`.

This replaces Ent AutoMigrate as the deploy mechanism. Ent stays as the *source of
truth* for the schema and as the CI oracle — it is no longer what runs against a
production database.

```
migrations/versioned/postgres/     dbmate, ledger `schema_migrations`
migrations/versioned/clickhouse/   dbmate, one statement per file
scripts/migrations/                adoption + the CI gates
```

## Everyday change

```bash
# 1. edit ent/schema/x.go
make generate-ent

# 2. draft the SQL — Ent still knows what the schema needs
make migrate-ent-dry-run

# 3. create the file, paste the draft, then EDIT it
make migrate-new name=add_currency_to_invoices

# 4. locally
make migrate-up
make migrate-check
```

Step 3 is where `CONCURRENTLY`, lock timeouts and lane placement get added. The
draft is a starting point, never the answer.

## Rules

**One logical change per file.** dbmate runs a file in one transaction; small files
keep the blast radius of a failure small.

**Always write `-- migrate:down`,** even if it is only a comment explaining why
reversal is unsafe. A silent empty down is unreadable in six months.

**`transaction:false` files hold exactly one statement.** dbmate sends the body as a
single multi-statement query, which Postgres wraps in an implicit transaction — so
`CREATE INDEX CONCURRENTLY` fails if anything precedes it, including a `SET`.

**Timeouts come from the connection, not the file:**

```
?options=-c%20lock_timeout%3D3s%20-c%20statement_timeout%3D30s
```

and for any file building an index concurrently, `statement_timeout=0`. A build
killed by a timeout leaves an **INVALID** index that `IF NOT EXISTS` silently skips
forever, costing write overhead while inspection reports it as present. Drop it
before retrying.

**ClickHouse takes one statement per file** — the protocol rejects multi-statement
bodies outright. Enforced by `make migrate-check-clickhouse`.

## Adopting an existing database

Records the baseline as applied and executes nothing.

```bash
make migrate-adopt url="postgres://..." version=20260819000000
```

Verify with a fingerprint either side — it must be identical:

```bash
make migrate-fingerprint url="postgres://..."
```

## CI gates

`make migrate-check` runs all four.

| Gate | Catches |
|---|---|
| `migrate-check-sync` | a schema change that shipped without a migration |
| `migrate-check-checksum` | edits to a migration that already ran somewhere |
| `migrate-check-order` | parallel branches merging out of timestamp order |
| `migrate-check-clickhouse` | multi-statement ClickHouse files |

The sync check builds two throwaway databases — one from migrations alone, one from
migrations plus Ent — and compares schema fingerprints. It compares **end states,
not proposed statements**, because Ent emits permanent noise for any index predicate
whose spelling differs from Postgres' canonical form. "Is the diff empty?" can never
be a pass/fail test; "do the two schemas match?" can.

It needs a scratch Postgres:

```bash
docker run -d --name mig-pg -e POSTGRES_USER=flexprice \
  -e POSTGRES_PASSWORD=flexprice123 -e POSTGRES_DB=postgres \
  -p 5440:5432 postgres:16
```

## Index predicates

Write `entsql.IndexWhere(...)` in the form Postgres stores, not the form that reads
naturally. `checkout_status IN ('a','b')` is deparsed to `= ANY (...)`, the string
comparison never converges, and Ent proposes rebuilding the index on every run
forever. Copy `pg_get_indexdef` output.

Six of the eight statements pending against production on 2026-08-19 were this, and
nothing else.

## Open decision — ClickHouse `ON CLUSTER`

The incremental files here carry no `ON CLUSTER`, matching the existing single-node
set. A replicated cluster needs it, or DDL lands on one node only while the ledger
replicates — a replica then reports every migration applied while holding no tables.

`migrations/baseline/clickhouse_baseline_replicated_20260819.sql` covers this for a
fresh install. For incremental migrations the choice is a second directory, or
requiring every deployment to define a `{cluster}` macro so one set serves both.
Not decided here.
