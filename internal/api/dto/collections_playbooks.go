package dto

import (
	"sort"
	"strings"
	"time"

	"github.com/samber/lo"
	"github.com/shopspring/decimal"

	"github.com/flexprice/flexprice/internal/domain/collections"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

const (
	// PlaybookNameMaxLength and PlaybookDescriptionMaxLength bound a playbook's texts.
	PlaybookNameMaxLength        = 255
	PlaybookDescriptionMaxLength = 2000
	// PlaybookRunsDefaultPageSize and PlaybookRunsMaxPageSize page the in-progress list and the task list.
	PlaybookRunsDefaultPageSize = 25
	PlaybookRunsMaxPageSize     = 100
	// PlaybookPreviewSampleSize is how many matching invoices a preview lists.
	PlaybookPreviewSampleSize = 10
)

// CreatePlaybookRequest starts a draft playbook, blank or from a template.
type CreatePlaybookRequest struct {
	// Name of the playbook; defaults to the template's name, or "Untitled playbook".
	Name string `json:"name,omitempty"`
	// TemplateID starts the playbook from a template (GET /collections/playbook-templates).
	TemplateID string `json:"template_id,omitempty"`
}

func (r *CreatePlaybookRequest) Validate() error {
	r.Name, r.TemplateID = strings.TrimSpace(r.Name), strings.TrimSpace(r.TemplateID)
	if len(r.Name) > PlaybookNameMaxLength {
		return ierr.NewError("name too long").WithHint("The name may be at most 255 characters.").Mark(ierr.ErrValidation)
	}
	return nil
}

// UpdatePlaybookRequest saves the whole playbook. Version is the version the editor loaded; a playbook saved by
// someone else since is a 409.
type UpdatePlaybookRequest struct {
	Version      int                           `json:"version" binding:"required"`
	Name         string                        `json:"name"`
	Description  string                        `json:"description"`
	Trigger      types.PlaybookTrigger         `json:"trigger" binding:"required"`
	Audience     types.PlaybookAudience        `json:"audience"`
	Steps        []types.PlaybookStep          `json:"steps"`
	Stops        []types.PlaybookStopCondition `json:"stops"`
	MinGapDays   *int                          `json:"min_gap_days,omitempty"`
	SkipWeekends *bool                         `json:"skip_weekends,omitempty"`
}

// Validate checks the playbook's shape and normalizes it. It does not check readiness: a draft may have an empty
// subject. Activation checks readiness.
func (r *UpdatePlaybookRequest) Validate() error {
	r.Name, r.Description = strings.TrimSpace(r.Name), strings.TrimSpace(r.Description)
	switch {
	case r.Version < 1:
		return ierr.NewError("version required").WithHint("Send the version of the playbook you loaded.").Mark(ierr.ErrValidation)
	case len(r.Name) > PlaybookNameMaxLength:
		return ierr.NewError("name too long").WithHint("The name may be at most 255 characters.").Mark(ierr.ErrValidation)
	case len(r.Description) > PlaybookDescriptionMaxLength:
		return ierr.NewError("description too long").WithHint("The description may be at most 2000 characters.").Mark(ierr.ErrValidation)
	case r.Trigger == "":
		return ierr.NewError("trigger required").WithHint("trigger must be overdue or before_due.").Mark(ierr.ErrValidation)
	case len(r.Steps) > types.PlaybookMaxSteps:
		return ierr.NewError("too many steps").WithHint("A playbook may have at most 20 steps.").Mark(ierr.ErrValidation)
	}
	if err := r.Trigger.Validate(); err != nil {
		return err
	}
	if r.Audience.Kind == "" {
		r.Audience.Kind = types.PlaybookAudienceAll
	}
	r.Audience = r.Audience.Normalize()
	if err := r.Audience.Validate(); err != nil {
		return err
	}
	seen := map[string]bool{}
	for i := range r.Steps {
		r.Steps[i] = r.Steps[i].Normalize()
		if err := r.Steps[i].Validate(); err != nil {
			return err
		}
		if seen[r.Steps[i].ID] {
			return ierr.NewErrorf("duplicate step id %s", r.Steps[i].ID).WithHint("Every step needs its own id.").Mark(ierr.ErrValidation)
		}
		seen[r.Steps[i].ID] = true
	}
	for _, s := range r.Stops {
		if s == "" {
			return ierr.NewError("empty stop").WithHint("stops may hold invoice_paid, manually_paused, customer_replied or dispute_filed.").Mark(ierr.ErrValidation)
		}
		if err := s.Validate(); err != nil {
			return err
		}
	}
	r.Stops = types.NormalizePlaybookStops(r.Stops)
	if r.MinGapDays != nil && (*r.MinGapDays < 0 || *r.MinGapDays > types.PlaybookMaxGapDays) {
		return ierr.NewError("invalid min_gap_days").WithHint("min_gap_days must be between 0 and 30.").Mark(ierr.ErrValidation)
	}
	return nil
}

// PlaybookVersionRequest names the version an action applies to, so it is refused if the playbook changed meanwhile.
type PlaybookVersionRequest struct {
	Version int `json:"version" binding:"required"`
}

func (r *PlaybookVersionRequest) Validate() error {
	if r.Version < 1 {
		return ierr.NewError("version required").WithHint("Send the version of the playbook you loaded.").Mark(ierr.ErrValidation)
	}
	return nil
}

// ReorderPlaybooksRequest lists every playbook of the environment, highest priority first.
type ReorderPlaybooksRequest struct {
	IDs []string `json:"ids" binding:"required"`
}

func (r *ReorderPlaybooksRequest) Validate() error {
	if len(r.IDs) == 0 {
		return ierr.NewError("ids required").WithHint("List every playbook id, highest priority first.").Mark(ierr.ErrValidation)
	}
	if len(lo.Uniq(r.IDs)) != len(r.IDs) {
		return ierr.NewError("duplicate ids").WithHint("List each playbook once.").Mark(ierr.ErrValidation)
	}
	return nil
}

// ListPlaybooksRequest filters the playbook list by state.
type ListPlaybooksRequest struct {
	// State is a comma-separated list of draft, live and paused; empty lists every playbook.
	State string `form:"state" json:"state,omitempty"`
}

// States parses the state filter.
func (r *ListPlaybooksRequest) States() ([]types.PlaybookState, error) {
	var out []types.PlaybookState
	for _, raw := range strings.Split(r.State, ",") {
		s := types.PlaybookState(strings.TrimSpace(raw))
		if s == "" {
			continue
		}
		if err := s.Validate(); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return lo.Uniq(out), nil
}

// PlaybookStatsResponse is how a playbook is doing over the last 30 days.
type PlaybookStatsResponse struct {
	// InProgress counts invoices in the playbook now (active or paused runs).
	InProgress int `json:"in_progress"`
	// RemindersSent30d counts reminder emails the playbook's steps sent or approved in the last 30 days.
	RemindersSent30d int `json:"reminders_sent_30d"`
	// Recovered30d is what was paid, per currency, on invoices within 7 days after one of the playbook's reminders.
	Recovered30d []CollectionsCurrencyTotal `json:"recovered_30d"`
	// PaidWithin7dRate is the share of reminded invoices paid within 7 days of a reminder; null when none were reminded.
	PaidWithin7dRate *float64 `json:"paid_within_7d_rate"`
}

// PlaybookResponse is a playbook with its stats.
type PlaybookResponse struct {
	ID           string                        `json:"id"`
	Name         string                        `json:"name"`
	Description  string                        `json:"description"`
	State        types.PlaybookState           `json:"state"`
	Priority     int                           `json:"priority"`
	Trigger      types.PlaybookTrigger         `json:"trigger"`
	Audience     types.PlaybookAudience        `json:"audience"`
	Steps        []types.PlaybookStep          `json:"steps"`
	Stops        []types.PlaybookStopCondition `json:"stops"`
	MinGapDays   int                           `json:"min_gap_days"`
	SkipWeekends bool                          `json:"skip_weekends"`
	TemplateID   string                        `json:"template_id,omitempty"`
	Version      int                           `json:"version"`
	ActivatedAt  *time.Time                    `json:"activated_at,omitempty"`
	// Problems lists what keeps the playbook from going live; empty when it is ready.
	Problems  []types.PlaybookProblem `json:"problems"`
	Stats     *PlaybookStatsResponse  `json:"stats,omitempty"`
	CreatedAt time.Time               `json:"created_at"`
	UpdatedAt time.Time               `json:"updated_at"`
	CreatedBy string                  `json:"created_by,omitempty"`
	UpdatedBy string                  `json:"updated_by,omitempty"`
}

// NewPlaybookResponse renders p; stats are attached when given.
func NewPlaybookResponse(p *collections.Playbook, stats *collections.PlaybookStats) *PlaybookResponse {
	resp := &PlaybookResponse{
		ID: p.ID, Name: p.Name, Description: p.Description, State: p.State, Priority: p.Priority,
		Trigger: p.Trigger, Audience: p.Audience, Steps: p.OrderedSteps(), Stops: p.Stops,
		MinGapDays: p.MinGapDays, SkipWeekends: p.SkipWeekends, TemplateID: p.TemplateID, Version: p.Version,
		ActivatedAt: p.ActivatedAt, Problems: lo.Ternary(p.Problems() == nil, []types.PlaybookProblem{}, p.Problems()),
		CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt, CreatedBy: p.CreatedBy, UpdatedBy: p.UpdatedBy,
	}
	if resp.Steps == nil {
		resp.Steps = []types.PlaybookStep{}
	}
	if stats != nil {
		resp.Stats = &PlaybookStatsResponse{
			InProgress:       stats.InProgress,
			RemindersSent30d: stats.RemindersSent30d,
			Recovered30d:     playbookTotals(stats.Recovered30d),
			PaidWithin7dRate: stats.PaidWithin7dRate(),
		}
	}
	return resp
}

// playbookTotals renders money per currency, sorted by currency; never summed across currencies.
func playbookTotals(totals []collections.CurrencyTotal) []CollectionsCurrencyTotal {
	out := make([]CollectionsCurrencyTotal, 0, len(totals))
	for _, t := range totals {
		out = append(out, CollectionsCurrencyTotal{Currency: t.Currency, Amount: t.Amount})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Currency < out[j].Currency })
	return out
}

// ListPlaybooksResponse lists playbooks in priority order.
type ListPlaybooksResponse struct {
	Items []*PlaybookResponse `json:"items"`
}

// PlaybookTemplateResponse is a starter playbook.
type PlaybookTemplateResponse struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Trigger     types.PlaybookTrigger  `json:"trigger"`
	Audience    types.PlaybookAudience `json:"audience"`
	Steps       []types.PlaybookStep   `json:"steps"`
}

