package flexpricebilling

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	flexpricebillingmodels "github.com/flexprice/flexprice/internal/temporal/models/flexprice_billing"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"go.temporal.io/sdk/activity"
)

// FlexpriceBillingActivities contains Temporal activities for Flexprice self-billing onboarding.
type FlexpriceBillingActivities struct {
	serviceParams service.ServiceParams
	logger        *logger.Logger
}

// NewFlexpriceBillingActivities creates a new instance of FlexpriceBillingActivities.
func NewFlexpriceBillingActivities(serviceParams service.ServiceParams, logger *logger.Logger) *FlexpriceBillingActivities {
	return &FlexpriceBillingActivities{
		serviceParams: serviceParams,
		logger:        logger,
	}
}

// CreateBillingCustomerActivity creates a customer in the Flexprice billing tenant for the
// newly onboarded tenant. Idempotent: if the customer already exists, returns the existing ID.
func (a *FlexpriceBillingActivities) CreateBillingCustomerActivity(
	ctx context.Context,
	input flexpricebillingmodels.FlexpriceBillingOnboardingInput,
) (string, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("Starting CreateBillingCustomerActivity", "tenant_id", input.TenantID)

	billingCtx := context.WithValue(ctx, types.CtxTenantID, input.BillingTenantID)
	billingCtx = context.WithValue(billingCtx, types.CtxEnvironmentID, input.BillingEnvironmentID)

	customerSvc := service.NewCustomerService(a.serviceParams)

	// Idempotent: return existing customer if already created
	existing, err := customerSvc.GetCustomerByLookupKey(billingCtx, input.TenantID)
	if err == nil && existing != nil && existing.ID != "" {
		logger.Info("Billing customer already exists", "customer_id", existing.ID, "tenant_id", input.TenantID)
		return existing.ID, nil
	}

	resp, err := customerSvc.CreateCustomer(billingCtx, dto.CreateCustomerRequest{
		Name:       input.TenantName,
		ExternalID: input.TenantID,
		Email:      input.Email,
		Metadata: map[string]string{
			"tenant_id":           input.TenantID,
			"created_by_workflow": "true",
		},
		SkipOnboardingWorkflow: true,
	})
	if err != nil {
		return "", ierr.WithError(err).
			WithHint("Failed to create billing customer").
			WithReportableDetails(map[string]interface{}{
				"tenant_id": input.TenantID,
			}).
			Mark(ierr.ErrInternal)
	}

	logger.Info("Billing customer created", "customer_id", resp.ID, "tenant_id", input.TenantID)
	return resp.ID, nil
}

// CreateBillingSubscriptionActivity assigns the configured BASE plan to the billing customer.
// If BillingPlanID is empty, the activity is a no-op.
func (a *FlexpriceBillingActivities) CreateBillingSubscriptionActivity(
	ctx context.Context,
	input flexpricebillingmodels.FlexpriceBillingOnboardingInput,
	customerID string,
) (string, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("Starting CreateBillingSubscriptionActivity", "tenant_id", input.TenantID, "customer_id", customerID)

	if input.BillingPlanID == "" {
		logger.Info("No billing plan configured, skipping subscription creation")
		return "", nil
	}

	billingCtx := context.WithValue(ctx, types.CtxTenantID, input.BillingTenantID)
	billingCtx = context.WithValue(billingCtx, types.CtxEnvironmentID, input.BillingEnvironmentID)

	subscriptionSvc := service.NewSubscriptionService(a.serviceParams)
	resp, err := subscriptionSvc.CreateSubscription(billingCtx, dto.CreateSubscriptionRequest{
		CustomerID:   customerID,
		PlanID:       input.BillingPlanID,
		BillingCycle: types.BillingCycleAnniversary,
		StartDate:    lo.ToPtr(time.Now().UTC()),
	})
	if err != nil {
		return "", ierr.WithError(err).
			WithHint("Failed to create billing subscription").
			WithReportableDetails(map[string]interface{}{
				"tenant_id":   input.TenantID,
				"customer_id": customerID,
				"plan_id":     input.BillingPlanID,
			}).
			Mark(ierr.ErrInternal)
	}

	logger.Info("Billing subscription created", "subscription_id", resp.ID, "tenant_id", input.TenantID)
	return resp.ID, nil
}
