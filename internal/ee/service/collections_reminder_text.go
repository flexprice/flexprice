package service

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/shopspring/decimal"

	"github.com/flexprice/flexprice/internal/types"
)

const (
	reminderFriendlyMaxDays = 30
	reminderFirmMaxDays     = 60
)

type reminderLine struct {
	Number      string
	Amount      string
	Due         string
	DaysOverdue int
}

type reminderTextInput struct {
	Greeting string
	Tenant   string
	Count    int
	Total    string
	Lines    []reminderLine
}

var reminderFuncs = template.FuncMap{"overdue": overdueLabel}

var reminderTemplates = map[types.ReminderTone]*template.Template{
	types.ReminderToneFriendly: template.Must(template.New("friendly").Funcs(reminderFuncs).Parse(`Hi {{.Greeting}},

A quick note that {{if eq .Count 1}}the invoice below is{{else}}the invoices below are{{end}} past due:

{{range .Lines}}  - {{.Number}}: {{.Amount}} (due {{.Due}}, {{overdue .DaysOverdue}})
{{end}}
Total outstanding: {{.Total}}

Could you reply with the date we can expect payment, or pay by bank transfer using the details on the {{if eq .Count 1}}invoice{{else}}invoices{{end}}? If you have already paid, please ignore this note and let us know the payment date.

Thanks,
{{.Tenant}} Billing
`)),
	types.ReminderToneFirm: template.Must(template.New("firm").Funcs(reminderFuncs).Parse(`Hi {{.Greeting}},

Our records show {{if eq .Count 1}}this invoice is{{else}}these invoices are{{end}} still unpaid after the due date:

{{range .Lines}}  - {{.Number}}: {{.Amount}} (due {{.Due}}, {{overdue .DaysOverdue}})
{{end}}
Total outstanding: {{.Total}}

Please reply with the date we will receive payment, or pay by bank transfer using the details on the {{if eq .Count 1}}invoice{{else}}invoices{{end}}, so we can keep your account in good standing. If something is wrong with {{if eq .Count 1}}the invoice{{else}}any of them{{end}}, tell us and we will sort it out.

Thanks,
{{.Tenant}} Billing
`)),
	types.ReminderToneFinal: template.Must(template.New("final").Funcs(reminderFuncs).Parse(`Hi {{.Greeting}},

This is a final notice. {{if eq .Count 1}}The invoice below has{{else}}The invoices below have{{end}} been overdue for a long time and we have not received payment:

{{range .Lines}}  - {{.Number}}: {{.Amount}} (due {{.Due}}, {{overdue .DaysOverdue}})
{{end}}
Total outstanding: {{.Total}}

Please pay by bank transfer using the details on the {{if eq .Count 1}}invoice{{else}}invoices{{end}}, or reply today with a confirmed payment date. If we do not hear from you, we may have to pause service or take further collection steps.

Thanks,
{{.Tenant}} Billing
`)),
}

func overdueLabel(days int) string {
	switch days {
	case 0:
		return "overdue today"
	case 1:
		return "1 day overdue"
	}
	return fmt.Sprintf("%d days overdue", days)
}

// reminderToneForDays maps the oldest overdue age to a tone: friendly up to 30 days, firm up to 60, then final.
func reminderToneForDays(days int) types.ReminderTone {
	switch {
	case days <= reminderFriendlyMaxDays:
		return types.ReminderToneFriendly
	case days <= reminderFirmMaxDays:
		return types.ReminderToneFirm
	}
	return types.ReminderToneFinal
}

func reminderToneLabel(tone types.ReminderTone) string {
	if tone == types.ReminderToneFinal {
		return "final notice"
	}
	return string(tone)
}

// reminderMoney is formatMoney without a zero fraction: $37,000 rather than $37,000.00.
func reminderMoney(amount decimal.Decimal, currency string) string {
	text := formatMoney(amount, currency)
	if amount.Equal(amount.Truncate(0)) {
		if i := strings.LastIndex(text, "."); i >= 0 {
			text = text[:i]
		}
	}
	return text
}

// renderReminder writes the subject and body of a reminder for a group of overdue receivables.
func renderReminder(tone types.ReminderTone, greeting, tenant string, g overdueGroup, now time.Time) (subject, body string, err error) {
	lines := make([]reminderLine, len(g.rows))
	for i, r := range g.rows {
		lines[i] = reminderLine{
			Number:      r.Number,
			Amount:      reminderMoney(r.AmountRemaining, g.currency),
			Due:         r.DueDate.UTC().Format("Jan 2"),
			DaysOverdue: calendarDaysBetween(*r.DueDate, now),
		}
	}
	total := reminderMoney(g.amount(), g.currency)
	input := reminderTextInput{Greeting: greeting, Tenant: tenant, Count: len(g.rows), Total: total, Lines: lines}

	var buf bytes.Buffer
	if err := reminderTemplates[tone].Execute(&buf, input); err != nil {
		return "", "", err
	}
	return reminderSubject(tone, g, total), buf.String(), nil
}

func reminderSubject(tone types.ReminderTone, g overdueGroup, total string) string {
	base := fmt.Sprintf("%d invoices past due: %s", len(g.rows), total)
	if len(g.rows) == 1 {
		base = fmt.Sprintf("Invoice %s is past due: %s", g.rows[0].Number, total)
	}
	switch tone {
	case types.ReminderToneFirm:
		return "Reminder: " + base
	case types.ReminderToneFinal:
		return "Final notice: " + base
	}
	return base
}
