package types

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
)

type TaskType string

const (
	TaskTypeImport TaskType = "IMPORT"
	TaskTypeExport TaskType = "EXPORT"
)

func (t TaskType) String() string {
	return string(t)
}

func (t TaskType) Validate() error {
	allowed := []TaskType{
		TaskTypeImport,
		TaskTypeExport,
	}
	if !lo.Contains(allowed, t) {
		return ierr.NewError("invalid task type").
			WithHint("Invalid task type").
			Mark(ierr.ErrValidation)
	}
	return nil
}

type EntityType string

const (
	EntityTypeEvents    EntityType = "EVENTS"
	EntityTypePrices    EntityType = "PRICES"
	EntityTypeCustomers EntityType = "CUSTOMERS"
	EntityTypeFeatures  EntityType = "FEATURES"
)

func (e EntityType) String() string {
	return string(e)
}

func (e EntityType) Validate() error {
	allowed := []EntityType{
		EntityTypeEvents,
		EntityTypePrices,
		EntityTypeCustomers,
		EntityTypeFeatures,
	}
	if !lo.Contains(allowed, e) {
		return ierr.NewError("invalid entity type").
			WithHint("Invalid entity type").
			Mark(ierr.ErrValidation)
	}
	return nil
}

type FileType string

const (
	FileTypeCSV  FileType = "CSV"
	FileTypeJSON FileType = "JSON"
)

func (f FileType) String() string {
	return string(f)
}

func (f FileType) Validate() error {
	allowed := []FileType{
		FileTypeCSV,
		FileTypeJSON,
	}
	if !lo.Contains(allowed, f) {
		return ierr.NewError("invalid file type").
			WithHint("Invalid file type").
			Mark(ierr.ErrValidation)
	}
	return nil
}

type TaskStatus string

const (
	TaskStatusPending    TaskStatus = "PENDING"
	TaskStatusProcessing TaskStatus = "PROCESSING"
	TaskStatusCompleted  TaskStatus = "COMPLETED"
	TaskStatusFailed     TaskStatus = "FAILED"
)

func (s TaskStatus) String() string {
	return string(s)
}

func (s TaskStatus) Validate() error {
	allowed := []TaskStatus{
		TaskStatusPending,
		TaskStatusProcessing,
		TaskStatusCompleted,
		TaskStatusFailed,
	}
	if !lo.Contains(allowed, s) {
		return ierr.NewError("invalid task status").
			WithHint("Invalid task status").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// TaskFilter defines the filter parameters for listing tasks
type TaskFilter struct {
	*QueryFilter
	*TimeRangeFilter

	TaskType        *TaskType   `json:"task_type,omitempty" form:"task_type"`
	EntityType      *EntityType `json:"entity_type,omitempty" form:"entity_type"`
	TaskStatus      *TaskStatus `json:"task_status,omitempty" form:"task_status"`
	ScheduledTaskID string      `json:"scheduled_task_id,omitempty" form:"scheduled_task_id"`
	CreatedBy       string      `json:"created_by,omitempty" form:"created_by"`
	StartTime       *time.Time  `json:"start_time,omitempty" form:"start_time"`
	EndTime         *time.Time  `json:"end_time,omitempty" form:"end_time"`
}

// Validate validates the task filter
func (f *TaskFilter) Validate() error {
	if f.QueryFilter != nil {
		if err := f.QueryFilter.Validate(); err != nil {
			return err
		}
	}

	if f.TimeRangeFilter != nil {
		if err := f.TimeRangeFilter.Validate(); err != nil {
			return err
		}
	}

	return nil
}

// GetLimit returns the limit value for the filter
func (f *TaskFilter) GetLimit() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

// GetOffset returns the offset value for the filter
func (f *TaskFilter) GetOffset() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}

// GetSort returns the sort value for the filter
func (f *TaskFilter) GetSort() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetSort()
	}
	return f.QueryFilter.GetSort()
}

// GetOrder returns the order value for the filter
func (f *TaskFilter) GetOrder() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOrder()
	}
	return f.QueryFilter.GetOrder()
}

// GetStatus returns the status value for the filter
func (f *TaskFilter) GetStatus() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetStatus()
	}
	return f.QueryFilter.GetStatus()
}

// GetExpand returns the expand value for the filter
func (f *TaskFilter) GetExpand() Expand {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetExpand()
	}
	return f.QueryFilter.GetExpand()
}

// IsUnlimited returns true if the filter is unlimited
func (f *TaskFilter) IsUnlimited() bool {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().IsUnlimited()
	}
	return f.QueryFilter.IsUnlimited()
}
