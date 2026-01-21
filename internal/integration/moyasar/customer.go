package moyasar

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

// MoyasarCustomerService defines the interface for Moyasar customer operations
// Note: Moyasar doesn't have a first-class customer API like Stripe or Razorpay.
// This service provides a facade for managing customer metadata mappings.
type MoyasarCustomerService interface {
	GetFlexPriceCustomerID(ctx context.Context, moyasarCustomerRef string) (string, error)
	StoreCustomerMapping(ctx context.Context, flexpriceCustomerID string, moyasarCustomerRef string) error
	HasCustomerMapping(ctx context.Context, flexpriceCustomerID string) (bool, error)
	// Token management methods
	GetCustomerTokens(ctx context.Context, customerID string, customerService interfaces.CustomerService) ([]string, error)
	SaveCustomerToken(ctx context.Context, customerID string, tokenID string) error
}

// CustomerService handles Moyasar customer operations
type CustomerService struct {
	client                       MoyasarClient
	entityIntegrationMappingRepo entityintegrationmapping.Repository
	logger                       *logger.Logger
}

// NewCustomerService creates a new Moyasar customer service
func NewCustomerService(
	client MoyasarClient,
	entityIntegrationMappingRepo entityintegrationmapping.Repository,
	logger *logger.Logger,
) MoyasarCustomerService {
	return &CustomerService{
		client:                       client,
		entityIntegrationMappingRepo: entityIntegrationMappingRepo,
		logger:                       logger,
	}
}

// GetFlexPriceCustomerID retrieves the FlexPrice customer ID from a Moyasar customer reference
func (s *CustomerService) GetFlexPriceCustomerID(ctx context.Context, moyasarCustomerRef string) (string, error) {
	if moyasarCustomerRef == "" {
		return "", nil
	}

	// Look up the entity integration mapping
	filter := &types.EntityIntegrationMappingFilter{
		QueryFilter:       types.NewNoLimitQueryFilter(),
		ProviderEntityIDs: []string{moyasarCustomerRef},
		ProviderTypes:     []string{string(types.SecretProviderMoyasar)},
		EntityType:        types.IntegrationEntityTypeCustomer,
	}

	mappings, err := s.entityIntegrationMappingRepo.List(ctx, filter)
	if err != nil {
		s.logger.Errorw("failed to look up customer mapping",
			"moyasar_customer_ref", moyasarCustomerRef,
			"error", err)
		return "", err
	}

	if len(mappings) > 0 {
		return mappings[0].EntityID, nil
	}

	return "", nil
}

// StoreCustomerMapping stores a mapping between FlexPrice and Moyasar customer IDs
func (s *CustomerService) StoreCustomerMapping(ctx context.Context, flexpriceCustomerID string, moyasarCustomerRef string) error {
	if flexpriceCustomerID == "" || moyasarCustomerRef == "" {
		return nil
	}

	// Check if mapping already exists
	hasMapping, err := s.HasCustomerMapping(ctx, flexpriceCustomerID)
	if err != nil {
		return err
	}
	if hasMapping {
		s.logger.Debugw("customer mapping already exists",
			"flexprice_customer_id", flexpriceCustomerID,
			"moyasar_customer_ref", moyasarCustomerRef)
		return nil
	}

	// Create new mapping
	mapping := &entityintegrationmapping.EntityIntegrationMapping{
		EntityID:         flexpriceCustomerID,
		EntityType:       types.IntegrationEntityTypeCustomer,
		ProviderType:     string(types.SecretProviderMoyasar),
		ProviderEntityID: moyasarCustomerRef,
	}

	err = s.entityIntegrationMappingRepo.Create(ctx, mapping)
	if err != nil {
		s.logger.Errorw("failed to create customer mapping",
			"flexprice_customer_id", flexpriceCustomerID,
			"moyasar_customer_ref", moyasarCustomerRef,
			"error", err)
		return err
	}

	s.logger.Infow("stored customer mapping",
		"flexprice_customer_id", flexpriceCustomerID,
		"moyasar_customer_ref", moyasarCustomerRef)

	return nil
}

