package dto

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/scheduledtask"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func TestCreateScheduledTaskRequestValidate(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{"today payload ok", `{"connection_id":"c1","entity_type":"invoice","interval":"daily","enabled":true,"job_config":{"bucket":"b","region":"r"}}`, false},
		{"today payload without job_config", `{"connection_id":"c1","entity_type":"invoice","interval":"daily"}`, true},
		{"today custom interval still rejected", `{"connection_id":"c1","entity_type":"invoice","interval":"custom","job_config":{"bucket":"b"}}`, true},
		{"connection weekly with schedule", `{"connection_id":"c1","entity_type":"invoice","interval":"weekly","schedule":{"day_of_week":1,"timezone":"UTC"},"job_config":{"bucket":"b"}}`, false},
		{"no connection metadata only", `{"entity_type":"invoice","interval":"daily","job_config":{"export_metadata_fields":[],"compression":"gzip"}}`, false},
		{"no connection no job_config", `{"entity_type":"invoice","interval":"daily"}`, false},
		{"no connection bucket", `{"entity_type":"invoice","interval":"daily","job_config":{"bucket":"b"}}`, true},
		{"no connection provider", `{"entity_type":"invoice","interval":"daily","job_config":{"provider":"gcs"}}`, true},
		{"weekly without schedule", `{"entity_type":"invoice","interval":"weekly"}`, true},
		{"manual with schedule", `{"entity_type":"invoice","interval":"manual","schedule":{"timezone":"UTC"}}`, true},
		{"manual ok", `{"entity_type":"invoice","interval":"manual"}`, false},
		{"daily without schedule", `{"entity_type":"invoice","interval":"daily"}`, false},
		{"custom no connection", `{"entity_type":"invoice","interval":"custom"}`, true},
		{"15MIN no connection", `{"entity_type":"invoice","interval":"15MIN"}`, true},
		{"name too long", `{"name":"` + strings.Repeat("a", 121) + `","entity_type":"invoice","interval":"daily"}`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req CreateScheduledTaskRequest
			require.NoError(t, json.Unmarshal([]byte(tt.body), &req))
			err := req.Validate()
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestUpdateScheduledTaskRequestValidate(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{"empty", `{}`, true},
		{"enabled false", `{"enabled":false}`, false},
		{"name", `{"name":"x"}`, false},
		{"schedule", `{"schedule":{"timezone":"UTC"}}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req UpdateScheduledTaskRequest
			require.NoError(t, json.Unmarshal([]byte(tt.body), &req))
			err := req.Validate()
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestScheduledTaskResponse_OldShapeUnchanged(t *testing.T) {
	task := &scheduledtask.ScheduledTask{
		ID:           "st1",
		ConnectionID: "c1",
		EntityType:   types.ScheduledTaskEntityTypeInvoice,
		Interval:     types.ScheduledTaskIntervalDaily,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	raw, err := json.Marshal(ToScheduledTaskResponse(task))
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))
	for _, k := range []string{"name", "managed_copy", "schedule"} {
		require.NotContains(t, m, k)
	}
}
