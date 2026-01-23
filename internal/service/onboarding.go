package service

import (
	"context"
	"math/rand"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/environment"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/user"
	"github.com/flexprice/flexprice/internal/email"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/pubsub"
	pubsubRouter "github.com/flexprice/flexprice/internal/pubsub/router"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

const (
	OnboardingEventsTopic = "onboarding_events"
)

// OnboardingService handles onboarding-related operations
type OnboardingService interface {
	GenerateEvents(ctx context.Context, req *dto.OnboardingEventsRequest) (*dto.OnboardingEventsResponse, error)
	RegisterHandler(router *pubsubRouter.Router)
	OnboardNewUserWithTenant(ctx context.Context, userID, email, tenantName, tenantID string) error
	SetupSandboxEnvironment(ctx context.Context, tenantID, userID, envID string) error
}

type onboardingService struct {
	ServiceParams
	pubSub pubsub.PubSub
}

// NewOnboardingService creates a new onboarding service
func NewOnboardingService(
	params ServiceParams,
	pubSub pubsub.PubSub,
) OnboardingService {
	return &onboardingService{
		ServiceParams: params,
		pubSub:        pubSub,
	}
}

// GenerateEvents generates events for a specific customer and feature or subscription
func (s *onboardingService) GenerateEvents(ctx context.Context, req *dto.OnboardingEventsRequest) (*dto.OnboardingEventsResponse, error) {
	var customerID string
	meters := make([]types.MeterInfo, 0)
	featureService := NewFeatureService(s.ServiceParams)
	featureFilter := types.NewNoLimitFeatureFilter()
	featureFilter.Expand = lo.ToPtr(string(types.ExpandMeters))

	// If subscription ID is provided, fetch customer and feature information from the subscription
	if req.SubscriptionID != "" {
		// Get subscription
		subscription, subscriptionLineItems, err := s.SubRepo.GetWithLineItems(ctx, req.SubscriptionID)
		if err != nil {
			return nil, err
		}

		// Set customer ID from subscription
		customerID = subscription.CustomerID

		featureFilter.MeterIDs = []string{}
		for _, lineItem := range subscriptionLineItems {
			if lineItem.PriceType == types.PRICE_TYPE_USAGE {
				featureFilter.MeterIDs = append(featureFilter.MeterIDs, lineItem.MeterID)
			}
		}

	} else {
		customerID = req.CustomerID
		featureFilter.FeatureIDs = []string{req.FeatureID}
	}

	customer, err := s.CustomerRepo.Get(ctx, customerID)
	if err != nil {
		return nil, err
	}

	features, err := featureService.GetFeatures(ctx, featureFilter)
	if err != nil {
		return nil, err
	}

	for _, feature := range features.Items {
		meters = append(meters, createMeterInfoFromMeter(feature.Meter))
	}

	if len(meters) == 0 {
		return nil, ierr.NewError("no meters found for feature %s").
			WithHint("No meters found for feature").
			WithReportableDetails(
				map[string]interface{}{
					"feature_id": req.FeatureID,
				},
			).
			Mark(ierr.ErrValidation)
	}

	// Set the customer and feature IDs in the request for logging
	selectedFeature := features.Items[0]
	req.CustomerID = customerID
	req.FeatureID = selectedFeature.ID

	// Create a message with the request details
	msg := &types.OnboardingEventsMessage{
		CustomerID:       customerID,
		CustomerExtID:    customer.ExternalID,
		FeatureID:        selectedFeature.ID,
		FeatureName:      selectedFeature.Name,
		Duration:         req.Duration,
		Meters:           meters,
		TenantID:         types.GetTenantID(ctx),
		EnvironmentID:    types.GetEnvironmentID(ctx),
		UserID:           types.GetUserID(ctx),
		RequestTimestamp: time.Now(),
		SubscriptionID:   req.SubscriptionID,
	}

	// Publish the message to the onboarding events topic
	messageID := watermill.NewUUID()
	payload, err := msg.Marshal()
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to marshal message").
			Mark(ierr.ErrValidation)
	}

	watermillMsg := message.NewMessage(messageID, payload)
	watermillMsg.Metadata.Set("tenant_id", types.GetTenantID(ctx))
	watermillMsg.Metadata.Set("environment_id", types.GetEnvironmentID(ctx))
	watermillMsg.Metadata.Set("user_id", types.GetUserID(ctx))

	s.Logger.Infow("publishing onboarding events message",
		"message_id", messageID,
		"customer_id", customerID,
		"feature_id", selectedFeature.ID,
		"subscription_id", req.SubscriptionID,
		"duration", req.Duration,
	)

	if err := s.pubSub.Publish(ctx, OnboardingEventsTopic, watermillMsg); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to publish message").
			Mark(ierr.ErrValidation)
	}

	return &dto.OnboardingEventsResponse{
		Message:        "Event generation started",
		StartedAt:      time.Now(),
		Duration:       req.Duration,
		Count:          req.Duration * 5, // Five events per second
		CustomerID:     customerID,
		FeatureID:      selectedFeature.ID,
		SubscriptionID: req.SubscriptionID,
	}, nil
}

