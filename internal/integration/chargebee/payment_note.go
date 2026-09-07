package chargebee

import "strings"

// paymentNoteTag prefixes the Flexprice payment id stamped on every Chargebee
// invoice we create, so a webhook can name the payment it settled. The id is
// visible to the customer on the invoice PDF.
const paymentNoteTag = "Flexprice payment: "

func PaymentNote(flexPaymentID string) string {
	if flexPaymentID == "" {
		return ""
	}
	return paymentNoteTag + flexPaymentID
}

// ParsePaymentIDFromNotes finds our payment id among an invoice's notes. Chargebee
// merges the general notes of the plan, addon, coupon, subscription, customer and
// site into one array and returns them without an entity_type, so ours is matched
// by tag and every other entry has to be tolerated. First match wins.
func ParsePaymentIDFromNotes(notes []string) string {
	for _, n := range notes {
		if strings.HasPrefix(n, paymentNoteTag) {
			return strings.TrimSpace(strings.TrimPrefix(n, paymentNoteTag))
		}
	}
	return ""
}
