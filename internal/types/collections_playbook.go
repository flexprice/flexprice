package types

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/samber/lo"
	"github.com/shopspring/decimal"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

// PlaybookState is where a collection playbook is in its lifecycle. Only live playbooks enrol invoices and run steps.
type PlaybookState string

const (
	PlaybookStateDraft  PlaybookState = "draft"
	PlaybookStateLive   PlaybookState = "live"
	PlaybookStatePaused PlaybookState = "paused"
)

func (s PlaybookState) Validate() error {
	return validateEnum("state", s, []PlaybookState{PlaybookStateDraft, PlaybookStateLive, PlaybookStatePaused})
}

// PlaybookTrigger is when a playbook picks an invoice up.
type PlaybookTrigger string

const (
	// PlaybookTriggerOverdue enrols an invoice once it is past its due date.
	PlaybookTriggerOverdue PlaybookTrigger = "overdue"
	// PlaybookTriggerBeforeDue enrols an invoice on the day of its earliest step, which falls before the due date.
	PlaybookTriggerBeforeDue PlaybookTrigger = "before_due"
)

func (t PlaybookTrigger) Validate() error {
	return validateEnum("trigger", t, []PlaybookTrigger{PlaybookTriggerOverdue, PlaybookTriggerBeforeDue})
}

// PlaybookAudienceKind says which invoices a playbook applies to.
type PlaybookAudienceKind string

const (
	PlaybookAudienceAll             PlaybookAudienceKind = "all"
	PlaybookAudienceSegment         PlaybookAudienceKind = "segment"
	PlaybookAudienceAmountOver      PlaybookAudienceKind = "amount_over"
	PlaybookAudienceDaysOverdueOver PlaybookAudienceKind = "days_overdue_over"
)

// PlaybookSegmentDefaultKey is the customer metadata key a segment audience reads when none is given.
const PlaybookSegmentDefaultKey = "segment"

// PlaybookAudience narrows the invoices a playbook applies to. Only the fields of its kind are set.
type PlaybookAudience struct {
	Kind PlaybookAudienceKind `json:"kind"`
	// Key and Value match customers whose metadata[Key] equals Value, ignoring case (kind segment).
	Key   string `json:"key,omitempty"`
	Value string `json:"value,omitempty"`
	// Amount and Currency match invoices in Currency whose remaining balance is above Amount (kind amount_over).
	Amount   *decimal.Decimal `json:"amount,omitempty"`
	Currency string           `json:"currency,omitempty"`
	// Days matches invoices more than Days days past due (kind days_overdue_over).
	Days int `json:"days,omitempty"`
}

// Normalize drops the fields of other kinds and lower-cases what is matched without case.
func (a PlaybookAudience) Normalize() PlaybookAudience {
	switch a.Kind {
	case PlaybookAudienceSegment:
		key := strings.TrimSpace(a.Key)
		if key == "" {
			key = PlaybookSegmentDefaultKey
		}
		return PlaybookAudience{Kind: a.Kind, Key: key, Value: strings.TrimSpace(a.Value)}
	case PlaybookAudienceAmountOver:
		return PlaybookAudience{Kind: a.Kind, Amount: a.Amount, Currency: strings.ToLower(strings.TrimSpace(a.Currency))}
	case PlaybookAudienceDaysOverdueOver:
		return PlaybookAudience{Kind: a.Kind, Days: a.Days}
	}
	return PlaybookAudience{Kind: a.Kind}
}

func (a PlaybookAudience) Validate() error {
	switch a.Kind {
	case PlaybookAudienceAll:
		return nil
	case PlaybookAudienceSegment:
		if strings.TrimSpace(a.Value) == "" {
			return playbookInvalid("audience.value", "A segment audience needs the segment value to match, for example enterprise.")
		}
		if len(a.Key) > 100 || len(a.Value) > 255 {
			return playbookInvalid("audience", "The segment key may be at most 100 characters and the value at most 255.")
		}
		return nil
	case PlaybookAudienceAmountOver:
		if a.Amount == nil || !a.Amount.IsPositive() {
			return playbookInvalid("audience.amount", "An amount audience needs a positive amount.")
		}
		if len(strings.TrimSpace(a.Currency)) != 3 {
			return playbookInvalid("audience.currency", "An amount audience needs a three-letter currency code.")
		}
		return nil
	case PlaybookAudienceDaysOverdueOver:
		if a.Days < 1 || a.Days > PlaybookMaxStepDay {
			return playbookInvalid("audience.days", fmt.Sprintf("Days overdue must be between 1 and %d.", PlaybookMaxStepDay))
		}
		return nil
	}
	return playbookInvalid("audience.kind", "audience.kind must be all, segment, amount_over or days_overdue_over.")
}

