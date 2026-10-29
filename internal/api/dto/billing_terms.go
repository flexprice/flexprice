package dto

// BillingPagination describes a stable owner-scoped continuation.
type BillingPagination struct {
	Limit      int     `json:"limit"`
	HasMore    bool    `json:"has_more"`
	NextCursor *string `json:"next_cursor"`
}
