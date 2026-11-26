package service

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/shopspring/decimal"

	"github.com/flexprice/flexprice/internal/email/render"
	"github.com/flexprice/flexprice/internal/types"
)

const (
	reminderFriendlyMaxDays = 30
	reminderFirmMaxDays     = 60
)

// reminderInvoice is what a reminder says about its invoice.
type reminderInvoice struct {
	number    string
	currency  string
	remaining decimal.Decimal
	due       time.Time
}

// renderReminder writes the subject and body of the reminder of one overdue invoice in a tone.
func renderReminder(tone types.ReminderTone, greeting, tenant string, inv reminderInvoice, now time.Time) (subject, body string, err error) {
	return render.PaymentReminderDraft(tone, render.ReminderDraft{
		Greeting: singleLine(greeting),
		Tenant:   singleLine(tenant),
		Number:   singleLine(inv.number),
		Amount:   reminderMoney(inv.remaining, inv.currency),
		Due:      inv.due.UTC().Format("Jan 2"),
		Overdue:  overdueLabel(types.CalendarDaysBetween(inv.due, now)),
	})
}

// singleLine turns control characters (line breaks, tabs, NUL) in a value typed by a person into spaces and squeezes
// the whitespace, so a customer name, tenant name, invoice number or subject cannot add lines or header text.
func singleLine(value string) string {
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}

		return r
	}, value)

	return strings.Join(strings.Fields(cleaned), " ")
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
