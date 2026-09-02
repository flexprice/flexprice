package dto

import (
	"context"
	"regexp"
	"strings"

	"github.com/flexprice/flexprice/internal/domain/task"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
)

// FileProviderCSVBox routes an upload_id to the Flexprice-managed imports
// bucket. It's the only provider accepted today; adding another means
// implementing a matching FileProvider in the service layer.
const FileProviderCSVBox = "csvbox"

// uploadIDPattern is deliberately strict: alnum, underscore, dash, up to 128
// chars. This is the whole security boundary of the new API — anything that
// would let a caller inject path segments (../), scheme prefixes, whitespace,
// or a URL shape is rejected here and never reaches the S3 key.
var uploadIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// CreateTaskRequest is the input for POST /tasks. It replaces the previous
// caller-supplied file_url with an upload_id sourced from a trusted upstream
// uploader (CSV Box → Flexprice-managed S3), so the server never fetches an
// arbitrary URL. See the SSRF removal in commit f05a1e65f for context.
type CreateTaskRequest struct {
	TaskType     types.TaskType         `json:"task_type" binding:"required"`
	EntityType   types.EntityType       `json:"entity_type" binding:"required"`
	FileType     types.FileType         `json:"file_type" binding:"required"`
	FileProvider string                 `json:"file_provider" binding:"required"`
	UploadID     string                 `json:"upload_id" binding:"required"`
	FileName     *string                `json:"file_name,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

func (r *CreateTaskRequest) Validate() error {
	r.UploadID = strings.TrimSpace(r.UploadID)
	r.FileProvider = strings.TrimSpace(strings.ToLower(r.FileProvider))

	if err := r.TaskType.Validate(); err != nil {
		return err
	}
	if err := r.EntityType.Validate(); err != nil {
		return err
	}
	if err := r.FileType.Validate(); err != nil {
		return err
	}

	// Only usage-event CSV imports are exposed today. The pipeline supports the
	// other entity types, but they're intentionally gated off until we've
	// re-audited each chunk processor for the new upload-id shape.
	if r.TaskType != types.TaskTypeImport {
		return ierr.NewError("only IMPORT task_type is supported").
			WithHint("Use task_type=IMPORT").
			Mark(ierr.ErrInvalidOperation)
	}
	if r.EntityType != types.EntityTypeEvents {
		return ierr.NewError("only EVENTS entity_type is supported for imports").
			WithHint("Set entity_type=EVENTS").
			WithReportableDetails(map[string]interface{}{
				"entity_type": r.EntityType,
			}).
			Mark(ierr.ErrInvalidOperation)
	}
	if r.FileType != types.FileTypeCSV {
		return ierr.NewError("only CSV file_type is supported for imports").
			WithHint("Set file_type=CSV").
			WithReportableDetails(map[string]interface{}{
				"file_type": r.FileType,
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	if r.FileProvider != FileProviderCSVBox {
		return ierr.NewErrorf("unsupported file_provider %q", r.FileProvider).
			WithHintf("Set file_provider=%q", FileProviderCSVBox).
			Mark(ierr.ErrValidation)
	}

	if r.UploadID == "" {
		return ierr.NewError("upload_id cannot be empty").
			WithHint("upload_id is required").
			Mark(ierr.ErrValidation)
	}
	if !uploadIDPattern.MatchString(r.UploadID) {
		return ierr.NewError("invalid upload_id").
			WithHint("upload_id must be 1-128 chars of [A-Za-z0-9_-]").
			Mark(ierr.ErrValidation)
	}

	return validator.ValidateRequest(r)
}

// ToTask assembles the domain task. Nothing caller-supplied lands in the S3
// key path: the service recomposes the key from config + ctx (tenant, env) +
// upload_id at process time, so the persisted metadata is treated as an
// audit log, not a source of truth. FileURL is intentionally empty — there
// is no URL to persist; the object is addressed by key derivation.
func (r *CreateTaskRequest) ToTask(ctx context.Context) *task.Task {
	metadata := r.Metadata
	if metadata == nil {
		metadata = make(map[string]interface{})
	}
	metadata["file_provider"] = r.FileProvider
	metadata["upload_id"] = r.UploadID

	return &task.Task{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TASK),
		TaskType:      r.TaskType,
		EntityType:    r.EntityType,
		FileName:      r.FileName,
		FileType:      r.FileType,
		TaskStatus:    types.TaskStatusPending,
		Metadata:      metadata,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
}

// TaskResponse represents a task in responses
type TaskResponse struct {
	task.Task
}

// NewTaskResponse creates a new task response from a domain task
func NewTaskResponse(t *task.Task) *TaskResponse {
	if t == nil {
		return nil
	}

	return &TaskResponse{
		Task: task.Task{
			ID:                t.ID,
			TaskType:          t.TaskType,
			EntityType:        t.EntityType,
			EnvironmentID:     t.EnvironmentID,
			FileURL:           t.FileURL,
			FileName:          t.FileName,
			FileType:          t.FileType,
			TaskStatus:        t.TaskStatus,
			TotalRecords:      t.TotalRecords,
			ProcessedRecords:  t.ProcessedRecords,
			SuccessfulRecords: t.SuccessfulRecords,
			FailedRecords:     t.FailedRecords,
			ErrorSummary:      t.ErrorSummary,
			Metadata:          t.Metadata,
			StartedAt:         t.StartedAt,
			CompletedAt:       t.CompletedAt,
			FailedAt:          t.FailedAt,
			BaseModel:         t.BaseModel,
		},
	}
}

// ListTasksResponse represents the response for listing tasks
type ListTasksResponse struct {
	Items      []*TaskResponse          `json:"items"`
	Pagination types.PaginationResponse `json:"pagination"`
}

// UpdateTaskStatusRequest represents a request to update a task's status
type UpdateTaskStatusRequest struct {
	TaskStatus types.TaskStatus `json:"task_status" binding:"required"`
}

func (r *UpdateTaskStatusRequest) Validate() error {
	if r.TaskStatus == "" {
		return ierr.NewError("task_status cannot be empty").
			WithHint("Task status cannot be empty").
			Mark(ierr.ErrValidation)
	}
	return nil
}
