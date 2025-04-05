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
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/domain/user"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/pubsub"
	pubsubRouter "github.com/flexprice/flexprice/internal/pubsub/router"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
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
	featureService := NewFeatureService(s.FeatureRepo, s.MeterRepo, s.Logger)
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

	// Start a goroutine to generate events at a rate of 1 per second
	go s.generateEvents(bgCtx, &eventMsg)

	return nil
}

// generateEvents generates events at a rate of 1 per second
func (s *onboardingService) generateEvents(ctx context.Context, eventMsg *types.OnboardingEventsMessage) {
	eventService := NewEventService(s.EventRepo, s.MeterRepo, s.EventPublisher, s.Logger)

	// Create a ticker to generate events at a rate of 5 per second
	ticker := time.NewTicker(time.Millisecond * 200)
	defer ticker.Stop()

	// Create a counter for successful events
	successCount := 0
	errorCount := 0

	s.Logger.Infow("starting event generation",
		"customer_id", eventMsg.CustomerID,
		"feature_id", eventMsg.FeatureID,
		"duration", eventMsg.Duration,
	)

	// multiply duration by 5
	duration := eventMsg.Duration * 5

	// Generate events
	for i := 0; i < duration; i++ {
		select {
		case <-ticker.C:
			// Generate an event for each meter
			for _, meter := range eventMsg.Meters {
				// Create event request
				eventReq := s.createEventRequest(eventMsg, &meter)

				// Ingest the event
				if err := eventService.CreateEvent(ctx, &eventReq); err != nil {
					errorCount++
					s.Logger.Errorw("failed to create event",
						"error", err,
						"customer_id", eventMsg.CustomerID,
						"event_name", meter.EventName,
						"event_number", i+1,
						"total_events", eventMsg.Duration,
					)
					continue
				}

				successCount++
				s.Logger.Infow("created onboarding event",
					"customer_id", eventMsg.CustomerID,
					"event_name", meter.EventName,
					"event_id", eventReq.EventID,
					"event_number", i+1,
					"total_events", eventMsg.Duration,
				)
			}
		case <-ctx.Done():
			s.Logger.Warnw("context cancelled, stopping event generation",
				"customer_id", eventMsg.CustomerID,
				"feature_id", eventMsg.FeatureID,
				"events_generated", successCount,
				"events_failed", errorCount,
				"total_expected", eventMsg.Duration,
				"reason", ctx.Err(),
			)
			return
		}
	}

	s.Logger.Infow("completed generating onboarding events",
		"customer_id", eventMsg.CustomerID,
		"feature_id", eventMsg.FeatureID,
		"duration", eventMsg.Duration,
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

	// Create tenant
	newTenant := &tenant.Tenant{
		ID:        tenantID,
		Name:      tenantName,
		Status:    types.StatusPublished,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := s.TenantRepo.Create(ctx, newTenant); err != nil {
		return err
	}

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

	// Create default environments (development, production)
	envTypes := []types.EnvironmentType{
		types.EnvironmentDevelopment,
		types.EnvironmentProduction,
	}

	sandboxEnvironmentID := ""
	for _, envType := range envTypes {
		env := &environment.Environment{
			ID:   types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENVIRONMENT),
			Name: envType.DisplayTitle(),
			Type: envType,
			BaseModel: types.BaseModel{
				TenantID:  newTenant.ID,
				Status:    types.StatusPublished,
				CreatedBy: userID,
				UpdatedBy: userID,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
		}

		if envType == types.EnvironmentDevelopment {
			sandboxEnvironmentID = env.ID
		}

		if err := s.EnvironmentRepo.Create(ctx, env); err != nil {
			return err
		}
	}

	err := s.SetupSandboxEnvironment(ctx, tenantID, userID, sandboxEnvironmentID)
	if err != nil {
		return err
	}

	return nil
}

// SetupSandboxEnvironment sets up the sandbox environment with https://www.cursor.com/pricing as an example
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
		s.Logger.Infow("setting up sandbox environment with Cursor pricing model",
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

		// Step 3: Create plans (Hobby, Pro, Business)
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

		// Optional steps - can be skipped for now
		// Step 6: Create wallets
		// Step 7: Create invoices
		// Step 8: Create payments

		s.Logger.Infow("successfully set up sandbox environment with Cursor pricing model",
			"tenant_id", tenantID,
			"user_id", userID,
			"environment_id", envID,
		)

		return nil
	})

	return err
}

