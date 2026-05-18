package paddle

import (
	"context"
	"fmt"
	"strings"
	"time"

	paddlesdk "github.com/PaddleHQ/paddle-go-sdk/v4"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// PaddleSyncService orchestrates syncing FlexPrice entities to Paddle.
type PaddleSyncService struct {
	client           PaddleClient
	customerRepo     customer.Repository
	invoiceRepo      invoice.Repository
	subscriptionRepo subscription.Repository
	mappingRepo      entityintegrationmapping.Repository
	connectionRepo   connection.Repository
	logger           *logger.Logger
	authSecret       string
}

// NewPaddleSyncService creates a new PaddleSyncService.
func NewPaddleSyncService(
	client PaddleClient,
	customerRepo customer.Repository,
	invoiceRepo invoice.Repository,
	subscriptionRepo subscription.Repository,
	mappingRepo entityintegrationmapping.Repository,
	connectionRepo connection.Repository,
	log *logger.Logger,
	authSecret string,
) *PaddleSyncService {
	return &PaddleSyncService{
		client:           client,
		customerRepo:     customerRepo,
		invoiceRepo:      invoiceRepo,
		subscriptionRepo: subscriptionRepo,
		mappingRepo:      mappingRepo,
		connectionRepo:   connectionRepo,
		logger:           log,
		authSecret:       authSecret,
	}
}

// EnsureCustomerSynced ensures the given FlexPrice customer exists in Paddle
// and that a corresponding EntityIntegrationMapping row is present.
// It is idempotent: if the customer is already synced it returns the existing
// Paddle IDs and Created=false.
func (s *PaddleSyncService) EnsureCustomerSynced(ctx context.Context, req EnsureCustomerSyncedRequest) (*EnsureCustomerSyncedResponse, error) {
	flexCustomer, err := s.customerRepo.Get(ctx, req.CustomerID)
	if err != nil {
		return nil, ierr.WithError(err).WithHint("Failed to load customer").Mark(ierr.ErrDatabase)
	}
	if flexCustomer.Email == "" {
		return nil, ierr.NewError("customer email is required for Paddle sync").
			WithHint("Add email to the customer before syncing to Paddle").
			WithReportableDetails(map[string]interface{}{"customer_id": req.CustomerID}).
			Mark(ierr.ErrValidation)
	}

	// Check for an existing mapping.
	filter := &types.EntityIntegrationMappingFilter{
		EntityID:      req.CustomerID,
		EntityType:    types.IntegrationEntityTypeCustomer,
		ProviderTypes: []string{string(types.SecretProviderPaddle)},
	}
	mappings, err := s.mappingRepo.List(ctx, filter)
	if err != nil {
		return nil, ierr.WithError(err).WithHint("Failed to query customer mapping").Mark(ierr.ErrDatabase)
	}

	if len(mappings) > 0 {
		m := mappings[0]
		paddleCustomerID := m.ProviderEntityID
		paddleAddressID, _ := m.Metadata[MetaKeyPaddleAddressID].(string)

		paddleAddressID, err = s.syncAddressForMapping(ctx, flexCustomer, paddleCustomerID, paddleAddressID, m)
		if err != nil {
			return nil, err
		}
		s.logger.Infow("customer already synced to Paddle",
			"customer_id", req.CustomerID, "paddle_customer_id", paddleCustomerID)
		return &EnsureCustomerSyncedResponse{
			PaddleCustomerID: paddleCustomerID,
			PaddleAddressID:  paddleAddressID,
			Created:          false,
		}, nil
	}

	// Create customer in Paddle.
	createReq := &paddlesdk.CreateCustomerRequest{
		Email: flexCustomer.Email,
		CustomData: map[string]interface{}{
			"flexprice_customer_id": flexCustomer.ID,
			"environment_id":        types.GetEnvironmentID(ctx),
		},
	}
	if flexCustomer.Name != "" {
		createReq.Name = paddlesdk.PtrTo(flexCustomer.Name)
	}
	paddleCustomer, err := s.client.CreateCustomer(ctx, createReq)
	if err != nil {
		return nil, ierr.WithError(err).WithHint("Failed to create customer in Paddle").Mark(ierr.ErrInternal)
	}
	paddleCustomerID := paddleCustomer.ID

	var paddleAddressID string
	if flexCustomer.AddressCountry != "" {
		addr, addrErr := s.client.CreateAddress(ctx, paddleCustomerID, buildCreateAddressRequest(flexCustomer))
		if addrErr != nil {
			s.logger.Warnw("failed to create Paddle address after customer creation — proceeding",
				"customer_id", req.CustomerID, "error", addrErr)
		} else {
			paddleAddressID = addr.ID
		}
	}

	meta := map[string]interface{}{
		MetaKeyCreatedVia:       CreatedViaFlexpriceToProvider,
		MetaKeyPaddleCustomerID: paddleCustomerID,
		MetaKeySyncedAt:         time.Now().UTC().Format(time.RFC3339),
	}
	if paddleAddressID != "" {
		meta[MetaKeyPaddleAddressID] = paddleAddressID
	}
	mapping := &entityintegrationmapping.EntityIntegrationMapping{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
		EntityID:         flexCustomer.ID,
		EntityType:       types.IntegrationEntityTypeCustomer,
		ProviderType:     string(types.SecretProviderPaddle),
		ProviderEntityID: paddleCustomerID,
		Metadata:         meta,
		EnvironmentID:    types.GetEnvironmentID(ctx),
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}
	if createErr := s.mappingRepo.Create(ctx, mapping); createErr != nil {
		if ierr.IsAlreadyExists(createErr) {
			// Concurrent race — use the mapping that won.
			existing, listErr := s.mappingRepo.List(ctx, filter)
			if listErr == nil && len(existing) > 0 {
				s.logger.Warnw("concurrent customer creation detected — discarding orphaned Paddle customer",
					"customer_id", req.CustomerID,
					"discarded_paddle_customer_id", paddleCustomerID,
					"winner_paddle_customer_id", existing[0].ProviderEntityID)
				existingAddressID, _ := existing[0].Metadata[MetaKeyPaddleAddressID].(string)
				return &EnsureCustomerSyncedResponse{
					PaddleCustomerID: existing[0].ProviderEntityID,
					PaddleAddressID:  existingAddressID,
					Created:          false,
				}, nil
			}
		}
		return nil, ierr.WithError(createErr).WithHint("Failed to persist customer mapping").Mark(ierr.ErrDatabase)
	}

	s.logger.Infow("successfully created customer in Paddle",
		"customer_id", req.CustomerID, "paddle_customer_id", paddleCustomerID)
	return &EnsureCustomerSyncedResponse{
		PaddleCustomerID: paddleCustomerID,
		PaddleAddressID:  paddleAddressID,
		Created:          true,
	}, nil
}

// EnsureProductSynced ensures a Paddle catalog product+price exists for the given FlexPrice price.
// The mapping key is the FlexPrice priceID. The returned PaddlePriceID (pri_xxx) can be used
// in SubscriptionChargeItemFromCatalog. No-op if the mapping already exists.
func (s *PaddleSyncService) EnsureProductSynced(ctx context.Context, req EnsureProductSyncedRequest) (*EnsureProductSyncedResponse, error) {
	if req.PriceID == "" {
		return nil, ierr.NewError("price ID is required").Mark(ierr.ErrValidation)
	}

	filter := &types.EntityIntegrationMappingFilter{
		EntityID:      req.PriceID,
		EntityType:    types.IntegrationEntityTypePrice,
		ProviderTypes: []string{string(types.SecretProviderPaddle)},
	}
	mappings, err := s.mappingRepo.List(ctx, filter)
	if err != nil {
		return nil, ierr.WithError(err).WithHint("Failed to query price mapping").Mark(ierr.ErrDatabase)
	}
	if len(mappings) > 0 {
		m := mappings[0]
		paddleProductID, _ := m.Metadata[MetaKeyPaddleProductID].(string)
		return &EnsureProductSyncedResponse{
			PaddlePriceID:   m.ProviderEntityID,
			PaddleProductID: paddleProductID,
			Created:         false,
		}, nil
	}

	// Create Paddle product.
	productName := req.Name
	if productName == "" {
		productName = defaultProductName
	}
	product, err := s.client.CreateProduct(ctx, &paddlesdk.CreateProductRequest{
		Name:        productName,
		TaxCategory: defaultTaxCategory,
		CustomData: map[string]interface{}{
			"flexprice_price_id": req.PriceID,
			"environment_id":     types.GetEnvironmentID(ctx),
		},
	})
	if err != nil {
		return nil, ierr.WithError(err).
			WithHintf("Failed to create Paddle product for price %s", req.PriceID).
			Mark(ierr.ErrInternal)
	}

	// Create catalog price (one-time, no billing_cycle).
	currency := strings.ToUpper(req.Currency)
	if currency == "" {
		currency = "USD"
	}
	amountCents := req.Amount.Mul(decimal.NewFromInt(100)).IntPart()
	if amountCents < 0 {
		amountCents = 0
	}
	price, err := s.client.CreatePrice(ctx, &paddlesdk.CreatePriceRequest{
		ProductID:   product.ID,
		Description: fmt.Sprintf("FlexPrice price %s", req.PriceID),
		Name:        paddlesdk.PtrTo(productName),
		UnitPrice: paddlesdk.Money{
			Amount:       fmt.Sprintf("%d", amountCents),
			CurrencyCode: paddlesdk.CurrencyCode(currency),
		},
		Quantity: &paddlesdk.PriceQuantity{Minimum: 1, Maximum: 100000},
		CustomData: map[string]interface{}{
			"flexprice_price_id": req.PriceID,
			"environment_id":     types.GetEnvironmentID(ctx),
		},
	})
	if err != nil {
		return nil, ierr.WithError(err).
			WithHintf("Failed to create Paddle price for price %s", req.PriceID).
			Mark(ierr.ErrInternal)
	}

	// Persist mapping: EntityID=priceID → ProviderEntityID=paddlePriceID.
	mapping := &entityintegrationmapping.EntityIntegrationMapping{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
		EntityID:         req.PriceID,
		EntityType:       types.IntegrationEntityTypePrice,
		ProviderType:     string(types.SecretProviderPaddle),
		ProviderEntityID: price.ID,
		Metadata: map[string]interface{}{
			MetaKeyPaddleProductID: product.ID,
			MetaKeyPaddlePriceID:   price.ID,
			MetaKeySyncedAt:        time.Now().UTC().Format(time.RFC3339),
		},
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
	if err := s.mappingRepo.Create(ctx, mapping); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Product+price created in Paddle but mapping failed to save").
			Mark(ierr.ErrDatabase)
	}

	s.logger.Infow("successfully created Paddle product+price for FlexPrice price",
		"price_id", req.PriceID, "paddle_product_id", product.ID, "paddle_price_id", price.ID)
	return &EnsureProductSyncedResponse{
		PaddlePriceID:   price.ID,
		PaddleProductID: product.ID,
		Created:         true,
	}, nil
}

// EnsureProductsSynced is the bulk form of EnsureProductSynced.
// It issues a single mapping query for all price IDs, then creates only the unmapped ones.
func (s *PaddleSyncService) EnsureProductsSynced(ctx context.Context, req EnsureProductsSyncedRequest) (*EnsureProductsSyncedResponse, error) {
	if len(req.Items) == 0 {
		return &EnsureProductsSyncedResponse{PriceIDToPaddlePriceID: map[string]string{}}, nil
	}

	priceIDs := make([]string, 0, len(req.Items))
	for _, item := range req.Items {
		if item.PriceID != "" {
			priceIDs = append(priceIDs, item.PriceID)
		}
	}

	// Bulk query existing mappings.
	bulkFilter := &types.EntityIntegrationMappingFilter{
		EntityIDs:     priceIDs,
		EntityType:    types.IntegrationEntityTypePrice,
		ProviderTypes: []string{string(types.SecretProviderPaddle)},
	}
	mappings, err := s.mappingRepo.List(ctx, bulkFilter)
	if err != nil {
		return nil, ierr.WithError(err).WithHint("Failed to bulk query price mappings").Mark(ierr.ErrDatabase)
	}
	result := make(map[string]string, len(req.Items))
	for _, m := range mappings {
		result[m.EntityID] = m.ProviderEntityID
	}

	// Create missing ones.
	for _, item := range req.Items {
		if item.PriceID == "" || result[item.PriceID] != "" {
			continue
		}
		resp, err := s.EnsureProductSynced(ctx, item)
		if err != nil {
			return nil, err
		}
		result[item.PriceID] = resp.PaddlePriceID
	}

	return &EnsureProductsSyncedResponse{PriceIDToPaddlePriceID: result}, nil
}

// syncAddressForMapping ensures the Paddle address for an already-mapped customer is up to date.
// paddleAddressID is the value currently stored in the mapping metadata (may be empty).
// It returns the final paddleAddressID (possibly newly created).
func (s *PaddleSyncService) syncAddressForMapping(
	ctx context.Context,
	c *customer.Customer,
	paddleCustomerID, paddleAddressID string,
	mapping *entityintegrationmapping.EntityIntegrationMapping,
) (string, error) {
	if c.AddressCountry == "" {
		return paddleAddressID, nil
	}
	if paddleAddressID != "" {
		updateReq := &paddlesdk.UpdateAddressRequest{
			CountryCode: paddlesdk.NewPatchField(toCountryCode(c.AddressCountry)),
		}
		if c.AddressLine1 != "" {
			updateReq.FirstLine = paddlesdk.NewPtrPatchField(c.AddressLine1)
		}
		if c.AddressLine2 != "" {
			updateReq.SecondLine = paddlesdk.NewPtrPatchField(c.AddressLine2)
		}
		if c.AddressCity != "" {
			updateReq.City = paddlesdk.NewPtrPatchField(c.AddressCity)
		}
		if c.AddressPostalCode != "" {
			updateReq.PostalCode = paddlesdk.NewPtrPatchField(c.AddressPostalCode)
		}
		if c.AddressState != "" {
			updateReq.Region = paddlesdk.NewPtrPatchField(c.AddressState)
		}
		if _, err := s.client.UpdateAddress(ctx, paddleCustomerID, paddleAddressID, updateReq); err != nil {
			s.logger.Warnw("failed to update Paddle address — using existing",
				"error", err, "customer_id", c.ID, "paddle_address_id", paddleAddressID)
		}
		return paddleAddressID, nil
	}

	// No address ID yet — create one.
	addr, err := s.client.CreateAddress(ctx, paddleCustomerID, buildCreateAddressRequest(c))
	if err != nil {
		return "", ierr.WithError(err).WithHint("Failed to create Paddle address").Mark(ierr.ErrInternal)
	}
	if mapping != nil {
		if mapping.Metadata == nil {
			mapping.Metadata = make(map[string]interface{})
		}
		mapping.Metadata[MetaKeyPaddleAddressID] = addr.ID
		mapping.Metadata[MetaKeySyncedAt] = time.Now().UTC().Format(time.RFC3339)
		if err := s.mappingRepo.Update(ctx, mapping); err != nil {
			s.logger.Warnw("failed to update mapping with new Paddle address ID", "error", err)
		}
	}
	return addr.ID, nil
}