// HasCustomerMapping checks if a FlexPrice customer has a Moyasar mapping
func (s *CustomerService) HasCustomerMapping(ctx context.Context, flexpriceCustomerID string) (bool, error) {
	if flexpriceCustomerID == "" {
		return false, nil
	}

	filter := &types.EntityIntegrationMappingFilter{
		QueryFilter:   types.NewNoLimitQueryFilter(),
		EntityID:      flexpriceCustomerID,
		ProviderTypes: []string{string(types.SecretProviderMoyasar)},
		EntityType:    types.IntegrationEntityTypeCustomer,
	}

	mappings, err := s.entityIntegrationMappingRepo.List(ctx, filter)
	if err != nil {
		return false, err
	}

	return len(mappings) > 0, nil
}

// ============================================================================
// Token Management Methods
// ============================================================================

// GetCustomerTokens retrieves all saved token IDs for a customer
// Tokens are stored as entity integration mappings with entity type "payment_method"
// Customer ID is stored in EntityID, and Token ID is stored in ProviderEntityID
func (s *CustomerService) GetCustomerTokens(ctx context.Context, customerID string, customerService interfaces.CustomerService) ([]string, error) {
	if customerID == "" {
		return nil, nil
	}

	// Look up token mappings for this customer
	// We use a composite key: customerID is stored in metadata, and entity_id is the token ID
	filter := &types.EntityIntegrationMappingFilter{
		QueryFilter:   types.NewNoLimitQueryFilter(),
		EntityID:      customerID, // Customer ID is the EntityID
		ProviderTypes: []string{string(types.SecretProviderMoyasar) + "_token"},
		EntityType:    "payment_method", // EntityType is "payment_method"
	}

	mappings, err := s.entityIntegrationMappingRepo.List(ctx, filter)
	if err != nil {
		s.logger.Errorw("failed to look up customer tokens",
			"customer_id", customerID,
			"error", err)
		return nil, err
	}

	var tokenIDs []string
	for _, mapping := range mappings {
		tokenIDs = append(tokenIDs, mapping.ProviderEntityID)
	}

	s.logger.Debugw("retrieved customer tokens",
		"customer_id", customerID,
		"token_count", len(tokenIDs))

	return tokenIDs, nil
}

// SaveCustomerToken saves a token ID for a customer
func (s *CustomerService) SaveCustomerToken(ctx context.Context, customerID string, tokenID string) error {
	if customerID == "" || tokenID == "" {
		return nil
	}

	// Check if mapping already exists
	filter := &types.EntityIntegrationMappingFilter{
		QueryFilter:       types.NewNoLimitQueryFilter(),
		EntityID:          customerID,
		ProviderEntityIDs: []string{tokenID},
		ProviderTypes:     []string{string(types.SecretProviderMoyasar) + "_token"},
		EntityType:        "payment_method",
	}

	mappings, err := s.entityIntegrationMappingRepo.List(ctx, filter)
	if err == nil && len(mappings) > 0 {
		s.logger.Debugw("token mapping already exists",
			"customer_id", customerID,
			"token_id", tokenID)
		return nil
	}

	// Create new token mapping
	mapping := &entityintegrationmapping.EntityIntegrationMapping{
		EntityID:         customerID,
		EntityType:       "payment_method",
		ProviderType:     string(types.SecretProviderMoyasar) + "_token",
		ProviderEntityID: tokenID,
	}

	err = s.entityIntegrationMappingRepo.Create(ctx, mapping)
	if err != nil {
		s.logger.Errorw("failed to save customer token",
			"customer_id", customerID,
			"token_id", tokenID,
			"error", err)
		return err
	}

	s.logger.Infow("saved customer token",
		"customer_id", customerID,
		"token_id", tokenID)

	return nil
}
