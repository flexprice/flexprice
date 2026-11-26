package render

import "strings"

// Reminder is the payment reminder email: the text frozen at approval, in the shared layout.
type Reminder struct {
	Merchant     string
	ContactEmail string
	// Body is the approved text; blank lines separate paragraphs.
	Body string
	// ViewURL opens the hosted invoice; empty means no button.
	ViewURL     string
	PDFAttached bool
	// PDFURL is the invoice's own PDF link, shown when nothing is attached.
	PDFURL string
}

// Paragraphs splits Body at blank lines into paragraphs of lines.
func (v Reminder) Paragraphs() [][]string {
	var paragraphs [][]string
	for _, block := range strings.Split(strings.ReplaceAll(strings.TrimSpace(v.Body), "\r\n", "\n"), "\n\n") {
		if block = strings.Trim(block, "\n"); block != "" {
			paragraphs = append(paragraphs, strings.Split(block, "\n"))
		}
	}
	return paragraphs
}

var reminderPage = newPage("invoice_payment_reminder")

// PaymentReminder renders the email sent for an approved payment reminder; subject is the approved subject.
func PaymentReminder(subject string, v Reminder) (Content, error) {
	return reminderPage.render(subject, v)
}