func (s *onboardingService) createDefaultMeters(ctx context.Context) ([]*meter.Meter, error) {
	s.Logger.Infow("creating default meters for Cursor pricing model")

	// Create a meter service instance
	meterService := NewMeterService(s.MeterRepo)

	// Define meters based on Cursor pricing
	modelFilters := []meter.Filter{
		{
			Key:    "model",
			Values: []string{"gpt-4o", "gpt-4o-mini", "claude-3-5-sonnet", "claude-3-7-sonnet"},
		},
	}

	completionsMeter := &dto.CreateMeterRequest{
		Name:      "Completions",
		EventName: "completion",
		Aggregation: meter.Aggregation{
			Type: types.AggregationCount,
		},
		Filters: modelFilters,
	}

	slowPremiumRequestsMeter := &dto.CreateMeterRequest{
		Name:      "Premium Requests (Slow)",
		EventName: "premium_request_slow",
		Aggregation: meter.Aggregation{
			Type:  types.AggregationSum,
			Field: "tool_calls",
		},
		Filters: modelFilters,
	}

	fastPremiumRequestsMeter := &dto.CreateMeterRequest{
		Name:      "Premium Requests (Fast)",
		EventName: "premium_request_fast",
		Aggregation: meter.Aggregation{
			Type:  types.AggregationSum,
			Field: "tool_calls",
		},
		Filters: modelFilters,
	}

	// Create meters
	completionsResp, err := meterService.CreateMeter(ctx, completionsMeter)
	if err != nil {
		return nil, err
	}
	s.Logger.Infow("created completions meter", "meter_id", completionsResp.ID)

	slowPremiumResp, err := meterService.CreateMeter(ctx, slowPremiumRequestsMeter)
	if err != nil {
		return nil, err
	}
	s.Logger.Infow("created slow premium requests meter", "meter_id", slowPremiumResp.ID)

	fastPremiumResp, err := meterService.CreateMeter(ctx, fastPremiumRequestsMeter)
	if err != nil {
		return nil, err
	}
	s.Logger.Infow("created fast premium requests meter", "meter_id", fastPremiumResp.ID)

	return []*meter.Meter{completionsResp, slowPremiumResp, fastPremiumResp}, nil
}

