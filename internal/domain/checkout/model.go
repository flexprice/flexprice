package checkout

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
)

// CheckoutSession represents a checkout session domain model.
type CheckoutSession struct {
	ID                string                         `db:"id" json:"id"`
	TenantID          string                         `db:"tenant_id" json:"tenant_id"`
	EnvironmentID     string                         `db:"environment_id" json:"environment_id"`
	CustomerID        string                         `db:"customer_id" json:"customer_id"`
	Action            types.CheckoutAction           `db:"action" json:"action"`
	CheckoutStatus    types.CheckoutStatus           `db:"checkout_status" json:"checkout_status"`
	PaymentProvider   *types.CheckoutPaymentProvider `db:"payment_provider" json:"payment_provider,omitempty"`
	CheckoutInvoiceID *string                        `db:"checkout_invoice_id" json:"checkout_invoice_id,omitempty"`
	CheckoutPaymentID *string                        `db:"checkout_payment_id" json:"checkout_payment_id,omitempty"`
	Configuration     types.CheckoutConfiguration    `db:"configuration" json:"configuration"`
	Result            *types.CheckoutResult          `db:"result" json:"result,omitempty"`
	ProviderResult    *types.CheckoutProviderResult  `db:"provider_result" json:"provider_result,omitempty"`
	IdempotencyKey    *string                        `db:"idempotency_key" json:"idempotency_key,omitempty"`
	SuccessURL        *string                        `db:"success_url" json:"success_url,omitempty"`
	FailureURL        *string                        `db:"failure_url" json:"failure_url,omitempty"`
	CancelURL         *string                        `db:"cancel_url" json:"cancel_url,omitempty"`
	ExpiresAt         *time.Time                     `db:"expires_at" json:"expires_at,omitempty"`
	CompletedAt       *time.Time                     `db:"completed_at" json:"completed_at,omitempty"`
	CancelledAt       *time.Time                     `db:"cancelled_at" json:"cancelled_at,omitempty"`
	FailureReason     *string                        `db:"failure_reason" json:"failure_reason,omitempty"`
	Metadata          map[string]string              `db:"metadata" json:"metadata,omitempty"`

	types.BaseModel
}

func FromEnt(ent *ent.CheckoutSession) *CheckoutSession {
	return &CheckoutSession{
		ID:                ent.ID,
		TenantID:          ent.TenantID,
		EnvironmentID:     ent.EnvironmentID,
		CustomerID:        ent.CustomerID,
		Action:            ent.Action,
		CheckoutStatus:    types.CheckoutStatus(ent.CheckoutStatus),
		PaymentProvider:   ent.PaymentProvider,
		CheckoutInvoiceID: ent.CheckoutInvoiceID,
		CheckoutPaymentID: ent.CheckoutPaymentID,
		Configuration:     ent.Configuration,
		Result:            ent.Result,
		ProviderResult:    ent.ProviderResult,
		IdempotencyKey:    ent.IdempotencyKey,
		SuccessURL:        ent.SuccessURL,
		FailureURL:        ent.FailureURL,
		CancelURL:         ent.CancelURL,
		ExpiresAt:         ent.ExpiresAt,
		CompletedAt:       ent.CompletedAt,
		CancelledAt:       ent.CancelledAt,
		FailureReason:     ent.FailureReason,
		Metadata:          types.Metadata(ent.Metadata),
		BaseModel: types.BaseModel{
			TenantID:  ent.TenantID,
			Status:    types.Status(ent.Status),
			CreatedAt: ent.CreatedAt,
			UpdatedAt: ent.UpdatedAt,
			CreatedBy: ent.CreatedBy,
			UpdatedBy: ent.UpdatedBy,
		},
	}
}
