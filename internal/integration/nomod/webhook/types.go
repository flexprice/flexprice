package webhook

// NomodWebhookPayload represents incoming webhook from Nomod
type NomodWebhookPayload struct {
	ID            string  `json:"id"`                        // Charge ID (required)
	InvoiceID     *string `json:"invoice_id,omitempty"`      // Present if invoice payment
	PaymentLinkID *string `json:"payment_link_id,omitempty"` // Present if payment link payment
}
