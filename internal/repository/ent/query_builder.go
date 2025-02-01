package ent

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// ApplyBaseFilters applies common filters like tenant ID and status using the query options
func ApplyBaseFilters[T any](ctx context.Context, query T, filter types.BaseFilter, opts BaseQueryOptions[T]) T {
	query = opts.ApplyTenantFilter(ctx, query)
	query = opts.ApplyStatusFilter(query, filter.GetStatus())
	return query
}

// ApplyPagination applies pagination to the query if the filter is not unlimited
func ApplyPagination[T any](query T, filter types.BaseFilter, opts BaseQueryOptions[T]) T {
	if filter == nil {
		return query
	}

	if !filter.IsUnlimited() && filter.GetLimit() > 0 {
		query = opts.ApplyPaginationFilter(query, filter.GetLimit(), filter.GetOffset())
	}
	return query
}

// ApplySorting applies sorting to the query
func ApplySorting[T any](query T, filter types.BaseFilter, opts BaseQueryOptions[T]) T {
	if filter == nil {
		// Apply default sorting if filter is nil
		return opts.ApplySortFilter(query, "created_at", "desc")
	}
	return opts.ApplySortFilter(query, filter.GetSort(), filter.GetOrder())
}

// ApplyQueryOptions applies all common query options (base filters, pagination, sorting)
func ApplyQueryOptions[T any](ctx context.Context, query T, filter types.BaseFilter, opts BaseQueryOptions[T]) T {
	query = ApplyBaseFilters(ctx, query, filter, opts)
	if filter == nil {
		return query
	}
	query = ApplySorting(query, filter, opts)
	return ApplyPagination(query, filter, opts)
}
