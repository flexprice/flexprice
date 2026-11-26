package render

import (
	"bytes"
	// The draft is plain text: it is stored and edited as typed, and html/template escapes it when it goes into the
	// HTML email. html/template here would store entities such as &amp; in the text.
	texttemplate "text/template" // nosemgrep: go.lang.security.audit.xss.import-text-template.import-text-template

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// ReminderDraft is what the drafted text of a payment reminder says. Every field is already formatted and on one line.
type ReminderDraft struct {
	Greeting string
	Tenant   string
	Number   string
	Amount   string
	Due      string
	// Overdue is the age of the invoice, such as "10 days overdue".
	Overdue string
}

var draftTemplates = map[types.ReminderTone]*texttemplate.Template{
	types.ReminderToneFriendly: parseDraft("friendly"),
	types.ReminderToneFirm:     parseDraft("firm"),
	types.ReminderToneFinal:    parseDraft("final"),
}

func parseDraft(tone string) *texttemplate.Template {
	return texttemplate.Must(texttemplate.ParseFS(files, "templates/reminder_draft_"+tone+".txt"))
}

// PaymentReminderDraft writes the subject and body a payment reminder starts with in a tone.
func PaymentReminderDraft(tone types.ReminderTone, d ReminderDraft) (subject, body string, err error) {
	t, ok := draftTemplates[tone]
	if !ok {
		return "", "", ierr.NewErrorf("unknown reminder tone %q", tone).
			WithHint("Failed to write the reminder").
			Mark(ierr.ErrValidation)
	}

	var subjectBuf, bodyBuf bytes.Buffer
	if err := t.ExecuteTemplate(&subjectBuf, "subject", d); err != nil {
		return "", "", ierr.WithError(err).WithHint("Failed to write the reminder").Mark(ierr.ErrSystem)
	}
	if err := t.ExecuteTemplate(&bodyBuf, "body", d); err != nil {
		return "", "", ierr.WithError(err).WithHint("Failed to write the reminder").Mark(ierr.ErrSystem)
	}

	return subjectBuf.String(), bodyBuf.String(), nil
}
