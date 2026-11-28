package render

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPaymentReminder_RendersTheFrozenTextInTheLayout(t *testing.T) {
	body := "Hi Acme,\n\nInvoice INV-1 is <b>past due</b>.\n  - INV-1: $100\n\nThanks,\nFlex Billing\n"
	got, err := PaymentReminder("Reminder: Invoice INV-1 is past due", Reminder{
		Merchant: "Flex", ContactEmail: "billing@flex.test", Body: body, PDFAttached: true,
	})
	require.NoError(t, err)
	require.Equal(t, "Reminder: Invoice INV-1 is past due", got.Subject)
	require.Contains(t, got.Text, body)
	require.Contains(t, got.Text, "The invoice PDF is attached.")
	require.Contains(t, got.Text, "Contact Flex at billing@flex.test")
	require.Contains(t, got.HTML, "<p style=\"margin:0 0 14px;\">Hi Acme,</p>")
	require.Contains(t, got.HTML, "&lt;b&gt;past due&lt;/b&gt;", "tenant text is escaped")
	require.Contains(t, got.HTML, "past due&lt;/b&gt;.<br>  - INV-1: $100")
	require.NotContains(t, got.HTML, "View invoice")

	linked, err := PaymentReminder("s", Reminder{Merchant: "Flex", Body: "x", ViewURL: "https://pay.test/inv"})
	require.NoError(t, err)
	require.Contains(t, linked.HTML, "View invoice")
	require.Contains(t, linked.Text, "View invoice: https://pay.test/inv")
}

func TestReminder_Paragraphs(t *testing.T) {
	require.Equal(t, [][]string{{"a", "b"}, {"c"}}, Reminder{Body: "a\nb\n\nc\n"}.Paragraphs())
	require.Empty(t, Reminder{Body: "  \n"}.Paragraphs())
}
