package errors

// ErrorResponse represents the standard flat error response structure.
type ErrorResponse struct {
	Code           ErrorCode      `json:"code"`
	Message        string         `json:"message"`
	HTTPStatusCode int            `json:"http_status_code"`
	Details        map[string]any `json:"details,omitempty"`
}