func (s *onboardingService) createDefaultFeatures(ctx context.Context, meters []*meter.Meter) ([]*dto.FeatureResponse, error) {
	s.Logger.Infow("creating default features for Cursor pricing model")

	var completionsMeter, slowPremiumMeter, fastPremiumMeter *meter.Meter
	for _, m := range meters {
		switch m.Name {
		case "Completions":
			completionsMeter = m
		case "Premium Requests (Slow)":
			slowPremiumMeter = m
		case "Premium Requests (Fast)":
			fastPremiumMeter = m
		}
	}

	// Create a feature service instance
	featureService := NewFeatureService(s.FeatureRepo, s.MeterRepo, s.Logger)

	// Define features based on Cursor pricing
	features := []dto.CreateFeatureRequest{
		{
			Name:        "SSO",
			Description: "SAML/OIDC SSO integration",
			Type:        types.FeatureTypeStatic,
			LookupKey:   "sso",
		},
		{
			Name:        "Admin Dashboard",
			Description: "Admin dashboard with usage statistics",
			Type:        types.FeatureTypeBoolean,
			LookupKey:   "admin_dashboard",
		},
		{
			Name:        "Team Billing",
			Description: "Centralized team billing",
			Type:        types.FeatureTypeBoolean,
			LookupKey:   "team_billing",
		},
		{
			Name:        "Privacy Mode",
			Description: "Enforce privacy mode across the organization",
			Type:        types.FeatureTypeBoolean,
			LookupKey:   "privacy_mode",
		},
		{
			Name:        "Premium Requests (Fast)",
			Description: "Fast premium requests for AI assistance",
			Type:        types.FeatureTypeMetered,
			LookupKey:   "premium_requests_fast",
			MeterID:     fastPremiumMeter.ID,
		},
		{
			Name:        "Premium Requests (Slow)",
			Description: "Slow premium requests for AI assistance",
			Type:        types.FeatureTypeMetered,
			LookupKey:   "premium_requests_slow",
			MeterID:     slowPremiumMeter.ID,
		},
		{
			Name:        "Completions",
			Description: "AI completions for code generation and assistance",
			Type:        types.FeatureTypeMetered,
			LookupKey:   "completions",
			MeterID:     completionsMeter.ID,
		},
	}

	// Create each feature and collect responses
	featureResponses := make([]*dto.FeatureResponse, 0, len(features))
	for i := range features {
		resp, err := featureService.CreateFeature(ctx, features[i])
		if err != nil {
			return nil, err
		}
		s.Logger.Infow("created feature",
			"feature_id", resp.ID,
			"feature_name", resp.Name,
			"feature_type", resp.Type,
		)
		featureResponses = append(featureResponses, resp)
	}

	return featureResponses, nil
}

func (s *onboardingService) createDefaultPlans(ctx context.Context, features []*dto.FeatureResponse, meters []*meter.Meter) ([]*dto.CreatePlanResponse, error) {
	s.Logger.Infow("creating default plans for Cursor pricing model")

	// Create a plan service instance with all required dependencies
	planService := NewPlanService(
		s.DB,
		s.PlanRepo,
		s.PriceRepo,
		s.MeterRepo,
		s.EntitlementRepo,
		s.FeatureRepo,
		s.Logger,
	)

	// Define plans based on Cursor pricing
	plans := []*dto.CreatePlanRequest{
		{
			Name:        "Business",
			Description: "Business tier with team features",
			LookupKey:   "business",
		},
		{
			Name:        "Pro",
			Description: "Professional tier with unlimited completions",
			LookupKey:   "pro",
		},
		{
			Name:        "Hobby",
			Description: "Free tier for personal use",
			LookupKey:   "hobby",
		},
	}

	// Create prices for the plans and meters
	err := s.setDefaultPriceRequests(ctx, plans, meters)
	if err != nil {
		return nil, err
	}

	// Create entitlements for the plans and features
	err = s.setDefaultEntitlements(ctx, plans, features)
	if err != nil {
		return nil, err
	}

	// Create each plan and collect responses
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

	return planResponses, nil
}

