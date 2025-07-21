package service

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/integration"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
)

// CustomerIntegrationService defines the interface for customer integration operations
type CustomerIntegrationService interface {
	// Customer Mapping Operations
	CreateCustomerMapping(ctx context.Context, req CreateCustomerMappingRequest) (*CustomerMappingResponse, error)
	GetCustomerMapping(ctx context.Context, customerID string, providerType integration.ProviderType) (*CustomerMappingResponse, error)
	UpdateCustomerMapping(ctx context.Context, customerID string, providerType integration.ProviderType, req UpdateCustomerMappingRequest) (*CustomerMappingResponse, error)
	DeleteCustomerMapping(ctx context.Context, customerID string, providerType integration.ProviderType) error
	ListCustomerMappings(ctx context.Context, filter *integration.EntityIntegrationMappingFilter) (*ListCustomerMappingsResponse, error)

	// Lookup Operations
	GetCustomerByProviderID(ctx context.Context, providerType integration.ProviderType, providerCustomerID string) (*CustomerMappingResponse, error)
	GetProviderCustomerID(ctx context.Context, customerID string, providerType integration.ProviderType) (string, error)

	// Webhook Processing
	ProcessStripeCustomerWebhook(ctx context.Context, webhook *webhookDto.StripeWebhookEvent) (*WebhookProcessingResponse, error)
	ProcessCustomerCreatedWebhook(ctx context.Context, customerData *webhookDto.StripeCustomerData) (*WebhookProcessingResponse, error)
	ProcessCustomerUpdatedWebhook(ctx context.Context, customerData *webhookDto.StripeCustomerData) (*WebhookProcessingResponse, error)

	// Bulk Migration Operations
	BulkCreateCustomerMappings(ctx context.Context, req BulkCreateCustomerMappingsRequest) (*BulkCreateCustomerMappingsResponse, error)
	ValidateCustomerMappings(ctx context.Context, req ValidateCustomerMappingsRequest) (*ValidateCustomerMappingsResponse, error)
	MigrateExistingCustomers(ctx context.Context, req MigrateExistingCustomersRequest) (*MigrateExistingCustomersResponse, error)
}

// DTO types for Customer Integration Service

// CreateCustomerMappingRequest represents a request to create customer mapping
type CreateCustomerMappingRequest struct {
	CustomerID         string                   `json:"customer_id" validate:"required"`
	ProviderType       integration.ProviderType `json:"provider_type" validate:"required"`
	ProviderCustomerID string                   `json:"provider_customer_id" validate:"required"`
	ExternalID         string                   `json:"external_id,omitempty"`
	Metadata           map[string]interface{}   `json:"metadata,omitempty"`
}

// UpdateCustomerMappingRequest represents a request to update customer mapping
type UpdateCustomerMappingRequest struct {
	ProviderCustomerID *string                `json:"provider_customer_id,omitempty"`
	ExternalID         *string                `json:"external_id,omitempty"`
	Metadata           map[string]interface{} `json:"metadata,omitempty"`
}

// CustomerMappingResponse represents a customer mapping in the response
type CustomerMappingResponse struct {
	ID                 string                   `json:"id"`
	CustomerID         string                   `json:"customer_id"`
	ProviderType       integration.ProviderType `json:"provider_type"`
	ProviderCustomerID string                   `json:"provider_customer_id"`
	ExternalID         string                   `json:"external_id"`
	Customer           *customer.Customer       `json:"customer,omitempty"`
	Metadata           map[string]interface{}   `json:"metadata"`
	TenantID           string                   `json:"tenant_id"`
	EnvironmentID      string                   `json:"environment_id"`
	CreatedAt          time.Time                `json:"created_at"`
	UpdatedAt          time.Time                `json:"updated_at"`
}

// ListCustomerMappingsResponse represents the response for listing customer mappings
type ListCustomerMappingsResponse struct {
	Items      []CustomerMappingResponse `json:"items"`
	Pagination types.PaginationResponse  `json:"pagination"`
}