// PlaybookStepKind is what a step does.
type PlaybookStepKind string

const (
	// PlaybookStepEmail writes a payment reminder to the customer.
	PlaybookStepEmail PlaybookStepKind = "email"
	// PlaybookStepInternal tells the team, by email (Slack is accepted and skipped until a Slack connection exists).
	PlaybookStepInternal PlaybookStepKind = "internal"
	// PlaybookStepTask records a task for a person and emails it to them.
	PlaybookStepTask PlaybookStepKind = "task"
)

// PlaybookInternalChannel is where an internal alert goes.
type PlaybookInternalChannel string

const (
	PlaybookChannelEmail PlaybookInternalChannel = "email"
	PlaybookChannelSlack PlaybookInternalChannel = "slack"
)

const (
	// PlaybookMinStepDay and PlaybookMaxStepDay bound a step's day, counted from the due date.
	PlaybookMinStepDay = -30
	PlaybookMaxStepDay = 180
	// PlaybookMaxSteps caps the steps of one playbook.
	PlaybookMaxSteps = 20
	// PlaybookMaxGapDays caps the minimum gap between customer emails.
	PlaybookMaxGapDays = 30
)

// PlaybookTaskAssignee is who gets a task: a user of the environment, or an email address.
type PlaybookTaskAssignee struct {
	UserID string `json:"user_id,omitempty"`
	Email  string `json:"email,omitempty"`
}

// PlaybookStep is one node of a playbook's sequence. Day is counted from the invoice's due date (negative: before it)
// and is what orders the steps. Only the fields of its kind are set.
type PlaybookStep struct {
	ID   string           `json:"id"`
	Kind PlaybookStepKind `json:"kind"`
	Day  int              `json:"day"`

	// Email steps.
	Tone              ReminderTone `json:"tone,omitempty"`
	Subject           string       `json:"subject,omitempty"`
	Body              string       `json:"body,omitempty"`
	SendWithoutReview bool         `json:"send_without_review,omitempty"`

	// Internal alert steps.
	Channel PlaybookInternalChannel `json:"channel,omitempty"`
	To      string                  `json:"to,omitempty"`

	// Task steps.
	Assignee *PlaybookTaskAssignee `json:"assignee,omitempty"`

	// Note is the text of an internal alert or a task.
	Note string `json:"note,omitempty"`
}

// Normalize drops the fields of other kinds and trims text, so stored steps carry only what applies to them.
func (s PlaybookStep) Normalize() PlaybookStep {
	out := PlaybookStep{ID: strings.TrimSpace(s.ID), Kind: s.Kind, Day: s.Day}
	switch s.Kind {
	case PlaybookStepEmail:
		out.Tone, out.Subject, out.Body, out.SendWithoutReview = s.Tone, strings.TrimSpace(s.Subject), strings.TrimSpace(s.Body), s.SendWithoutReview
	case PlaybookStepInternal:
		out.Channel, out.To, out.Note = s.Channel, strings.TrimSpace(s.To), strings.TrimSpace(s.Note)
	case PlaybookStepTask:
		if s.Assignee != nil {
			out.Assignee = &PlaybookTaskAssignee{UserID: strings.TrimSpace(s.Assignee.UserID), Email: strings.ToLower(strings.TrimSpace(s.Assignee.Email))}
		}
		out.Note = strings.TrimSpace(s.Note)
	}
	return out
}

