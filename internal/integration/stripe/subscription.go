package stripe

import (
	"context"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stripe/stripe-go/v82"
)

type StripeSubscriptionService interface {
	CreateSubscription(ctx context.Context, stripeSubscriptionID string, services *ServiceDependencies) (*dto.SubscriptionResponse, error)
	UpdateSubscription(ctx context.Context, stripeSubscriptionID string, services *ServiceDependencies) error
	CancelSubscription(ctx context.Context, stripeSubscriptionID string, services *ServiceDependencies) error
}

type stripeSubscriptionService struct {
	client        *Client
	logger        *logger.Logger
	stripePlanSvc StripePlanService
}

func NewStripeSubscriptionService(client *Client, logger *logger.Logger, stripePlanSvc StripePlanService) *stripeSubscriptionService {
	return &stripeSubscriptionService{
		client:        client,
		logger:        logger,
		stripePlanSvc: stripePlanSvc,
	}
}

// fetchStripeSubscription retrieves a subscription from Stripe
func (s *stripeSubscriptionService) fetchStripeSubscription(ctx context.Context, subscriptionID string) (*stripe.Subscription, error) {

	stripeClient, _, err := s.client.GetStripeClient(ctx)
	if err != nil {
		return nil, ierr.NewError("failed to get Stripe client").
			WithHint("Could not initialize Stripe client").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": subscriptionID,
				"error":           err.Error(),
			}).
			Mark(ierr.ErrSystem)
	}

	// Retrieve the subscription from Stripe with expanded fields
	params := &stripe.SubscriptionRetrieveParams{
		Expand: []*string{
			stripe.String("customer"),
			stripe.String("items.data.price.product"),
		},
	}

	stripeSub, err := stripeClient.V1Subscriptions.Retrieve(ctx, subscriptionID, params)
	if err != nil {
		s.logger.Errorw("failed to retrieve subscription from Stripe",
			"error", err,
			"subscription_id", subscriptionID,
		)
		return nil, ierr.NewError("failed to retrieve subscription from Stripe").
			WithHint("Could not fetch subscription information from Stripe").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": subscriptionID,
				"error":           err.Error(),
			}).
			Mark(ierr.ErrSystem)
	}

	return stripeSub, nil
}