// WebhookProcessingResponse represents the response for webhook processing
type WebhookProcessingResponse struct {
	Success            bool                   `json:"success"`
	Action             string                 `json:"action"`
	CustomerID         string                 `json:"customer_id,omitempty"`
	ProviderCustomerID string                 `json:"provider_customer_id"`
	MappingID          string                 `json:"mapping_id,omitempty"`
	Message            string                 `json:"message"`
	ProcessedAt        time.Time              `json:"processed_at"`
	Metadata           map[string]interface{} `json:"metadata,omitempty"`
}

// BulkCreateCustomerMappingsRequest represents a request for bulk customer mapping creation
type BulkCreateCustomerMappingsRequest struct {
	ProviderType integration.ProviderType  `json:"provider_type" validate:"required"`
	Mappings     []BulkCustomerMappingItem `json:"mappings" validate:"required,min=1,max=1000"`
	ConflictMode BulkOperationConflictMode `json:"conflict_mode"`
	DryRun       bool                      `json:"dry_run"`
}

// BulkCustomerMappingItem represents a single mapping item in bulk operations
type BulkCustomerMappingItem struct {
	CustomerID         string                 `json:"customer_id,omitempty"`
	ExternalID         string                 `json:"external_id" validate:"required"`
	ProviderCustomerID string                 `json:"provider_customer_id" validate:"required"`
	Metadata           map[string]interface{} `json:"metadata,omitempty"`
}

// BulkOperationConflictMode defines how to handle conflicts in bulk operations
type BulkOperationConflictMode string

const (
	BulkConflictModeSkip   BulkOperationConflictMode = "skip"
	BulkConflictModeUpdate BulkOperationConflictMode = "update"
	BulkConflictModeError  BulkOperationConflictMode = "error"
)

// BulkCreateCustomerMappingsResponse represents the response for bulk customer mapping creation
type BulkCreateCustomerMappingsResponse struct {
	TotalRequested int                 `json:"total_requested"`
	SuccessCount   int                 `json:"success_count"`
	SkippedCount   int                 `json:"skipped_count"`
	ErrorCount     int                 `json:"error_count"`
	Results        []BulkMappingResult `json:"results"`
	ProcessedAt    time.Time           `json:"processed_at"`
}

// BulkMappingResult represents the result of a single mapping operation in bulk
type BulkMappingResult struct {
	ExternalID         string              `json:"external_id"`
	ProviderCustomerID string              `json:"provider_customer_id"`
	CustomerID         string              `json:"customer_id,omitempty"`
	MappingID          string              `json:"mapping_id,omitempty"`
	Status             BulkOperationStatus `json:"status"`
	Message            string              `json:"message,omitempty"`
	ErrorCode          string              `json:"error_code,omitempty"`
}

// BulkOperationStatus defines the status of a single operation in bulk
type BulkOperationStatus string

const (
	BulkStatusSuccess BulkOperationStatus = "success"
	BulkStatusSkipped BulkOperationStatus = "skipped"
	BulkStatusError   BulkOperationStatus = "error"
)

// ValidateCustomerMappingsRequest represents a request to validate customer mappings
type ValidateCustomerMappingsRequest struct {
	ProviderType integration.ProviderType  `json:"provider_type" validate:"required"`
	Mappings     []BulkCustomerMappingItem `json:"mappings" validate:"required,min=1,max=1000"`
}

// ValidateCustomerMappingsResponse represents the response for customer mapping validation
type ValidateCustomerMappingsResponse struct {
	TotalItems        int                `json:"total_items"`
	ValidItems        int                `json:"valid_items"`
	InvalidItems      int                `json:"invalid_items"`
	ValidationResults []ValidationResult `json:"validation_results"`
	ValidatedAt       time.Time          `json:"validated_at"`
}

