package creditgrantapplication

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type CreditGrantApplication struct {
	ID             string `db:"id" json:"id"`
	CreditGrantID  string `db:"credit_grant_id" json:"credit_grant_id"`
	SubscriptionID string `db:"subscription_id" json:"subscription_id"`

	// Timing
	ScheduledFor time.Time  `db:"scheduled_for" json:"scheduled_for"`
	AppliedAt    *time.Time `db:"applied_at" json:"applied_at,omitempty"`

	// Billing period context
	PeriodStart time.Time `db:"period_start" json:"period_start"`
	PeriodEnd   time.Time `db:"period_end" json:"period_end"`

	// Application details
	ApplicationStatus types.ApplicationStatus `db:"application_status" json:"application_status"`
	CreditsApplied    decimal.Decimal         `db:"credits_applied" json:"credits_applied"`
	Currency          string                  `db:"currency" json:"currency"`

	// Context
	ApplicationReason               string `db:"application_reason" json:"application_reason"`
	SubscriptionStatusAtApplication string `db:"subscription_status_at_application" json:"subscription_status_at_application"`

	// Prorating
	IsProrated       bool             `db:"is_prorated" json:"is_prorated"`
	ProrationFactor  *decimal.Decimal `db:"proration_factor" json:"proration_factor,omitempty"`
	FullPeriodAmount *decimal.Decimal `db:"full_period_amount" json:"full_period_amount,omitempty"`

	// Retry handling
	RetryCount    int        `db:"retry_count" json:"retry_count"`
	FailureReason *string    `db:"failure_reason" json:"failure_reason,omitempty"`
	NextRetryAt   *time.Time `db:"next_retry_at" json:"next_retry_at,omitempty"`

	Metadata types.Metadata `db:"metadata" json:"metadata,omitempty"`

	// EnvironmentID is the environment identifier for the credit grant application
	EnvironmentID  string `db:"environment_id" json:"environment_id"`
	IdempotencyKey string `db:"idempotency_key" json:"idempotency_key"`

	types.BaseModel
}

// FromEnt converts an ent.CreditGrantApplication to a CreditGrantApplication
func FromEnt(e *ent.CreditGrantApplication) *CreditGrantApplication {
	return &CreditGrantApplication{
		ID:                              e.ID,
		CreditGrantID:                   e.CreditGrantID,
		SubscriptionID:                  e.SubscriptionID,
		ScheduledFor:                    e.ScheduledFor,
		AppliedAt:                       e.AppliedAt,
		PeriodStart:                     e.PeriodStart,
		PeriodEnd:                       e.PeriodEnd,
		ApplicationStatus:               types.ApplicationStatus(e.ApplicationStatus),
		CreditsApplied:                  e.CreditsApplied,
		Currency:                        e.Currency,
		ApplicationReason:               e.ApplicationReason,
		SubscriptionStatusAtApplication: e.SubscriptionStatusAtApplication,
		IsProrated:                      e.IsProrated,
		ProrationFactor:                 lo.ToPtr(e.ProrationFactor),
		FullPeriodAmount:                lo.ToPtr(e.FullPeriodAmount),
		RetryCount:                      e.RetryCount,
		FailureReason:                   e.FailureReason,
		NextRetryAt:                     e.NextRetryAt,
		Metadata:                        e.Metadata,
		EnvironmentID:                   e.EnvironmentID,
		BaseModel: types.BaseModel{
			CreatedAt: e.CreatedAt,
			UpdatedAt: e.UpdatedAt,
			CreatedBy: e.CreatedBy,
			UpdatedBy: e.UpdatedBy,
			TenantID:  e.TenantID,
			Status:    types.Status(e.Status),
		},
	}
}

// FromEntList converts a list of ent.CreditGrantApplication to a list of CreditGrantApplication
func FromEntList(e []*ent.CreditGrantApplication) []*CreditGrantApplication {
	return lo.Map(e, func(item *ent.CreditGrantApplication, _ int) *CreditGrantApplication {
		return FromEnt(item)
	})
}
