package paddle

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	paddlesdk "github.com/PaddleHQ/paddle-go-sdk/v4"
	"github.com/PaddleHQ/paddle-go-sdk/v4/pkg/paddlenotification"
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/golang-jwt/jwt/v4"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

const (
	defaultProductName = "Flexprice Invoice Item"
	defaultTaxCategory = paddlesdk.TaxCategoryStandard
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

// getOrCreateZeroDollarPrice returns the Paddle price ID used to bootstrap zero-dollar
// subscriptions. Created once per Paddle connection and cached in connection.Metadata.
func (s *PaddleSyncService) getOrCreateZeroDollarPrice(ctx context.Context) (string, error) {
	conn, err := s.connectionRepo.GetByProvider(ctx, types.SecretProviderPaddle)
	if err != nil {
		return "", ierr.WithError(err).WithHint("Failed to load Paddle connection").Mark(ierr.ErrDatabase)
	}
	if conn == nil {
		return "", ierr.NewError("Paddle connection not found").Mark(ierr.ErrNotFound)
	}

	// Return cached price ID if present.
	if conn.Metadata != nil {
		if priceID, ok := conn.Metadata[ConnKeyZeroDollarPriceID].(string); ok && priceID != "" {
			return priceID, nil
		}
	}

	// Bootstrap: create product + $0/month price.
	product, err := s.client.CreateProduct(ctx, &paddlesdk.CreateProductRequest{
		Name:        "FlexPrice Subscription Anchor",
		TaxCategory: defaultTaxCategory,
		CustomData:  map[string]interface{}{"created_by": "flexprice_subscription_bootstrap"},
	})
	if err != nil {
		return "", ierr.WithError(err).
			WithHint("Failed to create Paddle anchor product for $0 subscription").
			Mark(ierr.ErrInternal)
	}

	price, err := s.client.CreatePrice(ctx, &paddlesdk.CreatePriceRequest{
		ProductID:   product.ID,
		Description: "FlexPrice zero-dollar subscription anchor price",
		Name:        paddlesdk.PtrTo("FlexPrice Subscription Anchor"),
		UnitPrice: paddlesdk.Money{
			Amount:       "0",
			CurrencyCode: paddlesdk.CurrencyCodeUSD,
		},
		BillingCycle: &paddlesdk.Duration{
			Interval:  paddlesdk.IntervalMonth,
			Frequency: 1,
		},
		Quantity: &paddlesdk.PriceQuantity{Minimum: 1, Maximum: 1},
	})
	if err != nil {
		return "", ierr.WithError(err).
			WithHint("Failed to create Paddle $0/month anchor price").
			Mark(ierr.ErrInternal)
	}

	// Cache in connection metadata.
	if conn.Metadata == nil {
		conn.Metadata = make(map[string]interface{})
	}
	conn.Metadata[ConnKeyZeroDollarProductID] = product.ID
	conn.Metadata[ConnKeyZeroDollarPriceID] = price.ID
	conn.UpdatedAt = time.Now().UTC()
	if err := s.connectionRepo.Update(ctx, conn); err != nil {
		s.logger.Warnw("created Paddle anchor price but failed to cache in connection metadata — will re-create on next call",
			"paddle_price_id", price.ID, "error", err)
	}

	s.logger.Infow("successfully bootstrapped Paddle zero-dollar subscription anchor",
		"paddle_product_id", product.ID, "paddle_price_id", price.ID)
	return price.ID, nil
}

// EnsureSubscriptionSynced ensures a zero-dollar Paddle subscription exists for the given
// FlexPrice subscription. The Paddle subscription is bootstrapped via a $0 transaction.
// Returns immediately if already mapped — this is the primary guard against duplicate subscriptions.
func (s *PaddleSyncService) EnsureSubscriptionSynced(ctx context.Context, req EnsureSubscriptionSyncedRequest) (*EnsureSubscriptionSyncedResponse, error) {
	if req.SubscriptionID == "" {
		return nil, ierr.NewError("subscription ID is required").Mark(ierr.ErrValidation)
	}

	// Idempotency check — prevents creating duplicate Paddle subscriptions.
	filter := &types.EntityIntegrationMappingFilter{
		EntityID:      req.SubscriptionID,
		EntityType:    types.IntegrationEntityTypeSubscription,
		ProviderTypes: []string{string(types.SecretProviderPaddle)},
	}
	mappings, err := s.mappingRepo.List(ctx, filter)
	if err != nil {
		return nil, ierr.WithError(err).WithHint("Failed to query subscription mapping").Mark(ierr.ErrDatabase)
	}
	if len(mappings) > 0 {
		s.logger.Infow("subscription already synced to Paddle",
			"subscription_id", req.SubscriptionID, "paddle_subscription_id", mappings[0].ProviderEntityID)
		return &EnsureSubscriptionSyncedResponse{
			PaddleSubscriptionID: mappings[0].ProviderEntityID,
			Created:              false,
		}, nil
	}

	// Get the $0 anchor price (created once per connection).
	zeroPriceID, err := s.getOrCreateZeroDollarPrice(ctx)
	if err != nil {
		return nil, err
	}

	// Ensure customer is synced first.
	customerResp, err := s.EnsureCustomerSynced(ctx, EnsureCustomerSyncedRequest{CustomerID: req.CustomerID})
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Customer must be synced to Paddle before subscription can be created").
			Mark(ierr.ErrInternal)
	}
	if customerResp.PaddleAddressID == "" {
		return nil, ierr.NewError("Paddle address ID not found after customer sync").
			WithHint("Customer must have an address (country required) for Paddle subscription creation").
			WithReportableDetails(map[string]interface{}{"customer_id": req.CustomerID}).
			Mark(ierr.ErrValidation)
	}

	// Create a $0 transaction; Paddle processes it automatically and creates a subscription.
	txn, err := s.client.CreateTransaction(ctx, &paddlesdk.CreateTransactionRequest{
		CustomerID:     paddlesdk.PtrTo(customerResp.PaddleCustomerID),
		AddressID:      paddlesdk.PtrTo(customerResp.PaddleAddressID),
		CollectionMode: paddlesdk.PtrTo(paddlesdk.CollectionModeAutomatic),
		Items: []paddlesdk.CreateTransactionItems{
			*paddlesdk.NewCreateTransactionItemsTransactionItemFromCatalog(
				&paddlesdk.TransactionItemFromCatalog{
					PriceID:  zeroPriceID,
					Quantity: 1,
				},
			),
		},
		CustomData: map[string]interface{}{
			"flexprice_subscription_id": req.SubscriptionID,
			"environment_id":            types.GetEnvironmentID(ctx),
		},
	})
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to create $0 Paddle transaction for subscription bootstrap").
			Mark(ierr.ErrInternal)
	}

	if txn.SubscriptionID == nil || *txn.SubscriptionID == "" {
		return nil, ierr.NewError("Paddle transaction did not produce a subscription_id").
			WithHint("Ensure the anchor price has billing_cycle set (monthly) so Paddle creates a subscription").
			WithReportableDetails(map[string]interface{}{"paddle_transaction_id": txn.ID}).
			Mark(ierr.ErrInternal)
	}
	paddleSubID := *txn.SubscriptionID

	// Persist mapping.
	mapping := &entityintegrationmapping.EntityIntegrationMapping{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
		EntityID:         req.SubscriptionID,
		EntityType:       types.IntegrationEntityTypeSubscription,
		ProviderType:     string(types.SecretProviderPaddle),
		ProviderEntityID: paddleSubID,
		Metadata: map[string]interface{}{
			MetaKeyCreatedVia:          CreatedViaFlexpriceToProvider,
			MetaKeyPaddleTransactionID: txn.ID,
			MetaKeySyncedAt:            time.Now().UTC().Format(time.RFC3339),
		},
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
	if err := s.mappingRepo.Create(ctx, mapping); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Subscription created in Paddle but mapping failed to save").
			Mark(ierr.ErrDatabase)
	}

	s.logger.Infow("successfully created Paddle subscription for FlexPrice subscription",
		"subscription_id", req.SubscriptionID, "paddle_subscription_id", paddleSubID)
	return &EnsureSubscriptionSyncedResponse{PaddleSubscriptionID: paddleSubID, Created: true}, nil
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

