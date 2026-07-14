// Backfill invoice collection_method and payment_behavior from parent subscriptions.
//
// Usage:
//
//	# Dry run (prints what would change, no writes)
//	DRY_RUN=true go run ./scripts -cmd backfill-invoice-collection-method
//
//	# Live run with custom batch size (default 1000)
//	BATCH_SIZE=500 go run ./scripts -cmd backfill-invoice-collection-method
//
//	# Full production run
//	go run ./scripts -cmd backfill-invoice-collection-method

package internal

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/flexprice/flexprice/internal/config"
	_ "github.com/lib/pq"
)

const (
	backfillBatchSize = 1000

	// Schema defaults — invoices already carrying both of these need no update.
	backfillDefaultCollectionMethod = "charge_automatically"
	backfillDefaultPaymentBehavior  = "default_active"

	// Legacy value that predates the CollectionMethod/PaymentBehavior split.
	// Normalises to charge_automatically + allow_incomplete.
	backfillLegacyCollectionMethod = "default_incomplete"
)

// BackfillInvoiceCollectionMethod copies collection_method and payment_behavior
// from the parent subscription onto every invoice that still has the schema
// defaults. Processes in batches of BATCH_SIZE (default 1 000) using keyset
// (cursor) pagination and a single bulk UPDATE per batch — safe to run on tables
// with hundreds of thousands of rows.
//
// Environment variables:
//
//	DRY_RUN=true   — print what would change without writing
//	BATCH_SIZE=<n> — rows per batch (default 1000)
func BackfillInvoiceCollectionMethod() error {
	isDryRun := os.Getenv("DRY_RUN") == "true"
	batchSize := backfillBatchSize
	if v := os.Getenv("BATCH_SIZE"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return fmt.Errorf("invalid BATCH_SIZE %q: must be a positive integer", v)
		}
		batchSize = n
	}

	if isDryRun {
		log.Println("DRY RUN MODE — no changes will be written")
	}
	log.Printf("Batch size: %d", batchSize)

	cfg, err := config.NewConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	db, err := sql.Open("postgres", cfg.Postgres.GetDSN())
	if err != nil {
		return fmt.Errorf("failed to open database connection: %w", err)
	}
	defer db.Close()

	ctx := context.Background()

	totalUpdated, err := runInvoiceBackfill(ctx, db, batchSize, isDryRun)
	if err != nil {
		return err
	}

	log.Printf("Backfill complete — total invoices updated: %d", totalUpdated)
	return nil
}

