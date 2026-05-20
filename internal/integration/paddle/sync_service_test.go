package paddle_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	paddlesdk "github.com/PaddleHQ/paddle-go-sdk/v4"
	apidto "github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/integration/paddle"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// inMemoryMappingService wraps entityintegrationmapping.Repository to implement
// interfaces.EntityIntegrationMappingService for testing.
type inMemoryMappingService struct {
	repo entityintegrationmapping.Repository
}

func newTestMappingService(repo entityintegrationmapping.Repository) interfaces.EntityIntegrationMappingService {
	return &inMemoryMappingService{repo: repo}
}

func (s *inMemoryMappingService) CreateEntityIntegrationMapping(ctx context.Context, req apidto.CreateEntityIntegrationMappingRequest) (*apidto.EntityIntegrationMappingResponse, error) {
	m := &entityintegrationmapping.EntityIntegrationMapping{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
		EntityID:         req.EntityID,
		EntityType:       req.EntityType,
		ProviderType:     req.ProviderType,
		ProviderEntityID: req.ProviderEntityID,
		Metadata:         req.Metadata,
		EnvironmentID:    types.GetEnvironmentID(ctx),
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}
	if err := s.repo.Create(ctx, m); err != nil {
		return nil, err
	}
	return toTestMappingResponse(m), nil
}

func (s *inMemoryMappingService) GetEntityIntegrationMapping(ctx context.Context, id string) (*apidto.EntityIntegrationMappingResponse, error) {
	m, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return toTestMappingResponse(m), nil
}

func (s *inMemoryMappingService) GetEntityIntegrationMappings(ctx context.Context, filter *types.EntityIntegrationMappingFilter) (*apidto.ListEntityIntegrationMappingsResponse, error) {
	if filter == nil {
		filter = &types.EntityIntegrationMappingFilter{QueryFilter: types.NewDefaultQueryFilter()}
	}
	mappings, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	total, err := s.repo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}
	items := make([]*apidto.EntityIntegrationMappingResponse, 0, len(mappings))
	for _, m := range mappings {
		items = append(items, toTestMappingResponse(m))
	}
	return &apidto.ListEntityIntegrationMappingsResponse{
		Items:      items,
		Pagination: types.NewPaginationResponse(total, filter.GetLimit(), filter.GetOffset()),
	}, nil
}

func (s *inMemoryMappingService) UpdateEntityIntegrationMapping(ctx context.Context, id string, req apidto.UpdateEntityIntegrationMappingRequest) (*apidto.EntityIntegrationMappingResponse, error) {
	m, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if req.ProviderEntityID != nil {
		m.ProviderEntityID = *req.ProviderEntityID
	}
	if req.Metadata != nil {
		if m.Metadata == nil {
			m.Metadata = make(map[string]interface{})
		}
		for k, v := range req.Metadata {
			m.Metadata[k] = v
		}
	}
	if err := s.repo.Update(ctx, m); err != nil {
		return nil, err
	}
	return toTestMappingResponse(m), nil
}

func (s *inMemoryMappingService) DeleteEntityIntegrationMapping(ctx context.Context, id string) error {
	m, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	return s.repo.Delete(ctx, m)
}

func (s *inMemoryMappingService) LinkIntegrationMapping(ctx context.Context, req apidto.LinkIntegrationMappingRequest) (*apidto.LinkIntegrationMappingResponse, error) {
	return nil, nil
}

func toTestMappingResponse(m *entityintegrationmapping.EntityIntegrationMapping) *apidto.EntityIntegrationMappingResponse {
	return &apidto.EntityIntegrationMappingResponse{
		ID:               m.ID,
		EntityID:         m.EntityID,
		EntityType:       m.EntityType,
		ProviderType:     m.ProviderType,
		ProviderEntityID: m.ProviderEntityID,
		Metadata:         m.Metadata,
		EnvironmentID:    m.EnvironmentID,
		TenantID:         m.TenantID,
	}
}