// NOTE: parsePaddleCents is defined in invoice.go. Once invoice.go is deleted (Task 7),
// it must be moved here. For now it remains there to avoid a duplicate-declaration error.

// getExistingInvoiceMapping checks entity_integration_mapping for a prior Paddle sync of this invoice.
func (s *PaddleSyncService) getExistingInvoiceMapping(ctx context.Context, invoiceID string) (*entityintegrationmapping.EntityIntegrationMapping, error) {
	filter := &types.EntityIntegrationMappingFilter{
		EntityType:    types.IntegrationEntityTypeInvoice,
		EntityID:      invoiceID,
		ProviderTypes: []string{string(types.SecretProviderPaddle)},
	}
	mappings, err := s.mappingRepo.List(ctx, filter)
	if err != nil {
		return nil, ierr.WithError(err).WithHint("Failed to check invoice mapping").Mark(ierr.ErrDatabase)
	}
	if len(mappings) == 0 {
		return nil, nil
	}
	return mappings[0], nil
}

// appendCheckoutToken appends a signed JWT containing client_side_token + success_url to the
// checkout URL so the frontend can initialize Paddle.js overlay checkout.
func (s *PaddleSyncService) appendCheckoutToken(ctx context.Context, checkoutURL string) string {
	if checkoutURL == "" {
		return checkoutURL
	}

	paddleConfig, err := s.client.GetPaddleConfig(ctx)
	if err != nil || paddleConfig == nil || paddleConfig.ClientSideToken == "" {
		s.logger.Debugw("skipping checkout token: client_side_token not configured")
		return checkoutURL
	}

	conn, err := s.client.GetConnection(ctx)
	if err != nil || conn == nil || conn.Metadata == nil {
		return checkoutURL
	}
	successURL, _ := conn.Metadata[ConnKeyRedirectURL].(string)

	claims := jwt.MapClaims{
		"client_side_token": paddleConfig.ClientSideToken,
		"success_url":       successURL,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signedToken, err := token.SignedString([]byte(s.authSecret))
	if err != nil {
		s.logger.Warnw("failed to sign Paddle checkout token", "error", err)
		return checkoutURL
	}

	parsed, err := url.Parse(checkoutURL)
	if err != nil {
		s.logger.Warnw("failed to parse Paddle checkout URL for token append",
			"error", err, "checkout_url", checkoutURL)
		return checkoutURL
	}
	q := parsed.Query()
	q.Set("token", signedToken)
	parsed.RawQuery = q.Encode()
	s.logger.Debugw("appended checkout token to Paddle checkout URL")
	return parsed.String()
}

// buildChargeItems converts FlexPrice invoice line items to Paddle SubscriptionChargeItemFromCatalog
// items using the priceID → paddlePriceID mapping.
func buildChargeItems(flexInvoice *invoice.Invoice, priceIDMap map[string]string) ([]paddlesdk.CreateSubscriptionChargeItems, error) {
	var items []paddlesdk.CreateSubscriptionChargeItems
	for _, li := range flexInvoice.LineItems {
		if li == nil {
			continue
		}
		priceID := lo.FromPtr(li.PriceID)
		paddlePriceID := priceIDMap[priceID]
		if paddlePriceID == "" {
			return nil, ierr.NewError(fmt.Sprintf("no Paddle price ID for FlexPrice price %s", priceID)).
				WithHint("Ensure all invoice line item prices are synced to Paddle before calling SyncInvoice").
				Mark(ierr.ErrValidation)
		}
		quantity := 1
		if !li.Quantity.IsZero() {
			if q := li.Quantity.IntPart(); q > 0 {
				quantity = int(q)
			}
		}
		items = append(items, *paddlesdk.NewCreateSubscriptionChargeItemsSubscriptionChargeItemFromCatalog(
			&paddlesdk.SubscriptionChargeItemFromCatalog{
				PriceID:  paddlePriceID,
				Quantity: quantity,
			},
		))
	}
	if len(items) == 0 {
		return nil, ierr.NewError("invoice has no syncable line items").Mark(ierr.ErrValidation)
	}
	return items, nil
}

// buildPreviewItemsForSync builds the preview items for the Paddle PreviewTransaction call,
// preserving the same order as non-zero FlexPrice line items so that
// preview.Details.LineItems[i] maps back to that same line item by index.
func buildPreviewItemsForSync(flexInvoice *invoice.Invoice) ([]paddlesdk.TransactionPreviewByCustomerItems, []*invoice.InvoiceLineItem) {
	var previewItems []paddlesdk.TransactionPreviewByCustomerItems
	var includedLineItems []*invoice.InvoiceLineItem

	for _, item := range flexInvoice.LineItems {
		if item.Amount.IsZero() {
			continue
		}

		quantity := 1
		if !item.Quantity.IsZero() {
			if q := item.Quantity.IntPart(); q > 0 {
				quantity = int(q)
			}
		}

		unitAmount := item.Amount
		if quantity > 1 {
			unitAmount = item.Amount.Div(decimal.NewFromInt(int64(quantity)))
		}

		amountInCents := unitAmount.Mul(decimal.NewFromInt(100)).IntPart()
		if amountInCents < 0 {
			amountInCents = 0
		}

		currency := strings.ToUpper(item.Currency)
		if currency == "" {
			currency = strings.ToUpper(flexInvoice.Currency)
		}
		if currency == "" {
			currency = "USD"
		}

		priceQuantity := paddlesdk.PriceQuantity{Minimum: 1, Maximum: 100}
		if quantity > 100 {
			priceQuantity.Maximum = quantity
		}

		description := ""
		productName := defaultProductName
		if item.DisplayName != nil && *item.DisplayName != "" {
			description = *item.DisplayName
			productName = *item.DisplayName
		}

		previewItem := paddlesdk.NewTransactionPreviewByCustomerItemsTransactionPreviewItemCreateWithProduct(
			&paddlesdk.TransactionPreviewItemCreateWithProduct{
				Quantity:        quantity,
				IncludeInTotals: true,
				Price: paddlesdk.TransactionPriceCreateWithProduct{
					Description: description,
					UnitPrice: paddlesdk.Money{
						Amount:       fmt.Sprintf("%d", amountInCents),
						CurrencyCode: paddlesdk.CurrencyCode(currency),
					},
					Quantity: priceQuantity,
					Product: paddlesdk.TransactionSubscriptionProductCreate{
						Name:        productName,
						TaxCategory: defaultTaxCategory,
					},
				},
			},
		)
		previewItems = append(previewItems, *previewItem)
		includedLineItems = append(includedLineItems, item)
	}

	return previewItems, includedLineItems
}

// previewAndSyncTax calls the Paddle transactions/preview endpoint to get the exact
// tax breakdown before creating the real transaction, then updates the FlexPrice invoice
// so that its totals match what Paddle will actually charge the customer.
func (s *PaddleSyncService) previewAndSyncTax(
	ctx context.Context,
	flexInvoice *invoice.Invoice,
	paddleCustomerID, paddleAddressID string,
) error {
	previewItems, includedLineItems := buildPreviewItemsForSync(flexInvoice)
	if len(previewItems) == 0 {
		s.logger.Debugw("no preview items to sync tax for", "invoice_id", flexInvoice.ID)
		return nil
	}

	currency := paddlesdk.CurrencyCode(strings.ToUpper(flexInvoice.Currency))
	if currency == "" {
		currency = paddlesdk.CurrencyCodeUSD
	}

	previewReq := paddlesdk.NewPreviewTransactionCreateRequestTransactionPreviewByCustomer(
		&paddlesdk.TransactionPreviewByCustomer{
			CustomerID:   paddlesdk.PtrTo(paddleCustomerID),
			AddressID:    paddleAddressID,
			CurrencyCode: paddlesdk.PtrTo(currency),
			Items:        previewItems,
		},
	)

	preview, err := s.client.PreviewTransaction(ctx, previewReq)
	if err != nil {
		return err
	}

	s.logger.Infow("received Paddle tax preview",
		"invoice_id", flexInvoice.ID,
		"tax_cents", preview.Details.Totals.Tax,
		"grand_total_cents", preview.Details.Totals.GrandTotal,
		"line_items_count", len(preview.Details.LineItems))

	// Per-line-item tax — preview.Details.LineItems is ordered the same as our previewItems input.
	for i, previewLineItem := range preview.Details.LineItems {
		if i >= len(includedLineItems) {
			break
		}
		flexLineItem := includedLineItems[i]
		lineTax := parsePaddleCents(previewLineItem.Totals.Tax)

		if flexLineItem.Metadata == nil {
			flexLineItem.Metadata = make(types.Metadata)
		}
		flexLineItem.Metadata[MetaKeyPaddleTaxAmount] = lineTax.String()
		flexLineItem.Metadata[MetaKeyPaddleTaxRate] = previewLineItem.TaxRate

		s.logger.Debugw("per-line Paddle tax synced",
			"invoice_id", flexInvoice.ID,
			"line_item_id", flexLineItem.ID,
			"line_tax", lineTax,
			"tax_rate", previewLineItem.TaxRate)
	}

	// Invoice-level aggregates — always use Paddle's own aggregate totals.
	aggTax := parsePaddleCents(preview.Details.Totals.Tax)
	grandTotal := parsePaddleCents(preview.Details.Totals.GrandTotal)

	if grandTotal.IsPositive() {
		flexInvoice.TotalTax = aggTax
		flexInvoice.Total = grandTotal
		flexInvoice.AmountDue = grandTotal
		flexInvoice.AmountRemaining = grandTotal.Sub(flexInvoice.AmountPaid)
		if flexInvoice.AmountRemaining.IsNegative() {
			flexInvoice.AmountRemaining = decimal.Zero
		}

		if flexInvoice.Metadata == nil {
			flexInvoice.Metadata = make(types.Metadata)
		}
		flexInvoice.Metadata[MetaKeyPaddleTaxAmount] = aggTax.String()
		flexInvoice.Metadata[MetaKeyPaddleGrandTotal] = grandTotal.String()
		flexInvoice.Metadata[MetaKeyPaddleSubtotal] = parsePaddleCents(preview.Details.Totals.Subtotal).String()

		if err := s.invoiceRepo.Update(ctx, flexInvoice); err != nil {
			s.logger.Errorw("failed to persist tax-synced invoice totals",
				"error", err, "invoice_id", flexInvoice.ID)
			return err
		}

		s.logger.Infow("successfully synced Paddle tax to FlexPrice invoice",
			"invoice_id", flexInvoice.ID,
			"total_tax", aggTax,
			"grand_total", grandTotal,
			"amount_due", flexInvoice.AmountDue)
	}

	return nil
}

// SyncInvoice is the main invoice sync orchestrator.
// Idempotent: returns early if the invoice is already mapped to a Paddle transaction.
// All invoices use CollectionModeAutomatic.
func (s *PaddleSyncService) SyncInvoice(ctx context.Context, req SyncInvoiceRequest) (*SyncInvoiceResponse, error) {
	s.logger.Infow("starting Paddle invoice sync", "invoice_id", req.InvoiceID)

	if !s.client.HasPaddleConnection(ctx) {
		return nil, ierr.NewError("Paddle connection not available").
			WithHint("Paddle integration must be configured for invoice sync").
			Mark(ierr.ErrNotFound)
	}

	flexInvoice, err := s.invoiceRepo.Get(ctx, req.InvoiceID)
	if err != nil {
		return nil, ierr.WithError(err).WithHint("Failed to load invoice").Mark(ierr.ErrDatabase)
	}

	// Primary idempotency guard.
	existingMapping, err := s.getExistingInvoiceMapping(ctx, req.InvoiceID)
	if err != nil {
		return nil, err
	}
	if existingMapping != nil {
		checkoutURL, _ := existingMapping.Metadata[MetaKeyPaddleCheckoutURL].(string)
		checkoutURL = s.appendCheckoutToken(ctx, checkoutURL)
		return &SyncInvoiceResponse{
			PaddleTransactionID: existingMapping.ProviderEntityID,
			CheckoutURL:         checkoutURL,
			AlreadySynced:       true,
		}, nil
	}

	// Secondary idempotency guard: invoice metadata may have transaction ID from a prior partial run.
	if txnID := flexInvoice.Metadata[MetaKeyPaddleTransactionID]; txnID != "" {
		existingCheckoutURL := flexInvoice.Metadata[MetaKeyPaddleCheckoutURL]
		checkoutURL := s.appendCheckoutToken(ctx, existingCheckoutURL)
		return &SyncInvoiceResponse{
			PaddleTransactionID: txnID,
			CheckoutURL:         checkoutURL,
			AlreadySynced:       true,
		}, nil
	}

	// Fail fast: subscription is required.
	if flexInvoice.SubscriptionID == nil || *flexInvoice.SubscriptionID == "" {
		return nil, ierr.NewError("invoice has no subscription_id").
			WithHint("Paddle subscription-charge sync requires the invoice to be linked to a FlexPrice subscription").
			Mark(ierr.ErrValidation)
	}

	// Step 1: Ensure customer.
	customerResp, err := s.EnsureCustomerSynced(ctx, EnsureCustomerSyncedRequest{CustomerID: flexInvoice.CustomerID})
	if err != nil {
		return nil, ierr.WithError(err).WithHint("Customer sync failed").Mark(ierr.ErrInternal)
	}

	// Step 2: Ensure products (catalog price IDs for all line items).
	productItems := make([]EnsureProductSyncedRequest, 0, len(flexInvoice.LineItems))
	for _, li := range flexInvoice.LineItems {
		if li == nil || lo.FromPtr(li.PriceID) == "" {
			continue
		}
		name := defaultProductName
		if li.DisplayName != nil && *li.DisplayName != "" {
			name = *li.DisplayName
		}
		currency := li.Currency
		if currency == "" {
			currency = flexInvoice.Currency
		}
		productItems = append(productItems, EnsureProductSyncedRequest{
			PriceID:  *li.PriceID,
			Name:     name,
			Amount:   li.Amount,
			Currency: currency,
		})
	}
	productsResp, err := s.EnsureProductsSynced(ctx, EnsureProductsSyncedRequest{Items: productItems})
	if err != nil {
		return nil, ierr.WithError(err).WithHint("Product sync failed").Mark(ierr.ErrInternal)
	}

	// Step 3: Ensure subscription.
	subResp, err := s.EnsureSubscriptionSynced(ctx, EnsureSubscriptionSyncedRequest{
		SubscriptionID: *flexInvoice.SubscriptionID,
		CustomerID:     flexInvoice.CustomerID,
	})
	if err != nil {
		return nil, ierr.WithError(err).WithHint("Subscription sync failed").Mark(ierr.ErrInternal)
	}

	// Optional tax preview (non-fatal).
	if !flexInvoice.Total.IsZero() {
		if err := s.previewAndSyncTax(ctx, flexInvoice, customerResp.PaddleCustomerID, customerResp.PaddleAddressID); err != nil {
			s.logger.Warnw("Paddle tax preview failed, proceeding without pre-sync",
				"error", err, "invoice_id", req.InvoiceID)
		}
	}

	// Step 4: Build charge items from catalog price IDs.
	chargeItems, err := buildChargeItems(flexInvoice, productsResp.PriceIDToPaddlePriceID)
	if err != nil {
		return nil, err
	}

	// Step 5: Create subscription charge.
	_, err = s.client.CreateSubscriptionCharge(ctx, &paddlesdk.CreateSubscriptionChargeRequest{
		SubscriptionID: subResp.PaddleSubscriptionID,
		EffectiveFrom:  paddlesdk.EffectiveFromImmediately,
		Items:          chargeItems,
	})
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to create Paddle subscription charge").
			Mark(ierr.ErrInternal)
	}

	// Step 6: Fetch the created transaction (CreateSubscriptionCharge returns Subscription, not Transaction).
	orderBy := "created_at[DESC]"
	perPage := 1
	txnCollection, err := s.client.ListTransactions(ctx, &paddlesdk.ListTransactionsRequest{
		SubscriptionID: []string{subResp.PaddleSubscriptionID},
		OrderBy:        &orderBy,
		PerPage:        &perPage,
	})
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Subscription charge created but failed to retrieve the resulting transaction").
			Mark(ierr.ErrInternal)
	}
	if txnCollection == nil || txnCollection.EstimatedTotal() == 0 {
		return nil, ierr.NewError("no transactions found for subscription after charge").
			WithReportableDetails(map[string]interface{}{"paddle_subscription_id": subResp.PaddleSubscriptionID}).
			Mark(ierr.ErrInternal)
	}
	// Retrieve first result from the collection.
	var txn *paddlesdk.Transaction
	if res := txnCollection.Next(ctx); res != nil && res.Ok() {
		txn = res.Value()
	}
	if txn == nil {
		return nil, ierr.NewError("failed to read transaction from Paddle collection after charge").
			WithReportableDetails(map[string]interface{}{"paddle_subscription_id": subResp.PaddleSubscriptionID}).
			Mark(ierr.ErrInternal)
	}

	checkoutURL := ""
	if txn.Checkout != nil {
		checkoutURL = lo.FromPtrOr(txn.Checkout.URL, "")
	}
	checkoutURL = s.appendCheckoutToken(ctx, checkoutURL)

	// Write metadata to invoice FIRST (idempotency guard for retry).
	if flexInvoice.Metadata == nil {
		flexInvoice.Metadata = make(types.Metadata)
	}
	flexInvoice.Metadata[MetaKeyPaddleTransactionID] = txn.ID
	if checkoutURL != "" {
		flexInvoice.Metadata[MetaKeyPaddleCheckoutURL] = checkoutURL
	}
	if err := s.invoiceRepo.Update(ctx, flexInvoice); err != nil {
		s.logger.Warnw("failed to write Paddle transaction ID to invoice metadata", "error", err, "invoice_id", req.InvoiceID)
	}

	// Persist invoice mapping.
	invoiceMeta := map[string]interface{}{
		MetaKeyPaddleTransactionID: txn.ID,
		MetaKeySyncedAt:            time.Now().UTC().Format(time.RFC3339),
	}
	if checkoutURL != "" {
		invoiceMeta[MetaKeyPaddleCheckoutURL] = checkoutURL
	}
	if txn.InvoiceNumber != nil {
		invoiceMeta[MetaKeyInvoiceNumber] = *txn.InvoiceNumber
	}
	invoiceMapping := &entityintegrationmapping.EntityIntegrationMapping{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
		EntityType:       types.IntegrationEntityTypeInvoice,
		EntityID:         req.InvoiceID,
		ProviderType:     string(types.SecretProviderPaddle),
		ProviderEntityID: txn.ID,
		Metadata:         invoiceMeta,
		EnvironmentID:    flexInvoice.EnvironmentID,
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}
	invoiceMapping.TenantID = flexInvoice.TenantID
	if err := s.mappingRepo.Create(ctx, invoiceMapping); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invoice synced to Paddle but mapping failed to save — retry will recover from invoice metadata").
			Mark(ierr.ErrDatabase)
	}

	s.logger.Infow("successfully synced invoice to Paddle via subscription charge",
		"invoice_id", req.InvoiceID, "paddle_transaction_id", txn.ID,
		"paddle_subscription_id", subResp.PaddleSubscriptionID)
	return &SyncInvoiceResponse{
		PaddleTransactionID: txn.ID,
		CheckoutURL:         checkoutURL,
		AlreadySynced:       false,
	}, nil
}

