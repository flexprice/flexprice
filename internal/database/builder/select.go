package builder

import (
	"context"

	sq "github.com/Masterminds/squirrel"
	"github.com/flexprice/flexprice/internal/types"
)

type SelectBuilder struct {
	sq.SelectBuilder
}

// WithContext adds tenant and environment filters based on context
func (b SelectBuilder) WithContext(ctx context.Context) SelectBuilder {
	b.SelectBuilder = b.SelectBuilder.Where(sq.Eq{"tenant_id": types.GetTenantID(ctx)})

	// Add environment filter for tables that need it
	if envID := types.GetEnvironmentID(ctx); envID != "" {
		b.SelectBuilder = b.SelectBuilder.Where(sq.Eq{"environment_id": envID})
	}

	return b
}

// Where adds WHERE conditions
func (b SelectBuilder) Where(pred interface{}, args ...interface{}) SelectBuilder {
	b.SelectBuilder = b.SelectBuilder.Where(pred, args...)
	return b
}

// From sets the FROM clause
func (b SelectBuilder) From(from string) SelectBuilder {
	b.SelectBuilder = b.SelectBuilder.From(from)
	return b
}

// OrderBy adds ORDER BY expressions
func (b SelectBuilder) OrderBy(clauses ...string) SelectBuilder {
	b.SelectBuilder = b.SelectBuilder.OrderBy(clauses...)
	return b
}

// ToSql generates the query and args
func (b SelectBuilder) ToSql() (string, []interface{}, error) {
	return b.SelectBuilder.ToSql()
}