type ListPlaybookTemplatesResponse struct {
	Items []PlaybookTemplateResponse `json:"items"`
}

// PlaybookProblemsDetails is what a 422 on activate carries in error.details.problems.
type PlaybookProblemsDetails struct {
	Problems []types.PlaybookProblem `json:"problems"`
}

// ListPlaybookRunsRequest pages a playbook's runs.
type ListPlaybookRunsRequest struct {
	// State is a comma-separated list of active, paused, stopped and completed; defaults to active,paused.
	State  string `form:"state" json:"state,omitempty"`
	Limit  int    `form:"limit" json:"limit,omitempty"`
	Offset int    `form:"offset" json:"offset,omitempty"`
}

func (r *ListPlaybookRunsRequest) Validate() error {
	if r.Limit < 0 || r.Limit > PlaybookRunsMaxPageSize || r.Offset < 0 {
		return ierr.NewError("invalid page").WithHint("limit must be between 1 and 100 and offset at least 0.").Mark(ierr.ErrValidation)
	}
	return nil
}

func (r *ListPlaybookRunsRequest) PageLimit() int {
	return lo.Ternary(r.Limit == 0, PlaybookRunsDefaultPageSize, r.Limit)
}

// States parses the state filter, defaulting to the live states.
func (r *ListPlaybookRunsRequest) States() ([]types.PlaybookRunState, error) {
	var out []types.PlaybookRunState
	for _, raw := range strings.Split(r.State, ",") {
		s := types.PlaybookRunState(strings.TrimSpace(raw))
		if s == "" {
			continue
		}
		if err := s.Validate(); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		return types.LivePlaybookRunStates, nil
	}
	return lo.Uniq(out), nil
}

