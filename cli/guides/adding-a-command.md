# Adding or renaming a curated command

The CLI does not generate command names from the API spec — it resolves them
through `cli/spec/commands.yaml`, a hand-maintained map from `resource
action` to an OpenAPI `operationId`. This is the single most common
maintenance task in this repository: the backend adds an endpoint, and
someone needs to give it a real name.

## Why this can't be automatic

There is no `GET /customers` in the API — listing is `POST
/customers/search` (`queryCustomer`). A name derived mechanically from the
path and HTTP method has nothing to derive `customers list` from. Worse, a
purely mechanical bootstrap tool that resolved collisions
alphabetically once produced two silent misassignments: `entitlements
retrieve` resolved to `getAddonEntitlements` instead of `getEntitlement`, and
`subscriptions list` resolved to `listAllSubscriptionSchedules` instead of
`querySubscription` — both because the wrong operation ID happened to sort
first. Every command name in this CLI reflects a human decision for exactly
this reason.

## What happens when you don't do anything

Nothing breaks. `internal/spec/registry.go`'s validation is default-allow: an
operation missing from `commands.yaml` gets an automatically derived name
(`kebab-case` of its tag and operationId) and a warning, printed under
`--debug`. CI (`.github/workflows/cli-validate.yml`) surfaces this as a
notice, not a failure — a backend PR that adds an endpoint is never blocked
on this file.

## Steps to give it a real name

1. Find the new operation. If you know its `operationId`, search for it in
   `docs/swagger/swagger-3-0.json`. If you only know roughly what it does,
   run the registry's bootstrap tool to see what name it would get by
   default:

   ```bash
   cd cli && go run ./tools/bootstrap-commands | grep -A2 -B2 <partial-name>
   ```

2. Open `cli/spec/commands.yaml` and find (or create) the resource block for
   the right resource name — match the existing pattern of domain nouns
   (`customers`, `invoices`, `subscriptions`), not the raw API tag.

3. Add the mapping using a domain verb, not an HTTP verb — `retrieve` not
   `get`, `list` not `search`, matching what similar existing entries in the
   file already use:

   ```yaml
   invoices:
     retrieve: getInvoice
     finalize: finalizeInvoice     # <- e.g. adding this line
   ```

4. If the new operation is a list operation, add a `columns:` entry so table
   output shows something useful instead of falling back to a generic
   heuristic:

   ```yaml
   invoices:
     columns: [id, invoice_number, invoice_status, total, created_at]
   ```

5. Run the registry's validation test:

   ```bash
   cd cli && go test ./internal/spec/ -run TestRegistry -v
   ```

   `TestRegistry_EveryOperationIsAccountedFor` will fail if you introduced a
   collision (two mappings resolving to the same resource+action) or pointed
   at an `operationId` that doesn't exist in the spec — both are real
   mistakes to fix, not test flakiness.

6. Verify the actual command works end to end:

   ```bash
   cd cli && go build -o bin/flexprice . && ./bin/flexprice <resource> --help
   ```

## When to exclude an operation instead

Some operations are superseded by a newer version (`recalculateInvoice` by
`recalculateInvoiceV2`) or otherwise shouldn't be a command at all. List
these under `commands.yaml`'s `exclude:` key with a one-line comment
explaining why — an operation must be either mapped or excluded, never
silently absent, which is exactly what
`TestRegistry_EveryOperationIsAccountedFor` enforces.
