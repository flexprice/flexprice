package webhookDto

type InternalCreditNoteEvent struct {
	CreditNoteID string `json:"credit_note_id"`
	TenantID     string `json:"tenant_id"`
}
