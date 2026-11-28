package collections

import (
	"context"
	"sort"
	"time"

	"github.com/shopspring/decimal"

	"github.com/flexprice/flexprice/internal/types"
)

// Playbook is a rule for chasing unpaid invoices: which invoices it picks up (trigger and audience), what happens
// when (steps, each on a day counted from the due date) and what ends it (stops). Live playbooks are checked in
// priority order and the first one an invoice matches runs it.
type Playbook struct {
	ID            string
	Name          string
	Description   string
	State         types.PlaybookState
	Priority      int
	Trigger       types.PlaybookTrigger
	Audience      types.PlaybookAudience
	Steps         []types.PlaybookStep
	Stops         []types.PlaybookStopCondition
	MinGapDays    int
	SkipWeekends  bool
	TemplateID    string
	Version       int
	ActivatedAt   *time.Time
	EnvironmentID string
	types.BaseModel
}

// OrderedSteps returns the steps in the order they run: by day, then the order they were written.
func (p *Playbook) OrderedSteps() []types.PlaybookStep {
	steps := append([]types.PlaybookStep(nil), p.Steps...)
	sort.SliceStable(steps, func(i, j int) bool { return steps[i].Day < steps[j].Day })
	return steps
}

// Step returns the step with the given id.
func (p *Playbook) Step(id string) (types.PlaybookStep, bool) {
	for _, s := range p.Steps {
		if s.ID == id {
			return s, true
		}
	}
	return types.PlaybookStep{}, false
}

// FirstStepDay is the day of the earliest step; 0 when there are none.
func (p *Playbook) FirstStepDay() int {
	steps := p.OrderedSteps()
	if len(steps) == 0 {
		return 0
	}
	return steps[0].Day
}

// EnrolFrom is the earliest instant an invoice due on dueDate may join this playbook: after the due date for an
// overdue playbook, on the day of the first step for a before-due one.
func (p *Playbook) EnrolFrom(dueDate time.Time) time.Time {
	if p.Trigger == types.PlaybookTriggerBeforeDue {
		return types.PlaybookStepDueOn(dueDate, p.FirstStepDay())
	}
	return dueDate
}

// Problems lists what keeps the playbook from going live; empty means it is ready.
func (p *Playbook) Problems() []types.PlaybookProblem {
	var out []types.PlaybookProblem
	if p.Name == "" {
		out = append(out, types.PlaybookProblem{Field: "name", Message: "Name the playbook"})
	}
	hasEmail, hasBeforeDue := false, false
	for _, s := range p.OrderedSteps() {
		hasEmail = hasEmail || s.Kind == types.PlaybookStepEmail
		hasBeforeDue = hasBeforeDue || s.Day <= 0
		out = append(out, s.Problems()...)
	}
	if !hasEmail {
		out = append(out, types.PlaybookProblem{Field: "steps", Message: "Add at least one reminder email"})
	}
	if p.Trigger == types.PlaybookTriggerBeforeDue && !hasBeforeDue {
		out = append(out, types.PlaybookProblem{Field: "trigger", Message: "A playbook that starts before the due date needs a step on or before the due date"})
	}
	return out
}

// HasStop reports whether the playbook stops on c.
func (p *Playbook) HasStop(c types.PlaybookStopCondition) bool {
	for _, s := range p.Stops {
		if s == c {
			return true
		}
	}
	return false
}

// PlaybookStats is how a playbook is doing. Money is per currency and never summed across currencies.
type PlaybookStats struct {
	InProgress       int
	RemindersSent30d int
	Recovered30d     []CurrencyTotal
	// RemindedInvoices30d and PaidWithin7d30d give the share of reminded invoices paid within 7 days of a reminder.
	RemindedInvoices30d int
	PaidWithin7d30d     int
}

// PaidWithin7dRate is the share of reminded invoices paid within 7 days; nil when nothing was reminded.
func (s PlaybookStats) PaidWithin7dRate() *float64 {
	if s.RemindedInvoices30d == 0 {
		return nil
	}
	rate := float64(s.PaidWithin7d30d) / float64(s.RemindedInvoices30d)
	return &rate
}

