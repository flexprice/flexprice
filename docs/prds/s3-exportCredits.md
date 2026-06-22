# s3 Export 

## Existing sys

The export system is built on three main pillars:
1. Temporal - For workflow orchestration and scheduling
2. Export Service - For data fetching, CSV generation, and routing
3. Amazon S3 - For file storage

┌─────────────────┐
│   User/API      │
│  Creates Task   │
└────────┬────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────┐
│                  TEMPORAL SCHEDULING                        │
│  • Cron-based schedules (hourly, daily)    │
│  • Manual "force runs" for specific date ranges             │
└────────┬────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────┐
│          TEMPORAL WORKFLOW (ExecuteExportWorkflow)          │
│                                                             │
│  Step 1: Fetch scheduled task config                        │
│  Step 2: Create task record (tracking)                      │
│  Step 3: Calculate time boundaries                          │
│  Step 4: → CALL EXPORT SERVICE ←                            │
│  Step 5: Update task status (success/failure)               │
└────────┬────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────┐
│              EXPORT SERVICE (base.go)                       │
│                                                             │
│  1. Routes to entity-specific exporter                      │
│     → EventExporter for events                              │
│     → InvoiceExporter for invoices                          │
│     → CreditTopupExporter for credit topups                 │
│                                                             │
│  2. PrepareData() - Fetch data in batches + convert to CSV  │
│  3. Get connection (credentials)                            │
│  4. Route to provider (S3)        │
└────────┬────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────┐
│            ENTITY EXPORTERS (Fetch & Transform)             │
│                                                             │
│  EventExporter (event_export.go):                           │
│    • Fetches feature_usage data in 500-record batches       │
│    • Converts to FeatureUsageCSV structs                    │
│    • Returns CSV bytes                                      │
│                                                             │
│  InvoiceExporter (invoice_export.go):                       │
│    • Fetches invoice data in 500-record batches             │
│    • Converts to InvoiceCSV structs                         │
│    • Returns CSV bytes                                      │
│                                                             │
│  CreditTopupExporter (credit_topup_export.go):              │
│    • Fetches wallet_transactions data in 500-record batches │
│    • Joins with wallets and customers tables                │
│    • Filters: type='credit', status='completed'             │
│    • Converts to CreditTopupCSV structs                     │
│    • Returns CSV bytes                                      │
└────────┬────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────┐
│              S3 INTEGRATION (Two Layers)                    │
│                                                             │
│  Layer 1: internal/integration/s3/                          │
│    • Client (client.go): Connection management              │
│      - Fetches connection from DB                           │
│      - Decrypts AWS credentials                             │
│      - Creates AWS SDK S3 client                            │
│    • Upload (upload.go): File upload logic                  │
│      - Gzip compression (optional)                          │
│      - Server-side encryption (AES256/KMS)                  │
│      - Generates S3 keys with prefixes                      │
│                                                             │
│  Layer 2: internal/s3/                                      │
│    • Service (service.go): Document management              │
│      - Used for invoice PDFs                                │
│      - Pre-signed URLs                                      │
│    • (Not used for exports)                                 │
└────────┬────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────┐
│                    AMAZON S3                                │
│                                                             │
│  File Structure:                                            │
│  s3://bucket/prefix/entity_type/filename.csv[.gz]           │
│                                                             |
│  Example:                                                   │
│  s3://my-bucket/exports/events/events-241107120000-         │
│                                  241107130000.csv.gz        │
└─────────────────────────────────────────────────────────────┘


## TWO S3 LAYERS: Why?

### Layer 1: internal/integration/s3/ (Used for Exports)
- Purpose: Generic S3 integration for scheduled exports
- Features:
    Decrypts connection credentials
    Supports gzip compression
    Server-side encryption (AES256/KMS)
    Dynamic bucket/region/prefix configuration
    Used by: Export workflows

### Layer 2: internal/s3/ (Used for Documents)
- Purpose: Document management (primarily invoice PDFs)
- Features:
    Pre-signed URL generation
    Document existence checks
    Fixed bucket configuration from app config
    Used by: Invoice PDF storage

Why separate? Different use cases:
- Exports: Customer-controlled S3 buckets (multi-tenant)
- Documents: FlexPrice-managed S3 bucket (single tenant)

