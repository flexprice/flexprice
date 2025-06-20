package creditgrantapplication

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type CreditGrantApplication struct {
	ID                              string                             `json:"id,omitempty" db:"id"`
	EnvironmentID                   string                             `json:"environment_id,omitempty" db:"environment_id"`
	CreditGrantID                   string                             `json:"credit_grant_id,omitempty" db:"credit_grant_id"`
	SubscriptionID                  string                             `json:"subscription_id,omitempty" db:"subscription_id"`
	ScheduledFor                    time.Time                          `json:"scheduled_for,omitempty" db:"scheduled_for"`
	AppliedAt                       *time.Time                         `json:"applied_at,omitempty" db:"applied_at"`
	PeriodStart                     *time.Time                         `json:"period_start,omitempty" db:"period_start"`
	PeriodEnd                       *time.Time                         `json:"period_end,omitempty" db:"period_end"`
	ApplicationStatus               types.ApplicationStatus            `json:"application_status,omitempty" db:"application_status"`
	CreditsApplied                  decimal.Decimal                    `json:"credits_applied,omitempty" db:"credits_applied"`
	ApplicationReason               types.CreditGrantApplicationReason `json:"application_reason,omitempty" db:"application_reason"`
	SubscriptionStatusAtApplication types.SubscriptionStatus           `json:"subscription_status_at_application,omitempty" db:"subscription_status_at_application"`
	RetryCount                      int                                `json:"retry_count,omitempty" db:"retry_count"`
	FailureReason                   *string                            `json:"failure_reason,omitempty" db:"failure_reason"`
	Metadata                        types.Metadata                     `json:"metadata,omitempty" db:"metadata"`
	IdempotencyKey                  string                             `json:"idempotency_key,omitempty" db:"idempotency_key"`

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
		ApplicationStatus:               e.ApplicationStatus,
		CreditsApplied:                  e.CreditsApplied,
		ApplicationReason:               e.ApplicationReason,
		SubscriptionStatusAtApplication: e.SubscriptionStatusAtApplication,
		RetryCount:                      e.RetryCount,
		FailureReason:                   e.FailureReason,
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
