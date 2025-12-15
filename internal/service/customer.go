package service

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	workflowModels "github.com/flexprice/flexprice/internal/temporal/models"
	temporalservice "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
	"github.com/samber/lo"
)

type CustomerService = interfaces.CustomerService

type customerService struct {
	ServiceParams
}

func NewCustomerService(params ServiceParams) CustomerService {
	return &customerService{
		ServiceParams: params,
	}
}

func (s *customerService) CreateCustomer(ctx context.Context, req dto.CreateCustomerRequest) (*dto.CustomerResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Resolve and validate parent customer if provided (by ID or external ID)
	if req.ParentCustomerExternalID != nil {
		parent, err := s.CustomerRepo.GetByLookupKey(ctx, *req.ParentCustomerExternalID)
		if err != nil {
			return nil, err
		}
		if parent.ParentCustomerID != nil {
			return nil, ierr.NewError("parent customer cannot be a child").
				WithHint("Choose a parent customer that isn't a child of another").
				Mark(ierr.ErrInvalidOperation)
		}
		// Normalize to internal ID for downstream logic
		req.ParentCustomerID = lo.ToPtr(parent.ID)
	} else if req.ParentCustomerID != nil {
		parentCustomer, err := s.CustomerRepo.Get(ctx, *req.ParentCustomerID)
		if err != nil {
			return nil, err
		}
		if parentCustomer.ParentCustomerID != nil {
			return nil, ierr.NewError("parent customer cannot be a child").
				WithHint("Choose a parent customer that isn't a child of another").
				Mark(ierr.ErrInvalidOperation)
		}
	}

	cust := req.ToCustomer(ctx)

	// Validate address fields
	if err := customer.ValidateAddress(cust); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid address information provided").
			Mark(ierr.ErrValidation)
	}

	// Validate integration entity mappings if provided
	if len(req.IntegrationEntityMapping) > 0 {
		// Validation: Check that provider types are valid
		for _, mapping := range req.IntegrationEntityMapping {
			if mapping.Provider != string(types.SecretProviderStripe) {
				return nil, ierr.NewError("unsupported provider type").
					WithHint("Only Stripe provider is currently supported").
					Mark(ierr.ErrValidation)
			}
			if mapping.ID == "" {
				return nil, ierr.NewError("provider entity ID is required").
					WithHint("Provider entity ID must be provided for integration mapping").
					Mark(ierr.ErrValidation)
			}
		}
	}

	if err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		if err := s.CustomerRepo.Create(txCtx, cust); err != nil {
			// No need to wrap the error as the repository already returns properly formatted errors
			return err
		}

		// Create integration entity mappings if provided
		if len(req.IntegrationEntityMapping) > 0 {
			entityMappingService := NewEntityIntegrationMappingService(s.ServiceParams)
			for _, mapping := range req.IntegrationEntityMapping {
				mappingReq := dto.CreateEntityIntegrationMappingRequest{
					EntityID:         cust.ID,
					EntityType:       types.IntegrationEntityTypeCustomer,
					ProviderType:     mapping.Provider,
					ProviderEntityID: mapping.ID,
					Metadata: map[string]interface{}{
						"created_via": "api",
						"skip_sync":   true, // Skip automatic sync since we're using existing provider entity
					},
				}

				_, err := entityMappingService.CreateEntityIntegrationMapping(txCtx, mappingReq)
				if err != nil {
					return err
				}

				// Update customer metadata to include provider mapping info
				if cust.Metadata == nil {
					cust.Metadata = make(map[string]string)
				}
				cust.Metadata[mapping.Provider+"_customer_id"] = mapping.ID

				// Update provider customer metadata with FlexPrice info
				if mapping.Provider == string(types.SecretProviderStripe) {
					// Get Stripe integration
					stripeIntegration, err := s.IntegrationFactory.GetStripeIntegration(ctx)
					if err != nil {
						s.Logger.Warnw("failed to get Stripe integration for metadata update",
							"error", err,
							"customer_id", cust.ID)
						// Don't fail the entire operation, just log the error
						continue
					}

					// Update Stripe customer metadata with FlexPrice customer information
					err = stripeIntegration.CustomerSvc.UpdateStripeCustomerMetadata(ctx, mapping.ID, cust)
					if err != nil {
						s.Logger.Warnw("failed to update Stripe customer metadata",
							"error", err,
							"stripe_customer_id", mapping.ID,
							"customer_id", cust.ID)
						// Don't fail the entire operation, just log the error
					}
				}
			}

			// Update customer with the new metadata
			if err := s.CustomerRepo.Update(txCtx, cust); err != nil {
				return err
			}
		}

		taxService := NewTaxService(s.ServiceParams)

		// Link tax rates to customer if provided
		// If no tax rate overrides are provided, link the tenant tax rate to the customer
		if len(req.TaxRateOverrides) > 0 {
			err := taxService.LinkTaxRatesToEntity(txCtx, dto.LinkTaxRateToEntityRequest{
				EntityType:       types.TaxRateEntityTypeCustomer,
				EntityID:         cust.ID,
				TaxRateOverrides: req.TaxRateOverrides,
			})
			if err != nil {
				return err
			}
		}

		// If no tax rate overrides are provided, link the tenant tax rate to the customer
		if req.TaxRateOverrides == nil {
			filter := types.NewNoLimitTaxAssociationFilter()
			filter.EntityType = types.TaxRateEntityTypeTenant
			filter.EntityID = types.GetTenantID(txCtx)
			filter.AutoApply = lo.ToPtr(true)
			tenantTaxAssociations, err := taxService.ListTaxAssociations(txCtx, filter)
			if err != nil {
				return err
			}

			err = taxService.LinkTaxRatesToEntity(txCtx, dto.LinkTaxRateToEntityRequest{
				EntityType:              types.TaxRateEntityTypeCustomer,
				EntityID:                cust.ID,
				ExistingTaxAssociations: tenantTaxAssociations.Items,
			})
			if err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}

	// Publish webhook event for customer creation
	s.publishWebhookEvent(ctx, types.WebhookEventCustomerCreated, cust.ID)

	if err := s.handleCustomerOnboarding(ctx, cust); err != nil {
		s.Logger.Errorw("failed to handle customer onboarding workflow", "customer_id", cust.ID, "error", err)
	}

	return &dto.CustomerResponse{Customer: cust}, nil
}

