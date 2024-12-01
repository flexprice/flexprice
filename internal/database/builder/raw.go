package builder

import (
	"context"
	"strings"

	"github.com/flexprice/flexprice/internal/types"
)

// RawQuery helps add tenant and environment filters to raw SQL
type RawQuery struct {
	SQL  string
	Args []interface{}
}

// WithContext adds tenant and environment filters to raw SQL
func (q *RawQuery) WithContext(ctx context.Context) (*RawQuery, error) {
	// Simple SQL parser to add WHERE/AND clauses
	sql := q.SQL
	args := q.Args

	hasWhere := strings.Contains(strings.ToUpper(sql), "WHERE")

	// Add tenant filter
	if hasWhere {
		sql += " AND tenant_id = ?"
	} else {
		sql += " WHERE tenant_id = ?"
	}
	args = append(args, types.GetTenantID(ctx))

	// Add environment filter if needed
	if envID := types.GetEnvironmentID(ctx); envID != "" {
		sql += " AND environment_id = ?"
		args = append(args, envID)
	}

	return &RawQuery{SQL: sql, Args: args}, nil
}
