package dto

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCreateScheduledTaskRequest_ReportSources(t *testing.T) {
	sched := `"schedule":{"hour":9,"minute":0,"timezone":"UTC"}`
	tests := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{"report daily ok", `{"entity_type":"report","interval":"daily",` + sched + `,"source_config":{"report_key":"invoice_total","report_params":{"shape":"timeseries"}}}`, false},
		{"analytics_view manual ok", `{"entity_type":"analytics_view","interval":"manual","source_config":{"view_id":"view_1"}}`, false},
		{"report without source_config", `{"entity_type":"report","interval":"daily",` + sched + `}`, true},
		{"report with view_id", `{"entity_type":"report","interval":"daily",` + sched + `,"source_config":{"report_key":"k","view_id":"v","report_params":{"shape":"s"}}}`, true},
		{"analytics_view with report_key", `{"entity_type":"analytics_view","interval":"manual","source_config":{"view_id":"v","report_key":"k"}}`, true},
		{"report without shape", `{"entity_type":"report","interval":"daily",` + sched + `,"source_config":{"report_key":"k","report_params":{}}}`, true},
		{"report hourly", `{"entity_type":"report","interval":"hourly",` + sched + `,"source_config":{"report_key":"k","report_params":{"shape":"s"}}}`, true},
		{"report daily without schedule", `{"entity_type":"report","interval":"daily","source_config":{"report_key":"k","report_params":{"shape":"s"}}}`, true},
		{"report with metadata fields", `{"entity_type":"report","interval":"manual","job_config":{"export_metadata_fields":[{"entity_type":"customer","field_key":"name","column_name":"c"}]},"source_config":{"report_key":"k","report_params":{"shape":"s"}}}`, true},
		{"events with source_config", `{"entity_type":"events","interval":"daily","source_config":{"report_key":"k"}}`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req CreateScheduledTaskRequest
			require.NoError(t, json.Unmarshal([]byte(tt.body), &req))
			err := req.Validate()
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestUpdateScheduledTaskRequest_EmailConfigOnly(t *testing.T) {
	var req UpdateScheduledTaskRequest
	require.NoError(t, json.Unmarshal([]byte(`{"email_config":{"recipients":[]}}`), &req))
	require.NoError(t, req.Validate())
	require.NotNil(t, req.EmailConfig)

	var empty UpdateScheduledTaskRequest
	require.Error(t, empty.Validate())
}
