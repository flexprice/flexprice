package dto

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/creditgrant"
	"github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// CreateCreditGrantRequest represents the request to create a new credit grant
type CreateCreditGrantRequest struct {
	Name                   string                               `json:"name" binding:"required"`
	Scope                  types.CreditGrantScope               `json:"scope" binding:"required"`
	PlanID                 *string                              `json:"plan_id,omitempty"`
	SubscriptionID         *string                              `json:"subscription_id,omitempty"`
	Credits                decimal.Decimal                      `json:"credits" binding:"required"`
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

	if err := r.Scope.Validate(); err != nil {
		return err
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

	if r.Credits.LessThanOrEqual(decimal.Zero) {
		return errors.NewError("credits must be greater than zero").
			WithHint("Please provide a positive credits").
			WithReportableDetails(map[string]interface{}{
				"credits": r.Credits,
			}).
			Mark(errors.ErrValidation)
	}

	if err := r.Cadence.Validate(); err != nil {
		return err
	}

	if err := r.ExpirationType.Validate(); err != nil {
		return err
	}

	// Validate based on cadence
	if r.Cadence == types.CreditGrantCadenceRecurring {
		if r.Period == nil || lo.FromPtr(r.Period) == "" {
			return errors.NewError("period is required for RECURRING cadence").
				WithHint("Please provide a valid period (e.g., MONTHLY, YEARLY)").
				WithReportableDetails(map[string]interface{}{
					"cadence": r.Cadence,
				}).
				Mark(errors.ErrValidation)
		}

		if err := r.Period.Validate(); err != nil {
			return err
		}

		if r.PeriodCount == nil || lo.FromPtr(r.PeriodCount) <= 0 {
			return errors.NewError("period_count is required for RECURRING cadence").
				WithHint("Please provide a valid period_count").
				WithReportableDetails(map[string]interface{}{
					"period_count": lo.FromPtr(r.PeriodCount),
				}).
				Mark(errors.ErrValidation)
		}
	}

	if err := r.ExpirationType.Validate(); err != nil {
		return err
	}

	if r.ExpirationType == types.CreditGrantExpiryTypeDuration {

		if r.ExpirationDurationUnit == nil {
			return errors.NewError("expiration_duration_unit is required for DURATION expiration type").
				WithHint("Please provide a valid expiration duration unit").
				WithReportableDetails(map[string]interface{}{
					"expiration_type": r.ExpirationType,
				}).
				Mark(errors.ErrValidation)
		}

		if err := r.ExpirationDurationUnit.Validate(); err != nil {
			return err
		}

		if r.ExpirationDuration == nil || lo.FromPtr(r.ExpirationDuration) <= 0 {
			return errors.NewError("expiration_duration is required for DURATION expiration type").
				WithHint("Please provide a valid expiration duration").
				WithReportableDetails(map[string]interface{}{
					"expiration_type": r.ExpirationType,
				}).
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
		Credits:                r.Credits,
		Cadence:                r.Cadence,
		Period:                 r.Period,
		PeriodCount:            r.PeriodCount,
		ExpirationType:         r.ExpirationType,
		ExpirationDuration:     r.ExpirationDuration,
		ExpirationDurationUnit: r.ExpirationDurationUnit,
		Priority:               r.Priority,
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

type ProcessScheduledCreditGrantApplicationsResponse struct {
	SuccessApplicationsCount int `json:"success_applications_count"`
	FailedApplicationsCount  int `json:"failed_applications_count"`
	TotalApplicationsCount   int `json:"total_applications_count"`
}
