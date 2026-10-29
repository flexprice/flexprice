package ent

import (
	"context"

	"entgo.io/ent/dialect/sql"
	"github.com/flexprice/flexprice/ent/customer"
	"github.com/flexprice/flexprice/ent/invoice"
	"github.com/flexprice/flexprice/internal/types"
)

// customerIDsOfEntity selects the IDs of the request environment's customers of one billing entity.
func customerIDsOfEntity(ctx context.Context, billingEntityID string) *sql.Selector {
	t := sql.Table(customer.Table)
	return sql.Select(t.C(customer.FieldID)).From(t).Where(sql.And(
		sql.EQ(t.C(customer.FieldTenantID), types.GetTenantID(ctx)),
		sql.EQ(t.C(customer.FieldEnvironmentID), types.GetEnvironmentID(ctx)),
		sql.EQ(t.C(customer.FieldBillingEntityID), billingEntityID),
	))
}

// invoiceIDsOfEntity selects the IDs of the request environment's invoices of one billing entity's customers.
func invoiceIDsOfEntity(ctx context.Context, billingEntityID string) *sql.Selector {
	t := sql.Table(invoice.Table)
	return sql.Select(t.C(invoice.FieldID)).From(t).Where(sql.And(
		sql.EQ(t.C(invoice.FieldTenantID), types.GetTenantID(ctx)),
		sql.EQ(t.C(invoice.FieldEnvironmentID), types.GetEnvironmentID(ctx)),
		sql.In(t.C(invoice.FieldCustomerID), customerIDsOfEntity(ctx, billingEntityID)),
	))
}

// columnInSelector keeps rows whose column value is returned by the subquery.
func columnInSelector(column string, sub *sql.Selector) func(*sql.Selector) {
	return func(s *sql.Selector) {
		s.Where(sql.In(s.C(column), sub))
	}
}