// mockPaddleClient implements paddle.PaddleClient for testing.
// Each method has a function-field override; unset fields return safe zero values.
type mockPaddleClient struct {
	createCustomerFn           func(ctx context.Context, req *paddlesdk.CreateCustomerRequest) (*paddlesdk.Customer, error)
	createAddressFn            func(ctx context.Context, customerID string, req *paddlesdk.CreateAddressRequest) (*paddlesdk.Address, error)
	updateAddressFn            func(ctx context.Context, customerID string, addressID string, req *paddlesdk.UpdateAddressRequest) (*paddlesdk.Address, error)
	createTransactionFn        func(ctx context.Context, req *paddlesdk.CreateTransactionRequest) (*paddlesdk.Transaction, error)
	previewTransactionFn       func(ctx context.Context, req *paddlesdk.PreviewTransactionCreateRequest) (*paddlesdk.TransactionPreview, error)
	verifyWebhookSignatureFn   func(ctx context.Context, payload []byte, signature string, webhookSecret string) error
	createProductFn            func(ctx context.Context, req *paddlesdk.CreateProductRequest) (*paddlesdk.Product, error)
	createPriceFn              func(ctx context.Context, req *paddlesdk.CreatePriceRequest) (*paddlesdk.Price, error)
	createSubscriptionChargeFn func(ctx context.Context, req *paddlesdk.CreateSubscriptionChargeRequest) (*paddlesdk.Subscription, error)
	listTransactionsFn         func(ctx context.Context, req *paddlesdk.ListTransactionsRequest) (*paddlesdk.Collection[*paddlesdk.Transaction], error)
	getTransactionFn           func(ctx context.Context, transactionID string) (*paddlesdk.Transaction, error)
	listSubscriptionsFn        func(ctx context.Context, req *paddlesdk.ListSubscriptionsRequest) (*paddlesdk.Collection[*paddlesdk.Subscription], error)
	getPaddleConfigFn          func(ctx context.Context) (*paddle.PaddleConfig, error)
	getDecryptedPaddleConfigFn func(conn *connection.Connection) (*paddle.PaddleConfig, error)
	hasPaddleConnectionFn      func(ctx context.Context) bool
	getConnectionFn            func(ctx context.Context) (*connection.Connection, error)
	getSDKClientFn             func(ctx context.Context) (*paddlesdk.SDK, *paddle.PaddleConfig, error)

	// Track calls for assertion
	createCustomerCalled           bool
	createTransactionCalled        bool
	createSubscriptionChargeCalled bool
	createProductCalled            bool
}

func (m *mockPaddleClient) GetPaddleConfig(ctx context.Context) (*paddle.PaddleConfig, error) {
	if m.getPaddleConfigFn != nil {
		return m.getPaddleConfigFn(ctx)
	}
	return &paddle.PaddleConfig{APIKey: "test-key"}, nil
}

func (m *mockPaddleClient) GetDecryptedPaddleConfig(conn *connection.Connection) (*paddle.PaddleConfig, error) {
	if m.getDecryptedPaddleConfigFn != nil {
		return m.getDecryptedPaddleConfigFn(conn)
	}
	return &paddle.PaddleConfig{APIKey: "test-key"}, nil
}

func (m *mockPaddleClient) HasPaddleConnection(ctx context.Context) bool {
	if m.hasPaddleConnectionFn != nil {
		return m.hasPaddleConnectionFn(ctx)
	}
	return true
}

func (m *mockPaddleClient) GetConnection(ctx context.Context) (*connection.Connection, error) {
	if m.getConnectionFn != nil {
		return m.getConnectionFn(ctx)
	}
	return &connection.Connection{
		ID:            "conn_test",
		ProviderType:  types.SecretProviderPaddle,
		EnvironmentID: "env_sandbox",
		BaseModel: types.BaseModel{
			TenantID: types.DefaultTenantID,
			Status:   types.StatusPublished,
		},
	}, nil
}

func (m *mockPaddleClient) GetSDKClient(ctx context.Context) (*paddlesdk.SDK, *paddle.PaddleConfig, error) {
	if m.getSDKClientFn != nil {
		return m.getSDKClientFn(ctx)
	}
	return nil, &paddle.PaddleConfig{APIKey: "test-key"}, nil
}

func (m *mockPaddleClient) CreateCustomer(ctx context.Context, req *paddlesdk.CreateCustomerRequest) (*paddlesdk.Customer, error) {
	m.createCustomerCalled = true
	if m.createCustomerFn != nil {
		return m.createCustomerFn(ctx, req)
	}
	return &paddlesdk.Customer{ID: "ctm_test"}, nil
}

