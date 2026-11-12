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

func TestCreateScheduledTaskRequest_RunWindowNeedsBothBounds(t *testing.T) {
	base := `{"entity_type":"analytics_view","interval":"manual","enabled":true,"run_now":true,"source_config":{"view_id":"v"},`
	tests := []struct {
		name    string
		window  string
		wantErr bool
	}{
		{"both", `"run_window":{"start_time":"2026-11-01T00:00:00Z","end_time":"2026-11-02T00:00:00Z"}}`, false},
		{"start only", `"run_window":{"start_time":"2026-11-01T00:00:00Z"}}`, true},
		{"end only", `"run_window":{"end_time":"2026-11-02T00:00:00Z"}}`, true},
		{"empty", `"run_window":{}}`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req CreateScheduledTaskRequest
			require.NoError(t, json.Unmarshal([]byte(base+tt.window), &req))
			err := req.Validate()
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}