// ValidationResult represents the result of validating a single mapping
type ValidationResult struct {
	ExternalID         string            `json:"external_id"`
	ProviderCustomerID string            `json:"provider_customer_id"`
	IsValid            bool              `json:"is_valid"`
	Issues             []ValidationIssue `json:"issues,omitempty"`
	CustomerExists     bool              `json:"customer_exists"`
	MappingExists      bool              `json:"mapping_exists"`
}

// ValidationIssue represents a validation issue
type ValidationIssue struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// MigrateExistingCustomersRequest represents a request to migrate existing customers
type MigrateExistingCustomersRequest struct {
	ProviderType  integration.ProviderType `json:"provider_type" validate:"required"`
	BatchSize     int                      `json:"batch_size" validate:"min=1,max=100"`
	DryRun        bool                     `json:"dry_run"`
	ForceUpdate   bool                     `json:"force_update"`
	FilterOptions *CustomerMigrationFilter `json:"filter_options,omitempty"`
}

// CustomerMigrationFilter represents filtering options for customer migration
type CustomerMigrationFilter struct {
	CustomerIDs       []string   `json:"customer_ids,omitempty"`
	ExternalIDPattern string     `json:"external_id_pattern,omitempty"`
	CreatedAfter      *time.Time `json:"created_after,omitempty"`
	CreatedBefore     *time.Time `json:"created_before,omitempty"`
	HasMetadataKey    string     `json:"has_metadata_key,omitempty"`
}

// MigrateExistingCustomersResponse represents the response for customer migration
type MigrateExistingCustomersResponse struct {
	TotalCustomers      int                       `json:"total_customers"`
	ProcessedCount      int                       `json:"processed_count"`
	MigratedCount       int                       `json:"migrated_count"`
	SkippedCount        int                       `json:"skipped_count"`
	ErrorCount          int                       `json:"error_count"`
	MigrationResults    []CustomerMigrationResult `json:"migration_results"`
	EstimatedCompletion *time.Time                `json:"estimated_completion,omitempty"`
	MigratedAt          time.Time                 `json:"migrated_at"`
}

// CustomerMigrationResult represents the result of migrating a single customer
type CustomerMigrationResult struct {
	CustomerID         string              `json:"customer_id"`
	ExternalID         string              `json:"external_id"`
	ProviderCustomerID string              `json:"provider_customer_id,omitempty"`
	MappingID          string              `json:"mapping_id,omitempty"`
	Status             BulkOperationStatus `json:"status"`
	Message            string              `json:"message,omitempty"`
}

// Implementation

type customerIntegrationService struct {
	ServiceParams
}

// NewCustomerIntegrationService creates a new customer integration service
func NewCustomerIntegrationService(params ServiceParams) CustomerIntegrationService {
	return &customerIntegrationService{
		ServiceParams: params,
	}
}

// Customer Mapping Operations Implementation

func (s *customerIntegrationService) CreateCustomerMapping(ctx context.Context, req CreateCustomerMappingRequest) (*CustomerMappingResponse, error) {
	if err := s.validateCreateCustomerMappingRequest(req); err != nil {
		return nil, err
	}

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	// Verify customer exists
	customer, err := s.CustomerRepo.Get(ctx, req.CustomerID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Customer not found").
			Mark(ierr.ErrValidation)
	}

	// Check for duplicate mapping
	existing, err := s.EntityIntegrationMappingRepo.GetByEntityAndProvider(ctx, req.CustomerID, integration.EntityTypeCustomer, req.ProviderType)
	if err != nil && !ierr.IsNotFound(err) {
		return nil, err
	}
	if existing != nil {
		return nil, ierr.NewError("customer mapping already exists").
			WithHint("Use update endpoint to modify existing mapping").
			Mark(ierr.ErrValidation)
	}

	// Create mapping using the domain helper
	mapping := integration.CreateStripeCustomerMapping(
		req.CustomerID,
		req.ProviderCustomerID,
		req.ExternalID,
		environmentID,
	)

	// Set additional metadata
	if req.Metadata != nil {
		for key, value := range req.Metadata {
			mapping.Metadata[key] = value
		}
	}

	mapping.TenantID = tenantID
	mapping.BaseModel = types.GetDefaultBaseModel(ctx)

	if err := s.EntityIntegrationMappingRepo.Create(ctx, mapping); err != nil {
		return nil, err
	}

	return s.mapCustomerMappingToResponse(mapping, customer), nil
}