##657 KEY FEATURES
1. Batching for Performance
- Fetches 500 records at a time to avoid memory issues
- Works for millions of records

2. Empty CSV Handling
- If no data is found, still uploads CSV with headers only
- Ensures consistent file structure

3. Temporal Orchestration
- Retries: Up to 3 attempts for heavy operations
- Timeouts: 15-minute max per workflow
- Scheduling: Cron-based (hourly, daily, weekly, monthly)
- Manual Runs: Force runs with custom date ranges

4. Security
- AWS credentials are encrypted in database
- Decrypted only when needed
- Supports temporary credentials (STS tokens)
- Server-side encryption on S3

5. Monitoring & Tracking
- Creates Task record for each export
- Tracks status: Pending → Running → Completed/Failed
- Stores file URL, record count, file size
- Links to parent ScheduledTask


### SUMMARY

USER → API → Create ScheduledTask
                ↓
         Temporal Schedule (Cron)
                ↓
         ExecuteExportWorkflow
                ↓
         1. Fetch config
         2. Create task record
         3. Calculate time boundaries
         4. → ExportService.Export()
                ↓
            EventExporter or InvoiceExporter or CreditTopupExporter
                ↓
            • Fetch data (500 records/batch)
            • Convert to CSV structs
            • Marshal to CSV bytes
                ↓
         5. S3 Integration
            • Get connection from DB
            • Decrypt AWS credentials
            • Create AWS S3 client
            • Compress (gzip)
            • Encrypt (AES256/KMS)
            • Upload to S3
                ↓
         6. Update task status
                ↓
         🎉 CSV file in Amazon S3!


## Credit Top-Up Export Feature

### Entity Type: `credit_topups`

### CSV Schema (CreditTopupCSV)
The CSV export will contain the following columns:

type CreditTopUpReportCSV {
    topup_id,
    external_id,
    name,
    wallet_id,
    amount,
    credit_balance_before,
    credit_balance_after,
    reference_id,
    transaction_reason,
    created_at
}

### Query Logic
```sql
SELECT
    wt.id AS topup_id,
    c.external_id,
    c.name AS customer_name,
    wt.wallet_id,
    wt.amount,
    wt.credit_balance_before,
    wt.credit_balance_after,
    wt.reference_id,
    wt.transaction_reason,
    wt.created_at
FROM
    wallet_transactions wt
    INNER JOIN wallets w ON w.id = wt.wallet_id
    INNER JOIN customers c ON c.id = w.customer_id
WHERE
    wt.tenant_id = ?
    AND wt.environment_id = ?
    AND wt.type = 'credit'
    AND wt.transaction_status = 'completed'
    AND wt.status = 'published'
    AND wt.created_at >= ?
    AND wt.created_at < ?
ORDER BY wt.created_at ASC
LIMIT ? OFFSET ?
```

### Implementation Files to Create

1. **`internal/ee/service/sync/export/credit_topup_export.go`**
   - CreditTopupExporter struct
   - CreditTopupCSV struct (CSV schema)
   - PrepareData() method with batching
   - convertToCSVRecords() helper
   - GetFilenamePrefix() returns "credit_topups"

2. **Update `internal/ee/service/sync/export/base.go`**
   - Add CreditTopupExporter to getExporter() switch case
   - Add case for types.ScheduledTaskEntityTypeCreditTopup

3. **Repository Method (if not exists)**
   - `internal/domain/wallet/repository.go` - Add interface method
   - `internal/repository/ent/wallet_transaction.go` - Implement query

4. **Entity Type Constant**
   - Add `ScheduledTaskEntityTypeCreditTopup` to `internal/types/scheduled_task.go`

### File Structure Example
```
s3://customer-bucket/exports/credit_topups/credit_topups-250115120000-250115130000.csv.gz
```

### Batch Processing
- Batch size: 500 records per iteration
- Orders by created_at ASC for consistent pagination
- Joins optimized with indexed foreign keys

### Use Cases
1. **Finance Teams**: Track all credit top-ups for revenue recognition
2. **Customer Success**: Monitor customer wallet activity and usage patterns
3. **Analytics**: Analyze top-up trends and customer behavior
4. **Compliance**: Audit trail for all credit transactions

### Scheduling Options
- **Hourly**: Real-time credit tracking for high-volume customers
- **Daily**: Standard daily reconciliation reports