// PlaybookStepRunResponse is one scheduled step of a run.
type PlaybookStepRunResponse struct {
	ID         string                       `json:"id"`
	StepID     string                       `json:"step_id"`
	Kind       types.PlaybookStepKind       `json:"kind"`
	DueAt      time.Time                    `json:"due_at"`
	State      types.PlaybookStepRunState   `json:"state"`
	SkipReason types.PlaybookStepSkipReason `json:"skip_reason,omitempty"`
	// ResultRef is what the step made: a reminder id, a task id or an email log id.
	ResultRef  string     `json:"result_ref,omitempty"`
	Attempts   int        `json:"attempts"`
	Error      string     `json:"error,omitempty"`
	ExecutedAt *time.Time `json:"executed_at,omitempty"`
}

func NewPlaybookStepRunResponse(s *collections.PlaybookStepRun) *PlaybookStepRunResponse {
	return &PlaybookStepRunResponse{
		ID: s.ID, StepID: s.StepID, Kind: s.Kind, DueAt: s.DueAt, State: s.State, SkipReason: s.SkipReason,
		ResultRef: s.ResultRef, Attempts: s.Attempts, Error: s.Error, ExecutedAt: s.ExecutedAt,
	}
}

// PlaybookRunResponse is one invoice's run through a playbook.
type PlaybookRunResponse struct {
	ID              string                   `json:"id"`
	PlaybookID      string                   `json:"playbook_id"`
	InvoiceID       string                   `json:"invoice_id"`
	InvoiceNumber   string                   `json:"invoice_number,omitempty"`
	CustomerID      string                   `json:"customer_id"`
	CustomerName    string                   `json:"customer_name,omitempty"`
	Currency        string                   `json:"currency,omitempty"`
	AmountRemaining *decimal.Decimal         `json:"amount_remaining,omitempty" swaggertype:"string"`
	InvoiceDueDate  time.Time                `json:"invoice_due_date"`
	DaysOverdue     int                      `json:"days_overdue"`
	State           types.PlaybookRunState   `json:"state"`
	StopReason      types.PlaybookStopReason `json:"stop_reason,omitempty"`
	PausedReason    string                   `json:"paused_reason,omitempty"`
	StartedAt       time.Time                `json:"started_at"`
	EndedAt         *time.Time               `json:"ended_at,omitempty"`
	NextStep        *PlaybookStepRunResponse `json:"next_step,omitempty"`
}