func (s *customerIntegrationService) GetCustomerMapping(ctx context.Context, customerID string, providerType integration.ProviderType) (*CustomerMappingResponse, error) {
	if customerID == "" {
		return nil, ierr.NewError("customer_id is required").
			WithHint("Customer ID must not be empty").
			Mark(ierr.ErrValidation)
	}

	mapping, err := s.EntityIntegrationMappingRepo.GetByEntityAndProvider(ctx, customerID, integration.EntityTypeCustomer, providerType)
	if err != nil {
		return nil, err
	}

	// Get customer details
	customer, err := s.CustomerRepo.Get(ctx, customerID)
	if err != nil {
		s.Logger.Warn("Failed to get customer details for mapping", "customer_id", customerID, "error", err)
		customer = nil // Continue without customer details
	}

	return s.mapCustomerMappingToResponse(mapping, customer), nil
}

func (s *customerIntegrationService) UpdateCustomerMapping(ctx context.Context, customerID string, providerType integration.ProviderType, req UpdateCustomerMappingRequest) (*CustomerMappingResponse, error) {
	mapping, err := s.EntityIntegrationMappingRepo.GetByEntityAndProvider(ctx, customerID, integration.EntityTypeCustomer, providerType)
	if err != nil {
		return nil, err
	}

	// Update fields
	if req.ProviderCustomerID != nil {
		mapping.SetProviderCustomerID(*req.ProviderCustomerID)
	}

	if req.ExternalID != nil {
		mapping.SetExternalID(*req.ExternalID)
	}

	if req.Metadata != nil {
		if mapping.Metadata == nil {
			mapping.Metadata = make(map[string]interface{})
		}
		for key, value := range req.Metadata {
			mapping.Metadata[key] = value
		}
	}

	mapping.UpdatedAt = time.Now().UTC()
	mapping.UpdatedBy = types.GetUserID(ctx)

	if err := s.EntityIntegrationMappingRepo.Update(ctx, mapping); err != nil {
		return nil, err
	}

	// Get customer details
	customer, err := s.CustomerRepo.Get(ctx, customerID)
	if err != nil {
		s.Logger.Warn("Failed to get customer details for mapping", "customer_id", customerID, "error", err)
		customer = nil
	}

	return s.mapCustomerMappingToResponse(mapping, customer), nil
}

func (s *customerIntegrationService) DeleteCustomerMapping(ctx context.Context, customerID string, providerType integration.ProviderType) error {
	mapping, err := s.EntityIntegrationMappingRepo.GetByEntityAndProvider(ctx, customerID, integration.EntityTypeCustomer, providerType)
	if err != nil {
		return err
	}

	return s.EntityIntegrationMappingRepo.Delete(ctx, mapping)
}

func (s *customerIntegrationService) ListCustomerMappings(ctx context.Context, filter *integration.EntityIntegrationMappingFilter) (*ListCustomerMappingsResponse, error) {
	// Ensure we're only getting customer mappings
	if filter == nil {
		filter = &integration.EntityIntegrationMappingFilter{}
	}
	filter.EntityTypes = []integration.EntityType{integration.EntityTypeCustomer}

	mappings, err := s.EntityIntegrationMappingRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	count, err := s.EntityIntegrationMappingRepo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}

	// Get customer details for each mapping
	responses := make([]CustomerMappingResponse, len(mappings))
	for i, mapping := range mappings {
		customer, err := s.CustomerRepo.Get(ctx, mapping.EntityID)
		if err != nil {
			s.Logger.Warn("Failed to get customer details for mapping", "customer_id", mapping.EntityID, "error", err)
			customer = nil
		}
		responses[i] = *s.mapCustomerMappingToResponse(mapping, customer)
	}

	return &ListCustomerMappingsResponse{
		Items:      responses,
		Pagination: types.NewPaginationResponse(count, filter.GetLimit(), filter.GetOffset()),
	}, nil
}

