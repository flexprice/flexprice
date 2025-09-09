package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
	"github.com/samber/lo"
)

type CustomerService interface {
	CreateCustomer(ctx context.Context, req dto.CreateCustomerRequest) (*dto.CustomerResponse, error)
	GetCustomer(ctx context.Context, id string) (*dto.CustomerResponse, error)
	GetCustomers(ctx context.Context, filter *types.CustomerFilter) (*dto.ListCustomersResponse, error)
	UpdateCustomer(ctx context.Context, id string, req dto.UpdateCustomerRequest) (*dto.CustomerResponse, error)
	DeleteCustomer(ctx context.Context, id string) error
	GetCustomerByLookupKey(ctx context.Context, lookupKey string) (*dto.CustomerResponse, error)
}

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

	cust := req.ToCustomer(ctx)

	// Validate address fields
	if err := customer.ValidateAddress(cust); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid address information provided").
			Mark(ierr.ErrValidation)
	}

	// Validate integration entity mappings if provided
	if len(req.IntegrationEntityMapping) > 0 {
		integrationService := NewIntegrationService(s.ServiceParams)
		if err := integrationService.ValidateIntegrationEntityMappings(ctx, req.IntegrationEntityMapping); err != nil {
			return nil, err
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
				integrationService := NewIntegrationService(s.ServiceParams)
				if err := integrationService.UpdateProviderCustomerMetadata(txCtx, mapping.Provider, mapping.ID, cust); err != nil {
					s.Logger.Errorw("failed to update provider customer metadata",
						"provider", mapping.Provider,
						"provider_customer_id", mapping.ID,
						"customer_id", cust.ID,
						"error", err)
					// Don't fail the request, just log the error
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
		// No need to wrap the error as the repository already returns properly formatted errors
		return nil, err
	}

	return &dto.CustomerResponse{Customer: customer}, nil
}

func (s *customerService) GetCustomers(ctx context.Context, filter *types.CustomerFilter) (*dto.ListCustomersResponse, error) {
	if filter == nil {
		filter = &types.CustomerFilter{
			QueryFilter: types.NewDefaultQueryFilter(),
		}
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
		integrationService := NewIntegrationService(s.ServiceParams)
		if err := integrationService.ValidateIntegrationEntityMappings(ctx, req.IntegrationEntityMapping); err != nil {
			return nil, err
		}
	}

	cust, err := s.CustomerRepo.Get(ctx, id)
	if err != nil {
		// No need to wrap the error as the repository already returns properly formatted errors
		return nil, err
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
			integrationService := NewIntegrationService(s.ServiceParams)
			if err := integrationService.UpdateProviderCustomerMetadata(ctx, mapping.Provider, mapping.ID, cust); err != nil {
				s.Logger.Errorw("failed to update provider customer metadata",
					"provider", mapping.Provider,
					"provider_customer_id", mapping.ID,
					"customer_id", cust.ID,
					"error", err)
				// Don't fail the request, just log the error
			}
		}
	}

	// Validate address fields after update
	if err := customer.ValidateAddress(cust); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid address information provided").
			Mark(ierr.ErrValidation)
	}

	cust.UpdatedAt = time.Now().UTC()
	cust.UpdatedBy = types.GetUserID(ctx)

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