func (s *customerService) GetCustomer(ctx context.Context, id string) (*dto.CustomerResponse, error) {
	if id == "" {
		return nil, ierr.NewError("customer ID is required").
			WithHint("Customer ID is required").
			Mark(ierr.ErrValidation)
	}

	customer, err := s.CustomerRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	resp := &dto.CustomerResponse{Customer: customer}

	if customer.ParentCustomerID != nil {
		parentResp, err := s.GetCustomer(ctx, *customer.ParentCustomerID)
		if err != nil {
			return nil, err
		}
		resp.ParentCustomer = parentResp
	}

	return resp, nil
}

func (s *customerService) GetCustomers(ctx context.Context, filter *types.CustomerFilter) (*dto.ListCustomersResponse, error) {
	if filter == nil {
		filter = &types.CustomerFilter{
			QueryFilter: types.NewDefaultQueryFilter(),
		}
	}

	// Validate expand fields
	if err := filter.GetExpand().Validate(types.CustomerExpandConfig); err != nil {
		return nil, err
	}

	if err := filter.Validate(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation)
	}

	customers, err := s.CustomerRepo.List(ctx, filter)
	if err != nil {
		// No need to wrap the error as the repository already returns properly formatted errors
		return nil, err
	}

	total, err := s.CustomerRepo.Count(ctx, filter)
	if err != nil {
		// No need to wrap the error as the repository already returns properly formatted errors
		return nil, err
	}

	response := make([]*dto.CustomerResponse, 0, len(customers))
	for _, c := range customers {
		response = append(response, &dto.CustomerResponse{Customer: c})
	}

	if len(response) == 0 {
		return &dto.ListCustomersResponse{
			Items:      response,
			Pagination: types.NewPaginationResponse(total, filter.GetLimit(), filter.GetOffset()),
		}, nil
	}

	// Expand parent customers if requested
	var parentCustomersByID map[string]*dto.CustomerResponse
	if filter.GetExpand().Has(types.ExpandParentCustomer) {
		// Collect all unique parent customer IDs
		parentCustomerIDs := make([]string, 0)
		parentCustomerIDSet := make(map[string]bool)
		for _, c := range customers {
			if c.ParentCustomerID != nil && !parentCustomerIDSet[*c.ParentCustomerID] {
				parentCustomerIDs = append(parentCustomerIDs, *c.ParentCustomerID)
				parentCustomerIDSet[*c.ParentCustomerID] = true
			}
		}

		if len(parentCustomerIDs) > 0 {
			// Fetch parent customers in bulk
			parentFilter := types.NewNoLimitCustomerFilter()
			parentFilter.CustomerIDs = parentCustomerIDs

			parentCustomers, err := s.CustomerRepo.List(ctx, parentFilter)
			if err != nil {
				return nil, err
			}

			// Create a map for quick parent customer lookup
			parentCustomersByID = make(map[string]*dto.CustomerResponse, len(parentCustomers))
			for _, pc := range parentCustomers {
				parentCustomersByID[pc.ID] = &dto.CustomerResponse{Customer: pc}
			}

			s.Logger.Debugw("fetched parent customers for customers", "count", len(parentCustomers))
		}
	}

	// Attach parent customers to response items
	for _, resp := range response {
		if resp.Customer.ParentCustomerID != nil {
			if parentCustomer, ok := parentCustomersByID[*resp.Customer.ParentCustomerID]; ok {
				resp.ParentCustomer = parentCustomer
			}
		}
	}

	return &dto.ListCustomersResponse{
		Items:      response,
		Pagination: types.NewPaginationResponse(total, filter.GetLimit(), filter.GetOffset()),
	}, nil
}