func (s *onboardingService) setDefaultPriceRequests(_ context.Context, plans []*dto.CreatePlanRequest, meters []*meter.Meter) error {
	s.Logger.Infow("creating default prices for Cursor pricing model")

	// Find plans by name
	var hobbyPlan, proPlan, businessPlan *dto.CreatePlanRequest
	for _, p := range plans {
		switch p.Name {
		case "Hobby":
			hobbyPlan = p
		case "Pro":
			proPlan = p
		case "Business":
			businessPlan = p
		}
	}

	// Find meters by name
	var completionsMeter, slowPremiumMeter, fastPremiumMeter *meter.Meter
	for _, m := range meters {
		switch m.Name {
		case "Completions":
			completionsMeter = m
		case "Premium Requests (Slow)":
			slowPremiumMeter = m
		case "Premium Requests (Fast)":
			fastPremiumMeter = m
		}
	}

	// Validate that we found all required plans and meters
	if hobbyPlan == nil || proPlan == nil || businessPlan == nil {
		return ierr.NewError("not all required plans were found").
			WithHint("Not all required plans were found").
			Mark(ierr.ErrValidation)
	}

	if completionsMeter == nil || slowPremiumMeter == nil || fastPremiumMeter == nil {
		return ierr.NewError("not all required meters were found").
			WithHint("Not all required meters were found").
			Mark(ierr.ErrValidation)
	}

	usageBasedPriceRequests := []dto.CreatePlanPriceRequest{
		{
			CreatePriceRequest: &dto.CreatePriceRequest{
				Amount:             "0.05",
				Currency:           "USD",
				Type:               types.PRICE_TYPE_USAGE,
				BillingModel:       types.BILLING_MODEL_PACKAGE,
				BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
				BillingPeriodCount: 1,
				BillingCadence:     types.BILLING_CADENCE_RECURRING,
				InvoiceCadence:     types.InvoiceCadenceArrear,
				MeterID:            completionsMeter.ID,
				TransformQuantity: &price.TransformQuantity{
					DivideBy: 100,
				},
			},
		}, {
			CreatePriceRequest: &dto.CreatePriceRequest{
				Amount:             "0.05",
				Currency:           "USD",
				Type:               types.PRICE_TYPE_USAGE,
				BillingModel:       types.BILLING_MODEL_PACKAGE,
				BillingPeriod:      types.BILLING_PERIOD_ANNUAL,
				BillingPeriodCount: 1,
				BillingCadence:     types.BILLING_CADENCE_RECURRING,
				InvoiceCadence:     types.InvoiceCadenceArrear,
				MeterID:            completionsMeter.ID,
				TransformQuantity: &price.TransformQuantity{
					DivideBy: 100,
				},
			},
		},
		{
			CreatePriceRequest: &dto.CreatePriceRequest{
				Amount:             "0.1",
				Currency:           "USD",
				Type:               types.PRICE_TYPE_USAGE,
				BillingModel:       types.BILLING_MODEL_PACKAGE,
				BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
				BillingPeriodCount: 1,
				BillingCadence:     types.BILLING_CADENCE_RECURRING,
				InvoiceCadence:     types.InvoiceCadenceArrear,
				MeterID:            fastPremiumMeter.ID,
				TransformQuantity: &price.TransformQuantity{
					DivideBy: 100,
				},
			},
		},
		{
			CreatePriceRequest: &dto.CreatePriceRequest{
				Amount:             "0.1",
				Currency:           "USD",
				Type:               types.PRICE_TYPE_USAGE,
				BillingModel:       types.BILLING_MODEL_PACKAGE,
				BillingPeriod:      types.BILLING_PERIOD_ANNUAL,
				BillingPeriodCount: 1,
				BillingCadence:     types.BILLING_CADENCE_RECURRING,
				InvoiceCadence:     types.InvoiceCadenceArrear,
				MeterID:            fastPremiumMeter.ID,
				TransformQuantity: &price.TransformQuantity{
					DivideBy: 100,
				},
			},
		},
	}

	hobbyPlan.Prices = []dto.CreatePlanPriceRequest{
		{
			CreatePriceRequest: &dto.CreatePriceRequest{
				Amount:             "0",
				Currency:           "USD",
				Type:               types.PRICE_TYPE_FIXED,
				BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
				BillingPeriodCount: 1,
				BillingModel:       types.BILLING_MODEL_FLAT_FEE,
				BillingCadence:     types.BILLING_CADENCE_RECURRING,
				InvoiceCadence:     types.InvoiceCadenceAdvance,
				Description:        "Free tier for personal use",
			},
		},
		{
			CreatePriceRequest: &dto.CreatePriceRequest{
				Amount:             "0",
				Currency:           "USD",
				Type:               types.PRICE_TYPE_FIXED,
				BillingPeriod:      types.BILLING_PERIOD_ANNUAL,
				BillingPeriodCount: 1,
				BillingModel:       types.BILLING_MODEL_FLAT_FEE,
				BillingCadence:     types.BILLING_CADENCE_RECURRING,
				InvoiceCadence:     types.InvoiceCadenceAdvance,
			},
		},
	}
	hobbyPlan.Prices = append(hobbyPlan.Prices, usageBasedPriceRequests...)

	proPlan.Prices = []dto.CreatePlanPriceRequest{
		{
			CreatePriceRequest: &dto.CreatePriceRequest{
				Amount:             "20",
				Currency:           "USD",
				Type:               types.PRICE_TYPE_FIXED,
				BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
				BillingPeriodCount: 1,
				BillingModel:       types.BILLING_MODEL_FLAT_FEE,
				BillingCadence:     types.BILLING_CADENCE_RECURRING,
				InvoiceCadence:     types.InvoiceCadenceAdvance,
			},
		},
		{
			CreatePriceRequest: &dto.CreatePriceRequest{
				Amount:             "16",
				Currency:           "USD",
				Type:               types.PRICE_TYPE_FIXED,
				BillingPeriod:      types.BILLING_PERIOD_ANNUAL,
				BillingPeriodCount: 1,
				BillingModel:       types.BILLING_MODEL_FLAT_FEE,
				BillingCadence:     types.BILLING_CADENCE_RECURRING,
				InvoiceCadence:     types.InvoiceCadenceAdvance,
			},
		},
	}
	proPlan.Prices = append(proPlan.Prices, usageBasedPriceRequests...)

	businessPlan.Prices = []dto.CreatePlanPriceRequest{
		{
			CreatePriceRequest: &dto.CreatePriceRequest{
				Amount:             "40",
				Currency:           "USD",
				Type:               types.PRICE_TYPE_FIXED,
				BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
				BillingPeriodCount: 1,
				BillingModel:       types.BILLING_MODEL_FLAT_FEE,
				BillingCadence:     types.BILLING_CADENCE_RECURRING,
				InvoiceCadence:     types.InvoiceCadenceAdvance,
			},
		},
		{
			CreatePriceRequest: &dto.CreatePriceRequest{
				Amount:             "32",
				Currency:           "USD",
				Type:               types.PRICE_TYPE_FIXED,
				BillingPeriod:      types.BILLING_PERIOD_ANNUAL,
				BillingPeriodCount: 1,
				BillingModel:       types.BILLING_MODEL_FLAT_FEE,
				BillingCadence:     types.BILLING_CADENCE_RECURRING,
				InvoiceCadence:     types.InvoiceCadenceAdvance,
			},
		},
	}
	businessPlan.Prices = append(businessPlan.Prices, usageBasedPriceRequests...)

	return nil
}

