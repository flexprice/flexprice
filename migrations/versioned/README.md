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

# 2. draft the migration — writes the file with the DDL already in it
make migrate-generate name=add_currency_to_invoices

# 3. EDIT the draft, then verify
make migrate-check
```

`migrate-generate` builds a throwaway database from the committed migrations, asks
Ent what is still missing, and writes that DDL into a new dbmate file. You do not
write SQL from scratch. If nothing is missing it says so and writes nothing.

**The draft is not the answer.** It has no `CONCURRENTLY`, no lane placement, and a
`TODO` where the down block goes. Editing it is the review step, and it is where the
decisions in this document get applied.

Use `make migrate-new name=...` for an empty file when the change is not something
Ent models — data backfills, functions, anything hand-written.

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

Adoption records a **claim** that the database already contains everything those
migrations would have created. Nothing verifies it afterwards, and if the claim is
wrong dbmate skips that DDL forever. So pass a reference — a scratch database built
from the same migration set — and the fingerprints must match before anything is
written:

```bash
./scripts/migrations/adopt.sh "$PROD_URL" migrations/versioned/postgres \
  20260819000000 --reference "$SCRATCH_URL"
```

Without `--reference` it warns and proceeds; treat that as a local-only shortcut.

Verify with a fingerprint either side — it must be identical:

```bash
make migrate-fingerprint url="postgres://..."
```

## CI gates

`make migrate-check` runs the Postgres gates; CI adds two more.

| Gate | Runs in | Catches |
|---|---|---|
| ent codegen current | CI | a schema edit without `make generate-ent` — first, because a stale `ent/` makes every later gate read the wrong schema and pass |
| `migrate-check-sync` | both | a schema change that shipped without a migration |
| `migrate-check-checksum` | both | edits to a shipped migration, and migrations missing from `.hashes` |
| `migrate-check-order` | both | parallel branches merging out of timestamp order |
| replay from zero | CI | the set does not apply to an empty database |
| `migrate-check-clickhouse` | neither — disabled | multi-statement ClickHouse files |

`.hashes` is authoritative and is not rewritten by the check. Adding a migration
means recording it deliberately:

```bash
./scripts/migrations/checksum-check.sh migrations/versioned migrations/versioned/.hashes --update
git add migrations/versioned/.hashes
```

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

## Orphaned indexes — a known, unsolved gap

`DropIndex` is in the skip set at every level, including CI. So an index that exists
in a database but is not declared in `ent/schema/` is never reported and never
removed. Two ways they appear:

- **Ent renames on change.** Adding a column to an index gives it a new name, so the
  diff emits `CREATE` for the new one and nothing for the one it replaced. Both then
  exist. This is how
  `entitlement_tenant_id_environment_id_entity_type_entity_id_feat` ended up
  duplicating the index Ent manages — migration `20260825000400` cleans that one up.
- **Deliberate hand-made indexes** added during an incident and never declared.

Unskipping `DropIndex` was tried and reverted: against the production baseline it
proposes dropping a dozen indexes the schema does not declare, some of them
deliberate. That is a report for a human, not DDL to auto-draft.

Nothing here detects them yet. The mechanism to build is a one-directional
comparison the other way — objects the database has that Ent does not declare —
run as a report against real deployments, not as a PR gate.

## Index predicates — cosmetic, but keep the convention

Ent compares index predicates as strings. `checkout_status IN ('a','b')` is stored by
Postgres as `= ANY (...)`, the comparison never converges, and Ent proposes rebuilding
the index on every run forever. Six of the eight statements pending against production
on 2026-08-19 were exactly this and nothing else.

**Under versioned migrations this stops mattering for correctness.** AutoMigrate no
longer runs against a real database, so a phantom cannot cause DDL anywhere. And the
sync check compares end states rather than proposed statements, so both spellings
deparse to the same stored form and cancel out — verified: reverting `usagerecord.go`
to the naive spelling leaves `make migrate-check-sync` green.

What it still costs is a noisy draft, and that is not harmless. `make
migrate-generate` runs with `--allow-index-changes` so it can draft a genuine
predicate change — measured: a real change drafts exactly the `DROP` + `CREATE` pair
you want. But a non-canonical spelling drafts the **same two statements** for an
index that needs nothing.

The reviewer cannot tell those apart by looking. Miss it, and you ship a migration
that drops and rebuilds an index for no reason — on `events` or `feature_usage` that
is an incident, not a nit.

So keep writing predicates in the stored form. Copy `pg_get_indexdef` output. It is
not a correctness gate any more; it is what keeps generated drafts trustworthy.

## Open decision — ClickHouse `ON CLUSTER`

The incremental files here carry no `ON CLUSTER`, matching the existing single-node
set. A replicated cluster needs it, or DDL lands on one node only while the ledger
replicates — a replica then reports every migration applied while holding no tables.

`migrations/baseline/clickhouse_baseline_replicated_20260819.sql` covers this for a
fresh install. For incremental migrations the choice is a second directory, or
requiring every deployment to define a `{cluster}` macro so one set serves both.
Not decided here.