// Lookup Operations Implementation

func (s *customerIntegrationService) GetCustomerByProviderID(ctx context.Context, providerType integration.ProviderType, providerCustomerID string) (*CustomerMappingResponse, error) {
	if providerCustomerID == "" {
		return nil, ierr.NewError("provider_customer_id is required").
			WithHint("Provider customer ID must not be empty").
			Mark(ierr.ErrValidation)
	}

	mapping, err := s.EntityIntegrationMappingRepo.GetByProviderEntityID(ctx, providerType, providerCustomerID)
	if err != nil {
		return nil, err
	}

	if !mapping.IsCustomerMapping() {
		return nil, ierr.NewError("mapping is not for a customer").
			WithHint("The found mapping is not a customer mapping").
			Mark(ierr.ErrValidation)
	}

	// Get customer details
	customer, err := s.CustomerRepo.Get(ctx, mapping.EntityID)
	if err != nil {
		s.Logger.Warn("Failed to get customer details for mapping", "customer_id", mapping.EntityID, "error", err)
		customer = nil
	}

	return s.mapCustomerMappingToResponse(mapping, customer), nil
}

func (s *customerIntegrationService) GetProviderCustomerID(ctx context.Context, customerID string, providerType integration.ProviderType) (string, error) {
	mapping, err := s.EntityIntegrationMappingRepo.GetByEntityAndProvider(ctx, customerID, integration.EntityTypeCustomer, providerType)
	if err != nil {
		return "", err
	}

	return mapping.GetProviderCustomerID(), nil
}

// Webhook Processing Implementation

func (s *customerIntegrationService) ProcessStripeCustomerWebhook(ctx context.Context, webhook *webhookDto.StripeWebhookEvent) (*WebhookProcessingResponse, error) {
	if webhook == nil {
		return nil, ierr.NewError("webhook is required").
			WithHint("Webhook event must not be nil").
			Mark(ierr.ErrValidation)
	}

	if err := webhook.Validate(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid webhook format").
			Mark(ierr.ErrValidation)
	}

	// Extract customer data from webhook
	customerData, err := webhook.GetCustomerData()
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to extract customer data from webhook").
			Mark(ierr.ErrValidation)
	}

	// Process based on webhook type
	switch {
	case webhook.IsCustomerCreated():
		return s.ProcessCustomerCreatedWebhook(ctx, customerData)
	case webhook.IsCustomerUpdated():
		return s.ProcessCustomerUpdatedWebhook(ctx, customerData)
	default:
		return &WebhookProcessingResponse{
			Success:            false,
			Action:             "ignored",
			ProviderCustomerID: customerData.ID,
			Message:            fmt.Sprintf("Webhook type %s not supported", webhook.Type),
			ProcessedAt:        time.Now().UTC(),
		}, nil
	}
}

