package ent

import (
	"context"

	"entgo.io/ent/dialect/sql"
	"github.com/flexprice/flexprice/ent/connectionassociation"
	"github.com/flexprice/flexprice/internal/types"
)

// connectionIDsOfEntity selects the IDs of the request environment's connections owned by one billing entity.
func connectionIDsOfEntity(ctx context.Context, billingEntityID string) *sql.Selector {
	t := sql.Table(connectionassociation.Table)
	return sql.Select(t.C(connectionassociation.FieldConnectionID)).From(t).Where(sql.And(
		sql.EQ(t.C(connectionassociation.FieldTenantID), types.GetTenantID(ctx)),
		sql.EQ(t.C(connectionassociation.FieldEnvironmentID), types.GetEnvironmentID(ctx)),
		sql.EQ(t.C(connectionassociation.FieldEntityType), string(types.ConnectionAssociationEntityTypeBillingEntity)),
		sql.EQ(t.C(connectionassociation.FieldEntityID), billingEntityID),
		sql.EQ(t.C(connectionassociation.FieldStatus), string(types.StatusPublished)),
	))
}

// columnInSelector keeps rows whose column value is returned by the subquery.
func columnInSelector(column string, sub *sql.Selector) func(*sql.Selector) {
	return func(s *sql.Selector) {
		s.Where(sql.In(s.C(column), sub))
	}
}