// Validate checks the step's shape: what it may hold. A step can be valid but not ready (an empty subject is
// allowed in a draft); see Problems for what blocks activation.
func (s PlaybookStep) Validate() error {
	if !playbookStepIDPattern.MatchString(s.ID) {
		return playbookInvalid("steps.id", "Every step needs an id of 1 to 50 letters, digits, - or _.")
	}
	if s.Day < PlaybookMinStepDay || s.Day > PlaybookMaxStepDay {
		return playbookInvalid("steps.day", fmt.Sprintf("Step %s: day must be between %d and %d.", s.ID, PlaybookMinStepDay, PlaybookMaxStepDay))
	}
	texts := map[string]string{}
	switch s.Kind {
	case PlaybookStepEmail:
		if !s.Tone.Valid() {
			return playbookInvalid("steps.tone", fmt.Sprintf("Step %s: tone must be friendly, firm or final.", s.ID))
		}
		if len(s.Subject) > 255 {
			return playbookInvalid("steps.subject", fmt.Sprintf("Step %s: the subject may be at most 255 characters.", s.ID))
		}
		texts["subject"], texts["body"] = s.Subject, s.Body
	case PlaybookStepInternal:
		if s.Channel != PlaybookChannelEmail && s.Channel != PlaybookChannelSlack {
			return playbookInvalid("steps.channel", fmt.Sprintf("Step %s: channel must be email or slack.", s.ID))
		}
		texts["note"] = s.Note
	case PlaybookStepTask:
		if s.Assignee != nil && s.Assignee.UserID != "" && s.Assignee.Email != "" {
			return playbookInvalid("steps.assignee", fmt.Sprintf("Step %s: assign the task to a user or an email address, not both.", s.ID))
		}
		texts["note"] = s.Note
	default:
		return playbookInvalid("steps.kind", fmt.Sprintf("Step %s: kind must be email, internal or task.", s.ID))
	}
	for field, text := range texts {
		if len(text) > 10000 {
			return playbookInvalid("steps."+field, fmt.Sprintf("Step %s: the %s may be at most 10000 characters.", s.ID, field))
		}
		if unknown := UnknownPlaybookVariables(text); len(unknown) > 0 {
			return playbookInvalid("steps."+field, fmt.Sprintf("Step %s: unknown variable %s. Use %s.", s.ID, strings.Join(unknown, ", "), strings.Join(PlaybookVariables, ", ")))
		}
	}
	return nil
}