// runInvoiceBackfill drives the cursor-paginated UPDATE loop.
// Cursor pagination (WHERE i.id > $cursor ORDER BY i.id LIMIT $n) avoids the
// O(offset) cost of OFFSET-based pagination — each batch is equally fast even
// at row 400 000.
func runInvoiceBackfill(ctx context.Context, db *sql.DB, batchSize int, isDryRun bool) (int64, error) {
	var (
		cursor    = "" // exclusive lower bound on i.id; empty = start from beginning
		totalRows int64
		batchNum  int
	)

	// Keyset SELECT: fetch the next batch of invoice IDs that need updating,
	// resolving legacy collection_method at query time via CASE expressions.
	//
	// The cursor condition flips between two forms:
	//   first batch  → no lower bound (cursor == "")
	//   later batches → i.id > $3
	//
	// Index hint: invoices(id) PK scan + subscriptions(id) PK lookup — both are
	// point-lookups, so this stays fast regardless of total table size.
	const selectTemplate = `
SELECT
    i.id,
    CASE
        WHEN s.collection_method = '` + backfillLegacyCollectionMethod + `' THEN 'charge_automatically'
        ELSE s.collection_method
    END AS resolved_cm,
    CASE
        WHEN s.collection_method = '` + backfillLegacyCollectionMethod + `' THEN 'allow_incomplete'
        WHEN s.payment_behavior  = '' OR s.payment_behavior IS NULL THEN '` + backfillDefaultPaymentBehavior + `'
        ELSE s.payment_behavior
    END AS resolved_pb
FROM invoices i
JOIN subscriptions s ON s.id = i.subscription_id
WHERE i.status = 'published'
  AND i.subscription_id IS NOT NULL
  AND (
        i.collection_method = '` + backfillDefaultCollectionMethod + `'
     OR i.payment_behavior  = '` + backfillDefaultPaymentBehavior + `'
  )
  AND (
        s.collection_method != '` + backfillDefaultCollectionMethod + `'
     OR s.collection_method  = '` + backfillLegacyCollectionMethod + `'
     OR s.payment_behavior   != '` + backfillDefaultPaymentBehavior + `'
  )
`

	for {
		batchNum++

		var (
			querySQL  string
			queryArgs []any
		)
		if cursor == "" {
			querySQL = selectTemplate + `ORDER BY i.id LIMIT $1`
			queryArgs = []any{batchSize}
		} else {
			querySQL = selectTemplate + `  AND i.id > $2
ORDER BY i.id LIMIT $1`
			queryArgs = []any{batchSize, cursor}
		}

		rows, err := db.QueryContext(ctx, querySQL, queryArgs...)
		if err != nil {
			return totalRows, fmt.Errorf("batch %d: select failed: %w", batchNum, err)
		}

		var batch []invoiceBackfillRow
		for rows.Next() {
			var r invoiceBackfillRow
			if err := rows.Scan(&r.id, &r.collectionMethod, &r.paymentBehavior); err != nil {
				rows.Close()
				return totalRows, fmt.Errorf("batch %d: scan failed: %w", batchNum, err)
			}
			batch = append(batch, r)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return totalRows, fmt.Errorf("batch %d: rows error: %w", batchNum, err)
		}

		if len(batch) == 0 {
			log.Printf("Batch %d: no more rows — done", batchNum)
			break
		}

		// Advance cursor to the last ID in this batch.
		cursor = batch[len(batch)-1].id

		log.Printf("Batch %d: fetched %d invoices (cursor → %s)", batchNum, len(batch), cursor)

		if isDryRun {
			for _, r := range batch {
				log.Printf("  [DRY RUN] invoice %s → collection_method=%s payment_behavior=%s",
					r.id, r.collectionMethod, r.paymentBehavior)
			}
			totalRows += int64(len(batch))
			if len(batch) < batchSize {
				break
			}
			continue
		}

		// Bulk UPDATE: single round-trip for the whole batch via VALUES list.
		updateSQL, updateArgs := buildInvoiceBulkUpdate(batch)
		result, err := db.ExecContext(ctx, updateSQL, updateArgs...)
		if err != nil {
			return totalRows, fmt.Errorf("batch %d: bulk update failed: %w", batchNum, err)
		}

		affected, _ := result.RowsAffected()
		totalRows += affected
		log.Printf("Batch %d: updated %d rows (cumulative: %d)", batchNum, affected, totalRows)

		if len(batch) < batchSize {
			log.Printf("Batch %d: partial batch — no more rows", batchNum)
			break
		}
	}

	return totalRows, nil
}

type invoiceBackfillRow struct {
	id               string
	collectionMethod string
	paymentBehavior  string
}

// buildInvoiceBulkUpdate constructs:
//
//	UPDATE invoices AS i
//	  SET collection_method = v.cm, payment_behavior = v.pb, updated_at = NOW()
//	  FROM (VALUES ($1::varchar,$2::varchar,$3::varchar), ...) AS v(id, cm, pb)
//	  WHERE i.id = v.id AND i.status = 'published'
func buildInvoiceBulkUpdate(batch []invoiceBackfillRow) (string, []any) {
	args := make([]any, 0, len(batch)*3)
	placeholders := make([]string, 0, len(batch))

	for i, r := range batch {
		base := i * 3
		placeholders = append(placeholders,
			fmt.Sprintf("($%d::varchar,$%d::varchar,$%d::varchar)", base+1, base+2, base+3),
		)
		args = append(args, r.id, r.collectionMethod, r.paymentBehavior)
	}

	updateSQL := fmt.Sprintf(`
UPDATE invoices AS i
SET
    collection_method = v.cm,
    payment_behavior  = v.pb,
    updated_at        = NOW()
FROM (VALUES %s) AS v(id, cm, pb)
WHERE i.id = v.id
  AND i.status = 'published'
`, joinWithComma(placeholders))

	return updateSQL, args
}

func joinWithComma(ss []string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += ", "
		}
		out += s
	}
	return out
}
