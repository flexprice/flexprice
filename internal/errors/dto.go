package errors

// ErrorResponse represents the standard error response structure
type ErrorResponse struct {
	Success bool        `json:"success"`
	Error   ErrorDetail `json:"error"`
}

// ErrorDetail contains error information
type ErrorDetail struct {
	Display       string         `json:"message"`
	InternalError string         `json:"internal_error,omitempty"`
	Details       map[string]any `json:"details,omitempty" swaggerignore:"true"`
}