func NewPlaybookRunResponse(r *collections.PlaybookRun, now time.Time) *PlaybookRunResponse {
	return &PlaybookRunResponse{
		ID: r.ID, PlaybookID: r.PlaybookID, InvoiceID: r.InvoiceID, CustomerID: r.CustomerID,
		InvoiceDueDate: r.InvoiceDueDate, DaysOverdue: types.CalendarDaysBetween(r.InvoiceDueDate, now),
		State: r.State, StopReason: r.StopReason, PausedReason: r.PausedReason, StartedAt: r.StartedAt, EndedAt: r.EndedAt,
	}
}

// NewPlaybookRunViewResponse renders a run joined to its invoice.
func NewPlaybookRunViewResponse(v collections.PlaybookRunView, now time.Time) *PlaybookRunResponse {
	resp := NewPlaybookRunResponse(&v.Run, now)
	resp.InvoiceNumber, resp.CustomerName, resp.Currency = v.InvoiceNumber, v.CustomerName, v.Currency
	resp.AmountRemaining = lo.ToPtr(v.AmountRemaining)
	if v.NextStep != nil {
		resp.NextStep = NewPlaybookStepRunResponse(v.NextStep)
	}
	return resp
}

type ListPlaybookRunsResponse struct {
	Items      []*PlaybookRunResponse   `json:"items"`
	Pagination types.PaginationResponse `json:"pagination"`
}

// PlaybookPreviewInvoice is an invoice a playbook would enrol today.
type PlaybookPreviewInvoice struct {
	InvoiceID       string          `json:"invoice_id"`
	InvoiceNumber   string          `json:"invoice_number"`
	CustomerID      string          `json:"customer_id"`
	CustomerName    string          `json:"customer_name"`
	Currency        string          `json:"currency"`
	AmountRemaining decimal.Decimal `json:"amount_remaining" swaggertype:"string"`
	DueDate         time.Time       `json:"due_date"`
	DaysOverdue     int             `json:"days_overdue"`
	// FirstStep is the first step this invoice would get, with its day; null when every step is already past.
	FirstStep *PlaybookPreviewStep `json:"first_step"`
}

type PlaybookPreviewStep struct {
	StepID string                 `json:"step_id"`
	Kind   types.PlaybookStepKind `json:"kind"`
	DueAt  time.Time              `json:"due_at"`
}

