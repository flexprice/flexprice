package service

import (
	"bytes"
	"fmt"
	"strings"
	"text/template" // nosemgrep: go.lang.security.audit.xss.import-text-template.import-text-template
	"time"

	"github.com/shopspring/decimal"

	"github.com/flexprice/flexprice/internal/types"
)

// The reminder is plain text, escaped into the HTML layout when it is emailed; html/template would store '&amp;' and '&#39;' in it.
const (
	reminderFriendlyMaxDays = 30
	reminderFirmMaxDays     = 60
)

type reminderTextInput struct {
	Greeting    string
	Tenant      string
	Number      string
	Amount      string
	Due         string
	DaysOverdue int
}

var reminderFuncs = template.FuncMap{"overdue": overdueLabel}

var reminderTemplates = map[types.ReminderTone]*template.Template{
	types.ReminderToneFriendly: template.Must(template.New("friendly").Funcs(reminderFuncs).Parse(`Hi {{.Greeting}},

A quick note that invoice {{.Number}} is past due:

  - {{.Number}}: {{.Amount}} (due {{.Due}}, {{overdue .DaysOverdue}})

Could you reply with the date we can expect payment, or pay by bank transfer using the details on the invoice? If you have already paid, please ignore this note and let us know the payment date.

Thanks,
{{.Tenant}} Billing
`)),
	types.ReminderToneFirm: template.Must(template.New("firm").Funcs(reminderFuncs).Parse(`Hi {{.Greeting}},

Our records show invoice {{.Number}} is still unpaid after the due date:

  - {{.Number}}: {{.Amount}} (due {{.Due}}, {{overdue .DaysOverdue}})

Please reply with the date we will receive payment, or pay by bank transfer using the details on the invoice, so we can keep your account in good standing. If something is wrong with the invoice, tell us and we will sort it out.

Thanks,
{{.Tenant}} Billing
`)),
	types.ReminderToneFinal: template.Must(template.New("final").Funcs(reminderFuncs).Parse(`Hi {{.Greeting}},

This is a final notice. Invoice {{.Number}} has been overdue for a long time and we have not received payment:

  - {{.Number}}: {{.Amount}} (due {{.Due}}, {{overdue .DaysOverdue}})

Please pay by bank transfer using the details on the invoice, or reply today with a confirmed payment date. If we do not hear from you, we may have to pause service or take further collection steps.

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

// reminderInvoice is what a reminder says about its invoice.
type reminderInvoice struct {
	number    string
	currency  string
	remaining decimal.Decimal
	due       time.Time
}

// renderReminder writes the subject and body of the reminder of one overdue invoice in a tone.
func renderReminder(tone types.ReminderTone, greeting, tenant string, inv reminderInvoice, now time.Time) (subject, body string, err error) {
	amount := reminderMoney(inv.remaining, inv.currency)
	input := reminderTextInput{
		Greeting: greeting, Tenant: tenant, Number: inv.number, Amount: amount,
		Due: inv.due.UTC().Format("Jan 2"), DaysOverdue: calendarDaysBetween(inv.due, now),
	}
	var buf bytes.Buffer
	if err := reminderTemplates[tone].Execute(&buf, input); err != nil {
		return "", "", err
	}
	return reminderSubject(tone, inv.number, amount), buf.String(), nil
}

func reminderSubject(tone types.ReminderTone, number, amount string) string {
	base := fmt.Sprintf("Invoice %s is past due: %s", number, amount)
	switch tone {
	case types.ReminderToneFirm:
		return "Reminder: " + base
	case types.ReminderToneFinal:
		return "Final notice: " + base
	}
	return base
}
