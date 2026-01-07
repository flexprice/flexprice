package dto

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/creditgrant"
	domainCreditGrantApplication "github.com/flexprice/flexprice/internal/domain/creditgrantapplication"
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
	Credits                decimal.Decimal                      `json:"credits" binding:"required" swaggertype:"string"`
	Cadence                types.CreditGrantCadence             `json:"cadence" binding:"required"`
	Period                 *types.CreditGrantPeriod             `json:"period,omitempty"`
	PeriodCount            *int                                 `json:"period_count,omitempty"`
	ExpirationType         types.CreditGrantExpiryType          `json:"expiration_type,omitempty"`
	ExpirationDuration     *int                                 `json:"expiration_duration,omitempty"`
	ExpirationDurationUnit *types.CreditGrantExpiryDurationUnit `json:"expiration_duration_unit,omitempty"`
	Priority               *int                                 `json:"priority,omitempty"`
	Metadata               types.Metadata                       `json:"metadata,omitempty"`
	CreditGrantAnchor      *time.Time                           `json:"-"`
	StartDate              *time.Time                           `json:"-"`
	EndDate                *time.Time                           `json:"-"`

	// amount in the currency =  number of credits * conversion_rate
	// ex if conversion_rate is 1, then 1 USD = 1 credit
	// ex if conversion_rate is 2, then 1 USD = 0.5 credits
	// ex if conversion_rate is 0.5, then 1 USD = 2 credits
	ConversionRate *decimal.Decimal `json:"conversion_rate,omitempty" swaggertype:"string"`

	// topup_conversion_rate is the conversion rate for the topup to the currency
	// ex if topup_conversion_rate is 1, then 1 USD = 1 credit
	// ex if topup_conversion_rate is 2, then 1 USD = 0.5 credits
	// ex if topup_conversion_rate is 0.5, then 1 USD = 2 credits
	TopupConversionRate *decimal.Decimal `json:"topup_conversion_rate,omitempty" swaggertype:"string"`
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
		// For plan-scoped grants, StartDate and EndDate are not allowed
		if r.StartDate != nil || r.EndDate != nil {
			return errors.NewError("start_date and end_date are not allowed for PLAN-scoped grants").
				WithHint("Start date and end date are not allowed for PLAN-scoped grants").
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
		// StartDate is required for subscription-scoped grants
		if r.StartDate == nil {
			return errors.NewError("start_date is required for SUBSCRIPTION-scoped grants").
				WithHint("Please provide a start date for the credit grant").
				WithReportableDetails(map[string]interface{}{
					"scope": r.Scope,
				}).
				Mark(errors.ErrValidation)
		}

		// If EndDate is provided, it must be >= StartDate
		if r.EndDate != nil && r.EndDate.Before(lo.FromPtr(r.StartDate)) {
			return errors.NewError("end_date cannot be before start_date").
				WithHint("Credit grant end date must be on or after the start date").
				WithReportableDetails(map[string]interface{}{
					"start_date": r.StartDate,
					"end_date":   r.EndDate,
				}).
				Mark(errors.ErrValidation)
		}
		// Credit grant anchor cannot be before start date
		if r.CreditGrantAnchor != nil && r.StartDate != nil && r.CreditGrantAnchor.Before(*r.StartDate) {
			return errors.NewError("credit_grant_anchor cannot be before start_date").
				WithHint("Credit grant anchor must be on or after the start date").
				WithReportableDetails(map[string]interface{}{
					"start_date":          r.StartDate,
					"credit_grant_anchor": r.CreditGrantAnchor,
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

	if r.ConversionRate != nil {
		if r.ConversionRate.LessThanOrEqual(decimal.Zero) {
			return errors.NewError("conversion_rate must be greater than zero").
				WithHint("Please provide a positive conversion rate").
				Mark(errors.ErrValidation)
		}
	}

	if r.TopupConversionRate != nil {
		if r.TopupConversionRate.LessThanOrEqual(decimal.Zero) {
			return errors.NewError("topup_conversion_rate must be greater than zero").
				WithHint("Please provide a positive topup conversion rate").
				Mark(errors.ErrValidation)
		}
	}

	return nil
}

// ToCreditGrant converts CreateCreditGrantRequest to domain CreditGrant
func (r *CreateCreditGrantRequest) ToCreditGrant(ctx context.Context) *creditgrant.CreditGrant {
	cg := &creditgrant.CreditGrant{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CREDIT_GRANT),
		Name:           r.Name,
		Scope:          r.Scope,
		Credits:        r.Credits,
		Cadence:        r.Cadence,
		ExpirationType: r.ExpirationType,
		EnvironmentID:  types.GetEnvironmentID(ctx),
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}

	// Set fields based on scope
	switch r.Scope {
	case types.CreditGrantScopePlan:
		cg.PlanID = r.PlanID
		// For PLAN scope, these fields must be nil
		cg.SubscriptionID = nil
		cg.StartDate = nil
		cg.EndDate = nil
		cg.CreditGrantAnchor = nil

	case types.CreditGrantScopeSubscription:
		cg.SubscriptionID = r.SubscriptionID
		cg.StartDate = r.StartDate
		// Set optional fields if provided
		if r.EndDate != nil {
			cg.EndDate = r.EndDate
		}
		if r.CreditGrantAnchor != nil {
			cg.CreditGrantAnchor = r.CreditGrantAnchor
		}
		// PlanID can be provided for subscription-scoped grants
		if r.PlanID != nil {
			cg.PlanID = r.PlanID
		}
	}

	// Set fields based on cadence
	if r.Cadence == types.CreditGrantCadenceRecurring {
		cg.Period = r.Period
		cg.PeriodCount = r.PeriodCount
	} else {
		// For ONETIME cadence, these fields must be nil
		cg.Period = nil
		cg.PeriodCount = nil
	}

	// Set fields based on expiration type
	if r.ExpirationType == types.CreditGrantExpiryTypeDuration {
		cg.ExpirationDuration = r.ExpirationDuration
		cg.ExpirationDurationUnit = r.ExpirationDurationUnit
	} else {
		// For NEVER or BILLING_CYCLE, these fields must be nil
		cg.ExpirationDuration = nil
		cg.ExpirationDurationUnit = nil
	}

	// Set optional fields only if provided
	if r.Priority != nil {
		cg.Priority = r.Priority
	}
	if len(r.Metadata) > 0 {
		cg.Metadata = r.Metadata
	}

	// Set conversion rates if provided
	if r.ConversionRate != nil {
		cg.ConversionRate = r.ConversionRate
	}
	if r.TopupConversionRate != nil {
		cg.TopupConversionRate = r.TopupConversionRate
	}

	return cg
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

// CreditGrantApplicationResponse represents the response for a credit grant application
type CreditGrantApplicationResponse struct {
	*domainCreditGrantApplication.CreditGrantApplication
}

// ListCreditGrantApplicationsResponse represents a paginated list of credit grant applications
type ListCreditGrantApplicationsResponse = types.ListResponse[*CreditGrantApplicationResponse]

// CreateCreditGrantApplicationRequest represents the request to create a new credit grant application
type CreateCreditGrantApplicationRequest struct {
	CreditGrantID                   string                             `json:"credit_grant_id" binding:"required"`
	SubscriptionID                  string                             `json:"subscription_id"`
	ScheduledFor                    time.Time                          `json:"scheduled_for" binding:"required"`
	PeriodStart                     time.Time                          `json:"period_start" binding:"required"`
	PeriodEnd                       *time.Time                         `json:"period_end"`
	Credits                         decimal.Decimal                    `json:"credits" swaggertype:"string" binding:"required"`
	ApplicationReason               types.CreditGrantApplicationReason `json:"application_reason"`
	SubscriptionStatusAtApplication types.SubscriptionStatus           `json:"subscription_status_at_application"`
	IdempotencyKey                  string                             `json:"idempotency_key"`
}

// Validate validates the create credit grant application request
func (r *CreateCreditGrantApplicationRequest) Validate() error {

	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	if r.ScheduledFor.IsZero() {
		return errors.NewError("scheduled_for is required").
			WithHint("Please provide a valid scheduled date").
			Mark(errors.ErrValidation)
	}
	if r.PeriodStart.IsZero() {
		return errors.NewError("period_start is required").
			WithHint("Please provide a valid period start date").
			Mark(errors.ErrValidation)
	}
	if r.Credits.IsNegative() {
		return errors.NewError("credits cannot be negative").
			WithHint("Please provide a non-negative credits amount").
			Mark(errors.ErrValidation)
	}

	if err := r.ApplicationReason.Validate(); err != nil {
		return err
	}

	if err := r.SubscriptionStatusAtApplication.Validate(); err != nil {
		return err
	}

	if r.PeriodEnd != nil {
		if r.PeriodEnd.Before(r.PeriodStart) {
			return errors.NewError("period_end must be after period_start").
				WithReportableDetails(map[string]interface{}{
					"period_end":   r.PeriodEnd,
					"period_start": r.PeriodStart,
				}).
				WithHint("Please provide a valid period end and start").
				Mark(errors.ErrValidation)
		}
	}

	return nil
}

// ToCreditGrantApplication converts CreateCreditGrantApplicationRequest to domain CreditGrantApplication
func (r *CreateCreditGrantApplicationRequest) ToCreditGrantApplication(ctx context.Context) *domainCreditGrantApplication.CreditGrantApplication {
	return &domainCreditGrantApplication.CreditGrantApplication{
		ID:                              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CREDIT_GRANT_APPLICATION),
		CreditGrantID:                   r.CreditGrantID,
		SubscriptionID:                  r.SubscriptionID,
		ScheduledFor:                    r.ScheduledFor,
		PeriodStart:                     r.PeriodStart,
		PeriodEnd:                       r.PeriodEnd,
		ApplicationStatus:               types.ApplicationStatusPending,
		ApplicationReason:               r.ApplicationReason,
		SubscriptionStatusAtApplication: r.SubscriptionStatusAtApplication,
		Credits:                         r.Credits,
		IdempotencyKey:                  r.IdempotencyKey,
		RetryCount:                      0,
		FailureReason:                   nil,
		EnvironmentID:                   types.GetEnvironmentID(ctx),
		BaseModel:                       types.GetDefaultBaseModel(ctx),
	}
}
