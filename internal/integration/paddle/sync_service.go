package paddle

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	paddlesdk "github.com/PaddleHQ/paddle-go-sdk/v4"
	"github.com/PaddleHQ/paddle-go-sdk/v4/pkg/paddlenotification"
	apidto "github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/golang-jwt/jwt/v4"
	"github.com/samber/lo"
)

// PaddleSyncService orchestrates syncing FlexPrice entities to Paddle.
type PaddleSyncService struct {
	client           PaddleClient
	customerRepo     customer.Repository
	invoiceRepo      invoice.Repository
	subscriptionRepo subscription.Repository
	mappingService   interfaces.EntityIntegrationMappingService
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
	mappingService interfaces.EntityIntegrationMappingService,
	connectionRepo connection.Repository,
	log *logger.Logger,
	authSecret string,
) *PaddleSyncService {
	return &PaddleSyncService{
		client:           client,
		customerRepo:     customerRepo,
		invoiceRepo:      invoiceRepo,
		subscriptionRepo: subscriptionRepo,
		mappingService:   mappingService,
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
		return nil, err
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
	resp, err := s.mappingService.GetEntityIntegrationMappings(ctx, filter)
	if err != nil {
		return nil, err
	}

	if len(resp.Items) > 0 {
		m := resp.Items[0]
		paddleCustomerID := m.ProviderEntityID
		paddleAddressID, _ := m.Metadata[MetaKeyPaddleAddressID].(string)

		paddleAddressID, err = s.syncAddressForMapping(ctx, flexCustomer, paddleCustomerID, paddleAddressID, m.ID)
		if err != nil {
			return nil, err
		}
		return &EnsureCustomerSyncedResponse{
			PaddleCustomerID: paddleCustomerID,
			PaddleAddressID:  paddleAddressID,
			Created:          false,
		}, nil
	}

	// Check if a Paddle customer with the same email already exists before attempting to create.
	// Paddle rejects CreateCustomer with customer_already_exists when the email is taken.
	paddleCustomerID, err := s.lookupPaddleCustomerByEmail(ctx, flexCustomer.Email)
	if err != nil {
		return nil, err
	}

	if paddleCustomerID == "" {
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
		paddleCustomer, createErr := s.client.CreateCustomer(ctx, createReq)
		if createErr != nil {
			return nil, ierr.WithError(createErr).WithHint("Failed to create customer in Paddle").Mark(ierr.ErrInternal)
		}
		paddleCustomerID = paddleCustomer.ID
	}

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
	_, createErr := s.mappingService.CreateEntityIntegrationMapping(ctx, apidto.CreateEntityIntegrationMappingRequest{
		EntityID:         flexCustomer.ID,
		EntityType:       types.IntegrationEntityTypeCustomer,
		ProviderType:     string(types.SecretProviderPaddle),
		ProviderEntityID: paddleCustomerID,
		Metadata:         meta,
	})
	if createErr != nil {
		return nil, createErr
	}

	return &EnsureCustomerSyncedResponse{
		PaddleCustomerID: paddleCustomerID,
		PaddleAddressID:  paddleAddressID,
		Created:          true,
	}, nil
}

// lookupPaddleCustomerByEmail returns the Paddle customer ID for the given email, or "" if none exists.
func (s *PaddleSyncService) lookupPaddleCustomerByEmail(ctx context.Context, email string) (string, error) {
	result, err := s.client.ListCustomers(ctx, &paddlesdk.ListCustomersRequest{
		Email: []string{email},
	})
	if err != nil {
		return "", fmt.Errorf("looking up Paddle customer by email: %w", err)
	}
	if result == nil {
		return "", nil
	}
	first := result.Next(ctx)
	if first == nil || !first.Ok() {
		return "", nil
	}
	customer := first.Value()
	s.logger.Infow("found existing Paddle customer by email", "paddle_customer_id", customer.ID, "email", email)
	return customer.ID, nil
}

// EnsureBulkProductSynced ensures Paddle catalog products exist for all given FlexPrice prices.
// It issues a single mapping query for all price IDs, then creates only the unmapped ones.
func (s *PaddleSyncService) EnsureBulkProductSynced(ctx context.Context, req EnsureBulkProductSyncedRequest) (*EnsureBulkProductSyncedResponse, error) {
	if len(req.Items) == 0 {
		return &EnsureBulkProductSyncedResponse{PriceIDToPaddleProductID: map[string]string{}}, nil
	}

	priceIDs := make([]string, 0, len(req.Items))
	for _, item := range req.Items {
		if item.PriceID != "" {
			priceIDs = append(priceIDs, item.PriceID)
		}
	}

	bulkResp, err := s.mappingService.GetEntityIntegrationMappings(ctx, &types.EntityIntegrationMappingFilter{
		EntityIDs:     priceIDs,
		EntityType:    types.IntegrationEntityTypePrice,
		ProviderTypes: []string{string(types.SecretProviderPaddle)},
	})
	if err != nil {
		return nil, fmt.Errorf("fetching existing product mappings: %w", err)
	}

	result := make(map[string]string, len(req.Items))
	for _, m := range bulkResp.Items {
		result[m.EntityID] = m.ProviderEntityID
	}

	for _, item := range req.Items {
		if item.PriceID == "" || result[item.PriceID] != "" {
			continue
		}
		name := item.Name
		if name == "" {
			name = item.PriceID
		}
		product, err := s.client.CreateProduct(ctx, &paddlesdk.CreateProductRequest{
			Name:        name,
			TaxCategory: paddlesdk.TaxCategoryStandard,
			CustomData: map[string]interface{}{
				"flexprice_price_id": item.PriceID,
				"environment_id":     types.GetEnvironmentID(ctx),
			},
		})
		if err != nil {
			return nil, fmt.Errorf("creating Paddle product for price %s: %w", item.PriceID, err)
		}
		_, err = s.mappingService.CreateEntityIntegrationMapping(ctx, apidto.CreateEntityIntegrationMappingRequest{
			EntityID:         item.PriceID,
			EntityType:       types.IntegrationEntityTypePrice,
			ProviderType:     string(types.SecretProviderPaddle),
			ProviderEntityID: product.ID,
			Metadata: map[string]interface{}{
				MetaKeyPaddleProductID: product.ID,
				MetaKeySyncedAt:        time.Now().UTC().Format(time.RFC3339),
			},
		})
		if err != nil {
			return nil, fmt.Errorf("persisting product mapping for price %s: %w", item.PriceID, err)
		}
		result[item.PriceID] = product.ID
	}

	return &EnsureBulkProductSyncedResponse{PriceIDToPaddleProductID: result}, nil
}

// EnsureSubscriptionSynced ensures a Paddle subscription exists for the given FlexPrice subscription.
// The Paddle subscription is bootstrapped via a $0 transaction using real mapped products with billing cycles.
// Returns immediately if already mapped — this is the primary guard against duplicate subscriptions.
func (s *PaddleSyncService) EnsureSubscriptionSynced(ctx context.Context, req EnsureSubscriptionSyncedRequest) (*EnsureSubscriptionSyncedResponse, error) {
	sub := req.Subscription
	if sub == nil || sub.ID == "" {
		return nil, ierr.NewError("subscription is required").Mark(ierr.ErrValidation)
	}

	filter := &types.EntityIntegrationMappingFilter{
		EntityID:      sub.ID,
		EntityType:    types.IntegrationEntityTypeSubscription,
		ProviderTypes: []string{string(types.SecretProviderPaddle)},
	}
	resp, err := s.mappingService.GetEntityIntegrationMappings(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("checking subscription mapping: %w", err)
	}
	if len(resp.Items) > 0 {
		return &EnsureSubscriptionSyncedResponse{
			PaddleSubscriptionID: resp.Items[0].ProviderEntityID,
			Created:              false,
		}, nil
	}

	customerResp, err := s.EnsureCustomerSynced(ctx, EnsureCustomerSyncedRequest{CustomerID: sub.CustomerID})
	if err != nil {
		return nil, fmt.Errorf("ensuring customer synced: %w", err)
	}
	if customerResp.PaddleAddressID == "" {
		return nil, ierr.NewError("Paddle address ID not found after customer sync").
			WithHint("Customer must have an address (country required) for Paddle subscription creation").
			WithReportableDetails(map[string]interface{}{"customer_id": sub.CustomerID}).
			Mark(ierr.ErrValidation)
	}

	billingCycle := paddleBillingCycle(sub.BillingPeriod, sub.BillingPeriodCount)
	currency := strings.ToUpper(sub.Currency)

	// Sort product IDs for deterministic ordering.
	type pricePair struct{ priceID, productID string }
	pairs := make([]pricePair, 0, len(req.PriceIDToProductID))
	for priceID, productID := range req.PriceIDToProductID {
		pairs = append(pairs, pricePair{priceID, productID})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].productID < pairs[j].productID })

	items := make([]paddlesdk.CreateTransactionItems, 0, len(pairs))
	for _, p := range pairs {
		items = append(items, *paddlesdk.NewCreateTransactionItemsTransactionItemCreateWithPrice(
			&paddlesdk.TransactionItemCreateWithPrice{
				Quantity: 1,
				Price: paddlesdk.TransactionPriceCreateWithProductID{
					ProductID:    p.productID,
					Description:  "FlexPrice Price",
					TaxMode:      paddlesdk.TaxModeAccountSetting,
					UnitPrice:    paddlesdk.Money{Amount: "0", CurrencyCode: paddlesdk.CurrencyCode(currency)},
					BillingCycle: billingCycle,
					Quantity:     paddlesdk.PriceQuantity{Minimum: 1, Maximum: 1},
				},
			},
		))
	}
	if len(items) == 0 {
		return nil, ierr.NewError("no products to bootstrap subscription with").Mark(ierr.ErrValidation)
	}

	// Use manual collection + status:billed for the $0 bootstrap transaction.
	// This tells Paddle the invoice is already paid, so it creates the subscription
	// immediately and returns subscription_id in the response.
	// Automatic collection keeps the transaction in "ready" state (awaiting payment
	// processing), which never produces a subscription_id synchronously.
	// The FlexPrice subscription's collection mode is applied per-charge via CreateSubscriptionCharge.
	billedStatus := paddlesdk.TransactionStatusBilled
	txn, err := s.client.CreateTransaction(ctx, &paddlesdk.CreateTransactionRequest{
		CustomerID:     paddlesdk.PtrTo(customerResp.PaddleCustomerID),
		AddressID:      paddlesdk.PtrTo(customerResp.PaddleAddressID),
		CollectionMode: paddlesdk.PtrTo(paddlesdk.CollectionModeManual),
		Status:         &billedStatus,
		BillingDetails: &paddlesdk.BillingDetails{
			PaymentTerms: paddlesdk.Duration{Interval: paddlesdk.IntervalDay, Frequency: 1},
		},
		Items: items,
		CustomData: map[string]interface{}{
			"flexprice_subscription_id": sub.ID,
			"environment_id":            types.GetEnvironmentID(ctx),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("creating bootstrap transaction: %w", err)
	}
	// Paddle may not include subscription_id in the transaction response when collection_mode=manual.
	// Fall back to listing subscriptions for the customer to find the one just created.
	var paddleSubID string
	if txn.SubscriptionID != nil && *txn.SubscriptionID != "" {
		paddleSubID = *txn.SubscriptionID
	} else {
		perPage := 5
		subCollection, listErr := s.client.ListSubscriptions(ctx, &paddlesdk.ListSubscriptionsRequest{
			CustomerID: []string{customerResp.PaddleCustomerID},
			PerPage:    &perPage,
		})
		if listErr != nil {
			return nil, fmt.Errorf("listing subscriptions after bootstrap transaction: %w", listErr)
		}
		if subCollection != nil {
			for {
				item := subCollection.Next(ctx)
				if item == nil || !item.Ok() {
					break
				}
				paddleSub := item.Value()
				if paddleSub != nil && paddleSub.ID != "" {
					paddleSubID = paddleSub.ID
					break
				}
			}
		}
		if paddleSubID == "" {
			return nil, ierr.NewError("Paddle transaction did not produce a subscription_id").
				WithHint("Ensure items have billing_cycle set so Paddle creates a subscription").
				WithReportableDetails(map[string]interface{}{"paddle_transaction_id": txn.ID}).
				Mark(ierr.ErrInternal)
		}
	}

	_, err = s.mappingService.CreateEntityIntegrationMapping(ctx, apidto.CreateEntityIntegrationMappingRequest{
		EntityID:         sub.ID,
		EntityType:       types.IntegrationEntityTypeSubscription,
		ProviderType:     string(types.SecretProviderPaddle),
		ProviderEntityID: paddleSubID,
		Metadata: map[string]interface{}{
			MetaKeyCreatedVia:          CreatedViaFlexpriceToProvider,
			MetaKeyPaddleTransactionID: txn.ID,
			MetaKeySyncedAt:            time.Now().UTC().Format(time.RFC3339),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("persisting subscription mapping: %w", err)
	}

	return &EnsureSubscriptionSyncedResponse{PaddleSubscriptionID: paddleSubID, Created: true}, nil
}

// paddleCollectionMode maps a FlexPrice CollectionMethod to a Paddle CollectionMode.
func paddleCollectionMode(m types.CollectionMethod) paddlesdk.CollectionMode {
	if m == types.CollectionMethodSendInvoice {
		return paddlesdk.CollectionModeManual
	}
	return paddlesdk.CollectionModeAutomatic
}

// paddleBillingCycle maps a FlexPrice BillingPeriod + count to a Paddle Duration.
func paddleBillingCycle(period types.BillingPeriod, count int) *paddlesdk.Duration {
	if count <= 0 {
		count = 1
	}
	switch period {
	case types.BILLING_PERIOD_DAILY:
		return &paddlesdk.Duration{Interval: paddlesdk.IntervalDay, Frequency: count}
	case types.BILLING_PERIOD_WEEKLY:
		return &paddlesdk.Duration{Interval: paddlesdk.IntervalWeek, Frequency: count}
	case types.BILLING_PERIOD_ANNUAL:
		return &paddlesdk.Duration{Interval: paddlesdk.IntervalYear, Frequency: count}
	case types.BILLING_PERIOD_QUARTER:
		return &paddlesdk.Duration{Interval: paddlesdk.IntervalMonth, Frequency: 3 * count}
	default:
		return &paddlesdk.Duration{Interval: paddlesdk.IntervalMonth, Frequency: count}
	}
}

// syncAddressForMapping ensures the Paddle address for an already-mapped customer is up to date.
// paddleAddressID is the value currently stored in the mapping metadata (may be empty).
// mappingID is the EntityIntegrationMapping ID (used to update metadata if a new address is created).
// It returns the final paddleAddressID (possibly newly created).
func (s *PaddleSyncService) syncAddressForMapping(
	ctx context.Context,
	c *customer.Customer,
	paddleCustomerID, paddleAddressID string,
	mappingID string,
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
		return "", err
	}
	if mappingID != "" {
		_, err = s.mappingService.UpdateEntityIntegrationMapping(ctx, mappingID, apidto.UpdateEntityIntegrationMappingRequest{
			Metadata: map[string]interface{}{
				MetaKeyPaddleAddressID: addr.ID,
				MetaKeySyncedAt:        time.Now().UTC().Format(time.RFC3339),
			},
		})
		if err != nil {
			s.logger.Warnw("failed to update mapping with new Paddle address ID", "error", err)
		}
	}
	return addr.ID, nil
}

// getExistingInvoiceMapping checks entity_integration_mapping for a prior Paddle sync of this invoice.
func (s *PaddleSyncService) getExistingInvoiceMapping(ctx context.Context, invoiceID string) (*apidto.EntityIntegrationMappingResponse, error) {
	filter := &types.EntityIntegrationMappingFilter{
		EntityType:    types.IntegrationEntityTypeInvoice,
		EntityID:      invoiceID,
		ProviderTypes: []string{string(types.SecretProviderPaddle)},
	}
	resp, err := s.mappingService.GetEntityIntegrationMappings(ctx, filter)
	if err != nil {
		return nil, err
	}
	if len(resp.Items) == 0 {
		return nil, nil
	}
	return resp.Items[0], nil
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

// SyncInvoice is the main invoice sync orchestrator.
// Idempotent: returns early if the invoice is already mapped to a Paddle transaction.
// Fetches the FlexPrice subscription, builds real product-backed charge items at the
// invoice amount, and creates a Paddle subscription charge.
func (s *PaddleSyncService) SyncInvoice(ctx context.Context, req SyncInvoiceRequest) (*SyncInvoiceResponse, error) {
	if !s.client.HasPaddleConnection(ctx) {
		return nil, ierr.NewError("Paddle connection not available").
			WithHint("Paddle integration must be configured for invoice sync").
			Mark(ierr.ErrNotFound)
	}

	flexInvoice, err := s.invoiceRepo.Get(ctx, req.InvoiceID)
	if err != nil {
		return nil, fmt.Errorf("fetching invoice: %w", err)
	}

	// Step 1: Idempotency guard.
	existingMapping, err := s.getExistingInvoiceMapping(ctx, req.InvoiceID)
	if err != nil {
		return nil, fmt.Errorf("checking invoice mapping: %w", err)
	}
	if existingMapping != nil {
		checkoutURL, _ := existingMapping.Metadata[MetaKeyPaddleCheckoutURL].(string)
		paddleSubID, _ := existingMapping.Metadata[MetaKeyPaddleSubscriptionID].(string)
		return &SyncInvoiceResponse{
			PaddleTransactionID:  existingMapping.ProviderEntityID,
			PaddleSubscriptionID: paddleSubID,
			CheckoutURL:          s.appendCheckoutToken(ctx, checkoutURL),
			AlreadySynced:        true,
		}, nil
	}

	if flexInvoice.SubscriptionID == nil || *flexInvoice.SubscriptionID == "" {
		return nil, ierr.NewError("invoice has no subscription_id").
			WithHint("Paddle subscription-charge sync requires the invoice to be linked to a FlexPrice subscription").
			Mark(ierr.ErrValidation)
	}

	// Step 2: Fetch FlexPrice subscription.
	flexSub, err := s.subscriptionRepo.Get(ctx, *flexInvoice.SubscriptionID)
	if err != nil {
		return nil, fmt.Errorf("fetching subscription: %w", err)
	}

	// Step 3: Ensure customer synced.
	_, err = s.EnsureCustomerSynced(ctx, EnsureCustomerSyncedRequest{CustomerID: flexInvoice.CustomerID})
	if err != nil {
		return nil, fmt.Errorf("ensuring customer synced: %w", err)
	}

	// Step 4: Ensure products synced.
	productItems := make([]EnsureBulkProductSyncedItem, 0, len(flexInvoice.LineItems))
	for _, li := range flexInvoice.LineItems {
		if li == nil || lo.FromPtr(li.PriceID) == "" {
			continue
		}
		name := lo.FromPtrOr(li.DisplayName, lo.FromPtr(li.PriceID))
		productItems = append(productItems, EnsureBulkProductSyncedItem{
			PriceID: lo.FromPtr(li.PriceID),
			Name:    name,
		})
	}
	productsResp, err := s.EnsureBulkProductSynced(ctx, EnsureBulkProductSyncedRequest{Items: productItems})
	if err != nil {
		return nil, fmt.Errorf("ensuring products synced: %w", err)
	}

	// Step 5: Ensure subscription synced.
	subResp, err := s.EnsureSubscriptionSynced(ctx, EnsureSubscriptionSyncedRequest{
		Subscription:       flexSub,
		PriceIDToProductID: productsResp.PriceIDToPaddleProductID,
	})
	if err != nil {
		return nil, fmt.Errorf("ensuring subscription synced: %w", err)
	}

	// Step 6: Build charge items — qty=1, full amount in cents, currency from line item.
	chargeItems := make([]paddlesdk.CreateSubscriptionChargeItems, 0, len(flexInvoice.LineItems))
	for _, li := range flexInvoice.LineItems {
		if li == nil {
			continue
		}
		priceID := lo.FromPtr(li.PriceID)
		paddleProductID := productsResp.PriceIDToPaddleProductID[priceID]
		if paddleProductID == "" {
			return nil, ierr.NewError(fmt.Sprintf("no Paddle product ID for FlexPrice price %s", priceID)).
				WithHint("Ensure all invoice line item prices are synced to Paddle").
				Mark(ierr.ErrValidation)
		}
		amountSmallest := types.ToSmallestUnit(li.Amount, li.Currency)
		displayName := lo.FromPtrOr(li.DisplayName, priceID)

		chargeItems = append(chargeItems, *paddlesdk.NewCreateSubscriptionChargeItemsSubscriptionChargeItemCreateWithPrice(
			&paddlesdk.SubscriptionChargeItemCreateWithPrice{
				Quantity: 1,
				Price: paddlesdk.SubscriptionChargeCreateWithPrice{
					ProductID:   paddleProductID,
					Description: "Flexrice Charge",
					Name:        paddlesdk.PtrTo(displayName),
					TaxMode:     paddlesdk.TaxModeAccountSetting,
					UnitPrice: paddlesdk.Money{
						Amount:       fmt.Sprintf("%d", amountSmallest),
						CurrencyCode: paddlesdk.CurrencyCode(strings.ToUpper(li.Currency)),
					},
					Quantity: paddlesdk.PriceQuantity{Minimum: 1, Maximum: 100000},
				},
			},
		))
	}
	if len(chargeItems) == 0 {
		return nil, ierr.NewError("invoice has no syncable line items").Mark(ierr.ErrValidation)
	}

	// Step 7: Create subscription charge.
	_, err = s.client.CreateSubscriptionCharge(ctx, &paddlesdk.CreateSubscriptionChargeRequest{
		SubscriptionID: subResp.PaddleSubscriptionID,
		EffectiveFrom:  paddlesdk.EffectiveFromImmediately,
		Items:          chargeItems,
	})
	if err != nil {
		return nil, fmt.Errorf("creating subscription charge: %w", err)
	}

	// Step 8: Fetch the created transaction (SDK returns Subscription, not Transaction).
	orderBy := "created_at[DESC]"
	perPage := 1
	txnCollection, err := s.client.ListTransactions(ctx, &paddlesdk.ListTransactionsRequest{
		SubscriptionID: []string{subResp.PaddleSubscriptionID},
		OrderBy:        &orderBy,
		PerPage:        &perPage,
	})
	if err != nil {
		return nil, fmt.Errorf("listing transactions after charge: %w", err)
	}
	if txnCollection == nil || txnCollection.EstimatedTotal() == 0 {
		return nil, ierr.NewError("no transactions found for subscription after charge").
			WithReportableDetails(map[string]interface{}{"paddle_subscription_id": subResp.PaddleSubscriptionID}).
			Mark(ierr.ErrInternal)
	}
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

	// Step 9: Persist invoice metadata + mapping.
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

	invoiceMeta := map[string]interface{}{
		MetaKeyPaddleTransactionID:  txn.ID,
		MetaKeyPaddleSubscriptionID: subResp.PaddleSubscriptionID,
		MetaKeySyncedAt:             time.Now().UTC().Format(time.RFC3339),
	}
	if checkoutURL != "" {
		invoiceMeta[MetaKeyPaddleCheckoutURL] = checkoutURL
	}
	if txn.InvoiceNumber != nil {
		invoiceMeta[MetaKeyInvoiceNumber] = *txn.InvoiceNumber
	}
	_, err = s.mappingService.CreateEntityIntegrationMapping(ctx, apidto.CreateEntityIntegrationMappingRequest{
		EntityType:       types.IntegrationEntityTypeInvoice,
		EntityID:         req.InvoiceID,
		ProviderType:     string(types.SecretProviderPaddle),
		ProviderEntityID: txn.ID,
		Metadata:         invoiceMeta,
	})
	if err != nil {
		return nil, fmt.Errorf("persisting invoice mapping: %w", err)
	}

	// Step 10: Return.
	return &SyncInvoiceResponse{
		PaddleTransactionID:  txn.ID,
		PaddleSubscriptionID: subResp.PaddleSubscriptionID,
		CheckoutURL:          checkoutURL,
		AlreadySynced:        false,
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
	resp, err := s.mappingService.GetEntityIntegrationMappings(ctx, filter)
	if err != nil {
		return "", err
	}
	if len(resp.Items) == 0 {
		return "", ierr.NewError("invoice mapping not found for Paddle transaction").
			WithReportableDetails(map[string]interface{}{"paddle_transaction_id": paddleTransactionID}).
			Mark(ierr.ErrNotFound)
	}
	return resp.Items[0].EntityID, nil
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

// ProcessCustomerCreatedWebhook creates a FlexPrice customer from Paddle webhook data (customer.created).
func (s *PaddleSyncService) ProcessCustomerCreatedWebhook(ctx context.Context, paddleCustomer *paddlenotification.CustomerNotification, customerService interfaces.CustomerService) error {
	paddleCustomerID := paddleCustomer.ID

	// Idempotency: check if mapping already exists
	filter := &types.EntityIntegrationMappingFilter{
		ProviderTypes:     []string{string(types.SecretProviderPaddle)},
		ProviderEntityIDs: []string{paddleCustomerID},
		EntityType:        types.IntegrationEntityTypeCustomer,
	}
	resp, err := s.mappingService.GetEntityIntegrationMappings(ctx, filter)
	if err != nil {
		s.logger.Errorw("failed to check Paddle customer mapping",
			"error", err,
			MetaKeyPaddleCustomerID, paddleCustomerID)
		return err
	}
	if len(resp.Items) > 0 {
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

			_, err = s.mappingService.CreateEntityIntegrationMapping(ctx, apidto.CreateEntityIntegrationMappingRequest{
				EntityID:         existingCustomer.ID,
				EntityType:       types.IntegrationEntityTypeCustomer,
				ProviderType:     string(types.SecretProviderPaddle),
				ProviderEntityID: paddleCustomerID,
				Metadata: map[string]interface{}{
					MetaKeyCreatedVia:          CreatedViaProviderToFlexprice,
					MetaKeyPaddleCustomerEmail: paddleCustomer.Email,
					MetaKeySyncedAt:            time.Now().UTC().Format(time.RFC3339),
				},
			})
			if err != nil {
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

	createReq := apidto.CreateCustomerRequest{
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
	_, err = s.mappingService.CreateEntityIntegrationMapping(ctx, apidto.CreateEntityIntegrationMappingRequest{
		EntityID:         customerResp.ID,
		EntityType:       types.IntegrationEntityTypeCustomer,
		ProviderType:     string(types.SecretProviderPaddle),
		ProviderEntityID: paddleCustomerID,
		Metadata: map[string]interface{}{
			MetaKeyCreatedVia:          CreatedViaProviderToFlexprice,
			MetaKeyPaddleCustomerEmail: paddleCustomer.Email,
			MetaKeySyncedAt:            time.Now().UTC().Format(time.RFC3339),
		},
	})
	if err != nil {
		s.logger.Warnw("failed to create mapping for new customer",
			"error", err,
			"customer_id", customerResp.ID,
			MetaKeyPaddleCustomerID, paddleCustomerID)
	}

	return nil
}

// ProcessAddressCreatedWebhook updates a FlexPrice customer's address from a Paddle address.created webhook.
func (s *PaddleSyncService) ProcessAddressCreatedWebhook(
	ctx context.Context,
	paddleCustomerID string,
	addr *paddlenotification.AddressNotification,
	customerService interfaces.CustomerService,
) error {
	// Look up FlexPrice customer by Paddle customer ID.
	filter := &types.EntityIntegrationMappingFilter{
		ProviderTypes:     []string{string(types.SecretProviderPaddle)},
		ProviderEntityIDs: []string{paddleCustomerID},
		EntityType:        types.IntegrationEntityTypeCustomer,
		QueryFilter:       types.NewDefaultQueryFilter(),
	}
	resp, err := s.mappingService.GetEntityIntegrationMappings(ctx, filter)
	if err != nil {
		return err
	}
	if len(resp.Items) == 0 {
		// No FlexPrice customer mapped — skip silently.
		return nil
	}
	flexCustomerID := resp.Items[0].EntityID
	mappingID := resp.Items[0].ID

	// Update FlexPrice customer address fields.
	updateReq := mapToUpdateCustomerAddressRequest(addr)
	_, err = customerService.UpdateCustomer(ctx, flexCustomerID, updateReq)
	if err != nil {
		return err
	}

	// Update mapping metadata with the new Paddle address ID.
	_, err = s.mappingService.UpdateEntityIntegrationMapping(ctx, mappingID, apidto.UpdateEntityIntegrationMappingRequest{
		Metadata: map[string]interface{}{MetaKeyPaddleAddressID: addr.ID},
	})
	if err != nil {
		s.logger.Warnw("failed to update customer mapping with paddle_address_id",
			"error", err, "flexprice_customer_id", flexCustomerID, "paddle_address_id", addr.ID)
	}
	return nil
}

// ProcessTransactionCompletedWebhook processes a transaction.completed Paddle webhook event.
// It finds the FlexPrice invoice via entity_integration_mapping and delegates payment processing.
func (s *PaddleSyncService) ProcessTransactionCompletedWebhook(
	ctx context.Context,
	txn *paddlenotification.TransactionNotification,
	paymentService interfaces.PaymentService,
	invoiceService interfaces.InvoiceService,
) error {
	// Find the FlexPrice invoice ID from entity_integration_mapping.
	flexpriceInvoiceID, err := s.GetFlexPriceInvoiceIDByTransaction(ctx, txn.ID)
	if err != nil {
		if ierr.IsNotFound(err) {
			// No mapping — this transaction may not be one we created, skip.
			s.logger.Warnw("no FlexPrice invoice found for Paddle transaction, skipping",
				"paddle_transaction_id", txn.ID)
			return nil
		}
		return err
	}

	// Process the payment (idempotent — checks if payment already exists).
	paymentSvc := NewPaymentService(s.logger)
	return paymentSvc.ProcessExternalPaddleTransaction(ctx, txn, flexpriceInvoiceID, paymentService, invoiceService)
}

// mapToUpdateCustomerAddressRequest maps Paddle AddressNotification to Flexprice UpdateCustomerRequest.
// Flexprice has no separate Address entity; address is embedded on Customer.
func mapToUpdateCustomerAddressRequest(addr *paddlenotification.AddressNotification) apidto.UpdateCustomerRequest {
	req := apidto.UpdateCustomerRequest{}
	if addr.FirstLine != nil && *addr.FirstLine != "" {
		req.AddressLine1 = addr.FirstLine
	}
	if addr.SecondLine != nil && *addr.SecondLine != "" {
		req.AddressLine2 = addr.SecondLine
	}
	if addr.City != nil && *addr.City != "" {
		req.AddressCity = addr.City
	}
	if addr.Region != nil && *addr.Region != "" {
		req.AddressState = addr.Region
	}
	if addr.PostalCode != nil && *addr.PostalCode != "" {
		req.AddressPostalCode = addr.PostalCode
	}
	if addr.CountryCode != "" {
		req.AddressCountry = lo.ToPtr(strings.ToUpper(string(addr.CountryCode)))
	}
	return req
}
