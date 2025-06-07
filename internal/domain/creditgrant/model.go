package creditgrant

import (
	"github.com/flexprice/flexprice/ent"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// CreditGrant represents a credit allocation for a customer
type CreditGrant struct {
	ID                     string                               `json:"id"`
	Name                   string                               `json:"name"`
	Scope                  types.CreditGrantScope               `json:"scope"`
	PlanID                 *string                              `json:"plan_id,omitempty"`
	SubscriptionID         *string                              `json:"subscription_id,omitempty"`
	Amount                 decimal.Decimal                      `json:"amount"`
	Currency               string                               `json:"currency"`
	Cadence                types.CreditGrantCadence             `json:"cadence"`
	Period                 *types.CreditGrantPeriod             `json:"period,omitempty"`
	PeriodCount            *int                                 `json:"period_count,omitempty"`
	ExpirationType         types.CreditGrantExpiryType          `json:"expiration_type,omitempty"`
	ExpirationDurationUnit *types.CreditGrantExpiryDurationUnit `json:"expiration_duration_unit,omitempty"`
	ExpirationDuration     *int                                 `json:"expiration_duration,omitempty"`
	Priority               *int                                 `json:"priority,omitempty"`
	Metadata               types.Metadata                       `json:"metadata,omitempty"`
	EnvironmentID          string                               `json:"environment_id"`
	types.BaseModel
}

// Validate performs validation on the credit grant
func (c *CreditGrant) Validate() error {
	if c.Name == "" {
		return ierr.NewError("name is required").
			WithHint("Please provide a name for the credit grant").
			Mark(ierr.ErrValidation)
	}

	if c.Scope == "" {
		return ierr.NewError("scope is required").
			WithHint("Please specify the scope (PLAN or SUBSCRIPTION)").
			Mark(ierr.ErrValidation)
	}

	// Validate based on scope
	switch c.Scope {
	case types.CreditGrantScopePlan:
		if c.PlanID == nil || *c.PlanID == "" {
			return ierr.NewError("plan_id is required for PLAN-scoped grants").
				WithHint("Please provide a valid plan ID").
				WithReportableDetails(map[string]interface{}{
					"scope": c.Scope,
				}).
				Mark(ierr.ErrValidation)
		}
	case types.CreditGrantScopeSubscription:
		if c.SubscriptionID == nil || *c.SubscriptionID == "" {
			return ierr.NewError("subscription_id is required for SUBSCRIPTION-scoped grants").
				WithHint("Please provide a valid subscription ID").
				WithReportableDetails(map[string]interface{}{
					"scope": c.Scope,
				}).
				Mark(ierr.ErrValidation)
		}
		if c.PlanID == nil || *c.PlanID == "" {
			return ierr.NewError("plan_id is required for SUBSCRIPTION-scoped grants").
				WithHint("Please provide a valid plan ID").
				WithReportableDetails(map[string]interface{}{
					"scope": c.Scope,
				}).
				Mark(ierr.ErrValidation)
		}
	default:
		return ierr.NewError("invalid scope").
			WithHint("Scope must be either PLAN or SUBSCRIPTION").
			WithReportableDetails(map[string]interface{}{
				"scope": c.Scope,
			}).
			Mark(ierr.ErrValidation)
	}

	if c.Amount.LessThanOrEqual(decimal.Zero) {
		return ierr.NewError("amount must be greater than zero").
			WithHint("Please provide a positive amount").
			WithReportableDetails(map[string]interface{}{
				"amount": c.Amount,
			}).
			Mark(ierr.ErrValidation)
	}

	if c.Currency == "" {
		return ierr.NewError("currency is required").
			WithHint("Please provide a valid currency code").
			Mark(ierr.ErrValidation)
	}

	if c.Cadence == "" {
		return ierr.NewError("cadence is required").
			WithHint("Please specify the cadence (ONETIME or RECURRING)").
			Mark(ierr.ErrValidation)
	}

	// Validate based on cadence
	if c.Cadence == types.CreditGrantCadenceRecurring {
		if c.Period == nil || *c.Period == "" {
			return ierr.NewError("period is required for RECURRING cadence").
				WithHint("Please provide a valid period (e.g., MONTHLY, YEARLY)").
				WithReportableDetails(map[string]interface{}{
					"cadence": c.Cadence,
				}).
				Mark(ierr.ErrValidation)
		}
	}

	if c.ExpirationType == types.CreditGrantExpiryTypeDuration {
		if c.ExpirationDuration == nil || *c.ExpirationDuration <= 0 {
			return ierr.NewError("expiry_duration is required for DURATION expiration type").
				WithHint("Please provide a valid expiry_duration").
				Mark(ierr.ErrValidation)
		}

		if c.ExpirationDurationUnit == nil || *c.ExpirationDurationUnit == "" {
			return ierr.NewError("expiry_duration_unit is required for DURATION expiration type").
				WithHint("Please provide a valid expiry_duration_unit").
				Mark(ierr.ErrValidation)
		}
	}

	if c.ExpirationType == types.CreditGrantExpiryTypeBillingCycle {
		if c.Period == nil || *c.Period == "" {
			return ierr.NewError("period is required for BILLING_CYCLE expiration type").
				WithHint("Please provide a valid period").
				Mark(ierr.ErrValidation)
		}
	}

	return nil
}

// FromEnt converts ent.CreditGrant to domain CreditGrant
func FromEnt(c *ent.CreditGrant) *CreditGrant {
	if c == nil {
		return nil
	}

	var period *types.CreditGrantPeriod
	if c.Period != nil {
		p := types.CreditGrantPeriod(*c.Period)
		period = &p
	}

	return &CreditGrant{
		ID:                     c.ID,
		Name:                   c.Name,
		Scope:                  types.CreditGrantScope(c.Scope),
		PlanID:                 c.PlanID,
		SubscriptionID:         c.SubscriptionID,
		Amount:                 c.Amount,
		Currency:               c.Currency,
		Cadence:                types.CreditGrantCadence(c.Cadence),
		Period:                 period,
		PeriodCount:            c.PeriodCount,
		Priority:               c.Priority,
		Metadata:               c.Metadata,
		EnvironmentID:          c.EnvironmentID,
		ExpirationDuration:     c.ExpirationDuration,
		ExpirationDurationUnit: c.ExpirationDurationUnit,
		ExpirationType:         types.CreditGrantExpiryType(*c.ExpirationType),
		BaseModel: types.BaseModel{
			TenantID:  c.TenantID,
			Status:    types.Status(c.Status),
			CreatedAt: c.CreatedAt,
			UpdatedAt: c.UpdatedAt,
			CreatedBy: c.CreatedBy,
			UpdatedBy: c.UpdatedBy,
		},
	}
}

// FromEntList converts []*ent.CreditGrant to []*CreditGrant
func FromEntList(list []*ent.CreditGrant) []*CreditGrant {
	result := make([]*CreditGrant, len(list))
	for i, c := range list {
		result[i] = FromEnt(c)
	}
	return result
}