// GetFlexPriceInvoiceIDByTransaction looks up the FlexPrice invoice ID for a Paddle transaction ID.
// Used by the webhook handler to find the invoice associated with a completed payment.
func (s *PaddleSyncService) GetFlexPriceInvoiceIDByTransaction(ctx context.Context, paddleTransactionID string) (string, error) {
	filter := &types.EntityIntegrationMappingFilter{
		ProviderEntityIDs: []string{paddleTransactionID},
		EntityType:        types.IntegrationEntityTypeInvoice,
		ProviderTypes:     []string{string(types.SecretProviderPaddle)},
		QueryFilter:       types.NewDefaultQueryFilter(),
	}
	mappings, err := s.mappingRepo.List(ctx, filter)
	if err != nil {
		return "", ierr.WithError(err).WithHint("Failed to look up invoice mapping").Mark(ierr.ErrDatabase)
	}
	if len(mappings) == 0 {
		return "", ierr.NewError("invoice mapping not found for Paddle transaction").
			WithReportableDetails(map[string]interface{}{"paddle_transaction_id": paddleTransactionID}).
			Mark(ierr.ErrNotFound)
	}
	return mappings[0].EntityID, nil
}

// buildCreateAddressRequest builds a Paddle CreateAddressRequest from a FlexPrice customer.
// Caller must ensure AddressCountry is non-empty before calling.
func buildCreateAddressRequest(c *customer.Customer) *paddlesdk.CreateAddressRequest {
	req := &paddlesdk.CreateAddressRequest{
		CountryCode: toCountryCode(c.AddressCountry),
	}
	if c.AddressLine1 != "" {
		req.FirstLine = paddlesdk.PtrTo(c.AddressLine1)
	}
	if c.AddressLine2 != "" {
		req.SecondLine = paddlesdk.PtrTo(c.AddressLine2)
	}
	if c.AddressCity != "" {
		req.City = paddlesdk.PtrTo(c.AddressCity)
	}
	if c.AddressPostalCode != "" {
		req.PostalCode = paddlesdk.PtrTo(c.AddressPostalCode)
	}
	if c.AddressState != "" {
		req.Region = paddlesdk.PtrTo(c.AddressState)
	}
	return req
}