// Helper function to create MeterInfo from a Meter
func createMeterInfoFromMeter(m *dto.MeterResponse) types.MeterInfo {
	filterInfos := make([]types.FilterInfo, len(m.Filters))
	for j, f := range m.Filters {
		filterInfos[j] = types.FilterInfo{
			Key:    f.Key,
			Values: f.Values,
		}
	}

	return types.MeterInfo{
		ID:        m.ID,
		EventName: m.EventName,
		Aggregation: types.AggregationInfo{
			Type:  m.Aggregation.Type,
			Field: m.Aggregation.Field,
		},
		Filters: filterInfos,
	}
}

// RegisterHandler registers a handler for onboarding events
func (s *onboardingService) RegisterHandler(router *pubsubRouter.Router) {
	router.AddNoPublishHandler(
		"onboarding_events_handler",
		OnboardingEventsTopic,
		s.pubSub,
		s.processMessage,
	)
}

// processMessage processes a single onboarding event message
func (s *onboardingService) processMessage(msg *message.Message) error {
	// We don't need the message context anymore since we're using a background context
	// Just log the message UUID for tracing
	s.Logger.Debugw("received onboarding event message", "message_uuid", msg.UUID)

	// Unmarshal the message
	var eventMsg types.OnboardingEventsMessage
	if err := eventMsg.Unmarshal(msg.Payload); err != nil {
		s.Logger.Errorw("failed to unmarshal onboarding event message",
			"error", err,
			"message_uuid", msg.UUID,
		)
		return nil // Don't retry on unmarshal errors
	}

	s.Logger.Infow("processing onboarding events",
		"customer_id", eventMsg.CustomerID,
		"feature_id", eventMsg.FeatureID,
		"subscription_id", eventMsg.SubscriptionID,
		"duration", eventMsg.Duration,
		"meters_count", len(eventMsg.Meters),
	)

	// Create a new background context instead of using the message context
	// This prevents the event generation from being cancelled when the HTTP request completes
	bgCtx := context.Background()

	// Copy tenant ID from original context to background context
	bgCtx = context.WithValue(bgCtx, types.CtxTenantID, eventMsg.TenantID)
	bgCtx = context.WithValue(bgCtx, types.CtxEnvironmentID, eventMsg.EnvironmentID)
	bgCtx = context.WithValue(bgCtx, types.CtxUserID, eventMsg.UserID)

	// Start a goroutine to generate events at a rate of 1 per second
	go s.generateEvents(bgCtx, &eventMsg)

	return nil
}

