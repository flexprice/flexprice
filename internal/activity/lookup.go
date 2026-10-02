package activity

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/lib/pq"
)

// customerVia returns a CustomerLookup that reads customer_id from a parent
// table by the id held in fields[idField].
func customerVia(table, idField string) func(context.Context, Querier, map[string]any) (string, error) {
	return func(ctx context.Context, q Querier, f map[string]any) (string, error) {
		id, _ := f[idField].(string)
		if id == "" {
			return "", nil
		}
		rows, err := q.QueryContext(ctx, fmt.Sprintf(`SELECT customer_id FROM %s WHERE id = $1`, pq.QuoteIdentifier(table)), id)
		if err != nil {
			return "", err
		}
		defer rows.Close()
		var out string
		if rows.Next() {
			if err := rows.Scan(&out); err != nil {
				return "", err
			}
		}
		return out, rows.Err()
	}
}

// customerViaEntity dispatches on entity_type/entity_id for association tables.
// Values match the lowercase string enums stored by addon/tax associations
// (types.AddonAssociationEntityType, types.TaxRateEntityType); entity types with
// no customer relation (plan, addon, tenant) fall through and return "".
func customerViaEntity() func(context.Context, Querier, map[string]any) (string, error) {
	tables := map[string]string{"subscription": "subscriptions", "invoice": "invoices", "customer": "customers"}
	return func(ctx context.Context, q Querier, f map[string]any) (string, error) {
		et, _ := f["entity_type"].(string)
		if et == "customer" {
			id, _ := f["entity_id"].(string)
			return id, nil
		}
		t, ok := tables[et]
		if !ok {
			return "", nil
		}
		return customerVia(t, "entity_id")(ctx, q, f)
	}
}

// customerViaPayment reads the invoice a payment targets, then its customer.
// destination_type is stored as types.PaymentDestinationTypeInvoice ("INVOICE").
func customerViaPayment() func(context.Context, Querier, map[string]any) (string, error) {
	return func(ctx context.Context, q Querier, f map[string]any) (string, error) {
		if dt, _ := f["destination_type"].(string); dt != string(types.PaymentDestinationTypeInvoice) {
			return "", nil
		}
		return customerVia("invoices", "destination_id")(ctx, q, f)
	}
}