func (s *customerService) UpdateCustomer(ctx context.Context, id string, req dto.UpdateCustomerRequest) (*dto.CustomerResponse, error) {
	if id == "" {
		return nil, ierr.NewError("customer ID is required").
			WithHint("Customer ID is required").
			Mark(ierr.ErrValidation)
	}

	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Validate integration entity mappings if provided
	if len(req.IntegrationEntityMapping) > 0 {
		// Validation: Check that provider types are valid
		for _, mapping := range req.IntegrationEntityMapping {
			if mapping.Provider != string(types.SecretProviderStripe) {
				return nil, ierr.NewError("unsupported provider type").
					WithHint("Only Stripe provider is currently supported").
					Mark(ierr.ErrValidation)
			}
			if mapping.ID == "" {
				return nil, ierr.NewError("provider entity ID is required").
					WithHint("Provider entity ID must be provided for integration mapping").
					Mark(ierr.ErrValidation)
			}
		}
	}

	cust, err := s.CustomerRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	if req.ParentCustomerID != nil {
		newParentID := strings.TrimSpace(*req.ParentCustomerID)
		currentParentID := ""
		if cust.ParentCustomerID != nil {
			currentParentID = strings.TrimSpace(*cust.ParentCustomerID)
		}

		// Only run validations if the hierarchy is changing
		if newParentID != currentParentID {
			if err := s.validateParentCustomerAssignment(ctx, cust, newParentID); err != nil {
				return nil, err
			}

			if newParentID == "" {
				cust.ParentCustomerID = nil
			} else {
				cust.ParentCustomerID = lo.ToPtr(newParentID)
			}
		}
	}

	// Update basic fields
	if req.ExternalID != nil && *req.ExternalID != cust.ExternalID {
		cust.ExternalID = *req.ExternalID
		oldExternalIDs, ok := cust.Metadata["old_external_ids"]
		if !ok {
			oldExternalIDs = ""
		}
		if oldExternalIDs == "" {
			cust.Metadata["old_external_ids"] = cust.ExternalID
		} else {
			cust.Metadata["old_external_ids"] = oldExternalIDs + "," + cust.ExternalID
		}
	}

	if req.Name != nil {
		cust.Name = *req.Name
	}
	if req.Email != nil {
		cust.Email = *req.Email
	}

	// Update address fields
	if req.AddressLine1 != nil {
		cust.AddressLine1 = *req.AddressLine1
	}
	if req.AddressLine2 != nil {
		cust.AddressLine2 = *req.AddressLine2
	}
	if req.AddressCity != nil {
		cust.AddressCity = *req.AddressCity
	}
	if req.AddressState != nil {
		cust.AddressState = *req.AddressState
	}
	if req.AddressPostalCode != nil {
		cust.AddressPostalCode = *req.AddressPostalCode
	}
	if req.AddressCountry != nil {
		cust.AddressCountry = *req.AddressCountry
	}

	// Update metadata if provided
	if req.Metadata != nil {
		cust.Metadata = req.Metadata
	}

	// Handle integration entity mappings if provided
	if len(req.IntegrationEntityMapping) > 0 {
		entityMappingService := NewEntityIntegrationMappingService(s.ServiceParams)
		for _, mapping := range req.IntegrationEntityMapping {
			// Check if mapping already exists
			filter := &types.EntityIntegrationMappingFilter{
				EntityID:      cust.ID,
				EntityType:    types.IntegrationEntityTypeCustomer,
				ProviderTypes: []string{mapping.Provider},
			}

			existingMappings, err := entityMappingService.GetEntityIntegrationMappings(ctx, filter)
			if err != nil {
				return nil, err
			}

			if existingMappings != nil && len(existingMappings.Items) > 0 {
				// Update existing mapping
				existingMapping := existingMappings.Items[0]
				updateReq := dto.UpdateEntityIntegrationMappingRequest{
					ProviderEntityID: &mapping.ID,
					Metadata: map[string]interface{}{
						"updated_via": "api",
						"skip_sync":   true,
						"updated_at":  time.Now().UTC().Format(time.RFC3339),
					},
				}

				_, err := entityMappingService.UpdateEntityIntegrationMapping(ctx, existingMapping.ID, updateReq)
				if err != nil {
					return nil, err
				}
			} else {
				// Create new mapping
				mappingReq := dto.CreateEntityIntegrationMappingRequest{
					EntityID:         cust.ID,
					EntityType:       types.IntegrationEntityTypeCustomer,
					ProviderType:     mapping.Provider,
					ProviderEntityID: mapping.ID,
					Metadata: map[string]interface{}{
						"created_via": "api",
						"skip_sync":   true,
						"created_at":  time.Now().UTC().Format(time.RFC3339),
					},
				}

				_, err := entityMappingService.CreateEntityIntegrationMapping(ctx, mappingReq)
				if err != nil {
					return nil, err
				}
			}

			// Update customer metadata to include provider mapping info
			if cust.Metadata == nil {
				cust.Metadata = make(map[string]string)
			}
			cust.Metadata[mapping.Provider+"_customer_id"] = mapping.ID

			// Update provider customer metadata with FlexPrice info
			if mapping.Provider == string(types.SecretProviderStripe) {
				// Get Stripe integration
				stripeIntegration, err := s.IntegrationFactory.GetStripeIntegration(ctx)
				if err != nil {
					s.Logger.Warnw("failed to get Stripe integration for metadata update",
						"error", err,
						"customer_id", cust.ID)
					// Don't fail the entire operation, just log the error
					continue
				}

				// Update Stripe customer metadata with FlexPrice customer information
				err = stripeIntegration.CustomerSvc.UpdateStripeCustomerMetadata(ctx, mapping.ID, cust)
				if err != nil {
					s.Logger.Warnw("failed to update Stripe customer metadata",
						"error", err,
						"stripe_customer_id", mapping.ID,
						"customer_id", cust.ID)
					// Don't fail the entire operation, just log the error
				}
			}
		}
	}

	// Validate address fields after update
	if err := customer.ValidateAddress(cust); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid address information provided").
			Mark(ierr.ErrValidation)
	}

	if err := s.CustomerRepo.Update(ctx, cust); err != nil {
		// No need to wrap the error as the repository already returns properly formatted errors
		return nil, err
	}

	s.publishWebhookEvent(ctx, types.WebhookEventCustomerUpdated, cust.ID)

	return &dto.CustomerResponse{Customer: cust}, nil
}