// generateEvents generates events at a rate of 1 per second
func (s *onboardingService) generateEvents(ctx context.Context, eventMsg *types.OnboardingEventsMessage) {
	eventService := NewEventService(s.EventRepo, s.MeterRepo, s.EventPublisher, s.Logger, s.Config)

	// Calculate total events to generate
	totalEvents := eventMsg.Duration * 5
	numMeters := len(eventMsg.Meters)

	if numMeters == 0 {
		s.Logger.Warnw("no meters found, skipping event generation",
			"customer_id", eventMsg.CustomerID,
			"feature_id", eventMsg.FeatureID,
		)
		return
	}

	// Calculate events per meter using floor division
	baseEventsPerMeter := totalEvents / numMeters
	remainder := totalEvents % numMeters

	// Create a counter for successful events
	successCount := 0
	errorCount := 0

	s.Logger.Infow("starting event generation",
		"customer_id", eventMsg.CustomerID,
		"feature_id", eventMsg.FeatureID,
		"duration", eventMsg.Duration,
		"total_events", totalEvents,
		"num_meters", numMeters,
		"base_events_per_meter", baseEventsPerMeter,
		"remainder", remainder,
	)

	// Create a ticker to generate events at a rate of 5 per second
	ticker := time.NewTicker(time.Millisecond * 200)
	defer ticker.Stop()

	// Generate events for each meter with proper distribution
	for meterIdx, meter := range eventMsg.Meters {
		// Calculate events for this specific meter
		eventsForThisMeter := baseEventsPerMeter
		if meterIdx < remainder {
			eventsForThisMeter++ // Give +1 extra event to first 'remainder' meters
		}

		s.Logger.Infow("generating events for meter",
			"meter_index", meterIdx+1,
			"meter_name", meter.EventName,
			"events_to_generate", eventsForThisMeter,
		)

		// Generate the allocated events for this meter
		for i := 0; i < eventsForThisMeter; i++ {
			select {
			case <-ticker.C:
				// Create event request
				eventReq := s.createEventRequest(eventMsg, &meter)

				// Ingest the event
				if err := eventService.CreateEvent(ctx, &eventReq); err != nil {
					errorCount++
					s.Logger.Errorw("failed to create event",
						"error", err,
						"customer_id", eventMsg.CustomerID,
						"event_name", meter.EventName,
						"meter_index", meterIdx+1,
						"event_number", i+1,
						"total_events_for_meter", eventsForThisMeter,
					)
					continue
				}

				successCount++
				s.Logger.Infow("created onboarding event",
					"customer_id", eventMsg.CustomerID,
					"event_name", meter.EventName,
					"event_id", eventReq.EventID,
					"meter_index", meterIdx+1,
					"event_number", i+1,
					"total_events_for_meter", eventsForThisMeter,
				)
			case <-ctx.Done():
				s.Logger.Warnw("context cancelled, stopping event generation",
					"customer_id", eventMsg.CustomerID,
					"feature_id", eventMsg.FeatureID,
					"events_generated", successCount,
					"events_failed", errorCount,
					"total_expected", totalEvents,
					"reason", ctx.Err(),
				)
				return
			}
		}
	}

	s.Logger.Infow("completed generating onboarding events",
		"customer_id", eventMsg.CustomerID,
		"feature_id", eventMsg.FeatureID,
		"duration", eventMsg.Duration,
		"total_events_expected", totalEvents,
		"events_generated", successCount,
		"events_failed", errorCount,
	)
}

// createEventRequest creates an event request for a meter
func (s *onboardingService) createEventRequest(eventMsg *types.OnboardingEventsMessage, meter *types.MeterInfo) dto.IngestEventRequest {
	// Generate properties based on meter configuration
	properties := make(map[string]interface{})

	// Handle properties based on meter aggregation and filters
	if meter.Aggregation.Type == types.AggregationSum ||
		meter.Aggregation.Type == types.AggregationCountUnique ||
		meter.Aggregation.Type == types.AggregationAvg {
		// For sum/avg aggregation, we need to generate a value for the aggregation field
		if meter.Aggregation.Field != "" {
			// Generate a random value between 1 and 100
			properties[meter.Aggregation.Field] = rand.Int63n(100) + 1
		}
	}

	// Apply filter values if available
	for _, filter := range meter.Filters {
		if len(filter.Values) > 0 {
			// Select a random value from the filter values
			properties[filter.Key] = filter.Values[rand.Intn(len(filter.Values))]
		}
	}

	return dto.IngestEventRequest{
		EventID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_EVENT),
		ExternalCustomerID: eventMsg.CustomerExtID,
		EventName:          meter.EventName,
		Timestamp:          time.Now(),
		Properties:         properties,
		Source:             "onboarding",
	}
}