func (m *mockPaddleClient) CreateAddress(ctx context.Context, customerID string, req *paddlesdk.CreateAddressRequest) (*paddlesdk.Address, error) {
	if m.createAddressFn != nil {
		return m.createAddressFn(ctx, customerID, req)
	}
	return &paddlesdk.Address{ID: "add_test"}, nil
}

func (m *mockPaddleClient) UpdateAddress(ctx context.Context, customerID string, addressID string, req *paddlesdk.UpdateAddressRequest) (*paddlesdk.Address, error) {
	if m.updateAddressFn != nil {
		return m.updateAddressFn(ctx, customerID, addressID, req)
	}
	return &paddlesdk.Address{ID: addressID}, nil
}

func (m *mockPaddleClient) CreateTransaction(ctx context.Context, req *paddlesdk.CreateTransactionRequest) (*paddlesdk.Transaction, error) {
	m.createTransactionCalled = true
	if m.createTransactionFn != nil {
		return m.createTransactionFn(ctx, req)
	}
	subID := "sub_test"
	return &paddlesdk.Transaction{ID: "txn_test", SubscriptionID: &subID}, nil
}

func (m *mockPaddleClient) PreviewTransaction(ctx context.Context, req *paddlesdk.PreviewTransactionCreateRequest) (*paddlesdk.TransactionPreview, error) {
	if m.previewTransactionFn != nil {
		return m.previewTransactionFn(ctx, req)
	}
	return &paddlesdk.TransactionPreview{}, nil
}

func (m *mockPaddleClient) VerifyWebhookSignature(ctx context.Context, payload []byte, signature string, webhookSecret string) error {
	if m.verifyWebhookSignatureFn != nil {
		return m.verifyWebhookSignatureFn(ctx, payload, signature, webhookSecret)
	}
	return nil
}

func (m *mockPaddleClient) CreateProduct(ctx context.Context, req *paddlesdk.CreateProductRequest) (*paddlesdk.Product, error) {
	m.createProductCalled = true
	if m.createProductFn != nil {
		return m.createProductFn(ctx, req)
	}
	return &paddlesdk.Product{ID: "pro_test"}, nil
}

func (m *mockPaddleClient) CreatePrice(ctx context.Context, req *paddlesdk.CreatePriceRequest) (*paddlesdk.Price, error) {
	if m.createPriceFn != nil {
		return m.createPriceFn(ctx, req)
	}
	return &paddlesdk.Price{ID: "pri_test"}, nil
}

func (m *mockPaddleClient) CreateSubscriptionCharge(ctx context.Context, req *paddlesdk.CreateSubscriptionChargeRequest) (*paddlesdk.Subscription, error) {
	m.createSubscriptionChargeCalled = true
	if m.createSubscriptionChargeFn != nil {
		return m.createSubscriptionChargeFn(ctx, req)
	}
	return &paddlesdk.Subscription{ID: "sub_test"}, nil
}

func (m *mockPaddleClient) ListTransactions(ctx context.Context, req *paddlesdk.ListTransactionsRequest) (*paddlesdk.Collection[*paddlesdk.Transaction], error) {
	if m.listTransactionsFn != nil {
		return m.listTransactionsFn(ctx, req)
	}
	return nil, nil
}

func (m *mockPaddleClient) GetTransaction(ctx context.Context, transactionID string) (*paddlesdk.Transaction, error) {
	if m.getTransactionFn != nil {
		return m.getTransactionFn(ctx, transactionID)
	}
	return &paddlesdk.Transaction{ID: transactionID}, nil
}

func (m *mockPaddleClient) ListCustomers(ctx context.Context, req *paddlesdk.ListCustomersRequest) (*paddlesdk.Collection[*paddlesdk.Customer], error) {
	return nil, nil
}

func (m *mockPaddleClient) ListSubscriptions(ctx context.Context, req *paddlesdk.ListSubscriptionsRequest) (*paddlesdk.Collection[*paddlesdk.Subscription], error) {
	if m.listSubscriptionsFn != nil {
		return m.listSubscriptionsFn(ctx, req)
	}
	return nil, nil
}

// --- Test helpers ---

func buildTestContext() context.Context {
	return testutil.SetupContext()
}

func buildTestLogger() *logger.Logger {
	return logger.NewNoopLogger()
}