// PlaybookProblem is one thing that keeps a playbook from going live.
type PlaybookProblem struct {
	StepID  string `json:"step_id,omitempty"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Problems lists what the step is missing before its playbook can go live; empty means ready.
func (s PlaybookStep) Problems() []PlaybookProblem {
	var out []PlaybookProblem
	add := func(field, message string) {
		out = append(out, PlaybookProblem{StepID: s.ID, Field: field, Message: message})
	}
	switch s.Kind {
	case PlaybookStepEmail:
		if s.Subject == "" {
			add("subject", "Add a subject")
		}
		if s.Body == "" {
			add("body", "Write the message")
		}
		if s.Day < 0 && !s.SendWithoutReview {
			// The reminder queue lists overdue invoices only, so a reminder before the due date cannot wait for review.
			add("send_without_review", "A reminder before the due date is sent without review; turn on send without review")
		}
	case PlaybookStepInternal:
		switch {
		case s.Channel == PlaybookChannelEmail && s.To == "":
			add("to", "Add an address")
		case s.Channel == PlaybookChannelEmail && !isEmailAddress(s.To):
			add("to", "Enter a valid email address")
		case s.Channel == PlaybookChannelSlack && s.To == "":
			add("to", "Pick a Slack channel")
		}
		if s.Note == "" {
			add("note", "Write the alert")
		}
	case PlaybookStepTask:
		switch {
		case s.Assignee == nil || (s.Assignee.UserID == "" && s.Assignee.Email == ""):
			add("assignee", "Pick who gets the task")
		case s.Assignee.Email != "" && !isEmailAddress(s.Assignee.Email):
			add("assignee", "Enter a valid email address")
		}
		if s.Note == "" {
			add("note", "Say what to do")
		}
	}
	return out
}

// PlaybookStopCondition is an event that ends outreach for an invoice.
type PlaybookStopCondition string

const (
	// PlaybookStopInvoicePaid is always on: outreach ends once the invoice is paid, voided or settled.
	PlaybookStopInvoicePaid PlaybookStopCondition = "invoice_paid"
	// PlaybookStopManuallyPaused lets anyone pause one invoice's run.
	PlaybookStopManuallyPaused PlaybookStopCondition = "manually_paused"
	// PlaybookStopCustomerReplied and PlaybookStopDisputeFiled are stored for the product to show; nothing detects
	// replies or disputes yet, so they never fire.
	PlaybookStopCustomerReplied PlaybookStopCondition = "customer_replied"
	PlaybookStopDisputeFiled    PlaybookStopCondition = "dispute_filed"
)

var playbookStopConditions = []PlaybookStopCondition{PlaybookStopInvoicePaid, PlaybookStopManuallyPaused, PlaybookStopCustomerReplied, PlaybookStopDisputeFiled}

func (c PlaybookStopCondition) Validate() error {
	return validateEnum("stops", c, playbookStopConditions)
}

// NormalizePlaybookStops dedupes the stops, keeps invoice_paid first and the rest in a fixed order.
func NormalizePlaybookStops(stops []PlaybookStopCondition) []PlaybookStopCondition {
	set := lo.SliceToMap(stops, func(c PlaybookStopCondition) (PlaybookStopCondition, bool) { return c, true })
	set[PlaybookStopInvoicePaid] = true
	return lo.Filter(playbookStopConditions, func(c PlaybookStopCondition, _ int) bool { return set[c] })
}

// PlaybookRunState is where one invoice's run through a playbook stands.
type PlaybookRunState string

const (
	PlaybookRunActive    PlaybookRunState = "active"
	PlaybookRunPaused    PlaybookRunState = "paused"
	PlaybookRunStopped   PlaybookRunState = "stopped"
	PlaybookRunCompleted PlaybookRunState = "completed"
)

// LivePlaybookRunStates are the states of a run that still holds its invoice.
var LivePlaybookRunStates = []PlaybookRunState{PlaybookRunActive, PlaybookRunPaused}

func (s PlaybookRunState) Validate() error {
	return validateEnum("state", s, []PlaybookRunState{PlaybookRunActive, PlaybookRunPaused, PlaybookRunStopped, PlaybookRunCompleted})
}

// PlaybookStopReason says why a run stopped.
type PlaybookStopReason string

const (
	PlaybookStopReasonPaid            PlaybookStopReason = "paid"
	PlaybookStopReasonVoided          PlaybookStopReason = "voided"
	PlaybookStopReasonPlaybookDeleted PlaybookStopReason = "playbook_deleted"
)

// PlaybookStepRunState is where one scheduled step stands.
type PlaybookStepRunState string

const (
	PlaybookStepRunPending PlaybookStepRunState = "pending"
	PlaybookStepRunDone    PlaybookStepRunState = "done"
	PlaybookStepRunSkipped PlaybookStepRunState = "skipped"
	PlaybookStepRunFailed  PlaybookStepRunState = "failed"
)

// PlaybookStepSkipReason says why a step did not run.
type PlaybookStepSkipReason string

const (
	PlaybookSkipRunStopped        PlaybookStepSkipReason = "run_stopped"
	PlaybookSkipEditedOut         PlaybookStepSkipReason = "edited_out"
	PlaybookSkipSuperseded        PlaybookStepSkipReason = "superseded"
	PlaybookSkipReminderHeld      PlaybookStepSkipReason = "reminder_held"
	PlaybookSkipNoRecipient       PlaybookStepSkipReason = "no_recipient"
	PlaybookSkipSlackNotConnected PlaybookStepSkipReason = "slack_not_connected"
	PlaybookSkipEmailsUnavailable PlaybookStepSkipReason = "emails_unavailable"
)

// PlaybookStepRunMaxAttempts is how many times a failing step is tried before it is left failed.
const PlaybookStepRunMaxAttempts = 3

// CollectionTaskState is where a task created by a playbook stands.
type CollectionTaskState string

const (
	CollectionTaskOpen CollectionTaskState = "open"
	CollectionTaskDone CollectionTaskState = "done"
)

func (s CollectionTaskState) Validate() error {
	return validateEnum("state", s, []CollectionTaskState{CollectionTaskOpen, CollectionTaskDone})
}

// Playbook template variables, filled in when a step runs.
const (
	PlaybookVarCustomer    = "{{customer}}"
	PlaybookVarInvoice     = "{{invoice}}"
	PlaybookVarAmount      = "{{amount}}"
	PlaybookVarDueDate     = "{{due_date}}"
	PlaybookVarDaysOverdue = "{{days_overdue}}"
	PlaybookVarPayLink     = "{{pay_link}}"
)

// PlaybookVariables are every variable a step's text may use.
var PlaybookVariables = []string{PlaybookVarCustomer, PlaybookVarInvoice, PlaybookVarAmount, PlaybookVarDueDate, PlaybookVarDaysOverdue, PlaybookVarPayLink}

var (
	playbookVariablePattern = regexp.MustCompile(`\{\{\s*[^{}]*\s*\}\}`)
	playbookStepIDPattern   = regexp.MustCompile(`^[A-Za-z0-9_-]{1,50}$`)
	emailAddressPattern     = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)
)

// UnknownPlaybookVariables returns the {{...}} tokens in text that are not playbook variables.
func UnknownPlaybookVariables(text string) []string {
	return lo.Uniq(lo.Filter(playbookVariablePattern.FindAllString(text, -1), func(token string, _ int) bool {
		return !lo.Contains(PlaybookVariables, token)
	}))
}

func isEmailAddress(s string) bool {
	return emailAddressPattern.MatchString(s)
}

// PlaybookStepDueOn is the UTC day a step runs for an invoice due on dueDate.
func PlaybookStepDueOn(dueDate time.Time, day int) time.Time {
	return UTCDay(dueDate).AddDate(0, 0, day)
}

func playbookInvalid(field, hint string) error {
	return ierr.NewErrorf("invalid playbook: %s", field).
		WithHint(hint).
		WithReportableDetails(map[string]any{"field": field}).
		Mark(ierr.ErrValidation)
}