func (s *customerService) DeleteCustomer(ctx context.Context, id string) error {
	if id == "" {
		return ierr.NewError("customer ID is required").
			WithHint("Customer ID is required").
			Mark(ierr.ErrValidation)
	}

	customer, err := s.CustomerRepo.Get(ctx, id)
	if err != nil {
		return err
	}

	if customer.Status != types.StatusPublished {
		return ierr.NewError("customer is not published").
			WithHint("Customer does not exist").
			Mark(ierr.ErrNotFound)
	}

	subscriptionFilter := types.NewSubscriptionFilter()
	subscriptionFilter.CustomerID = id
	subscriptionFilter.SubscriptionStatusNotIn = []types.SubscriptionStatus{types.SubscriptionStatusCancelled}
	subscriptionFilter.Limit = lo.ToPtr(1)
	subscriptions, err := s.SubRepo.List(ctx, subscriptionFilter)
	if err != nil {
		return err
	}

	if len(subscriptions) > 0 {
		return ierr.NewError("customer cannot be deleted due to active subscriptions").
			WithHint("Please cancel all subscriptions before deleting the customer").
			Mark(ierr.ErrInvalidOperation)
	}

	wallets, err := s.WalletRepo.GetWalletsByCustomerID(ctx, id)
	if err != nil {
		return err
	}

	if len(wallets) > 0 {
		return ierr.NewError("customer cannot be deleted due to associated wallets").
			WithHint("Customer cannot be deleted due to associated wallets").
			Mark(ierr.ErrInvalidOperation)
	}

	if err := s.CustomerRepo.Delete(ctx, customer); err != nil {
		return err
	}

	s.publishWebhookEvent(ctx, types.WebhookEventCustomerDeleted, id)
	return nil
}

