package errors

// ErrorResponse represents the standard error response structure
type ErrorResponse struct {
	Success bool        `json:"success"`
	Error   ErrorDetail `json:"error"`
}

// ErrorDetail contains error information
type ErrorDetail struct {
	Display string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}
