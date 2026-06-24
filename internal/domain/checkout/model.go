package checkout

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// CheckoutSession represents a checkout session domain model.
type CheckoutSession struct {
	ID             string                          `db:"id" json:"id"`
	TenantID       string                          `db:"tenant_id" json:"tenant_id"`
	EnvironmentID  string                          `db:"environment_id" json:"environment_id"`
	CustomerID     string                          `db:"customer_id" json:"customer_id"`
	Action         types.CheckoutAction            `db:"action" json:"action"`
	CheckoutStatus types.CheckoutStatus            `db:"checkout_status" json:"checkout_status"`
	PaymentProvider *types.CheckoutPaymentProvider `db:"payment_provider" json:"payment_provider,omitempty"`

	CheckoutInvoiceID *string `db:"checkout_invoice_id" json:"checkout_invoice_id,omitempty"`
	CheckoutPaymentID *string `db:"checkout_payment_id" json:"checkout_payment_id,omitempty"`

	Configuration  types.CheckoutConfiguration  `db:"configuration" json:"configuration"`
	Result         *types.CheckoutResult        `db:"result" json:"result,omitempty"`
	ProviderResult *types.CheckoutProviderResult `db:"provider_result" json:"provider_result,omitempty"`

	IdempotencyKey *string `db:"idempotency_key" json:"idempotency_key,omitempty"`
	SuccessURL     *string `db:"success_url" json:"success_url,omitempty"`
	FailureURL     *string `db:"failure_url" json:"failure_url,omitempty"`
	CancelURL      *string `db:"cancel_url" json:"cancel_url,omitempty"`

	ExpiresAt   time.Time  `db:"expires_at" json:"expires_at"`
	CompletedAt *time.Time `db:"completed_at" json:"completed_at,omitempty"`
	CancelledAt *time.Time `db:"cancelled_at" json:"cancelled_at,omitempty"`

	FailureReason *string           `db:"failure_reason" json:"failure_reason,omitempty"`
	Metadata      map[string]string `db:"metadata" json:"metadata,omitempty"`

	Status    types.Status `db:"status" json:"status"`
	CreatedAt time.Time    `db:"created_at" json:"created_at"`
	UpdatedAt time.Time    `db:"updated_at" json:"updated_at"`
	CreatedBy string       `db:"created_by" json:"created_by,omitempty"`
	UpdatedBy string       `db:"updated_by" json:"updated_by,omitempty"`
}

// Validate checks that the CheckoutSession has all required fields and valid enum values.
func (s *CheckoutSession) Validate() error {
	if s.CustomerID == "" {
		return ierr.NewError("customer_id is required").
			WithHint("Provide a valid customer_id").
			Mark(ierr.ErrValidation)
	}
	if err := s.Action.Validate(); err != nil {
		return err
	}
	if err := s.CheckoutStatus.Validate(); err != nil {
		return err
	}
	if s.PaymentProvider != nil {
		if err := s.PaymentProvider.Validate(); err != nil {
			return err
		}
	}
	if s.ExpiresAt.IsZero() {
		return ierr.NewError("expires_at is required").
			WithHint("Provide a future expiry timestamp").
			Mark(ierr.ErrValidation)
	}
	return nil
}
