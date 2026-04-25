package types

import "github.com/samber/lo"

// WorkflowExecutionFilter defines filters for workflow execution search.
// Uses the same format as FeatureFilter: QueryFilter, TimeRangeFilter, Filters, and Sort.
type WorkflowExecutionFilter struct {
	*QueryFilter
	*TimeRangeFilter

	*DSLFilter

	// Workflow-specific filters
	WorkflowID     string `json:"workflow_id,omitempty" form:"workflow_id"`
	WorkflowType   string `json:"workflow_type,omitempty" form:"workflow_type"`
	TaskQueue      string `json:"task_queue,omitempty" form:"task_queue"`
	WorkflowStatus string `json:"workflow_status,omitempty" form:"workflow_status"` // e.g. Running, Completed, Failed
	Entity         string `json:"entity,omitempty" form:"entity"`                   // e.g. plan, invoice, subscription
	EntityID       string `json:"entity_id,omitempty" form:"entity_id"`           // e.g. plan_01ABC123
}

// NewDefaultWorkflowExecutionFilter returns a filter with default query options (default sort: start_time desc).
func NewDefaultWorkflowExecutionFilter() *WorkflowExecutionFilter {
	q := NewDefaultQueryFilter()
	q.Sort = lo.ToPtr("start_time")
	q.Order = lo.ToPtr("desc")
	return &WorkflowExecutionFilter{
		QueryFilter: q,
	}
}

// NewNoLimitWorkflowExecutionFilter returns a filter with no pagination limit (for ListAll)
func NewNoLimitWorkflowExecutionFilter() *WorkflowExecutionFilter {
	return &WorkflowExecutionFilter{
		QueryFilter: NewNoLimitQueryFilter(),
	}
}

// Validate validates the filter and its nested Filters/Sort
func (f *WorkflowExecutionFilter) Validate() error {
	if f == nil {
		return nil
	}
	if f.QueryFilter == nil {
		f.QueryFilter = NewDefaultQueryFilter()
	}
	if err := f.QueryFilter.Validate(); err != nil {
		return err
	}
	if f.TimeRangeFilter != nil {
		if err := f.TimeRangeFilter.Validate(); err != nil {
			return err
		}
	}
	if err := f.DSLFilter.Validate(); err != nil {
		return err
	}
	return nil
}

// GetLimit returns the limit (implements BaseFilter for pagination)
func (f *WorkflowExecutionFilter) GetLimit() int {
	if f == nil || f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

// GetOffset returns the offset
func (f *WorkflowExecutionFilter) GetOffset() int {
	if f == nil || f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}

// GetSort returns the sort field; default for workflow executions is start_time.
func (f *WorkflowExecutionFilter) GetSort() string {
	if f == nil || f.QueryFilter == nil {
		return "start_time"
	}
	if f.QueryFilter.Sort == nil || *f.QueryFilter.Sort == "" {
		return "start_time"
	}
	return *f.QueryFilter.Sort
}

// GetOrder returns the simple order (asc/desc)
func (f *WorkflowExecutionFilter) GetOrder() string {
	if f == nil || f.QueryFilter == nil {
		return "desc"
	}
	if f.QueryFilter.Order == nil || *f.QueryFilter.Order == "" {
		return "desc"
	}
	return *f.QueryFilter.Order
}

// GetStatus is not used for workflow executions but required by BaseFilter
func (f *WorkflowExecutionFilter) GetStatus() string { return "" }

// GetExpand returns empty expand for workflow executions
func (f *WorkflowExecutionFilter) GetExpand() Expand { return NewExpand("") }

// IsUnlimited returns whether the filter has no limit
func (f *WorkflowExecutionFilter) IsUnlimited() bool {
	if f == nil || f.QueryFilter == nil {
		return false
	}
	return f.QueryFilter.IsUnlimited()
}