// OnboardNewUserWithTenant creates a new tenant, assigns it to the user, and sets up default environments
func (s *onboardingService) OnboardNewUserWithTenant(ctx context.Context, userID, email, tenantName, tenantID string) error {
	// Use default tenant name if not provided
	if tenantName == "" {
		tenantName = "Flexprice"
	}

	tenantService := NewTenantService(s.ServiceParams)

	resp, err := tenantService.CreateTenant(ctx, dto.CreateTenantRequest{
		Name: tenantName,
		ID:   tenantID,
	})
	if err != nil {
		return err
	}

	tenantID = resp.ID

	// Create a new user without a tenant ID initially
	newUser := &user.User{
		ID:    userID,
		Email: email,
		BaseModel: types.BaseModel{
			TenantID:  tenantID,
			Status:    types.StatusPublished,
			CreatedBy: userID,
			UpdatedBy: userID,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}

	if err := s.UserRepo.Create(ctx, newUser); err != nil {
		return err
	}

	// Create default environments (development, production, sandbox)
	envTypes := []types.EnvironmentType{
		types.EnvironmentDevelopment,
	}

	for _, envType := range envTypes {
		env := &environment.Environment{
			ID:   types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENVIRONMENT),
			Name: envType.DisplayTitle(),
			Type: envType,
			BaseModel: types.BaseModel{
				TenantID:  tenantID,
				Status:    types.StatusPublished,
				CreatedBy: userID,
				UpdatedBy: userID,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
		}

		if err := s.EnvironmentRepo.Create(ctx, env); err != nil {
			return err
		}

		if envType == types.EnvironmentDevelopment {
			if err := s.SetupSandboxEnvironment(ctx, tenantID, userID, env.ID); err != nil {
				return err
			}
		}
	}

	// Send onboarding email
	if err := s.sendOnboardingEmail(ctx, email); err != nil {
		// Log error but don't fail the onboarding process
		s.Logger.Errorw("failed to send onboarding email",
			"error", err,
			"email", email,
			"user_id", userID,
		)
	}

	return nil
}

// SetupSandboxEnvironment sets up the sandbox environment with generic AI-focused features for hackathon participants
func (s *onboardingService) SetupSandboxEnvironment(ctx context.Context, tenantID, userID, envID string) error {
	// Set tenant ID in context
	ctx = context.WithValue(ctx, types.CtxTenantID, tenantID)

	// Set environment ID in context
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, envID)

	// Set user ID in context
	ctx = context.WithValue(ctx, types.CtxUserID, userID)

	// validate if development environment
	env, err := s.EnvironmentRepo.Get(ctx, envID)
	if err != nil {
		return err
	}

	if env.Type != types.EnvironmentDevelopment {
		return ierr.NewError("environment to set up data must be a development environment").
			WithHint("Can only set up data for development environment").
			Mark(ierr.ErrInvalidOperation)
	}

	// create a db transaction
	err = s.DB.WithTx(ctx, func(ctx context.Context) error {
		s.Logger.Infow("setting up sandbox environment with generic AI pricing model for hackathon",
			"tenant_id", tenantID,
			"user_id", userID,
			"environment_id", envID,
		)

		// Step 1: Create meters
		meters, err := s.createDefaultMeters(ctx)
		if err != nil {
			return err
		}

		// Step 2: Create features using the meters
		features, err := s.createDefaultFeatures(ctx, meters)
		if err != nil {
			return err
		}

		// Step 3: Create plans (Starter, Basic, Pro)
		plans, err := s.createDefaultPlans(ctx, features, meters)
		if err != nil {
			return err
		}

		// Step 4: Create customers
		customers, err := s.createDefaultCustomers(ctx)
		if err != nil {
			return err
		}

		// Step 5: Create subscriptions for the customers and plans
		err = s.createDefaultSubscriptions(ctx, customers, plans)
		if err != nil {
			return err
		}

		s.Logger.Infow("successfully set up sandbox environment with generic AI pricing model",
			"tenant_id", tenantID,
			"user_id", userID,
			"environment_id", envID,
		)

		return nil
	})

	return err
}

func (s *onboardingService) createDefaultMeters(ctx context.Context) ([]*meter.Meter, error) {
	s.Logger.Infow("creating AI-focused meters for hackathon environment")

	// Create a meter service instance
	meterService := NewMeterService(s.MeterRepo)

	// Define simple LLM usage meter
	modelFilters := []meter.Filter{
		{
			Key:    "model",
			Values: []string{"gpt-4", "gpt-3.5-turbo", "claude-3", "claude-2", "llama-2", "palm-2"},
		},
	}

	llmUsageMeter := &dto.CreateMeterRequest{
		Name:      "LLM Usage",
		EventName: "llm_usage",
		Aggregation: meter.Aggregation{
			Type:  types.AggregationSum,
			Field: "value",
		},
		Filters: modelFilters,
	}

	// Create meter
	llmUsageResp, err := meterService.CreateMeter(ctx, llmUsageMeter)
	if err != nil {
		return nil, err
	}
	s.Logger.Infow("created LLM usage meter", "meter_id", llmUsageResp.ID)

	return []*meter.Meter{llmUsageResp}, nil
}

