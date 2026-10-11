package checkout

import (
	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
)

// FromEnt converts a stored checkout session.
func FromEnt(e *ent.CheckoutSession) *CheckoutSession {
	s := &CheckoutSession{
		ID:                    e.ID,
		EnvironmentID:         e.EnvironmentID,
		CustomerID:            e.CustomerID,
		Action:                e.Action,
		CheckoutStatus:        types.CheckoutStatus(e.CheckoutStatus),
		PaymentProvider:       e.PaymentProvider,
		CheckoutInvoiceID:     e.CheckoutInvoiceID,
		CheckoutPaymentID:     e.CheckoutPaymentID,
		Configuration:         JSONBCheckoutConfiguration(e.Configuration),
		PaymentProviderConfig: (*JSONBCheckoutPaymentProviderConfig)(e.PaymentProviderConfig),
		Result:                (*JSONBCheckoutResult)(e.Result),
		ProviderResult:        (*JSONBCheckoutProviderResult)(e.ProviderResult),
		IdempotencyKey:        e.IdempotencyKey,
		SuccessURL:            e.SuccessURL,
		FailureURL:            e.FailureURL,
		CancelURL:             e.CancelURL,
		CompletedAt:           e.CompletedAt,
		CancelledAt:           e.CancelledAt,
		FailureReason:         e.FailureReason,
		Metadata:              types.Metadata(e.Metadata),
		BaseModel: types.BaseModel{
			TenantID:  e.TenantID,
			Status:    types.Status(e.Status),
			CreatedAt: e.CreatedAt,
			UpdatedAt: e.UpdatedAt,
			CreatedBy: e.CreatedBy,
			UpdatedBy: e.UpdatedBy,
		},
	}
	if e.ExpiresAt != nil {
		s.ExpiresAt = *e.ExpiresAt
	}
	return s
}
