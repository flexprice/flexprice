package types

// PaginationResponse represents standardized pagination metadata
type PaginationResponse struct {
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// ListResponse represents a paginated response with items
type ListResponse[T any] struct {
	Items      []T                `json:"items"`
	Pagination PaginationResponse `json:"pagination"`
}

// NewPaginationResponse creates a new pagination response
func NewPaginationResponse(total, limit, offset int) PaginationResponse {
	return PaginationResponse{
		Total:  total,
		Limit:  limit,
		Offset: offset + limit,
	}
}

// NewListResponse creates a new list response with pagination
func NewListResponse[T any](items []T, total, limit, offset int) ListResponse[T] {
	return ListResponse[T]{
		Items: items,
		Pagination: PaginationResponse{
			Total:  total,
			Limit:  limit,
			Offset: offset,
		},
	}
}
