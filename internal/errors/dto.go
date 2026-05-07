package errors

// ErrorResponse represents the standard flat error response structure.
type ErrorResponse struct {
	Code           string         `json:"code" enums:"not_found,already_exists,version_conflict,validation_error,invalid_operation,permission_denied,http_client_error,database_error,system_error,internal_error,service_unavailable"`
	Message        string         `json:"message"`
	HTTPStatusCode int            `json:"http_status_code"`
	Details        map[string]any `json:"details,omitempty"`
}