func (s *onboardingService) createDefaultFeatures(ctx context.Context, meters []*meter.Meter) ([]*dto.FeatureResponse, error) {
	s.Logger.Infow("creating simple LLM usage feature for hackathon environment")

	var llmUsageMeter *meter.Meter
	for _, m := range meters {
		if m.Name == "LLM Usage" {
			llmUsageMeter = m
			break
		}
	}

	// Create a feature service instance
	featureService := NewFeatureService(s.ServiceParams)

	// Define single LLM usage feature
	feature := dto.CreateFeatureRequest{
		Name:        "LLM Usage",
		Description: "LLM API usage and requests",
		Type:        types.FeatureTypeMetered,
		LookupKey:   "llm_usage",
		MeterID:     llmUsageMeter.ID,
	}

	// Create feature
	resp, err := featureService.CreateFeature(ctx, feature)
	if err != nil {
		return nil, err
	}
	s.Logger.Infow("created feature",
		"feature_id", resp.ID,
		"feature_name", resp.Name,
		"feature_type", resp.Type,
	)

	return []*dto.FeatureResponse{resp}, nil
}

func (s *onboardingService) createDefaultPlans(ctx context.Context, features []*dto.FeatureResponse, meters []*meter.Meter) ([]*dto.CreatePlanResponse, error) {
	s.Logger.Infow("creating AI-focused plans for hackathon environment")

	// Create a plan service instance with all required dependencies
	planService := NewPlanService(
		s.ServiceParams,
	)

	// Define plans based on AI usage tiers
	plans := []*dto.CreatePlanRequest{
		{
			Name:        "Pro",
			Description: "Professional tier with unlimited AI usage",
			LookupKey:   "pro",
		},
		{
			Name:        "Basic",
			Description: "Basic tier with moderate AI usage limits",
			LookupKey:   "basic",
		},
		{
			Name:        "Starter",
			Description: "Starter tier for getting started with AI",
			LookupKey:   "starter",
		},
	}

	// Create each plan first
	planResponses := make([]*dto.CreatePlanResponse, 0, len(plans))
	for i := range plans {
		resp, err := planService.CreatePlan(ctx, lo.FromPtr(plans[i]))
		if err != nil {
			return nil, err
		}
		s.Logger.Infow("created plan",
			"plan_id", resp.ID,
			"plan_name", resp.Name,
		)

		planResponses = append(planResponses, resp)
	}

	// Create prices for each plan using the new flow
	priceService := NewPriceService(s.ServiceParams)
	err := s.createDefaultPrices(ctx, planResponses, priceService)
	if err != nil {
		return nil, err
	}

	return planResponses, nil
}

// createDefaultPrices creates prices for plans using the new flow (separate from plan creation)
func (s *onboardingService) createDefaultPrices(ctx context.Context, planResponses []*dto.CreatePlanResponse, priceService PriceService) error {
	s.Logger.Infow("creating AI-focused prices for hackathon environment")

	// Find plans by name
	var starterPlan, basicPlan, proPlan *dto.CreatePlanResponse
	for _, p := range planResponses {
		switch p.Name {
		case "Starter":
			starterPlan = p
		case "Basic":
			basicPlan = p
		case "Pro":
			proPlan = p
		}
	}

	// Validate that we found all required plans
	if starterPlan == nil || basicPlan == nil || proPlan == nil {
		return ierr.NewError("not all required plans were found").
			WithHint("Not all required plans were found").
			Mark(ierr.ErrValidation)
	}

	// Starter Plan - Free tier
	starterPriceReq := dto.CreatePriceRequest{
		Amount:             lo.ToPtr(decimal.Zero),
		Currency:           "USD",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           starterPlan.ID,
		Type:               types.PRICE_TYPE_FIXED,
		PriceUnitType:      types.PRICE_UNIT_TYPE_FIAT,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		Description:        "Free tier with usage limits",
		// DisplayName will be automatically extracted by getDisplayName helper
	}
	_, err := priceService.CreatePrice(ctx, starterPriceReq)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to create price for Starter plan").
			Mark(ierr.ErrDatabase)
	}

	// Basic Plan - $10/month
	basicPriceReq := dto.CreatePriceRequest{
		Amount:             lo.ToPtr(decimal.NewFromInt(10)),
		Currency:           "USD",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           basicPlan.ID,
		Type:               types.PRICE_TYPE_FIXED,
		PriceUnitType:      types.PRICE_UNIT_TYPE_FIAT,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		Description:        "Basic tier with moderate usage",
		// DisplayName will be automatically extracted by getDisplayName helper
	}
	_, err = priceService.CreatePrice(ctx, basicPriceReq)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to create price for Basic plan").
			Mark(ierr.ErrDatabase)
	}

	// Pro Plan - $50/month
	proPriceReq := dto.CreatePriceRequest{
		Amount:             lo.ToPtr(decimal.NewFromInt(50)),
		Currency:           "USD",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           proPlan.ID,
		Type:               types.PRICE_TYPE_FIXED,
		PriceUnitType:      types.PRICE_UNIT_TYPE_FIAT,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		Description:        "Pro tier with high usage limits",
		// DisplayName will be automatically extracted by getDisplayName helper
	}
	_, err = priceService.CreatePrice(ctx, proPriceReq)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to create price for Pro plan").
			Mark(ierr.ErrDatabase)
	}

	s.Logger.Infow("created prices for all plans")
	return nil
}

