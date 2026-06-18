package moyasar

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	domain "github.com/flexprice/flexprice/internal/domain/paymentmethod"
	ierr "github.com/flexprice/flexprice/internal/errors"
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
	// Payment method management (stored in payment_methods table)
	GetCustomerPaymentMethods(ctx context.Context, customerID string) ([]*domain.PaymentMethod, error)
	SavePaymentMethod(ctx context.Context, customerID, tokenID string, methodDetails map[string]interface{}) error
}

// CustomerService handles Moyasar customer operations
type CustomerService struct {
	client                       MoyasarClient
	entityIntegrationMappingRepo entityintegrationmapping.Repository
	paymentMethodRepo            domain.Repository
	logger                       *logger.Logger
}

// NewCustomerService creates a new Moyasar customer service
func NewCustomerService(
	client MoyasarClient,
	entityIntegrationMappingRepo entityintegrationmapping.Repository,
	paymentMethodRepo domain.Repository,
	logger *logger.Logger,
) MoyasarCustomerService {
	return &CustomerService{
		client:                       client,
		entityIntegrationMappingRepo: entityIntegrationMappingRepo,
		paymentMethodRepo:            paymentMethodRepo,
		logger:                       logger,
	}
}

// GetFlexPriceCustomerID retrieves the FlexPrice customer ID from a Moyasar customer reference
func (s *CustomerService) GetFlexPriceCustomerID(ctx context.Context, moyasarCustomerRef string) (string, error) {
	if moyasarCustomerRef == "" {
		return "", nil
	}

	filter := &types.EntityIntegrationMappingFilter{
		QueryFilter:       types.NewNoLimitQueryFilter(),
		ProviderEntityIDs: []string{moyasarCustomerRef},
		ProviderTypes:     []string{string(types.SecretProviderMoyasar)},
		EntityType:        types.IntegrationEntityTypeCustomer,
	}

	mappings, err := s.entityIntegrationMappingRepo.List(ctx, filter)
	if err != nil {
		s.logger.Error(ctx, "failed to look up customer mapping",
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

	hasMapping, err := s.HasCustomerMapping(ctx, flexpriceCustomerID)
	if err != nil {
		return err
	}
	if hasMapping {
		s.logger.Debug(ctx, "customer mapping already exists",
			"flexprice_customer_id", flexpriceCustomerID,
			"moyasar_customer_ref", moyasarCustomerRef)
		return nil
	}

	mapping := &entityintegrationmapping.EntityIntegrationMapping{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
		EntityID:         flexpriceCustomerID,
		EntityType:       types.IntegrationEntityTypeCustomer,
		ProviderType:     string(types.SecretProviderMoyasar),
		ProviderEntityID: moyasarCustomerRef,
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}

	err = s.entityIntegrationMappingRepo.Create(ctx, mapping)
	if err != nil {
		// Handle duplicate key error gracefully (race condition between check and create)
		if ierr.IsAlreadyExists(err) {
			s.logger.Debug(ctx, "customer mapping already exists (race condition)",
				"flexprice_customer_id", flexpriceCustomerID,
				"moyasar_customer_ref", moyasarCustomerRef)
			return nil // Mapping already exists, treat as success
		}
		s.logger.Error(ctx, "failed to create customer mapping",
			"flexprice_customer_id", flexpriceCustomerID,
			"moyasar_customer_ref", moyasarCustomerRef,
			"error", err)
		return err
	}

	s.logger.Info(ctx, "stored customer mapping",
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
// Payment Method Management (payment_methods table)
// ============================================================================

// GetCustomerPaymentMethods returns all active payment methods for a customer from the payment_methods table.
func (s *CustomerService) GetCustomerPaymentMethods(ctx context.Context, customerID string) ([]*domain.PaymentMethod, error) {
	if customerID == "" {
		return nil, nil
	}

	activeStatus := types.PaymentMethodStatusActive
	methods, err := s.paymentMethodRepo.List(ctx, &types.PaymentMethodFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		CustomerID:  customerID,
		Status:      &activeStatus,
	})
	if err != nil {
		s.logger.Error(ctx, "failed to list customer payment methods",
			"customer_id", customerID, "error", err)
		return nil, err
	}

	s.logger.Debug(ctx, "retrieved customer payment methods",
		"customer_id", customerID, "count", len(methods))

	return methods, nil
}

// SavePaymentMethod saves a Moyasar token as an active payment method for a customer.
// Card details are fetched from the Moyasar token API to ensure completeness (month, year, etc.)
// rather than relying on what Moyasar.js returns in the payment callback.
func (s *CustomerService) SavePaymentMethod(ctx context.Context, customerID, tokenID string, _ map[string]interface{}) error {
	if customerID == "" || tokenID == "" {
		return ierr.NewError("customer_id and token_id are required").Mark(ierr.ErrValidation)
	}

	// Fetch full card details from Moyasar token API.
	token, err := s.client.GetToken(ctx, tokenID)
	if err != nil {
		s.logger.Error(ctx, "failed to fetch token details from Moyasar",
			"customer_id", customerID, "token_id", tokenID, "error", err)
		return err
	}

	methodDetails := map[string]interface{}{
		"brand":     token.Brand,
		"last4":     token.Last4,
		"exp_month": token.Month,
		"exp_year":  token.Year,
		"name":      token.Name,
	}

	pm := &domain.PaymentMethod{
		ID:                  types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PAYMENT_METHOD),
		CustomerID:          customerID,
		Type:                types.PaymentMethodTypeCard,
		Gateway:             types.PaymentGatewayTypeMoyasar,
		GatewayMethodID:     tokenID,
		PaymentMethodStatus: types.PaymentMethodStatusInactive, // activated after payment confirmation
		IsDefault:           false,
		MethodDetails:       methodDetails,
		BaseModel:           types.GetDefaultBaseModel(ctx),
	}

	if err := s.paymentMethodRepo.Create(ctx, pm); err != nil {
		s.logger.Error(ctx, "failed to save payment method",
			"customer_id", customerID, "token_id", tokenID, "error", err)
		return err
	}

	s.logger.Info(ctx, "saved payment method",
		"customer_id", customerID, "token_id", tokenID, "payment_method_id", pm.ID)

	return nil
}