func buildTestSyncService(
	client paddle.PaddleClient,
	mappingRepo entityintegrationmapping.Repository,
	customerRepo customer.Repository,
	invoiceRepo invoice.Repository,
	subscriptionRepo subscription.Repository,
	connectionRepo connection.Repository,
) *paddle.PaddleSyncService {
	return paddle.NewPaddleSyncService(
		client,
		customerRepo,
		invoiceRepo,
		subscriptionRepo,
		newTestMappingService(mappingRepo),
		connectionRepo,
		buildTestLogger(),
		"test-auth-secret",
	)
}

// seedCustomer pre-populates the in-memory customer store with a test customer.
func seedCustomer(ctx context.Context, t *testing.T, store *testutil.InMemoryCustomerStore, id string) {
	t.Helper()
	c := &customer.Customer{
		ID:             id,
		Email:          "test@example.com",
		Name:           "Test Customer",
		AddressCountry: "US",
		EnvironmentID:  types.GetEnvironmentID(ctx),
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	require.NoError(t, store.Create(ctx, c))
}

// seedMapping pre-populates the mapping store with an existing entity→provider mapping.
func seedMapping(
	ctx context.Context,
	t *testing.T,
	store entityintegrationmapping.Repository,
	entityID string,
	entityType types.IntegrationEntityType,
	providerEntityID string,
	extraMeta map[string]interface{},
) {
	t.Helper()
	meta := map[string]interface{}{
		paddle.MetaKeyCreatedVia: paddle.CreatedViaFlexpriceToProvider,
		paddle.MetaKeySyncedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	for k, v := range extraMeta {
		meta[k] = v
	}
	m := &entityintegrationmapping.EntityIntegrationMapping{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
		EntityID:         entityID,
		EntityType:       entityType,
		ProviderType:     string(types.SecretProviderPaddle),
		ProviderEntityID: providerEntityID,
		Metadata:         meta,
		EnvironmentID:    types.GetEnvironmentID(ctx),
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}
	require.NoError(t, store.Create(ctx, m))
}

// --- Idempotency tests ---

// TestEnsureCustomerSynced_AlreadyMapped verifies that when a customer→Paddle mapping
// already exists in entity_integration_mapping, CreateCustomer is NOT called on Paddle.
func TestEnsureCustomerSynced_AlreadyMapped(t *testing.T) {
	ctx := buildTestContext()

	mockClient := &mockPaddleClient{}
	mappingStore := testutil.NewInMemoryEntityIntegrationMappingStore()
	customerStore := testutil.NewInMemoryCustomerStore()
	connectionStore := testutil.NewInMemoryConnectionStore()

	const customerID = "cust_already_synced"
	const paddleCustomerID = "ctm_existing"
	const paddleAddressID = "add_existing"

	// Seed the customer so Get() succeeds.
	seedCustomer(ctx, t, customerStore, customerID)

	// Seed an existing mapping — this is the key to the idempotency test.
	seedMapping(ctx, t, mappingStore, customerID, types.IntegrationEntityTypeCustomer, paddleCustomerID, map[string]interface{}{
		paddle.MetaKeyPaddleAddressID: paddleAddressID,
	})

	svc := buildTestSyncService(mockClient, mappingStore, customerStore, nil, testutil.NewInMemorySubscriptionStore(), connectionStore)

	resp, err := svc.EnsureCustomerSynced(ctx, paddle.EnsureCustomerSyncedRequest{CustomerID: customerID})
	require.NoError(t, err)

	// Must return the existing IDs.
	assert.Equal(t, paddleCustomerID, resp.PaddleCustomerID)
	assert.False(t, resp.Created, "should not be marked as created when mapping already exists")

	// The critical assertion: CreateCustomer must NOT have been called.
	assert.False(t, mockClient.createCustomerCalled, "CreateCustomer must NOT be called when customer is already mapped")
}

// TestEnsureSubscriptionSynced_AlreadyMapped verifies that when a subscription→Paddle mapping
// already exists in entity_integration_mapping, CreateTransaction is NOT called on Paddle.
func TestEnsureSubscriptionSynced_AlreadyMapped(t *testing.T) {
	ctx := buildTestContext()

	mockClient := &mockPaddleClient{}
	mappingStore := testutil.NewInMemoryEntityIntegrationMappingStore()
	customerStore := testutil.NewInMemoryCustomerStore()
	connectionStore := testutil.NewInMemoryConnectionStore()

	const subscriptionID = "sub_already_synced"
	const paddleSubID = "sub_existing_paddle"

	// Seed an existing subscription mapping.
	seedMapping(ctx, t, mappingStore, subscriptionID, types.IntegrationEntityTypeSubscription, paddleSubID, nil)

	svc := buildTestSyncService(mockClient, mappingStore, customerStore, nil, testutil.NewInMemorySubscriptionStore(), connectionStore)

	resp, err := svc.EnsureSubscriptionSynced(ctx, paddle.EnsureSubscriptionSyncedRequest{
		Subscription: &subscription.Subscription{
			ID:         subscriptionID,
			CustomerID: "cust_test",
		},
		PriceIDToProductID: map[string]string{},
	})
	require.NoError(t, err)

	// Must return the existing Paddle subscription ID.
	assert.Equal(t, paddleSubID, resp.PaddleSubscriptionID)
	assert.False(t, resp.Created, "should not be marked as created when mapping already exists")

	// The critical assertion: CreateTransaction must NOT have been called.
	assert.False(t, mockClient.createTransactionCalled, "CreateTransaction must NOT be called when subscription is already mapped")
}

// TestEnsureSubscriptionSynced_TxnInMetadata verifies Guard 2: when no mapping exists but
// the subscription metadata already has a paddle_transaction_id, EnsureSubscriptionSynced
// returns the stored checkout URL without calling CreateTransaction.
func TestEnsureSubscriptionSynced_TxnInMetadata(t *testing.T) {
	ctx := buildTestContext()

	mockClient := &mockPaddleClient{}
	mappingStore := testutil.NewInMemoryEntityIntegrationMappingStore()
	customerStore := testutil.NewInMemoryCustomerStore()
	subStore := testutil.NewInMemorySubscriptionStore()
	connectionStore := testutil.NewInMemoryConnectionStore()

	const subscriptionID = "sub_txn_in_meta"
	const storedTxnID = "txn_bootstrap_001"
	const storedCheckoutURL = "https://checkout.paddle.com/checkout/custom/abc"

	// Seed the subscription with paddle_transaction_id already in metadata.
	sub := &subscription.Subscription{
		ID:            subscriptionID,
		CustomerID:    "cust_test",
		Currency:      "usd",
		BillingPeriod: "month",
		Metadata: types.Metadata{
			paddle.MetaKeyPaddleTransactionID: storedTxnID,
			paddle.MetaKeyPaddleCheckoutURL:   storedCheckoutURL,
		},
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
	require.NoError(t, subStore.Create(ctx, sub))

	// No mapping seeded — only metadata is present.

	svc := buildTestSyncService(mockClient, mappingStore, customerStore, nil, subStore, connectionStore)

	resp, err := svc.EnsureSubscriptionSynced(ctx, paddle.EnsureSubscriptionSyncedRequest{
		Subscription:       sub,
		PriceIDToProductID: map[string]string{"pri_123": "pro_456"},
	})
	require.NoError(t, err)

	// PaddleSubscriptionID must be empty — not yet activated.
	assert.Empty(t, resp.PaddleSubscriptionID, "should not have a Paddle sub ID when checkout is still pending")
	assert.NotEmpty(t, resp.CheckoutURL, "should return a checkout URL from stored metadata")
	assert.False(t, resp.Created, "should not be marked as created when checkout was previously initiated")

	// The critical assertion: CreateTransaction must NOT have been called again.
	assert.False(t, mockClient.createTransactionCalled, "CreateTransaction must NOT be called when txn_id is already in subscription metadata")
}

// TestEnsureSubscriptionSynced_CreatesTransaction verifies Guard 3: when neither a mapping
// nor a paddle_transaction_id in metadata exists, a new bootstrap transaction is created,
// the subscription metadata is updated, and Created=true is returned.
func TestEnsureSubscriptionSynced_CreatesTransaction(t *testing.T) {
	ctx := buildTestContext()

	const subscriptionID = "sub_needs_bootstrap"
	const customerID = "cust_bootstrap"
	const paddleCustomerID = "ctm_new"
	const paddleAddressID = "add_new"
	const newTxnID = "txn_created_001"
	const newCheckoutURL = "https://checkout.paddle.com/checkout/custom/xyz"

	// Mock CreateTransaction to return a transaction with a checkout URL.
	mockClient := &mockPaddleClient{
		createCustomerFn: func(ctx context.Context, req *paddlesdk.CreateCustomerRequest) (*paddlesdk.Customer, error) {
			return &paddlesdk.Customer{ID: paddleCustomerID}, nil
		},
		createAddressFn: func(ctx context.Context, cID string, req *paddlesdk.CreateAddressRequest) (*paddlesdk.Address, error) {
			return &paddlesdk.Address{ID: paddleAddressID}, nil
		},
		createPriceFn: func(ctx context.Context, req *paddlesdk.CreatePriceRequest) (*paddlesdk.Price, error) {
			return &paddlesdk.Price{ID: "pri_bootstrap_paddle"}, nil
		},
		createTransactionFn: func(ctx context.Context, req *paddlesdk.CreateTransactionRequest) (*paddlesdk.Transaction, error) {
			checkURL := newCheckoutURL
			return &paddlesdk.Transaction{
				ID:       newTxnID,
				Checkout: &paddlesdk.TransactionCheckout{URL: &checkURL},
			}, nil
		},
	}

	mappingStore := testutil.NewInMemoryEntityIntegrationMappingStore()
	customerStore := testutil.NewInMemoryCustomerStore()
	subStore := testutil.NewInMemorySubscriptionStore()
	connectionStore := testutil.NewInMemoryConnectionStore()

	// Seed the customer so EnsureCustomerSynced can look it up.
	seedCustomer(ctx, t, customerStore, customerID)

	// Seed a customer→Paddle mapping so EnsureCustomerSynced skips creation.
	seedMapping(ctx, t, mappingStore, customerID, types.IntegrationEntityTypeCustomer, paddleCustomerID, map[string]interface{}{
		paddle.MetaKeyPaddleAddressID: paddleAddressID,
	})

	// Subscription has NO metadata — the clean state.
	sub := &subscription.Subscription{
		ID:                 subscriptionID,
		CustomerID:         customerID,
		Currency:           "usd",
		BillingPeriod:      "month",
		BillingPeriodCount: 1,
		EnvironmentID:      types.GetEnvironmentID(ctx),
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	require.NoError(t, subStore.Create(ctx, sub))

	svc := buildTestSyncService(mockClient, mappingStore, customerStore, nil, subStore, connectionStore)

	resp, err := svc.EnsureSubscriptionSynced(ctx, paddle.EnsureSubscriptionSyncedRequest{
		Subscription:       sub,
		PriceIDToProductID: map[string]string{"pri_fp_123": "pro_paddle_456"},
	})
	require.NoError(t, err)

	// Created=true since we just initiated the bootstrap.
	assert.True(t, resp.Created, "Created must be true for a newly bootstrapped subscription")
	// PaddleSubscriptionID is empty — mapping is created by the webhook, not here.
	assert.Empty(t, resp.PaddleSubscriptionID, "PaddleSubscriptionID must be empty before webhook fires")
	// CheckoutURL must be populated.
	assert.NotEmpty(t, resp.CheckoutURL, "CheckoutURL must be returned so the customer can complete checkout")

	// CreateTransaction must have been called.
	assert.True(t, mockClient.createTransactionCalled, "CreateTransaction must be called for a new bootstrap")

	// Subscription metadata must be updated with the txn ID.
	updatedSub, getErr := subStore.Get(ctx, subscriptionID)
	require.NoError(t, getErr)
	assert.Equal(t, newTxnID, updatedSub.Metadata[paddle.MetaKeyPaddleTransactionID],
		"subscription metadata must contain the new paddle_transaction_id")
}

// TestSyncInvoice_AlreadySynced verifies that when an invoice→Paddle mapping already exists
// in entity_integration_mapping, CreateSubscriptionCharge is NOT called on Paddle.
func TestSyncInvoice_AlreadySynced(t *testing.T) {
	ctx := buildTestContext()

	mockClient := &mockPaddleClient{}
	mappingStore := testutil.NewInMemoryEntityIntegrationMappingStore()
	customerStore := testutil.NewInMemoryCustomerStore()
	invoiceStore := testutil.NewInMemoryInvoiceStore()
	connectionStore := testutil.NewInMemoryConnectionStore()

	const invoiceID = "inv_already_synced"
	const paddleTxnID = "txn_existing"
	const checkoutURL = "https://checkout.paddle.com/checkout/custom/test"

	// Seed the invoice so invoiceRepo.Get() succeeds.
	subID := "sub_test"
	inv := &invoice.Invoice{
		ID:             invoiceID,
		CustomerID:     "cust_test",
		SubscriptionID: &subID,
		Currency:       "USD",
		EnvironmentID:  types.GetEnvironmentID(ctx),
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	require.NoError(t, invoiceStore.Create(ctx, inv))

	// Seed an existing invoice mapping — the primary idempotency guard.
	seedMapping(ctx, t, mappingStore, invoiceID, types.IntegrationEntityTypeInvoice, paddleTxnID, map[string]interface{}{
		paddle.MetaKeyPaddleCheckoutURL: checkoutURL,
	})

	svc := buildTestSyncService(mockClient, mappingStore, customerStore, invoiceStore, testutil.NewInMemorySubscriptionStore(), connectionStore)

	resp, err := svc.SyncInvoice(ctx, paddle.SyncInvoiceRequest{InvoiceID: invoiceID})
	require.NoError(t, err)

	// Must return the already-synced transaction ID.
	assert.Equal(t, paddleTxnID, resp.PaddleTransactionID)
	assert.True(t, resp.AlreadySynced, "response must report AlreadySynced=true")

	// The critical assertion: CreateSubscriptionCharge must NOT have been called.
	assert.False(t, mockClient.createSubscriptionChargeCalled, "CreateSubscriptionCharge must NOT be called when invoice is already mapped")
}

// TestSyncInvoice_UseCatalogPrices verifies that SyncInvoice creates a catalog price whose Name
// matches the line item's DisplayName, and that CreateSubscriptionCharge receives a
// SubscriptionChargeItemFromCatalog referencing that catalog price ID.
func TestSyncInvoice_UseCatalogPrices(t *testing.T) {
	ctx := buildTestContext()

	const invoiceID = "inv_catalog_price_test"
	const subID = "sub_catalog_price_test"
	const customerID = "cust_catalog_price_test"
	const priceID = "pri_fp_abc"
	const paddleProductID = "pro_paddle_abc"
	const paddleSubID = "sub_paddle_abc"
	const catalogPriceID = "pri_catalog_001"
	const displayName = "Acme Pro Seat"
	const chargeTxnID = "txn_charge_001"

	var capturedPriceReqName string
	var capturedChargeItems []paddlesdk.CreateSubscriptionChargeItems

	// Build a collection with one charge transaction for the ListTransactions call.
	listTxnJSON := []byte(`{
		"data": [{"id": "` + chargeTxnID + `", "origin": "subscription_charge"}],
		"meta": {
			"pagination": {
				"next_url": "",
				"per_page": 1,
				"has_more": false,
				"estimated_total": 1
			}
		}
	}`)
	txnCollection := &paddlesdk.Collection[*paddlesdk.Transaction]{}
	require.NoError(t, txnCollection.UnmarshalJSON(listTxnJSON))

	mockClient := &mockPaddleClient{
		createPriceFn: func(_ context.Context, req *paddlesdk.CreatePriceRequest) (*paddlesdk.Price, error) {
			if req.Name != nil {
				capturedPriceReqName = *req.Name
			}
			return &paddlesdk.Price{ID: catalogPriceID}, nil
		},
		createSubscriptionChargeFn: func(_ context.Context, req *paddlesdk.CreateSubscriptionChargeRequest) (*paddlesdk.Subscription, error) {
			capturedChargeItems = req.Items
			return &paddlesdk.Subscription{ID: paddleSubID}, nil
		},
		listTransactionsFn: func(_ context.Context, _ *paddlesdk.ListTransactionsRequest) (*paddlesdk.Collection[*paddlesdk.Transaction], error) {
			return txnCollection, nil
		},
		getTransactionFn: func(_ context.Context, txnID string) (*paddlesdk.Transaction, error) {
			return &paddlesdk.Transaction{ID: txnID}, nil
		},
	}

	mappingStore := testutil.NewInMemoryEntityIntegrationMappingStore()
	customerStore := testutil.NewInMemoryCustomerStore()
	invoiceStore := testutil.NewInMemoryInvoiceStore()
	subStore := testutil.NewInMemorySubscriptionStore()
	connectionStore := testutil.NewInMemoryConnectionStore()

	// Seed the invoice with one line item.
	fpPriceID := priceID
	fpDisplayName := displayName
	inv := &invoice.Invoice{
		ID:             invoiceID,
		CustomerID:     customerID,
		SubscriptionID: func() *string { s := subID; return &s }(),
		Currency:       "USD",
		EnvironmentID:  types.GetEnvironmentID(ctx),
		BaseModel:      types.GetDefaultBaseModel(ctx),
		LineItems: []*invoice.InvoiceLineItem{
			{
				PriceID:     &fpPriceID,
				DisplayName: &fpDisplayName,
				Amount:      decimal.NewFromInt(100),
				Currency:    "USD",
			},
		},
	}
	require.NoError(t, invoiceStore.Create(ctx, inv))

	// Seed the subscription so subscriptionRepo.Get() succeeds.
	sub := &subscription.Subscription{
		ID:            subID,
		CustomerID:    customerID,
		Currency:      "usd",
		BillingPeriod: "month",
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
	require.NoError(t, subStore.Create(ctx, sub))

	// Seed price→product mapping so EnsureBulkProductSynced returns without calling CreateProduct.
	seedMapping(ctx, t, mappingStore, priceID, types.IntegrationEntityTypePrice, paddleProductID, nil)

	// Seed subscription→Paddle mapping so EnsureSubscriptionSynced hits Guard 1.
	seedMapping(ctx, t, mappingStore, subID, types.IntegrationEntityTypeSubscription, paddleSubID, nil)

	svc := buildTestSyncService(mockClient, mappingStore, customerStore, invoiceStore, subStore, connectionStore)

	resp, err := svc.SyncInvoice(ctx, paddle.SyncInvoiceRequest{InvoiceID: invoiceID})
	require.NoError(t, err)

	// Verify the charge transaction ID was returned.
	assert.Equal(t, chargeTxnID, resp.PaddleTransactionID)
	assert.False(t, resp.AlreadySynced)

	// CreatePrice must have been called with the line item's display name.
	assert.Equal(t, displayName, capturedPriceReqName,
		"CreatePrice Name must match the invoice line item DisplayName")

	// CreateSubscriptionCharge must have been called with a catalog-price item referencing the returned price ID.
	require.NotEmpty(t, capturedChargeItems, "CreateSubscriptionCharge must receive at least one item")
	// The item should be a SubscriptionChargeItemFromCatalog — unwrap and check the price ID.
	// paddlesdk encodes the type in the value field; we check via JSON marshaling.
	itemJSON, marshalErr := json.Marshal(capturedChargeItems[0])
	require.NoError(t, marshalErr)
	assert.Contains(t, string(itemJSON), catalogPriceID,
		"CreateSubscriptionCharge item must reference the catalog price ID returned by CreatePrice")
}

// TestEnsureBulkProductSynced_AlreadyMapped verifies that when a price→Paddle product mapping
// already exists in entity_integration_mapping, CreateProduct is NOT called on Paddle.
func TestEnsureBulkProductSynced_AlreadyMapped(t *testing.T) {
	ctx := buildTestContext()
	mappingStore := testutil.NewInMemoryEntityIntegrationMappingStore()
	mockClient := &mockPaddleClient{}

	svc := buildTestSyncService(
		mockClient,
		mappingStore,
		testutil.NewInMemoryCustomerStore(),
		testutil.NewInMemoryInvoiceStore(),
		testutil.NewInMemorySubscriptionStore(),
		testutil.NewInMemoryConnectionStore(),
	)

	priceID := "pri_existing"
	seedMapping(ctx, t, mappingStore, priceID, types.IntegrationEntityTypePrice, "pro_already_exists", nil)

	resp, err := svc.EnsureBulkProductSynced(ctx, paddle.EnsureBulkProductSyncedRequest{
		Items: []paddle.EnsureBulkProductSyncedItem{{PriceID: priceID, Name: "My Product"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "pro_already_exists", resp.PriceIDToPaddleProductID[priceID])

	// The critical assertion: CreateProduct must NOT be called when mapping already exists.
	assert.False(t, mockClient.createProductCalled, "CreateProduct must NOT be called when mapping already exists")
}