func (s *onboardingService) createDefaultCustomers(ctx context.Context) ([]*dto.CustomerResponse, error) {
	s.Logger.Infow("creating default customers for Cursor pricing model")

	// Create a customer service instance
	customerService := NewCustomerService(s.ServiceParams)

	// Create a default customer
	customer := dto.CreateCustomerRequest{
		Name:       "Demo User",
		Email:      "demo@example.com",
		ExternalID: "demo_user_123",
	}

	resp, err := customerService.CreateCustomer(ctx, customer)
	if err != nil {
		return nil, err
	}

	s.Logger.Infow("created customer",
		"customer_id", resp.ID,
		"customer_name", resp.Name,
		"customer_email", resp.Email,
	)

	return []*dto.CustomerResponse{resp}, nil
}

func (s *onboardingService) createDefaultSubscriptions(ctx context.Context, customers []*dto.CustomerResponse, plans []*dto.CreatePlanResponse) error {
	s.Logger.Infow("creating default subscriptions for Cursor pricing model")
	subscriptionService := NewSubscriptionService(s.ServiceParams)

	// Validate that we have at least one customer
	if len(customers) == 0 {
		return ierr.NewError("no customers found to create subscriptions for").
			WithHint("No customers found to create subscriptions for").
			Mark(ierr.ErrValidation)
	}

	// Find the Pro plan
	var proPlan *dto.CreatePlanResponse
	for _, p := range plans {
		if p.Name == "Pro" {
			proPlan = p
			break
		}
	}

	if proPlan == nil {
		return ierr.NewError("pro plan not found").
			WithHint("Pro plan not found").
			Mark(ierr.ErrValidation)
	}

	// Get the first customer
	customer := customers[0]

	// Create a subscription for the customer
	subscription := dto.CreateSubscriptionRequest{
		CustomerID:         customer.ID,
		PlanID:             proPlan.ID,
		Currency:           "USD",
		StartDate:          lo.ToPtr(time.Now()),
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingCycle:       types.BillingCycleAnniversary,
	}

	resp, err := subscriptionService.CreateSubscription(ctx, subscription)
	if err != nil {
		return err
	}

	s.Logger.Infow("created subscription",
		"subscription_id", resp.ID,
		"subscription_status", resp.Status,
	)

	return nil
}

// sendOnboardingEmail sends a welcome email to a new user
func (s *onboardingService) sendOnboardingEmail(ctx context.Context, toEmail string) error {
	// Create email client
	emailClient := email.NewEmailClient(email.Config{
		Enabled:     s.Config.Email.Enabled,
		APIKey:      s.Config.Email.ResendAPIKey,
		FromAddress: s.Config.Email.FromAddress,
		ReplyTo:     s.Config.Email.ReplyTo,
	})

	if !emailClient.IsEnabled() {
		s.Logger.Debugw("email service is disabled, skipping onboarding email")
		return nil
	}

	// Create email service
	emailSvc := email.NewEmail(emailClient, s.Logger.Desugar())

	// Build template data from config
	configData := map[string]string{
		"calendar_url": s.Config.Email.CalendarURL,
	}

	templateData := email.BuildTemplateData(configData, toEmail)

	// Send email using template
	resp, err := emailSvc.SendEmailWithTemplate(ctx, email.SendEmailWithTemplateRequest{
		FromAddress:  s.Config.Email.FromAddress,
		ToAddress:    toEmail,
		Subject:      "Welcome to Flexprice!",
		TemplatePath: "welcome-email.html",
		Data:         templateData,
	})
	if err != nil {
		return err
	}

	if !resp.Success {
		s.Logger.Errorw("email send was not successful", "error", resp.Error)
	}

	return nil
}