func (s *customerIntegrationService) ProcessCustomerCreatedWebhook(ctx context.Context, customerData *webhookDto.StripeCustomerData) (*WebhookProcessingResponse, error) {
	if customerData == nil {
		return nil, ierr.NewError("customer data is required").
			WithHint("Customer data must not be nil").
			Mark(ierr.ErrValidation)
	}

	// Extract external_id from metadata
	externalID := customerData.Metadata["external_id"]
	if externalID == "" {
		return &WebhookProcessingResponse{
			Success:            false,
			Action:             "skipped",
			ProviderCustomerID: customerData.ID,
			Message:            "No external_id in customer metadata - cannot map to FlexPrice customer",
			ProcessedAt:        time.Now().UTC(),
		}, nil
	}

	// Find FlexPrice customer by external_id using lookup key
	customer, err := s.CustomerRepo.GetByLookupKey(ctx, externalID)
	if err != nil {
		if ierr.IsNotFound(err) {
			// Create new customer if not found
			customer, err = s.createCustomerFromStripeData(ctx, customerData, externalID)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	// Check if mapping already exists
	existing, err := s.EntityIntegrationMappingRepo.GetByProviderEntityID(ctx, integration.ProviderTypeStripe, customerData.ID)
	if err != nil && !ierr.IsNotFound(err) {
		return nil, err
	}

	if existing != nil {
		// Mapping already exists, update if needed
		if existing.EntityID != customer.ID {
			existing.EntityID = customer.ID
			existing.UpdatedAt = time.Now().UTC()
			existing.UpdatedBy = types.GetUserID(ctx)

			if err := s.EntityIntegrationMappingRepo.Update(ctx, existing); err != nil {
				return nil, err
			}
		}

		return &WebhookProcessingResponse{
			Success:            true,
			Action:             "updated",
			CustomerID:         customer.ID,
			ProviderCustomerID: customerData.ID,
			MappingID:          existing.ID,
			Message:            "Customer mapping updated",
			ProcessedAt:        time.Now().UTC(),
		}, nil
	}

	// Create new mapping
	mapping := integration.CreateStripeCustomerMapping(
		customer.ID,
		customerData.ID,
		externalID,
		types.GetEnvironmentID(ctx),
	)

	mapping.TenantID = types.GetTenantID(ctx)
	mapping.BaseModel = types.GetDefaultBaseModel(ctx)

	if err := s.EntityIntegrationMappingRepo.Create(ctx, mapping); err != nil {
		return nil, err
	}

	return &WebhookProcessingResponse{
		Success:            true,
		Action:             "created",
		CustomerID:         customer.ID,
		ProviderCustomerID: customerData.ID,
		MappingID:          mapping.ID,
		Message:            "Customer mapping created successfully",
		ProcessedAt:        time.Now().UTC(),
		Metadata: map[string]interface{}{
			"external_id":      externalID,
			"customer_created": customer.CreatedAt.Equal(mapping.CreatedAt),
		},
	}, nil
}

func (s *customerIntegrationService) ProcessCustomerUpdatedWebhook(ctx context.Context, customerData *webhookDto.StripeCustomerData) (*WebhookProcessingResponse, error) {
	// Find existing mapping
	mapping, err := s.EntityIntegrationMappingRepo.GetByProviderEntityID(ctx, integration.ProviderTypeStripe, customerData.ID)
	if err != nil {
		if ierr.IsNotFound(err) {
			// Try to create mapping if customer exists with external_id
			return s.ProcessCustomerCreatedWebhook(ctx, customerData)
		}
		return nil, err
	}

	// Update customer information if needed
	customer, err := s.CustomerRepo.Get(ctx, mapping.EntityID)
	if err != nil {
		return nil, err
	}

	updated := false
	if customerData.Email != "" && customer.Email != customerData.Email {
		customer.Email = customerData.Email
		updated = true
	}

	if customerData.Name != "" && customer.Name != customerData.Name {
		customer.Name = customerData.Name
		updated = true
	}

	if updated {
		customer.UpdatedAt = time.Now().UTC()
		customer.UpdatedBy = types.GetUserID(ctx)

		if err := s.CustomerRepo.Update(ctx, customer); err != nil {
			return nil, err
		}
	}

	// Update mapping metadata
	if mapping.Metadata == nil {
		mapping.Metadata = make(map[string]interface{})
	}
	mapping.Metadata["last_stripe_update"] = time.Now().UTC()
	mapping.UpdatedAt = time.Now().UTC()
	mapping.UpdatedBy = types.GetUserID(ctx)

	if err := s.EntityIntegrationMappingRepo.Update(ctx, mapping); err != nil {
		return nil, err
	}

	action := "synchronized"
	if updated {
		action = "updated_customer"
	}

	return &WebhookProcessingResponse{
		Success:            true,
		Action:             action,
		CustomerID:         customer.ID,
		ProviderCustomerID: customerData.ID,
		MappingID:          mapping.ID,
		Message:            "Customer information synchronized",
		ProcessedAt:        time.Now().UTC(),
		Metadata: map[string]interface{}{
			"customer_updated": updated,
		},
	}, nil
}

// Bulk Operations (Stub implementations - to be expanded)

func (s *customerIntegrationService) BulkCreateCustomerMappings(ctx context.Context, req BulkCreateCustomerMappingsRequest) (*BulkCreateCustomerMappingsResponse, error) {
	// Implementation to be added - placeholder
	return &BulkCreateCustomerMappingsResponse{
		ProcessedAt: time.Now().UTC(),
	}, nil
}

func (s *customerIntegrationService) ValidateCustomerMappings(ctx context.Context, req ValidateCustomerMappingsRequest) (*ValidateCustomerMappingsResponse, error) {
	// Implementation to be added - placeholder
	return &ValidateCustomerMappingsResponse{
		ValidatedAt: time.Now().UTC(),
	}, nil
}

func (s *customerIntegrationService) MigrateExistingCustomers(ctx context.Context, req MigrateExistingCustomersRequest) (*MigrateExistingCustomersResponse, error) {
	// Implementation to be added - placeholder
	return &MigrateExistingCustomersResponse{
		MigratedAt: time.Now().UTC(),
	}, nil
}

// Helper methods

func (s *customerIntegrationService) validateCreateCustomerMappingRequest(req CreateCustomerMappingRequest) error {
	if req.CustomerID == "" {
		return ierr.NewError("customer_id is required").
			WithHint("Customer ID must not be empty").
			Mark(ierr.ErrValidation)
	}

	if req.ProviderCustomerID == "" {
		return ierr.NewError("provider_customer_id is required").
			WithHint("Provider customer ID must not be empty").
			Mark(ierr.ErrValidation)
	}

	// Validate provider-specific formats
	if req.ProviderType == integration.ProviderTypeStripe {
		if !integration.ValidateStripeCustomerID(req.ProviderCustomerID) {
			return ierr.NewError("invalid stripe customer id format").
				WithHint("Stripe customer ID must start with 'cus_'").
				Mark(ierr.ErrValidation)
		}
	}

	return nil
}

func (s *customerIntegrationService) createCustomerFromStripeData(ctx context.Context, customerData *webhookDto.StripeCustomerData, externalID string) (*customer.Customer, error) {
	newCustomer := &customer.Customer{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER),
		ExternalID:    externalID,
		Name:          customerData.Name,
		Email:         customerData.Email,
		Metadata:      make(map[string]string),
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}

	// Copy metadata from Stripe
	for key, value := range customerData.Metadata {
		newCustomer.Metadata[key] = value
	}

	if err := s.CustomerRepo.Create(ctx, newCustomer); err != nil {
		return nil, err
	}

	return newCustomer, nil
}

func (s *customerIntegrationService) mapCustomerMappingToResponse(mapping *integration.EntityIntegrationMapping, customer *customer.Customer) *CustomerMappingResponse {
	response := &CustomerMappingResponse{
		ID:                 mapping.ID,
		CustomerID:         mapping.EntityID,
		ProviderType:       mapping.ProviderType,
		ProviderCustomerID: mapping.GetProviderCustomerID(),
		ExternalID:         mapping.GetExternalID(),
		Customer:           customer,
		Metadata:           mapping.Metadata,
		TenantID:           mapping.TenantID,
		EnvironmentID:      mapping.EnvironmentID,
		CreatedAt:          mapping.CreatedAt,
		UpdatedAt:          mapping.UpdatedAt,
	}

	return response
}
