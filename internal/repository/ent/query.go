package ent

import (
	"context"
)

// BaseQueryOptions defines common operations for query builders
type BaseQueryOptions[T any] interface {
	// ApplyTenantFilter applies tenant isolation
	ApplyTenantFilter(ctx context.Context, query T) T
	// ApplyStatusFilter applies status filter
	ApplyStatusFilter(query T, status string) T
	// ApplySortFilter applies sorting
	ApplySortFilter(query T, field string, order string) T
	// ApplyPaginationFilter applies pagination
	ApplyPaginationFilter(query T, limit int, offset int) T
	// GetFieldName returns the field name for a given filter field
	GetFieldName(field string) string
}
