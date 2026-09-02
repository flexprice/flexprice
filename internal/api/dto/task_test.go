package dto

import (
	"context"
	"strings"
	"testing"

	"github.com/flexprice/flexprice/internal/types"
)

func validCreateTaskRequest() *CreateTaskRequest {
	return &CreateTaskRequest{
		TaskType:     types.TaskTypeImport,
		EntityType:   types.EntityTypeEvents,
		FileType:     types.FileTypeCSV,
		FileProvider: FileProviderCSVBox,
		UploadID:     "abc_123-XYZ",
	}
}

func TestCreateTaskRequest_Validate(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(r *CreateTaskRequest)
		wantErr bool
	}{
		{name: "valid", mutate: func(*CreateTaskRequest) {}, wantErr: false},
		{
			name:    "trims_upload_id",
			mutate:  func(r *CreateTaskRequest) { r.UploadID = "  abc123  " },
			wantErr: false,
		},
		{
			name:    "lowercases_provider",
			mutate:  func(r *CreateTaskRequest) { r.FileProvider = "  CSVBOX  " },
			wantErr: false,
		},
		{
			name:    "empty_upload_id",
			mutate:  func(r *CreateTaskRequest) { r.UploadID = "" },
			wantErr: true,
		},
		{
			name:    "path_traversal",
			mutate:  func(r *CreateTaskRequest) { r.UploadID = "../../etc/passwd" },
			wantErr: true,
		},
		{
			name:    "url_shaped_upload_id",
			mutate:  func(r *CreateTaskRequest) { r.UploadID = "http://evil.example/a.csv" },
			wantErr: true,
		},
		{
			name:    "slash_in_upload_id",
			mutate:  func(r *CreateTaskRequest) { r.UploadID = "abc/def" },
			wantErr: true,
		},
		{
			name:    "spaces_in_upload_id",
			mutate:  func(r *CreateTaskRequest) { r.UploadID = "abc def" },
			wantErr: true,
		},
		{
			name:    "upload_id_too_long",
			mutate:  func(r *CreateTaskRequest) { r.UploadID = strings.Repeat("a", 129) },
			wantErr: true,
		},
		{
			name:    "wrong_provider",
			mutate:  func(r *CreateTaskRequest) { r.FileProvider = "dropbox" },
			wantErr: true,
		},
		{
			name:    "export_task_type_rejected",
			mutate:  func(r *CreateTaskRequest) { r.TaskType = types.TaskTypeExport },
			wantErr: true,
		},
		{
			name:    "non_events_entity_type_rejected",
			mutate:  func(r *CreateTaskRequest) { r.EntityType = types.EntityTypePrices },
			wantErr: true,
		},
		{
			name:    "non_csv_file_type_rejected",
			mutate:  func(r *CreateTaskRequest) { r.FileType = types.FileTypeJSON },
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := validCreateTaskRequest()
			tc.mutate(r)
			err := r.Validate()
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// ToTask preserves only what's needed for the audit trail; the S3 key is
// derived at process time from ctx + config + upload_id, not stored, so
// the caller can never influence what object gets fetched.
func TestCreateTaskRequest_ToTask_PersistsAuditMetadata(t *testing.T) {
	r := validCreateTaskRequest()
	if err := r.Validate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
	tsk := r.ToTask(context.Background())
	if tsk.FileURL != "" {
		t.Fatalf("FileURL should be empty; key is derived at process time, got %q", tsk.FileURL)
	}
	if tsk.Metadata["upload_id"] != r.UploadID {
		t.Fatalf("metadata upload_id not preserved: %#v", tsk.Metadata)
	}
	if tsk.Metadata["file_provider"] != r.FileProvider {
		t.Fatalf("metadata file_provider not preserved: %#v", tsk.Metadata)
	}
}