// PlaybookRun is one invoice going through one playbook. An invoice has at most one live (active or paused) run.
type PlaybookRun struct {
	ID             string
	PlaybookID     string
	InvoiceID      string
	CustomerID     string
	State          types.PlaybookRunState
	StopReason     types.PlaybookStopReason
	PausedReason   string
	InvoiceDueDate time.Time
	NextStepAt     *time.Time
	StartedAt      time.Time
	EndedAt        *time.Time
	EnvironmentID  string
	types.BaseModel
}

// IsLive reports a run that still holds its invoice.
func (r *PlaybookRun) IsLive() bool {
	return r.State == types.PlaybookRunActive || r.State == types.PlaybookRunPaused
}

// PlaybookStepRun is one step scheduled for one run. (run, step) is unique, which makes a step run at most once.
type PlaybookStepRun struct {
	ID            string
	RunID         string
	StepID        string
	Kind          types.PlaybookStepKind
	DueAt         time.Time
	State         types.PlaybookStepRunState
	SkipReason    types.PlaybookStepSkipReason
	ResultRef     string
	Attempts      int
	Error         string
	ExecutedAt    *time.Time
	TenantID      string
	EnvironmentID string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// CollectionTask is a to-do a playbook step created for a person.
type CollectionTask struct {
	ID             string
	InvoiceID      string
	CustomerID     string
	RunID          string
	StepID         string
	AssigneeUserID string
	AssigneeEmail  string
	Note           string
	State          types.CollectionTaskState
	DoneAt         *time.Time
	DoneBy         string
	EnvironmentID  string
	types.BaseModel
}

// PlaybookCandidate is an open invoice a playbook may enrol, with what its audience and steps need.
type PlaybookCandidate struct {
	InvoiceID       string
	InvoiceNumber   string
	CustomerID      string
	CustomerName    string
	Currency        string
	AmountRemaining decimal.Decimal
	DueDate         time.Time
}

// PlaybookRunView is a live run joined to its invoice, for the playbook's in-progress list.
type PlaybookRunView struct {
	Run             PlaybookRun
	InvoiceNumber   string
	CustomerName    string
	Currency        string
	AmountRemaining decimal.Decimal
	// NextStep is the earliest pending or failed step, nil when none is left.
	NextStep *PlaybookStepRun
}

// PlaybookEnvironment is a tenant and environment holding at least one live playbook.
type PlaybookEnvironment struct {
	TenantID      string
	EnvironmentID string
}

// PlaybookFilter selects playbooks of the current environment.
type PlaybookFilter struct {
	States []types.PlaybookState
}

type PlaybookRepository interface {
	Create(ctx context.Context, p *Playbook) error
	Get(ctx context.Context, id string) (*Playbook, error)
	// List returns the environment's playbooks in priority order.
	List(ctx context.Context, filter PlaybookFilter) ([]*Playbook, error)
	// Update saves p if the stored version is still previousVersion, bumping the version; a stale version is a
	// version conflict.
	Update(ctx context.Context, p *Playbook, previousVersion int) error
	// SetPriorities sets each playbook's priority (id to priority) in one transaction.
	SetPriorities(ctx context.Context, priorities map[string]int) error
	Delete(ctx context.Context, p *Playbook) error
	// MaxPriority is the highest priority in use, 0 when there are no playbooks.
	MaxPriority(ctx context.Context) (int, error)
	// ListLiveEnvironments returns every tenant and environment with a live playbook. It reads across tenants and is
	// called only by the scheduler, which then runs each environment in its own scoped context.
	ListLiveEnvironments(ctx context.Context) ([]PlaybookEnvironment, error)
	// Stats returns the stats of the given playbooks as of now.
	Stats(ctx context.Context, playbookIDs []string, now time.Time) (map[string]PlaybookStats, error)
}

type PlaybookRunRepository interface {
	// ListCandidates returns open finalized invoices with a due date and no run that holds them, which match the
	// playbook's audience and may be enrolled as of now. Invoices whose last run stopped because its playbook was
	// deleted are candidates again. Ordered by due date, then id.
	ListCandidates(ctx context.Context, p *Playbook, now time.Time, limit int) ([]PlaybookCandidate, error)
	// CreateRun stores the run with its step runs. A second live run for the invoice is ErrAlreadyExists.
	CreateRun(ctx context.Context, run *PlaybookRun, steps []*PlaybookStepRun) error
	GetRun(ctx context.Context, id string) (*PlaybookRun, error)
	UpdateRun(ctx context.Context, run *PlaybookRun) error
	// ListLiveRuns returns the live runs, of one playbook when playbookID is set, of one customer when customerID is set.
	ListLiveRuns(ctx context.Context, playbookID, customerID string) ([]*PlaybookRun, error)
	// ListRunViews pages the playbook's runs in the given states, oldest due date first.
	ListRunViews(ctx context.Context, playbookID string, states []types.PlaybookRunState, offset, limit int) ([]PlaybookRunView, int, error)
	// StopFinishedRuns stops every live run whose invoice is paid, settled, voided or gone, skipping its pending
	// steps, and returns how many it stopped.
	StopFinishedRuns(ctx context.Context, now time.Time) (int, error)
	// ListStepRuns returns the step runs of the given runs, by due date.
	ListStepRuns(ctx context.Context, runIDs []string) ([]*PlaybookStepRun, error)
	// ListDueRunIDs returns active runs of live playbooks with a pending or retryable failed step due by now.
	ListDueRunIDs(ctx context.Context, now time.Time, limit int) ([]string, error)
	CreateStepRuns(ctx context.Context, steps []*PlaybookStepRun) error
	// UpdateStepRun saves the step run if it is still in previousState; otherwise it is a version conflict.
	UpdateStepRun(ctx context.Context, step *PlaybookStepRun, previousState types.PlaybookStepRunState) error
	// LastCustomerEmailAt is when an email step last ran for the customer, across playbooks.
	LastCustomerEmailAt(ctx context.Context, customerID string) (*time.Time, error)

	CreateTask(ctx context.Context, task *CollectionTask) error
	GetTask(ctx context.Context, id string) (*CollectionTask, error)
	UpdateTask(ctx context.Context, task *CollectionTask) error
	// ListTasks pages the environment's tasks, newest first.
	ListTasks(ctx context.Context, filter TaskFilter, offset, limit int) ([]*CollectionTask, int, error)
}

// TaskFilter selects tasks of the current environment.
type TaskFilter struct {
	InvoiceID      string
	CustomerID     string
	AssigneeUserID string
	States         []types.CollectionTaskState
}

// CandidateDueWindow is the range of due dates [from, before) an invoice may have to join p as of now: one whose
// enrolment day has come, and whose last step is today or later (an invoice past every step is left alone, so a new
// playbook never sends a burst of old steps).
func CandidateDueWindow(p *Playbook, now time.Time) (from, before time.Time) {
	today := types.UTCDay(now)
	steps := p.OrderedSteps()
	lastDay := 0
	if len(steps) > 0 {
		lastDay = steps[len(steps)-1].Day
	}
	from = today.AddDate(0, 0, -lastDay)
	if p.Trigger == types.PlaybookTriggerBeforeDue {
		// Enrol once today >= due day + first step day, i.e. due day <= today - first step day.
		before = today.AddDate(0, 0, 1-p.FirstStepDay())
	} else {
		before = now
	}
	return from, before
}

// NextStepByRun maps each run to its earliest pending step; runs with none left are absent.
func NextStepByRun(steps []*PlaybookStepRun) map[string]*PlaybookStepRun {
	out := map[string]*PlaybookStepRun{}
	for _, s := range steps {
		if s.State != types.PlaybookStepRunPending {
			continue
		}
		if cur, ok := out[s.RunID]; !ok || s.DueAt.Before(cur.DueAt) {
			out[s.RunID] = s
		}
	}
	return out
}