func (s *stripeSubscriptionService) CreateSubscription(ctx context.Context, stripeSubscriptionID string, services *ServiceDependencies) (*dto.SubscriptionResponse, error) {
	var subscriptionResp *dto.SubscriptionResponse

	err := services.DB.WithTx(ctx, func(txCtx context.Context) error {
		// Extract Services
		subscriptionService := services.SubscriptionService
		entityMappingService := services.EntityIntegrationMappingService

		// Step 1: Check if the mapping with the stripe subscription id exists
		filter := &types.EntityIntegrationMappingFilter{
			EntityType:        types.IntegrationEntityTypeSubscription,
			ProviderTypes:     []string{"stripe"},
			ProviderEntityIDs: []string{stripeSubscriptionID},
		}

		existingMappings, err := entityMappingService.GetEntityIntegrationMappings(txCtx, filter)
		if err != nil {
			return ierr.WithError(err).
				WithHint("Failed to check for existing subscription mapping").
				Mark(ierr.ErrInternal)
		}

		// If mapping exists, return the existing subscription
		if len(existingMappings.Items) > 0 {
			existingMapping := existingMappings.Items[0]
			subscriptionResp, err = subscriptionService.GetSubscription(txCtx, existingMapping.EntityID)
			return err
		}

		// Step 2: Fetch Stripe subscription data
		stripeSubscription, err := s.fetchStripeSubscription(txCtx, stripeSubscriptionID)
		if err != nil {
			return err
		}
		// Step 3: Create or find customer
		customerID, err := s.createOrFindCustomer(txCtx, stripeSubscription, services)
		if err != nil {
			return err
		}

		// Step 4: Create or find plan
		planID, err := s.createOrFindPlan(txCtx, stripeSubscription, services)
		if err != nil {
			return err
		}

		// Step 5: Create subscription
		subscriptionResp, err = s.createFlexPriceSubscription(txCtx, stripeSubscription, customerID, planID, services)
		if err != nil {
			return err
		}

		// Step 6: Create entity mapping
		_, err = entityMappingService.CreateEntityIntegrationMapping(txCtx, dto.CreateEntityIntegrationMappingRequest{
			EntityID:         subscriptionResp.ID,
			EntityType:       types.IntegrationEntityTypeSubscription,
			ProviderType:     "stripe",
			ProviderEntityID: stripeSubscriptionID,
			Metadata: map[string]interface{}{
				"created_via":            "stripe_subscription_service",
				"stripe_subscription_id": stripeSubscriptionID,
				"mapping_type":           "proxy",
				"synced_at":              time.Now().UTC().Format(time.RFC3339),
			},
		})
		if err != nil {
			s.logger.Warnw("failed to create entity mapping for subscription",
				"error", err,
				"subscription_id", subscriptionResp.ID,
				"stripe_subscription_id", stripeSubscriptionID)
			// Don't fail the entire operation if entity mapping creation fails
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return subscriptionResp, nil
}

func (s *stripeSubscriptionService) UpdateSubscription(ctx context.Context, stripeSubscriptionID string, services *ServiceDependencies) error {
	err := services.DB.WithTx(ctx, func(txCtx context.Context) error {
		// Step 1: Fetch Stripe Subscription
		stripeSubscription, err := s.fetchStripeSubscription(txCtx, stripeSubscriptionID)
		if err != nil {
			return err
		}
		// Step 2: Check if the mapping with the stripe subscription id exists
		filter := &types.EntityIntegrationMappingFilter{
			EntityType:        types.IntegrationEntityTypeSubscription,
			ProviderTypes:     []string{"stripe"},
			ProviderEntityIDs: []string{stripeSubscriptionID},
		}

		existingMappings, err := services.EntityIntegrationMappingService.GetEntityIntegrationMappings(txCtx, filter)
		if err != nil {
			return ierr.WithError(err).
				WithHint("Failed to check for existing subscription mapping").
				Mark(ierr.ErrInternal)
		}

		// Step 3: Get the existing mapping
		if len(existingMappings.Items) == 0 {
			return ierr.NewError("no existing subscription mapping found").
				WithHint("Existing subscription mapping not found").
				Mark(ierr.ErrInternal)
		}

		existingSubscriptionMapping := existingMappings.Items[0]

		// Step 4: Get the exisitng subcription
		existingSubscription, err := services.SubscriptionService.GetSubscription(txCtx, existingSubscriptionMapping.EntityID)
		if err != nil {
			return err
		}

		planChange, err := s.isPlanChange(txCtx, existingSubscription, stripeSubscription, services)
		if err != nil {
			return err
		}

		if planChange {
			return s.handlePlanChange(txCtx, existingSubscription, stripeSubscription, services)
		} else {
			return s.handleNormalChange(txCtx, existingSubscription, stripeSubscription, services)
		}
	})
	if err != nil {
		s.logger.Errorw("failed to update subscription",
			"error", err,
			"subscription_id", stripeSubscriptionID)
		return err
	}
	return nil
}

func (s *stripeSubscriptionService) CancelSubscription(ctx context.Context, stripeSubscriptionID string, services *ServiceDependencies) error {
	return services.DB.WithTx(ctx, func(txCtx context.Context) error {
		filter := &types.EntityIntegrationMappingFilter{
			EntityType:        types.IntegrationEntityTypeSubscription,
			ProviderTypes:     []string{"stripe"},
			ProviderEntityIDs: []string{stripeSubscriptionID},
		}

		existingMappings, err := services.EntityIntegrationMappingService.GetEntityIntegrationMappings(txCtx, filter)
		if err != nil {
			return ierr.WithError(err).
				WithHint("Failed to check for existing subscription mapping").
				Mark(ierr.ErrInternal)
		}

		// If mapping exists, cancel the subscription
		if len(existingMappings.Items) > 0 {
			existingMapping := existingMappings.Items[0]

			// Step 3: Cancel the subscription
			_, err := services.SubscriptionService.CancelSubscription(txCtx, existingMapping.EntityID, &dto.CancelSubscriptionRequest{
				CancellationType: types.CancellationTypeImmediate,
				Reason:           "Customer cancelled subscription",
			})
			if err != nil {
				return err
			}

			return nil
		}

		// If no mapping exists, return an error indicating subscription not found
		return ierr.NewError("subscription not found in FlexPrice").
			WithHint("No FlexPrice subscription found for the given Stripe subscription ID").
			WithReportableDetails(map[string]interface{}{
				"stripe_subscription_id": stripeSubscriptionID,
			}).
			Mark(ierr.ErrNotFound)
	})
}

// createOrFindCustomer creates or finds a customer based on Stripe subscription data
func (s *stripeSubscriptionService) createOrFindCustomer(ctx context.Context, stripeSub *stripe.Subscription, services *ServiceDependencies) (string, error) {
	if stripeSub.Customer == nil {
		return "", ierr.NewError("no customer found in Stripe subscription").
			WithHint("Stripe subscription must have a customer").
			Mark(ierr.ErrValidation)
	}

	stripeCustomerID := stripeSub.Customer.ID

	// Check if customer already exists in our system
	filter := &types.EntityIntegrationMappingFilter{
		EntityType:        types.IntegrationEntityTypeCustomer,
		ProviderTypes:     []string{"stripe"},
		ProviderEntityIDs: []string{stripeCustomerID},
	}

	entityMappingService := services.EntityIntegrationMappingService

	existingMappings, err := entityMappingService.GetEntityIntegrationMappings(ctx, filter)
	if err != nil {
		return "", ierr.WithError(err).
			WithHint("Failed to check for existing customer mapping").
			Mark(ierr.ErrInternal)
	}

	// If customer exists, return the ID
	if len(existingMappings.Items) > 0 {
		return existingMappings.Items[0].EntityID, nil
	}

	// Create customer from Stripe data
	customerService := services.CustomerService

	// Check if customer has flexprice_customer_id in metadata
	var externalID string
	if flexpriceID, exists := stripeSub.Customer.Metadata["flexprice_customer_id"]; exists {
		externalID = flexpriceID
	} else {
		// Use Stripe customer ID as external ID
		externalID = stripeCustomerID
	}

	createReq := dto.CreateCustomerRequest{
		ExternalID: externalID,
		Name:       stripeSub.Customer.Name,
		Email:      stripeSub.Customer.Email,
		Metadata: map[string]string{
			"stripe_customer_id": stripeCustomerID,
		},
	}

	// Add address if available
	if stripeSub.Customer.Address != nil {
		createReq.AddressLine1 = stripeSub.Customer.Address.Line1
		createReq.AddressLine2 = stripeSub.Customer.Address.Line2
		createReq.AddressCity = stripeSub.Customer.Address.City
		createReq.AddressState = stripeSub.Customer.Address.State
		createReq.AddressPostalCode = stripeSub.Customer.Address.PostalCode
		createReq.AddressCountry = stripeSub.Customer.Address.Country
	}

	customerResp, err := customerService.CreateCustomer(ctx, createReq)
	if err != nil {
		return "", err
	}

	// Create entity mapping for customer
	_, err = entityMappingService.CreateEntityIntegrationMapping(ctx, dto.CreateEntityIntegrationMappingRequest{
		EntityID:         customerResp.ID,
		EntityType:       types.IntegrationEntityTypeCustomer,
		ProviderType:     "stripe",
		ProviderEntityID: stripeCustomerID,
		Metadata: map[string]interface{}{
			"created_via":        "stripe_subscription_service",
			"stripe_customer_id": stripeCustomerID,
			"synced_at":          time.Now().UTC().Format(time.RFC3339),
		},
	})
	if err != nil {
		s.logger.Warnw("failed to create entity mapping for customer",
			"error", err,
			"customer_id", customerResp.ID,
			"stripe_customer_id", stripeCustomerID)
		// Don't fail the entire operation if entity mapping creation fails
	}

	return customerResp.ID, nil
}

// createOrFindPlan creates or finds a plan based on Stripe subscription data
func (s *stripeSubscriptionService) createOrFindPlan(ctx context.Context, stripeSub *stripe.Subscription, services *ServiceDependencies) (string, error) {
	if stripeSub.Items == nil || len(stripeSub.Items.Data) == 0 {
		return "", ierr.NewError("no items found in Stripe subscription").
			WithHint("Stripe subscription must have at least one item").
			Mark(ierr.ErrValidation)
	}

	// Get the first item's product
	firstItem := stripeSub.Items.Data[0]
	if firstItem.Price == nil || firstItem.Price.Product == nil {
		return "", ierr.NewError("no product found in Stripe subscription item").
			WithHint("Stripe subscription item must have a product").
			Mark(ierr.ErrValidation)
	}

	stripeProductID := firstItem.Price.Product.ID

	createPlanResp, err := s.stripePlanSvc.CreatePlan(ctx, stripeProductID, services)
	if err != nil {
		return "", err
	}

	return createPlanResp, nil
}

func (s *stripeSubscriptionService) calculateBillingPeriod(stripeSub *stripe.Subscription) types.BillingPeriod {
	switch stripeSub.Items.Data[0].Price.Recurring.Interval {
	case stripe.PriceRecurringIntervalDay:
		return types.BILLING_PERIOD_DAILY
	case stripe.PriceRecurringIntervalWeek:
		return types.BILLING_PERIOD_WEEKLY
	case stripe.PriceRecurringIntervalMonth:
		return types.BILLING_PERIOD_MONTHLY
	case stripe.PriceRecurringIntervalYear:
		return types.BILLING_PERIOD_ANNUAL
	default:
		return types.BILLING_PERIOD_MONTHLY
	}
}

/* DevNote: As we only need billing cycle to calculate the billing anchor and we are explicitly setting it to anniversary,
we don't need to calculate the billing cycle from the stripe subscription data.
*/
// func (s *stripeSubscriptionService) calculateBillingCycle(stripeSub *stripe.Subscription) types.BillingCycle {
// 	// In Stripe, if billing_cycle_anchor is set to a specific timestamp,
// 	// it indicates calendar billing (aligned to specific dates).
// 	// If billing_cycle_anchor is "now" or not set, it indicates anniversary billing
// 	// (aligned to subscription start date).

// 	// Check if the subscription has a billing_cycle_anchor set to a specific date
// 	if stripeSub.BillingCycleAnchor != 0 {
// 		// If billing_cycle_anchor is set to a specific timestamp (not "now"),
// 		// it typically indicates calendar-based billing
// 		subscriptionStart := time.Unix(stripeSub.StartDate, 0)
// 		billingAnchor := time.Unix(stripeSub.BillingCycleAnchor, 0)

// 		// If the billing anchor is different from the start date,
// 		// it suggests calendar billing (e.g., aligned to month start)
// 		if !subscriptionStart.Equal(billingAnchor) {
// 			return types.BillingCycleCalendar
// 		}
// 	}

// 	// Default to anniversary billing, which aligns with Stripe's default behavior
// 	// where billing cycles are based on the subscription start date
// 	return types.BillingCycleAnniversary
// }

// createFlexPriceSubscription creates a FlexPrice subscription based on Stripe subscription data
func (s *stripeSubscriptionService) createFlexPriceSubscription(ctx context.Context, stripeSub *stripe.Subscription, customerID, planID string, services *ServiceDependencies) (*dto.SubscriptionResponse, error) {
	subscriptionService := services.SubscriptionService

	billingPeriod := s.calculateBillingPeriod(stripeSub)

	// Set start date
	startDate := time.Unix(stripeSub.StartDate, 0).UTC()

	billingAnchor := time.Unix(stripeSub.BillingCycleAnchor, 0).UTC()

	// Set trial dates if applicable
	var trialStart, trialEnd *time.Time
	if stripeSub.TrialStart != 0 {
		trialStartTime := time.Unix(stripeSub.TrialStart, 0).UTC()
		trialStart = &trialStartTime
	}
	if stripeSub.TrialEnd != 0 {
		trialEndTime := time.Unix(stripeSub.TrialEnd, 0).UTC()
		trialEnd = &trialEndTime
	}

	// Set end date if subscription is canceled
	var endDate *time.Time
	if stripeSub.CancelAt != 0 {
		endDateTime := time.Unix(stripeSub.CancelAt, 0).UTC()
		endDate = &endDateTime
	}

	createReq := dto.CreateSubscriptionRequest{
		CustomerID:         customerID,
		PlanID:             planID,
		Currency:           strings.ToUpper(string(stripeSub.Currency)),
		LookupKey:          stripeSub.ID,
		StartDate:          &startDate,
		EndDate:            endDate,
		TrialStart:         trialStart,
		TrialEnd:           trialEnd,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		BillingPeriod:      billingPeriod,
		BillingCycle:       types.BillingCycleAnniversary,
		BillingAnchor:      &billingAnchor,
		BillingPeriodCount: 1,
		Workflow:           lo.ToPtr(types.TemporalStripeIntegrationWorkflow),
		SubscriptionStatus: s.mapStripeStatusToFlexPrice(stripeSub.Status),
	}

	return subscriptionService.CreateSubscription(ctx, createReq)
}

// createFlexPriceSubscriptionDirect creates a FlexPrice subscription without nested transactions
func (s *stripeSubscriptionService) createFlexPriceSubscriptionWithoutTx(ctx context.Context, stripeSub *stripe.Subscription, services *ServiceDependencies) (*dto.SubscriptionResponse, error) {
	// Step 1: Create or find customer
	customerID, err := s.createOrFindCustomer(ctx, stripeSub, services)
	if err != nil {
		return nil, err
	}

	// Step 2: Create or find plan
	planID, err := s.createOrFindPlan(ctx, stripeSub, services)
	if err != nil {
		return nil, err
	}

	// Step 3: Create subscription directly (without transaction wrapper)

	billingPeriod := s.calculateBillingPeriod(stripeSub)

	// Set start date
	startDate := time.Unix(stripeSub.StartDate, 0).UTC()

	billingAnchor := time.Unix(stripeSub.BillingCycleAnchor, 0).UTC()

	// Set trial dates if applicable
	var trialStart, trialEnd *time.Time
	if stripeSub.TrialStart != 0 {
		trialStartTime := time.Unix(stripeSub.TrialStart, 0).UTC()
		trialStart = &trialStartTime
	}
	if stripeSub.TrialEnd != 0 {
		trialEndTime := time.Unix(stripeSub.TrialEnd, 0).UTC()
		trialEnd = &trialEndTime
	}

	// Set end date if subscription is canceled
	var endDate *time.Time
	if stripeSub.CancelAt != 0 {
		endDateTime := time.Unix(stripeSub.CancelAt, 0).UTC()
		endDate = &endDateTime
	}

	createReq := dto.CreateSubscriptionRequest{
		CustomerID:         customerID,
		PlanID:             planID,
		Currency:           strings.ToUpper(string(stripeSub.Currency)),
		LookupKey:          stripeSub.ID,
		StartDate:          &startDate,
		EndDate:            endDate,
		TrialStart:         trialStart,
		TrialEnd:           trialEnd,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		BillingPeriod:      billingPeriod,
		BillingCycle:       types.BillingCycleAnniversary,
		BillingAnchor:      &billingAnchor,
		BillingPeriodCount: 1,
		Workflow:           lo.ToPtr(types.TemporalStripeIntegrationWorkflow),
		SubscriptionStatus: s.mapStripeStatusToFlexPrice(stripeSub.Status),
	}

	// Create subscription using service (this will use the transaction context)
	subscriptionService := services.SubscriptionService
	subscriptionResp, err := subscriptionService.CreateSubscription(ctx, createReq)
	if err != nil {
		return nil, err
	}

	// Step 4: Create entity mapping
	_, err = services.EntityIntegrationMappingService.CreateEntityIntegrationMapping(ctx, dto.CreateEntityIntegrationMappingRequest{
		EntityID:         subscriptionResp.ID,
		EntityType:       types.IntegrationEntityTypeSubscription,
		ProviderType:     "stripe",
		ProviderEntityID: stripeSub.ID,
		Metadata: map[string]interface{}{
			"created_via":            "stripe_subscription_service",
			"stripe_subscription_id": stripeSub.ID,
			"mapping_type":           "proxy",
			"synced_at":              time.Now().UTC().Format(time.RFC3339),
		},
	})
	if err != nil {
		s.logger.Warnw("failed to create entity mapping for subscription",
			"error", err,
			"subscription_id", subscriptionResp.ID,
			"stripe_subscription_id", stripeSub.ID)
		// Don't fail the entire operation if entity mapping creation fails
	}

	return subscriptionResp, nil
}

func (s *stripeSubscriptionService) isPlanChange(ctx context.Context, existingSubscription *dto.SubscriptionResponse, stripeSubscription *stripe.Subscription, services *ServiceDependencies) (bool, error) {
	// Step 1: Get the exisiting Plan Mapping
	entityMappingService := services.EntityIntegrationMappingService
	filter := &types.EntityIntegrationMappingFilter{
		EntityType:    types.IntegrationEntityTypePlan,
		ProviderTypes: []string{"stripe"},
		EntityID:      existingSubscription.PlanID,
	}
	existingPlanMapping, err := entityMappingService.GetEntityIntegrationMappings(ctx, filter)
	if err != nil {
		return false, ierr.WithError(err).
			WithHint("Failed to get existing plan mapping").
			Mark(ierr.ErrInternal)
	}

	if len(existingPlanMapping.Items) == 0 {
		return false, ierr.NewError("no existing plan mapping found").
			WithHint("Existing plan mapping not found").
			Mark(ierr.ErrInternal)
	}

	existingPlanID := existingPlanMapping.Items[0].ProviderEntityID

	if existingPlanID != stripeSubscription.Items.Data[0].Plan.Product.ID {
		return true, nil
	}

	return false, nil
}

func (s *stripeSubscriptionService) handlePlanChange(ctx context.Context, existingSubscription *dto.SubscriptionResponse, stripeSubscription *stripe.Subscription, services *ServiceDependencies) error {
	s.logger.Infow("handling plan change for subscription",
		"existing_subscription_id", existingSubscription.ID,
		"stripe_subscription_id", stripeSubscription.ID)

	// STEP 1: Cancel existing subscription which will automatically generate proration invoice
	subscriptionService := services.SubscriptionService
	cancelReq := &dto.CancelSubscriptionRequest{
		CancellationType:  types.CancellationTypeImmediate,
		ProrationBehavior: types.ProrationBehaviorNone,
		Reason:            "Plan change - upgrading to new plan",
		SuppressWebhook:   true,
	}

	_, err := subscriptionService.CancelSubscription(ctx, existingSubscription.ID, cancelReq)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to cancel existing subscription during plan change").
			Mark(ierr.ErrInternal)
	}

	s.logger.Infow("successfully cancelled existing subscription",
		"subscription_id", existingSubscription.ID)

	// STEP 2: Delete the old mapping
	entityMappingService := services.EntityIntegrationMappingService

	// Find the existing mapping
	filter := &types.EntityIntegrationMappingFilter{
		EntityType:        types.IntegrationEntityTypeSubscription,
		ProviderTypes:     []string{"stripe"},
		ProviderEntityIDs: []string{stripeSubscription.ID},
	}

	existingMappings, err := entityMappingService.GetEntityIntegrationMappings(ctx, filter)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to find existing entity mapping").
			Mark(ierr.ErrInternal)
	}

	if len(existingMappings.Items) == 0 {
		return ierr.NewError("no existing entity mapping found").
			WithHint("Entity mapping should exist for the subscription").
			Mark(ierr.ErrInternal)
	}

	// Delete the old mapping and create a new one pointing to the new subscription
	existingMapping := existingMappings.Items[0]

	// Delete the old mapping
	err = entityMappingService.DeleteEntityIntegrationMapping(ctx, existingMapping.ID)
	if err != nil {
		s.logger.Warnw("failed to delete old entity mapping for subscription",
			"error", err,
			"mapping_id", existingMapping.ID,
			"old_subscription_id", existingSubscription.ID,
			"stripe_subscription_id", stripeSubscription.ID)
		// Don't fail the entire operation if entity mapping deletion fails
	}

	// STEP 3: Create new subscription (without nested transaction)
	_, err = s.createFlexPriceSubscriptionWithoutTx(ctx, stripeSubscription, services)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to create new subscription during plan change").
			Mark(ierr.ErrInternal)
	}

	return nil
}

