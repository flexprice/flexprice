package interceptor

import (
	"testing"

	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/stretchr/testify/assert"
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

func TestExtractWorkflowContext_CreditNote(t *testing.T) {
	input := models.ZohoBooksCreditNoteSyncWorkflowInput{
		CreditNoteID:  "cn_123",
		TenantID:      "tenant_1",
		EnvironmentID: "env_1",
	}

	for _, arg := range []interface{}{input, &input} {
		tenantID, environmentID, _, entity, entityID, metadata := extractWorkflowContext([]interface{}{arg})
		assert.Equal(t, "tenant_1", tenantID)
		assert.Equal(t, "env_1", environmentID)
		assert.Equal(t, "credit_note", entity)
		assert.Equal(t, "cn_123", entityID)
		assert.NotContains(t, metadata, "credit_note_id")
	}
}
