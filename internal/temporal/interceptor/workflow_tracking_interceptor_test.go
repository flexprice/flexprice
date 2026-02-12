package interceptor

import (
	"testing"
)

func TestToSnakeCase(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// Regular camelCase
		{"userName", "user_name"},
		{"firstName", "first_name"},

		// With ID suffix (acronym)
		{"UserID", "user_id"},
		{"TenantID", "tenant_id"},
		{"ScheduledTaskID", "scheduled_task_id"},
		{"EnvironmentID", "environment_id"},

		// With ID in middle
		{"UserIDName", "user_id_name"},

		// Multiple acronyms
		{"HTMLURL", "htmlurl"},
		{"HTMLParser", "html_parser"},

		// Already snake_case
		{"user_name", "user_name"},

		// Single word
		{"User", "user"},
		{"ID", "id"},

		// Empty
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := toSnakeCase(tt.input)
			if result != tt.expected {
				t.Errorf("toSnakeCase(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
