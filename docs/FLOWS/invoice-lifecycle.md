# Flow: Invoice lifecycle

## Trigger

Manual / API-driven issuance, automated billing schedules, PSP webhooks prompting reconciliation adjustments, Temporal invoice workflows (finalize/sync).

## Execution path

1. **Creation** (`InvoiceService` / `BillingService` pathways) allocates draft invoice scaffold with provisional line metadata.
2. **Line population** merges rated usage snapshots, recurring components, adjustments, promotional credits mapped through repositories (`invoice.Repository`, line item repos).
3. **Review / preview** endpoints (API handlers expose operations—see `internal/api/v1/invoice.go`).
4. **Finalize** transitions invoice to authoritative state; may enqueue **Temporal** workflows observed in registrations under `invoice` activities (sync to QuickBooks/Zoho/etc.).
5. **Payment capture** aligns with PSP state via `payment` domain + integration clients.
6. **Credit notes** reconcile partial reversals referencing original invoice lineage.

Temporal integration points noted in hotspots: **`internal/api/v1/invoice`** directly references global Temporal service at certain operations.

## Modules touched

- `internal/ee/service/invoice.go` (dominant orchestrator)
- `internal/api/v1/invoice.go`
- `internal/domain/invoice/**`
- `internal/repository/ent` invoice + line entities
- `internal/temporal/workflows/invoice*` + correlated activities (`internal/temporal/activities/invoice`)
- PSP-specific sync workflows (QuickBooks/Zoho/etc.)

## Database operations

- PostgreSQL persists canonical invoice numbering sequences (`billingsequence`-related schema) and aggregates.
- ClickHouse incidental reads if analytics overlays requested.

## External systems

Accounting / invoicing PSP sync partners; PDF generation subsystem (`internal/pdf`) + optional object storage (`internal/s3`).

## Async operations

Workflows synchronize external bookkeeping; webhook dispatch after major state changes (`internal/webhook`).

## Failure points

Finalization races, duplicate numbering under concurrency (mitigated via sequence tables but verify transactional boundaries).

External sync partial failures stranded in Temporal (monitor Temporal UI alerts).

Misaligned entitlement vs invoice line derivation under subscription amendments.

## Retry behavior

Temporal activities default policies; PSP operations may have finer-grained backoff.

Poison/partial commit prevention requires explicit compensation patterns implemented per workflow branch.

## State transitions

Consult `internal/types` + invoice domain enums; typical arcs:

```
draft → open/pending_payment → paid
draft → void
paid → refunded / credited (via credit notes)
```

## Related flows

- [billing.md](billing.md)
- [webhook-processing.md](webhook-processing.md)