// PlaybookPreviewResponse says which invoices the playbook would enrol if it were live now.
type PlaybookPreviewResponse struct {
	AsOf time.Time `json:"as_of"`
	// MatchingCount counts invoices that match the trigger and audience and are in no playbook now.
	MatchingCount int `json:"matching_count"`
	// TakenByHigherPriority counts those a live playbook higher in the list would take first.
	TakenByHigherPriority int `json:"taken_by_higher_priority"`
	// Truncated is true when more invoices match than were counted (the count stops at 1000).
	Truncated bool                     `json:"truncated"`
	Sample    []PlaybookPreviewInvoice `json:"sample"`
}

// PausePlaybookRunRequest pauses one invoice's run.
type PausePlaybookRunRequest struct {
	Reason string `json:"reason,omitempty"`
}

func (r *PausePlaybookRunRequest) Validate() error {
	r.Reason = strings.TrimSpace(r.Reason)
	if len(r.Reason) > 1000 {
		return ierr.NewError("reason too long").WithHint("The reason may be at most 1000 characters.").Mark(ierr.ErrValidation)
	}
	return nil
}

// CustomerPlaybookRunsResponse is what pausing or resuming a customer changed.
type CustomerPlaybookRunsResponse struct {
	RunsAffected int                    `json:"runs_affected"`
	Runs         []*PlaybookRunResponse `json:"runs"`
}

// ListCollectionTasksRequest pages the environment's tasks.
type ListCollectionTasksRequest struct {
	InvoiceID      string `form:"invoice_id" json:"invoice_id,omitempty"`
	CustomerID     string `form:"customer_id" json:"customer_id,omitempty"`
	AssigneeUserID string `form:"assignee_user_id" json:"assignee_user_id,omitempty"`
	// State is open or done; empty lists both.
	State  types.CollectionTaskState `form:"state" json:"state,omitempty"`
	Limit  int                       `form:"limit" json:"limit,omitempty"`
	Offset int                       `form:"offset" json:"offset,omitempty"`
}

func (r *ListCollectionTasksRequest) Validate() error {
	if r.Limit < 0 || r.Limit > PlaybookRunsMaxPageSize || r.Offset < 0 {
		return ierr.NewError("invalid page").WithHint("limit must be between 1 and 100 and offset at least 0.").Mark(ierr.ErrValidation)
	}
	return r.State.Validate()
}

func (r *ListCollectionTasksRequest) PageLimit() int {
	return lo.Ternary(r.Limit == 0, PlaybookRunsDefaultPageSize, r.Limit)
}

// CollectionTaskResponse is a to-do a playbook step created.
type CollectionTaskResponse struct {
	ID             string                    `json:"id"`
	InvoiceID      string                    `json:"invoice_id"`
	CustomerID     string                    `json:"customer_id"`
	RunID          string                    `json:"run_id,omitempty"`
	StepID         string                    `json:"step_id,omitempty"`
	AssigneeUserID string                    `json:"assignee_user_id,omitempty"`
	AssigneeEmail  string                    `json:"assignee_email,omitempty"`
	Note           string                    `json:"note"`
	State          types.CollectionTaskState `json:"state"`
	DoneAt         *time.Time                `json:"done_at,omitempty"`
	DoneBy         string                    `json:"done_by,omitempty"`
	CreatedAt      time.Time                 `json:"created_at"`
}

func NewCollectionTaskResponse(t *collections.CollectionTask) *CollectionTaskResponse {
	return &CollectionTaskResponse{
		ID: t.ID, InvoiceID: t.InvoiceID, CustomerID: t.CustomerID, RunID: t.RunID, StepID: t.StepID,
		AssigneeUserID: t.AssigneeUserID, AssigneeEmail: t.AssigneeEmail, Note: t.Note, State: t.State,
		DoneAt: t.DoneAt, DoneBy: t.DoneBy, CreatedAt: t.CreatedAt,
	}
}

type ListCollectionTasksResponse struct {
	Items      []*CollectionTaskResponse `json:"items"`
	Pagination types.PaginationResponse  `json:"pagination"`
}

// RunPlaybooksResponse is what one scheduler pass did in the current environment.
type RunPlaybooksResponse struct {
	RunsStopped   int `json:"runs_stopped"`
	RunsEnrolled  int `json:"runs_enrolled"`
	StepsExecuted int `json:"steps_executed"`
	StepsSkipped  int `json:"steps_skipped"`
	StepsFailed   int `json:"steps_failed"`
	StepsDeferred int `json:"steps_deferred"`
}
