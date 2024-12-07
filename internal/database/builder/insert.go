package builder

import (
	"context"

	sq "github.com/Masterminds/squirrel"
	"github.com/flexprice/flexprice/internal/types"
)

type InsertBuilder struct {
	sq.InsertBuilder
}

// WithContext adds tenant ID to the insert
func (b InsertBuilder) WithContext(ctx context.Context) InsertBuilder {
	b.InsertBuilder = b.InsertBuilder.Columns("tenant_id").Values(types.GetTenantID(ctx))

	// Add environment ID if present
	if envID := types.GetEnvironmentID(ctx); envID != "" {
		b.InsertBuilder = b.InsertBuilder.Columns("environment_id").Values(envID)
	}

	return b
}

// Columns adds columns to insert
func (b InsertBuilder) Columns(columns ...string) InsertBuilder {
	b.InsertBuilder = b.InsertBuilder.Columns(columns...)
	return b
}

// Values adds a single row of values
func (b InsertBuilder) Values(values ...interface{}) InsertBuilder {
	b.InsertBuilder = b.InsertBuilder.Values(values...)
	return b
}

// ToSql returns the SQL and arguments for the query
func (b InsertBuilder) ToSql() (string, []interface{}, error) {
	return b.InsertBuilder.ToSql()
}
