package dto

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/creditgrant"
	"github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/shopspring/decimal"
)

// CreateCreditGrantRequest represents the request to create a new credit grant
type CreateCreditGrantRequest struct {
	Name                   string                               `json:"name" binding:"required"`
	Scope                  types.CreditGrantScope               `json:"scope" binding:"required"`
	PlanID                 *string                              `json:"plan_id,omitempty"`
	SubscriptionID         *string                              `json:"subscription_id,omitempty"`
	Amount                 decimal.Decimal                      `json:"amount" binding:"required"`
	Currency               string                               `json:"currency" binding:"required"`
	Cadence                types.CreditGrantCadence             `json:"cadence" binding:"required"`
	Period                 *types.CreditGrantPeriod             `json:"period,omitempty"`
	PeriodCount            *int                                 `json:"period_count,omitempty"`
	ExpirationType         types.CreditGrantExpiryType          `json:"expiration_type,omitempty"`
	ExpirationDuration     *int                                 `json:"expiration_duration,omitempty"`
	ExpirationDurationUnit *types.CreditGrantExpiryDurationUnit `json:"expiration_duration_unit,omitempty"`
	Priority               *int                                 `json:"priority,omitempty"`
	Metadata               types.Metadata                       `json:"metadata,omitempty"`
}

// UpdateCreditGrantRequest represents the request to update an existing credit grant
type UpdateCreditGrantRequest struct {
	Name     *string         `json:"name,omitempty"`
	Metadata *types.Metadata `json:"metadata,omitempty"`
}

// CreditGrantResponse represents the response for a credit grant
type CreditGrantResponse struct {
	*creditgrant.CreditGrant
}

// ListCreditGrantsResponse represents a paginated list of credit grants
type ListCreditGrantsResponse = types.ListResponse[*CreditGrantResponse]

// Validate validates the create credit grant request
func (r *CreateCreditGrantRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	if r.Name == "" {
		return errors.NewError("name is required").
			WithHint("Please provide a name for the credit grant").
			Mark(errors.ErrValidation)
	}

	if r.Scope == "" {
		return errors.NewError("scope is required").
			WithHint("Please specify the scope (PLAN or SUBSCRIPTION)").
			Mark(errors.ErrValidation)
	}

	// Validate based on scope
	switch r.Scope {
	case types.CreditGrantScopePlan:
		if r.PlanID == nil || *r.PlanID == "" {
			return errors.NewError("plan_id is required for PLAN-scoped grants").
				WithHint("Please provide a valid plan ID").
				WithReportableDetails(map[string]interface{}{
					"scope": r.Scope,
				}).
				Mark(errors.ErrValidation)
		}
	case types.CreditGrantScopeSubscription:
		if r.SubscriptionID == nil || *r.SubscriptionID == "" {
			return errors.NewError("subscription_id is required for SUBSCRIPTION-scoped grants").
				WithHint("Please provide a valid subscription ID").
				WithReportableDetails(map[string]interface{}{
					"scope": r.Scope,
				}).
				Mark(errors.ErrValidation)
		}
		if r.PlanID == nil || *r.PlanID == "" {
			return errors.NewError("plan_id is required for SUBSCRIPTION-scoped grants").
				WithHint("Please provide a valid plan ID").
				WithReportableDetails(map[string]interface{}{
					"scope": r.Scope,
				}).
				Mark(errors.ErrValidation)
		}
	default:
		return errors.NewError("invalid scope").
			WithHint("Scope must be either PLAN or SUBSCRIPTION").
			WithReportableDetails(map[string]interface{}{
				"scope": r.Scope,
			}).
			Mark(errors.ErrValidation)
	}

	if r.Amount.LessThanOrEqual(decimal.Zero) {
		return errors.NewError("amount must be greater than zero").
			WithHint("Please provide a positive amount").
			WithReportableDetails(map[string]interface{}{
				"amount": r.Amount,
			}).
			Mark(errors.ErrValidation)
	}

	if r.Currency == "" {
		return errors.NewError("currency is required").
			WithHint("Please provide a valid currency code").
			Mark(errors.ErrValidation)
	}

	if r.Cadence == "" {
		return errors.NewError("cadence is required").
			WithHint("Please specify the cadence (ONETIME or RECURRING)").
			Mark(errors.ErrValidation)
	}

	// Validate based on cadence
	if r.Cadence == types.CreditGrantCadenceRecurring {
		if r.Period == nil || *r.Period == "" {
			return errors.NewError("period is required for RECURRING cadence").
				WithHint("Please provide a valid period (e.g., MONTHLY, YEARLY)").
				WithReportableDetails(map[string]interface{}{
					"cadence": r.Cadence,
				}).
				Mark(errors.ErrValidation)
		}
	}

	if r.ExpirationType == "" {
		return errors.NewError("expiration_type is required").
			WithHint("Please specify the expiration type (NEVER, DURATION, BILLING_CYCLE)").
			Mark(errors.ErrValidation)
	}

	if r.ExpirationType == types.CreditGrantExpiryTypeBillingCycle {
		if r.Period == nil || *r.Period == "" {
			return errors.NewError("period is required for BILLING_CYCLE expiration type").
				WithHint("Please provide a valid period").
				Mark(errors.ErrValidation)
		}
	}

	if r.ExpirationType == types.CreditGrantExpiryTypeDuration {
		if r.ExpirationDuration == nil || *r.ExpirationDuration <= 0 {
			return errors.NewError("expiry_duration is required for DURATION expiration type").
				WithHint("Please provide a valid expiry_duration").
				Mark(errors.ErrValidation)
		}

		if r.ExpirationDurationUnit == nil || *r.ExpirationDurationUnit == "" {
			return errors.NewError("expiry_duration_unit is required for DURATION expiration type").
				WithHint("Please provide a valid expiry_duration_unit").
				Mark(errors.ErrValidation)
		}
	}

	return nil
}

