# Versioned migrations

Every schema change ships as a reviewed SQL file. `dbmate` applies what a database
has not seen yet and records it in `schema_migrations`.

This replaces Ent AutoMigrate as the deploy mechanism. Ent stays the *source of
truth* for the schema and the CI oracle; it no longer runs against a real database.

```text
migrations/versioned/postgres/   the timeline, baseline first
scripts/migrations/              adoption + the CI gates
```

## Two kinds of database

**A new one** gets the whole timeline. `20260819000000_baseline.sql` builds the
schema, then everything after it applies. Nothing special to run.

**An existing one is adopted** — its versions are recorded and nothing executes:

```bash
make migrate-adopt url="postgres://..." dry=1   # print the plan, write nothing
make migrate-adopt url="postgres://..."         # record everything written so far
```

Run the dry pass first. This is done by hand, once per deployment and per client,
against production.

Only migrations added *after* that point ever run there. That is deliberate: the
existing files describe history each deployment already lived through by its own
route. India prod grew under AutoMigrate, GCP staging was DMS-migrated from AWS into
AlloyDB and then diverged — measured on 2026-08-26, by 610 catalog lines. Replaying
that history would be wrong, and `20260825000400` would drop a live uniqueness index
on staging.

**Forgetting to adopt is safe.** The baseline is the first migration, so it hits
`CREATE TABLE` on a table that already exists and stops with nothing recorded —
rather than applying later migrations to a schema nobody checked.

Once every current deployment is adopted, adoption is only needed for a database
that predates all this. New ones take the normal path.

## The rule for everything after the baseline

Migrations run on deployments you cannot inspect, so **do not assume a starting
state**. Guard on `to_regclass(...)` before touching a table, prefer matching
indexes on shape over Ent-derived names, and make re-running a no-op.

The `20260825000100`–`000400` entitlements migrations predate this rule and assume
India prod's layout. They are applied there by hand.

## How it runs on a deployment

Nobody applies migrations by hand. Both deploy paths run the same command,
`./migrate postgres up`, from the image being deployed — so the migrations that
run are always the ones that shipped with the code.

**Kubernetes / Helm** (GCP, and every client running the chart). The migration
Job is a `pre-install,pre-upgrade` hook, so it completes before any pod rolls.
Step 7 of that Job is `run-postgres-migrations`, gated on
`migration.steps.dbmate`. A client who will not allow a migration Job sets
`migration.enabled: false` and runs the same command themselves.

**AWS / ECS.** The `migrate` job in `.github/workflows/deploy.yml` runs one
task per target before the canary and before the production rollout, built from
the API service's own task definition so it inherits the same secrets, role,
subnets and security groups the application uses. A non-zero exit blocks every
deploy job.

Migrations therefore land **before** the new code, and on ECS before the
production approval gate. That only works because migrations are additive by the
rule above: the running old code keeps working against the migrated schema, and
a rejected approval leaves a schema the old image still serves.

The Job prints `--status` before applying, so the log says what was pending even
when a migration then fails. dbmate commits one file at a time, so a failed run
leaves the database at a known prefix of the timeline — never a half-applied
file.

To see what a database would receive without touching it:

```bash
./migrate postgres up --status
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

`migrate-generate` builds a throwaway database the way a fresh install is built,
asks Ent what is still missing, and writes that DDL into a new dbmate file. You do
not write SQL from scratch. If nothing is missing it says so and writes nothing.

Columns and tables come out with `IF NOT EXISTS` already applied — deployments hold
different schemas, so a migration may meet something that already exists. Index
creation deliberately does not: the draft is meant to gain `CONCURRENTLY` by hand,
and `IF NOT EXISTS` on a concurrent build silently skips an INVALID index left by an
earlier failure.

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

**Timeouts come from the connection, not the file**, because a `transaction:false`
file may hold exactly one statement and there is no room for a `SET`. Apply through
the wrapper rather than a bare `dbmate up`, which does not set them:

```bash
make migrate-up                      # or: ./scripts/migrations/apply.sh <dir>
```

It sets `lock_timeout=3s` so a blocked `ALTER` gives up instead of queueing every
query behind it, and `statement_timeout=0` because a `CREATE INDEX CONCURRENTLY`
killed by a timeout leaves an **INVALID** index — one that costs write overhead
forever while inspection reports it as present, so nothing retries it. Drop it
before retrying:

```bash
psql "$URL" -c "SELECT indexrelid::regclass FROM pg_index WHERE NOT indisvalid;"
```

**ClickHouse takes one statement per file** — the protocol rejects multi-statement
bodies outright. Enforced by `make migrate-check-clickhouse`.

## Adopting an existing database

Records the baseline as applied and executes nothing.

```bash
make migrate-adopt url="postgres://..."          # adopts at head
```

**Head is the normal choice for an existing deployment.** Everything already
written is recorded as applied and nothing runs, so only migrations added *after*
this point ever execute there. The existing files describe history each deployment
already lived through by its own route — replaying them would be wrong, and on GCP
staging `20260825000400` would drop a live uniqueness index.

Pass `version=<timestamp>` only when a deployment genuinely needs some of the
existing set applied.

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

It fails rather than printing a hash if the database is unreachable or returns
nothing. That matters: `psql ... | shasum` reports the *pipeline's* last status, so
a failed connection still exits 0 and hashes the empty string —
`e3b0c442...b7852b855` — and two unreachable databases then compare as identical.

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
./scripts/migrations/checksum-check.sh --update
git add migrations/.hashes
```

The sync check builds two throwaway databases the way a fresh install is built —
baseline, then migrations — and applies Ent to one of them, then compares schema
fingerprints. It compares **end states,
not proposed statements**, because Ent emits permanent noise for any index predicate
whose spelling differs from Postgres' canonical form. "Is the diff empty?" can never
be a pass/fail test; "do the two schemas match?" can.

It builds throwaway databases on your local Postgres — the one
`docker compose up -d postgres` provides on `:5432`. Point it elsewhere with
`PGHOST_` / `PGPORT_`.

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

## Index predicates — enforced by CI, not by memory

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

You do not have to remember this — `make migrate-generate` enforces it, using a
rule that needs no knowledge of what "canonical" means:

> once the migrations satisfy Ent, Ent must have nothing left to propose.

So it builds a database from the migrations, asks Ent what it would still change,
and fails on anything it names — because by definition that change is a no-op Ent
will keep proposing forever. The failure prints the exact string to paste:

```text
phantom check: FAIL — Ent proposes changes the migrations already satisfy.
  DROP INDEX "usagerecord_tenant_id_environm_38da5f..."
  CREATE UNIQUE INDEX "usagerecord_..." WHERE status = 'published';

Cause: an entsql.IndexWhere predicate is written in a form Postgres does not
store. Replace it with what Postgres actually stores:

  usagerecord_tenant_id_environm_38da5f...
    entsql.IndexWhere("((status)::text = 'published'::text)")
```

It applies that test to its own draft and **refuses to write the file** when it
finds residue. A draft containing no-op DDL is a trap: the
`DROP`+`CREATE` pair is indistinguishable from a real predicate change once it is
sitting in a migration. Fixing the annotation is two lines and the exact string is
printed, so blocking is cheaper than a draft nobody can safely review.

This only makes sense once the migrations already satisfy Ent: if a real migration
is missing, the residue is that missing change rather than a phantom. In practice
that holds, because you run the generator precisely to write the missing one.

## Open decision — ClickHouse `ON CLUSTER`

The incremental files here carry no `ON CLUSTER`, matching the existing single-node
set. A replicated cluster needs it, or DDL lands on one node only while the ledger
replicates — a replica then reports every migration applied while holding no tables.

`migrations/baseline/clickhouse_baseline_replicated_20260819.sql` covers this for a
fresh install. For incremental migrations the choice is a second directory, or
requiring every deployment to define a `{cluster}` macro so one set serves both.
Not decided here.
