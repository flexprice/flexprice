package service

import (
	"github.com/samber/lo"
	"github.com/shopspring/decimal"

	"github.com/flexprice/flexprice/internal/types"
)

// playbookTemplate is a starter playbook. Its steps are copied into the playbook when it is created, so changing a
// template never changes an existing playbook.
type playbookTemplate struct {
	ID          string
	Name        string
	Description string
	Trigger     types.PlaybookTrigger
	Audience    types.PlaybookAudience
	Steps       []types.PlaybookStep
}

// playbookToneCopy is the default subject and body of a reminder email in each tone.
var playbookToneCopy = map[types.ReminderTone][2]string{
	types.ReminderToneFriendly: {
		"A quick reminder about {{invoice}}",
		"Hi {{customer}},\n\nJust a friendly note that invoice {{invoice}} for {{amount}} was due on {{due_date}}. If it is already on its way, thank you and please ignore this.\n\nYou can pay here: {{pay_link}}",
	},
	types.ReminderToneFirm: {
		"{{invoice}} is {{days_overdue}} days overdue",
		"Hello {{customer}},\n\nInvoice {{invoice}} for {{amount}} is now {{days_overdue}} days past its due date of {{due_date}}. Please arrange payment this week, or reply to let us know when to expect it.\n\nPay now: {{pay_link}}",
	},
	types.ReminderToneFinal: {
		"Final notice: {{invoice}}",
		"Hello {{customer}},\n\nThis is our final reminder for invoice {{invoice}} ({{amount}}), due {{due_date}}. If we do not receive payment or hear from you within 5 business days, we will pause your account as set out in our terms.\n\nPay now: {{pay_link}}",
	},
}

// beforeDueCopy is the text of a heads-up sent before the due date.
var beforeDueCopy = [2]string{
	"Invoice {{invoice}} is due on {{due_date}}",
	"Hi {{customer}},\n\nA quick heads-up that invoice {{invoice}} for {{amount}} is due on {{due_date}}. If payment is already scheduled, thank you.\n\nYou can pay here: {{pay_link}}",
}

func emailStep(id string, day int, tone types.ReminderTone, sendWithoutReview bool) types.PlaybookStep {
	text := playbookToneCopy[tone]
	return types.PlaybookStep{ID: id, Kind: types.PlaybookStepEmail, Day: day, Tone: tone, Subject: text[0], Body: text[1], SendWithoutReview: sendWithoutReview}
}

// playbookTemplates are the starter playbooks, in the order the console lists them.
var playbookTemplates = []playbookTemplate{
	{
		ID:          "standard",
		Name:        "Standard reminders",
		Description: "Friendly, then firm, then a final notice. A sound default for most customers.",
		Trigger:     types.PlaybookTriggerOverdue,
		Audience:    types.PlaybookAudience{Kind: types.PlaybookAudienceAll},
		Steps: []types.PlaybookStep{
			emailStep("friendly", 3, types.ReminderToneFriendly, false),
			emailStep("firm", 10, types.ReminderToneFirm, false),
			emailStep("final", 21, types.ReminderToneFinal, false),
		},
	},
	{
		ID:          "before-due",
		Name:        "Heads-up before due",
		Description: "A courtesy note three days before the due date, then one friendly follow-up.",
		Trigger:     types.PlaybookTriggerBeforeDue,
		Audience:    types.PlaybookAudience{Kind: types.PlaybookAudienceAll},
		Steps: []types.PlaybookStep{
			{ID: "heads-up", Kind: types.PlaybookStepEmail, Day: -3, Tone: types.ReminderToneFriendly, Subject: beforeDueCopy[0], Body: beforeDueCopy[1], SendWithoutReview: true},
			emailStep("follow-up", 2, types.ReminderToneFriendly, false),
		},
	},
	{
		ID:          "enterprise",
		Name:        "Enterprise, account-managed",
		Description: "Longer gaps, and the account owner hears before the customer gets anything firm.",
		Trigger:     types.PlaybookTriggerOverdue,
		Audience:    types.PlaybookAudience{Kind: types.PlaybookAudienceSegment, Key: types.PlaybookSegmentDefaultKey, Value: "enterprise"},
		Steps: []types.PlaybookStep{
			emailStep("friendly", 7, types.ReminderToneFriendly, false),
			{ID: "owner-alert", Kind: types.PlaybookStepInternal, Day: 14, Channel: types.PlaybookChannelEmail, Note: "{{customer}} has {{amount}} {{days_overdue}} days overdue on {{invoice}}. Check in before the firm reminder goes out."},
			emailStep("firm", 21, types.ReminderToneFirm, false),
			{ID: "owner-call", Kind: types.PlaybookStepTask, Day: 35, Note: "Call {{customer}} about {{invoice}}."},
		},
	},
	{
		ID:          "high-value",
		Name:        "High-value invoices",
		Description: "For large balances: every email is reviewed, and finance is alerted early.",
		Trigger:     types.PlaybookTriggerOverdue,
		Audience:    types.PlaybookAudience{Kind: types.PlaybookAudienceAmountOver, Amount: lo.ToPtr(decimal.NewFromInt(10000)), Currency: "usd"},
		Steps: []types.PlaybookStep{
			emailStep("friendly", 1, types.ReminderToneFriendly, false),
			{ID: "finance-alert", Kind: types.PlaybookStepInternal, Day: 3, Channel: types.PlaybookChannelEmail, Note: "{{invoice}} ({{amount}}) is overdue. A reminder is waiting for review."},
			emailStep("firm", 7, types.ReminderToneFirm, false),
			emailStep("final", 14, types.ReminderToneFinal, false),
		},
	},
	{
		ID:          "final-push",
		Name:        "Final push",
		Description: "For invoices already a month late: one firm email, a final notice, then a person takes over.",
		Trigger:     types.PlaybookTriggerOverdue,
		Audience:    types.PlaybookAudience{Kind: types.PlaybookAudienceDaysOverdueOver, Days: 30},
		Steps: []types.PlaybookStep{
			emailStep("firm", 30, types.ReminderToneFirm, false),
			emailStep("final", 40, types.ReminderToneFinal, false),
			{ID: "lead-decision", Kind: types.PlaybookStepTask, Day: 45, Note: "Decide whether to pause service for {{customer}}."},
		},
	},
}

// findPlaybookTemplate returns the template with the given id.
func findPlaybookTemplate(id string) (playbookTemplate, bool) {
	return lo.Find(playbookTemplates, func(t playbookTemplate) bool { return t.ID == id })
}

// copySteps returns the template's steps as new values, so a playbook never shares a slice with a template.
func (t playbookTemplate) copySteps() []types.PlaybookStep {
	return lo.Map(t.Steps, func(s types.PlaybookStep, _ int) types.PlaybookStep {
		if s.Assignee != nil {
			a := *s.Assignee
			s.Assignee = &a
		}
		return s
	})
}
