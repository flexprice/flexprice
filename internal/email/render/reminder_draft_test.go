package render

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

func TestPaymentReminderDraft_WritesEachTone(t *testing.T) {
	d := ReminderDraft{Greeting: "Acme", Tenant: "Flex", Number: "INV-9", Amount: "$4,200", Due: "Nov 8", Overdue: "10 days overdue"}

	tests := []struct {
		tone    types.ReminderTone
		subject string
		opening string
	}{
		{types.ReminderToneFriendly, "Invoice INV-9 is past due: $4,200", "A quick note that invoice INV-9 is past due:"},
		{types.ReminderToneFirm, "Reminder: Invoice INV-9 is past due: $4,200", "Our records show invoice INV-9"},
		{types.ReminderToneFinal, "Final notice: Invoice INV-9 is past due: $4,200", "This is a final notice."},
	}
	for _, tt := range tests {
		subject, body, err := PaymentReminderDraft(tt.tone, d)
		require.NoError(t, err, tt.tone)
		require.Equal(t, tt.subject, subject, tt.tone)
		require.True(t, strings.HasPrefix(body, "Hi Acme,\n\n"), tt.tone)
		require.Contains(t, body, tt.opening, tt.tone)
		require.Contains(t, body, "  - INV-9: $4,200 (due Nov 8, 10 days overdue)\n", tt.tone)
		require.True(t, strings.HasSuffix(body, "Thanks,\nFlex Billing\n"), tt.tone)
	}
}

func TestPaymentReminderDraft_UnknownToneFails(t *testing.T) {
	_, _, err := PaymentReminderDraft("shouty", ReminderDraft{})
	require.True(t, ierr.IsValidation(err), "got %v", err)
}

// The draft keeps typed text as is, and only the HTML email escapes it.
func TestPaymentReminderDraft_StaysPlainTextAndIsEscapedInTheHTMLEmail(t *testing.T) {
	d := ReminderDraft{Greeting: "O'Brien & <script>alert(1)</script>", Tenant: "Smith & Co", Number: "INV-1", Amount: "$100", Due: "Nov 1", Overdue: "1 day overdue"}
	subject, body, err := PaymentReminderDraft(types.ReminderToneFriendly, d)
	require.NoError(t, err)
	require.Contains(t, body, "Hi O'Brien & <script>alert(1)</script>,", "the stored draft holds no HTML entities")
	require.Contains(t, body, "Smith & Co Billing")

	email, err := PaymentReminder(subject, Reminder{Merchant: "Smith & Co", Body: body})
	require.NoError(t, err)
	require.NotContains(t, email.HTML, "<script>")
	require.Contains(t, email.HTML, "&lt;script&gt;alert(1)&lt;/script&gt;")
	require.Contains(t, email.HTML, "O&#39;Brien &amp; ")
	require.Contains(t, email.Text, "Hi O'Brien & <script>alert(1)</script>,", "the plain-text part is not escaped")
}