// ToCreditGrant converts CreateCreditGrantRequest to domain CreditGrant
func (r *CreateCreditGrantRequest) ToCreditGrant(ctx context.Context) *creditgrant.CreditGrant {
	return &creditgrant.CreditGrant{
		ID:                     types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CREDIT_GRANT),
		Name:                   r.Name,
		Scope:                  r.Scope,
		PlanID:                 r.PlanID,
		SubscriptionID:         r.SubscriptionID,
		Amount:                 r.Amount,
		Currency:               r.Currency,
		Cadence:                r.Cadence,
		Period:                 r.Period,
		PeriodCount:            r.PeriodCount,
		Priority:               r.Priority,
		ExpirationType:         r.ExpirationType,
		ExpirationDuration:     r.ExpirationDuration,
		ExpirationDurationUnit: r.ExpirationDurationUnit,
		Metadata:               r.Metadata,
		EnvironmentID:          types.GetEnvironmentID(ctx),
		BaseModel:              types.GetDefaultBaseModel(ctx),
	}
}

// UpdateCreditGrant applies UpdateCreditGrantRequest to domain CreditGrant
func (r *UpdateCreditGrantRequest) UpdateCreditGrant(grant *creditgrant.CreditGrant, ctx context.Context) {
	user := types.GetUserID(ctx)
	grant.UpdatedBy = user

	if r.Name != nil {
		grant.Name = *r.Name
	}

	if r.Metadata != nil {
		if grant.Metadata == nil {
			grant.Metadata = make(map[string]string)
		}
		for k, v := range *r.Metadata {
			grant.Metadata[k] = v
		}
	}
}

// FromCreditGrant converts domain CreditGrant to CreditGrantResponse
func FromCreditGrant(grant *creditgrant.CreditGrant) *CreditGrantResponse {
	if grant == nil {
		return nil
	}

	return &CreditGrantResponse{
		CreditGrant: grant,
	}
}