func (s *customerService) GetCustomerByLookupKey(ctx context.Context, lookupKey string) (*dto.CustomerResponse, error) {
	if lookupKey == "" {
		return nil, ierr.NewError("lookup key is required").
			WithHint("Lookup key is required").
			Mark(ierr.ErrValidation)
	}

	customer, err := s.CustomerRepo.GetByLookupKey(ctx, lookupKey)
	if err != nil {
		return nil, err
	}

	return &dto.CustomerResponse{Customer: customer}, nil
}

func (s *customerService) validateParentCustomerAssignment(ctx context.Context, cust *customer.Customer, newParentID string) error {
	// Do not allow hierarchy changes when customer has non-cancelled subscriptions
	subFilter := types.NewSubscriptionFilter()
	subFilter.CustomerID = cust.ID
	subFilter.SubscriptionStatusNotIn = []types.SubscriptionStatus{types.SubscriptionStatusCancelled}
	subFilter.Limit = lo.ToPtr(1)

	subs, err := s.SubRepo.List(ctx, subFilter)
	if err != nil {
		return err
	}
	if len(subs) > 0 {
		return ierr.NewError("customer hierarchy cannot change with active subscriptions").
			WithHint("Cancel or transfer subscriptions before updating parent hierarchy").
			Mark(ierr.ErrInvalidOperation)
	}

	if newParentID == "" {
		// Resetting parent - nothing else to validate
		return nil
	}

	if newParentID == cust.ID {
		return ierr.NewError("customer cannot be its own parent").
			WithHint("Please provide a different customer as parent").
			Mark(ierr.ErrValidation)
	}

	parentCustomer, err := s.CustomerRepo.Get(ctx, newParentID)
	if err != nil {
		return err
	}
	if parentCustomer.ParentCustomerID != nil {
		return ierr.NewError("parent customer cannot have its own parent").
			WithHint("Nested hierarchies are not supported; pick a top-level customer as parent").
			Mark(ierr.ErrInvalidOperation)
	}

	// A customer that already has children cannot become a child itself
	childFilter := types.NewCustomerFilter()
	childFilter.ParentCustomerIDs = []string{cust.ID}
	childFilter.Limit = lo.ToPtr(1)

	children, err := s.CustomerRepo.List(ctx, childFilter)
	if err != nil {
		return err
	}
	if len(children) > 0 {
		return ierr.NewError("customer already acts as a parent").
			WithHint("A customer cannot be both parent and child; detach child customers first").
			Mark(ierr.ErrInvalidOperation)
	}

	return nil
}

func (s *customerService) publishWebhookEvent(ctx context.Context, eventName string, customerID string) {
	webhookPayload, err := json.Marshal(webhookDto.InternalCustomerEvent{
		CustomerID: customerID,
		TenantID:   types.GetTenantID(ctx),
	})

	if err != nil {
		s.Logger.Errorw("failed to marshal webhook payload", "error", err)
		return
	}

	webhookEvent := &types.WebhookEvent{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WEBHOOK_EVENT),
		EventName:     eventName,
		TenantID:      types.GetTenantID(ctx),
		EnvironmentID: types.GetEnvironmentID(ctx),
		UserID:        types.GetUserID(ctx),
		Timestamp:     time.Now().UTC(),
		Payload:       json.RawMessage(webhookPayload),
	}
	if err := s.WebhookPublisher.PublishWebhook(ctx, webhookEvent); err != nil {
		s.Logger.Errorf("failed to publish %s event: %v", webhookEvent.EventName, err)
	}
}