// CreateCustomerFromPaddle creates a FlexPrice customer from Paddle webhook data (customer.created).
func (s *PaddleSyncService) CreateCustomerFromPaddle(ctx context.Context, paddleCustomer *paddlenotification.CustomerNotification, customerService interfaces.CustomerService) error {
	paddleCustomerID := paddleCustomer.ID

	// Idempotency: check if mapping already exists
	filter := &types.EntityIntegrationMappingFilter{
		ProviderTypes:     []string{string(types.SecretProviderPaddle)},
		ProviderEntityIDs: []string{paddleCustomerID},
		EntityType:        types.IntegrationEntityTypeCustomer,
	}
	mappings, err := s.mappingRepo.List(ctx, filter)
	if err != nil {
		s.logger.Errorw("failed to check Paddle customer mapping",
			"error", err,
			MetaKeyPaddleCustomerID, paddleCustomerID)
		return err
	}
	if len(mappings) > 0 {
		s.logger.Infow("FlexPrice customer already exists for Paddle customer, skipping creation",
			"flexprice_customer_id", mappings[0].EntityID,
			MetaKeyPaddleCustomerID, paddleCustomerID)
		return nil
	}

	// Deduplication by email: if customer exists by email, create mapping and skip creation
	if paddleCustomer.Email != "" {
		emailFilter := &types.CustomerFilter{
			Email:       paddleCustomer.Email,
			QueryFilter: types.NewDefaultQueryFilter(),
		}
		existingCustomers, err := customerService.GetCustomers(ctx, emailFilter)
		if err == nil && existingCustomers != nil && len(existingCustomers.Items) > 0 {
			existingCustomer := existingCustomers.Items[0]
			s.logger.Infow("customer with same email already exists, creating mapping",
				"customer_id", existingCustomer.ID,
				MetaKeyPaddleCustomerID, paddleCustomerID)

			mapping := &entityintegrationmapping.EntityIntegrationMapping{
				ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
				EntityID:         existingCustomer.ID,
				EntityType:       types.IntegrationEntityTypeCustomer,
				ProviderType:     string(types.SecretProviderPaddle),
				ProviderEntityID: paddleCustomerID,
				Metadata: map[string]interface{}{
					MetaKeyCreatedVia:           CreatedViaProviderToFlexprice,
					MetaKeyPaddleCustomerEmail: paddleCustomer.Email,
					MetaKeySyncedAt:             time.Now().UTC().Format(time.RFC3339),
				},
				EnvironmentID: types.GetEnvironmentID(ctx),
				BaseModel:     types.GetDefaultBaseModel(ctx),
			}
			if err := s.mappingRepo.Create(ctx, mapping); err != nil {
				s.logger.Warnw("failed to create mapping for existing customer",
					"error", err,
					"customer_id", existingCustomer.ID,
					MetaKeyPaddleCustomerID, paddleCustomerID)
			}
			return nil
		}
	}

	// Create new customer
	name := paddleCustomerID
	if paddleCustomer.Name != nil && *paddleCustomer.Name != "" {
		name = *paddleCustomer.Name
	} else if paddleCustomer.Email != "" {
		name = paddleCustomer.Email
	}

	createReq := dto.CreateCustomerRequest{
		ExternalID:             paddleCustomerID,
		Name:                   name,
		Email:                  paddleCustomer.Email,
		SkipOnboardingWorkflow: true,
		Metadata: map[string]string{
			MetaKeyPaddleCustomerID: paddleCustomerID,
		},
	}

	customerResp, err := customerService.CreateCustomer(ctx, createReq)
	if err != nil {
		return err
	}

	// Create entity mapping
	mapping := &entityintegrationmapping.EntityIntegrationMapping{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
		EntityID:         customerResp.ID,
		EntityType:       types.IntegrationEntityTypeCustomer,
		ProviderType:     string(types.SecretProviderPaddle),
		ProviderEntityID: paddleCustomerID,
		Metadata: map[string]interface{}{
			MetaKeyCreatedVia:           CreatedViaProviderToFlexprice,
			MetaKeyPaddleCustomerEmail: paddleCustomer.Email,
			MetaKeySyncedAt:             time.Now().UTC().Format(time.RFC3339),
		},
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}

	if err := s.mappingRepo.Create(ctx, mapping); err != nil {
		s.logger.Warnw("failed to create mapping for new customer",
			"error", err,
			"customer_id", customerResp.ID,
			MetaKeyPaddleCustomerID, paddleCustomerID)
	}

	return nil
}

// parsePaddleCents converts a Paddle amount string (cents) to a decimal in the major currency unit.
// Paddle returns all monetary values as strings in the lowest denomination (e.g. "160" = $1.60).
func parsePaddleCents(s string) decimal.Decimal {
	if s == "" {
		return decimal.Zero
	}
	v, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Zero
	}
	return v.Div(decimal.NewFromInt(100))
}
