package creditgrantapplication

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type CreditGrantApplication struct {
	ID                              string                             `json:"id,omitempty"`
	EnvironmentID                   string                             `json:"environment_id,omitempty"`
	CreditGrantID                   string                             `json:"credit_grant_id,omitempty"`
	SubscriptionID                  string                             `json:"subscription_id,omitempty"`
	ScheduledFor                    time.Time                          `json:"scheduled_for,omitempty"`
	AppliedAt                       *time.Time                         `json:"applied_at,omitempty"`
	PeriodStart                     time.Time                          `json:"period_start,omitempty"`
	PeriodEnd                       *time.Time                         `json:"period_end,omitempty"`
	ApplicationStatus               types.ApplicationStatus            `json:"application_status,omitempty"`
	Credits                         decimal.Decimal                    `json:"credits,omitempty" swaggertype:"string"`
	ApplicationReason               types.CreditGrantApplicationReason `json:"application_reason,omitempty"`
	SubscriptionStatusAtApplication types.SubscriptionStatus           `json:"subscription_status_at_application,omitempty"`
	RetryCount                      int                                `json:"retry_count,omitempty"`
	FailureReason                   *string                            `json:"failure_reason,omitempty"`
	Metadata                        types.Metadata                     `json:"metadata,omitempty"`
	IdempotencyKey                  string                             `json:"idempotency_key,omitempty"`

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
		Credits:                         e.Credits,
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