// GetUpcomingCreditGrantApplications retrieves upcoming credit grant applications for all subscriptions of a customer
// This method gets all subscriptions for the customer and then fetches upcoming credit grant applications across all of them
func (s *customerService) GetUpcomingCreditGrantApplications(ctx context.Context, customerID string) (*dto.ListCreditGrantApplicationsResponse, error) {
	// Validate customer exists
	_, err := s.CustomerRepo.Get(ctx, customerID)
	if err != nil {
		return nil, err
	}

	// Get all subscriptions for this customer
	subscriptionService := NewSubscriptionService(s.ServiceParams)
	subscriptions, err := subscriptionService.ListByCustomerID(ctx, customerID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get subscriptions for customer").
			WithReportableDetails(map[string]interface{}{
				"customer_id": customerID,
			}).
			Mark(ierr.ErrDatabase)
	}

	// Extract subscription IDs
	subscriptionIDs := make([]string, 0, len(subscriptions))
	for _, sub := range subscriptions {
		if sub != nil && sub.ID != "" {
			subscriptionIDs = append(subscriptionIDs, sub.ID)
		}
	}

	// If no subscriptions found, return empty response
	if len(subscriptionIDs) == 0 {
		return &dto.ListCreditGrantApplicationsResponse{
			Items: []*dto.CreditGrantApplicationResponse{},
			Pagination: types.PaginationResponse{
				Total:  0,
				Limit:  0,
				Offset: 0,
			},
		}, nil
	}

	// Get upcoming credit grant applications for all subscriptions
	req := &dto.GetUpcomingCreditGrantApplicationsRequest{
		SubscriptionIDs: subscriptionIDs,
	}

	return subscriptionService.GetUpcomingCreditGrantApplications(ctx, req)
}

func (s *customerService) handleCustomerOnboarding(ctx context.Context, customer *customer.Customer) error {
	s.Logger.Infow("handling customer onboarding", "customer_id", customer.ID)

	// Get customer onboarding workflow config
	settingsService := &settingsService{
		ServiceParams: s.ServiceParams,
	}
	workflowConfig, err := GetSetting[*workflowModels.WorkflowConfig](settingsService, ctx, types.SettingKeyCustomerOnboarding)
	if err != nil {
		return err
	}

	if workflowConfig == nil {
		s.Logger.Infow("workflow config is nil, skipping customer onboarding", "customer_id", customer.ID)
		return nil
	}

	// If there are no actions, return
	if len(workflowConfig.Actions) == 0 {
		s.Logger.Infow("no actions found for customer onboarding", "customer_id", customer.ID)
		return nil
	}

	// Copy necessary context values
	tenantID := types.GetTenantID(ctx)
	envID := types.GetEnvironmentID(ctx)
	userID := types.GetUserID(ctx)

	s.Logger.Infow("executing customer onboarding workflow",
		"customer_id", customer.ID,
		"tenant_id", tenantID,
		"environment_id", envID,
		"user_id", userID,
		"action_count", len(workflowConfig.Actions))

	// Prepare workflow input with all necessary IDs
	input := &workflowModels.CustomerOnboardingWorkflowInput{
		CustomerID:     customer.ID,
		TenantID:       tenantID,
		EnvironmentID:  envID,
		UserID:         userID,
		WorkflowConfig: *workflowConfig,
	}

	// Validate input
	if err := input.Validate(); err != nil {
		s.Logger.Errorw("invalid workflow input for customer onboarding",
			"error", err,
			"customer_id", customer.ID)
		return ierr.WithError(err).
			WithHint("Invalid workflow input for customer onboarding").
			WithReportableDetails(map[string]interface{}{
				"customer_id": customer.ID,
			}).
			Mark(ierr.ErrValidation)
	}

	// Get global temporal service
	temporalSvc := temporalservice.GetGlobalTemporalService()
	if temporalSvc == nil {
		return ierr.NewError("temporal service not available").
			WithHint("Customer onboarding workflow requires Temporal service").
			WithReportableDetails(map[string]interface{}{
				"customer_id": customer.ID,
			}).
			Mark(ierr.ErrInternal)
	}

	// Execute workflow via Temporal
	workflowRun, err := temporalSvc.ExecuteWorkflow(
		ctx,
		types.TemporalCustomerOnboardingWorkflow,
		input,
	)
	if err != nil {
		s.Logger.Errorw("failed to start customer onboarding workflow",
			"error", err,
			"customer_id", customer.ID)
		return ierr.WithError(err).
			WithHint("Failed to start customer onboarding workflow").
			WithReportableDetails(map[string]interface{}{
				"customer_id": customer.ID,
			}).
			Mark(ierr.ErrInternal)
	}

	s.Logger.Infow("customer onboarding workflow started successfully",
		"customer_id", customer.ID,
		"workflow_id", workflowRun.GetID(),
		"run_id", workflowRun.GetRunID())

	return nil
}