func (s *stripeSubscriptionService) handleNormalChange(ctx context.Context, existingSubscription *dto.SubscriptionResponse, stripeSubscription *stripe.Subscription, services *ServiceDependencies) error {
	s.logger.Infow("handling normal subscription change",
		"existing_subscription_id", existingSubscription.ID,
		"stripe_subscription_id", stripeSubscription.ID)

	// Prepare update request with fields that can change
	updateReq := dto.UpdateSubscriptionRequest{
		Status: s.mapStripeStatusToFlexPrice(stripeSubscription.Status),
	}

	// Handle cancellation dates if subscription is cancelled in Stripe
	if stripeSubscription.CancelAt != 0 {
		cancelAt := time.Unix(stripeSubscription.CancelAt, 0).UTC()
		updateReq.CancelAt = &cancelAt
		updateReq.CancelAtPeriodEnd = stripeSubscription.CancelAtPeriodEnd
	}

	// Log the changes that will be made
	s.logger.Infow("subscription changes detected",
		"subscription_id", existingSubscription.ID,
		"stripe_status", stripeSubscription.Status,
		"flexprice_status", updateReq.Status,
		"cancel_at", updateReq.CancelAt,
		"cancel_at_period_end", updateReq.CancelAtPeriodEnd)

	// Update the subscription using the subscription service
	_, err := services.SubscriptionService.UpdateSubscription(ctx, existingSubscription.ID, updateReq)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to update subscription with Stripe changes").
			WithReportableDetails(map[string]interface{}{
				"subscription_id":        existingSubscription.ID,
				"stripe_subscription_id": stripeSubscription.ID,
				"error":                  err.Error(),
			}).
			Mark(ierr.ErrInternal)
	}

	s.logger.Infow("normal subscription change processed successfully",
		"subscription_id", existingSubscription.ID,
		"stripe_subscription_id", stripeSubscription.ID)

	return nil
}

// mapStripeStatusToFlexPrice maps Stripe subscription status to FlexPrice status
func (s *stripeSubscriptionService) mapStripeStatusToFlexPrice(stripeStatus stripe.SubscriptionStatus) types.SubscriptionStatus {
	switch stripeStatus {
	case stripe.SubscriptionStatusActive:
		return types.SubscriptionStatusActive
	case stripe.SubscriptionStatusCanceled:
		return types.SubscriptionStatusCancelled
	case stripe.SubscriptionStatusTrialing:
		return types.SubscriptionStatusActive
	case stripe.SubscriptionStatusIncomplete:
		return types.SubscriptionStatusIncomplete // Or create an incomplete status if needed
	case stripe.SubscriptionStatusPaused:
		return types.SubscriptionStatusPaused
	default:
		return types.SubscriptionStatusActive // Default fallback
	}
}