func (s *onboardingService) createDefaultCustomers(ctx context.Context) ([]*dto.CustomerResponse, error) {
	s.Logger.Infow("creating default customers for Cursor pricing model")

	// Create a customer service instance
	customerService := NewCustomerService(s.CustomerRepo)

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
		StartDate:          time.Now(),
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
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

func (s *onboardingService) setDefaultEntitlements(_ context.Context, plans []*dto.CreatePlanRequest, features []*dto.FeatureResponse) error {
	s.Logger.Infow("creating default entitlements for Cursor pricing model")

	// Find plans by name
	var hobbyPlan, proPlan, businessPlan *dto.CreatePlanRequest
	for _, p := range plans {
		switch p.Name {
		case "Hobby":
			hobbyPlan = p
			hobbyPlan.Entitlements = make([]dto.CreatePlanEntitlementRequest, 0)
		case "Pro":
			proPlan = p
			proPlan.Entitlements = make([]dto.CreatePlanEntitlementRequest, 0)
		case "Business":
			businessPlan = p
			businessPlan.Entitlements = make([]dto.CreatePlanEntitlementRequest, 0)
		}
	}

	// Find features by name
	featureMap := make(map[string]string)
	for _, f := range features {
		featureMap[f.Name] = f.ID
	}

	// Validate that we found all required plans
	if hobbyPlan == nil || proPlan == nil || businessPlan == nil {
		return ierr.NewError("not all required plans were found").
			WithHint("Not all required plans were found").
			Mark(ierr.ErrValidation)
	}

	// Validate that we found all required features
	requiredFeatures := []string{
		"Completions",
		"Premium Requests (Slow)",
		"Premium Requests (Fast)",
		"Privacy Mode",
		"Team Billing",
		"Admin Dashboard",
		"SSO",
	}

	for _, name := range requiredFeatures {
		if _, ok := featureMap[name]; !ok {
			return ierr.NewError("required feature '%s' not found").
				WithHint("Required feature not found").
				WithReportableDetails(map[string]interface{}{
					"feature_name": name,
				}).
				Mark(ierr.ErrValidation)
		}
	}

	hobbyPlan.Entitlements = []dto.CreatePlanEntitlementRequest{
		{
			CreateEntitlementRequest: &dto.CreateEntitlementRequest{
				FeatureID:        featureMap["Completions"],
				FeatureType:      types.FeatureTypeMetered,
				IsEnabled:        true,
				UsageLimit:       lo.ToPtr(int64(2000)),
				UsageResetPeriod: types.BILLING_PERIOD_MONTHLY,
				IsSoftLimit:      true,
			},
		},
		{
			CreateEntitlementRequest: &dto.CreateEntitlementRequest{
				FeatureID:        featureMap["Premium Requests (Slow)"],
				FeatureType:      types.FeatureTypeMetered,
				IsEnabled:        true,
				UsageLimit:       lo.ToPtr(int64(50)),
				UsageResetPeriod: types.BILLING_PERIOD_MONTHLY,
				IsSoftLimit:      true,
			},
		},
	}

	proPlan.Entitlements = []dto.CreatePlanEntitlementRequest{
		{
			CreateEntitlementRequest: &dto.CreateEntitlementRequest{
				FeatureID:   featureMap["Completions"],
				FeatureType: types.FeatureTypeMetered,
				IsEnabled:   true,
			},
		},
		{
			CreateEntitlementRequest: &dto.CreateEntitlementRequest{
				FeatureID:        featureMap["Premium Requests (Fast)"],
				FeatureType:      types.FeatureTypeMetered,
				IsEnabled:        true,
				UsageLimit:       lo.ToPtr(int64(500)),
				UsageResetPeriod: types.BILLING_PERIOD_MONTHLY,
				IsSoftLimit:      true,
			},
		},
		{
			CreateEntitlementRequest: &dto.CreateEntitlementRequest{
				FeatureID:   featureMap["Premium Requests (Slow)"],
				FeatureType: types.FeatureTypeMetered,
				IsEnabled:   true,
			},
		},
	}

	businessPlan.Entitlements = append(businessPlan.Entitlements, proPlan.Entitlements...)
	businessPlan.Entitlements = append(businessPlan.Entitlements, []dto.CreatePlanEntitlementRequest{
		{
			CreateEntitlementRequest: &dto.CreateEntitlementRequest{
				FeatureID:   featureMap["Privacy Mode"],
				FeatureType: types.FeatureTypeBoolean,
				IsEnabled:   true,
			},
		},
		{
			CreateEntitlementRequest: &dto.CreateEntitlementRequest{
				FeatureID:   featureMap["Team Billing"],
				FeatureType: types.FeatureTypeBoolean,
				IsEnabled:   true,
			},
		},
		{
			CreateEntitlementRequest: &dto.CreateEntitlementRequest{
				FeatureID:   featureMap["Admin Dashboard"],
				FeatureType: types.FeatureTypeBoolean,
				IsEnabled:   true,
			},
		},
		{
			CreateEntitlementRequest: &dto.CreateEntitlementRequest{
				FeatureID:   featureMap["SSO"],
				FeatureType: types.FeatureTypeStatic,
				IsEnabled:   true,
				StaticValue: "SAML/OIDC",
			},
		},
	}...)

	return nil
}
