package customer

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/activity"
)

// CustomerActivities contains all customer-related Temporal activities
type CustomerActivities struct {
	serviceParams service.ServiceParams
	logger        *logger.Logger
}

// NewCustomerActivities creates a new instance of CustomerActivities
func NewCustomerActivities(serviceParams service.ServiceParams, logger *logger.Logger) *CustomerActivities {
	return &CustomerActivities{
		serviceParams: serviceParams,
		logger:        logger,
	}
}

// CreateWalletActivity creates a wallet for a customer based on workflow configuration
func (a *CustomerActivities) CreateWalletActivity(ctx context.Context, input models.CreateWalletActivityInput) (*models.CreateWalletActivityResult, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("Starting CreateWalletActivity", "customer_id", input.CustomerID, "currency", input.WalletConfig.Currency)

	// Validate input
	if err := input.Validate(); err != nil {
		return nil, err
	}

	// Set tenant_id, environment_id, and user_id in context for proper BaseModel creation
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)
	ctx = types.SetUserID(ctx, input.UserID)

	// Create wallet service
	walletService := service.NewWalletService(a.serviceParams)

	// Convert workflow config to DTO
	createWalletReq, err := input.WalletConfig.ToDTO(&models.WorkflowActionParams{
		CustomerID: input.CustomerID,
		Currency:   input.WalletConfig.Currency,
	})
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to convert wallet config to DTO").
			Mark(ierr.ErrInternal)
	}

	walletDTOReq, ok := createWalletReq.(*dto.CreateWalletRequest)
	if !ok {
		return nil, ierr.NewError("failed to convert to CreateWalletRequest DTO").Mark(ierr.ErrInternal)
	}

	// Create the wallet
	walletResp, err := walletService.CreateWallet(ctx, walletDTOReq)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to create wallet").
			WithReportableDetails(map[string]interface{}{
				"customer_id": input.CustomerID,
				"currency":    input.WalletConfig.Currency,
			}).
			Mark(ierr.ErrInternal)
	}

	logger.Info("Successfully created wallet",
		"customer_id", input.CustomerID,
		"wallet_id", walletResp.ID,
		"currency", walletResp.Currency,
		"conversion_rate", walletResp.ConversionRate)

	return &models.CreateWalletActivityResult{
		WalletID:       walletResp.ID,
		CustomerID:     input.CustomerID,
		Currency:       walletResp.Currency,
		ConversionRate: walletResp.ConversionRate.String(),
		WalletType:     walletResp.WalletType,
		Status:         walletResp.WalletStatus,
	}, nil
}

// CreateSubscriptionActivity creates a subscription for a customer based on workflow configuration
func (a *CustomerActivities) CreateSubscriptionActivity(ctx context.Context, input models.CreateSubscriptionActivityInput) (*models.CreateSubscriptionActivityResult, error) {
	logger := activity.GetLogger(ctx)

	// Validate input first before accessing any nested fields
	if err := input.Validate(); err != nil {
		return nil, err
	}

	// Now safe to log plan_id after validation
	logger.Info("Starting CreateSubscriptionActivity", "customer_id", input.CustomerID, "plan_id", input.SubscriptionConfig.PlanID)

	// Set tenant_id, environment_id, and user_id in context for proper BaseModel creation
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)
	ctx = types.SetUserID(ctx, input.UserID)

	// Get price information for currency
	priceService := service.NewPriceService(a.serviceParams)
	pricesResp, err := priceService.GetPricesByPlanID(ctx, dto.GetPricesByPlanRequest{
		PlanID: input.SubscriptionConfig.PlanID,
	})
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get prices for plan").
			WithReportableDetails(map[string]interface{}{
				"plan_id": input.SubscriptionConfig.PlanID,
			}).
			Mark(ierr.ErrDatabase)
	}

	currency := "USD"
	if len(pricesResp.Items) > 0 && pricesResp.Items[0].Currency != "" {
		currency = pricesResp.Items[0].Currency
	}

	// Validate subscription config for this customer
	if err := input.SubscriptionConfig.Validate(); err != nil {
		return nil, err
	}

	// Create subscription service
	subscriptionService := service.NewSubscriptionService(a.serviceParams)

	// Convert workflow config to DTO
	createSubReq, err := input.SubscriptionConfig.ToDTO(&models.WorkflowActionParams{
		CustomerID: input.CustomerID,
		Currency:   currency,
	})
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to convert subscription config to DTO").
			Mark(ierr.ErrInternal)
	}

	subDTOReq, ok := createSubReq.(*dto.CreateSubscriptionRequest)
	if !ok {
		return nil, ierr.NewError("failed to convert to CreateSubscriptionRequest DTO").Mark(ierr.ErrInternal)
	}

	// Create the subscription
	subResp, err := subscriptionService.CreateSubscription(ctx, *subDTOReq)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to create subscription").
			WithReportableDetails(map[string]interface{}{
				"customer_id": input.CustomerID,
				"plan_id":     input.SubscriptionConfig.PlanID,
			}).
			Mark(ierr.ErrInternal)
	}

	logger.Info("Successfully created subscription",
		"customer_id", input.CustomerID,
		"subscription_id", subResp.ID,
		"plan_id", input.SubscriptionConfig.PlanID,
		"currency", currency)

	return &models.CreateSubscriptionActivityResult{
		SubscriptionID: subResp.ID,
		CustomerID:     input.CustomerID,
		PlanID:         input.SubscriptionConfig.PlanID,
		Currency:       currency,
		Status:         subResp.SubscriptionStatus,
		BillingCycle:   subResp.BillingCycle,
		StartDate:      &subResp.StartDate,
	}, nil
}
