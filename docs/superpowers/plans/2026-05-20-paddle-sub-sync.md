# Paddle Subscription Sync Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement a $0 checkout-based Paddle subscription bootstrap flow so FlexPrice subscriptions with `allow_incomplete` payment behaviour gate invoice syncing on the customer completing a card-capture checkout, and activate automatically via the `subscription.activated` Paddle webhook.

**Architecture:** A new `PaddleSubscriptionSyncWorkflow` Temporal workflow creates a $0 Paddle transaction, stores the checkout URL in subscription metadata, and returns the URL to the customer. Invoice sync is blocked via a `CheckSubscriptionSyncStatus` activity that fires-and-forgets the subscription sync if no mapping exists, then fails non-retryably. When the customer completes checkout Paddle fires `subscription.activated`; the webhook handler creates the entity_integration_mapping and activates the FlexPrice subscription.

**Tech Stack:** Go 1.23, Temporal SDK (`go.temporal.io/sdk`), Paddle Go SDK v4 (`github.com/PaddleHQ/paddle-go-sdk/v4`), Uber FX DI

---

## File Map

| File | Action | Purpose |
|---|---|---|
| `internal/temporal/models/paddle_subscription_sync.go` | Create | Workflow input model |
| `internal/integration/paddle/dto.go` | Modify | Add `CheckoutURL` to `EnsureSubscriptionSyncedResponse` |
| `internal/types/temporal.go` | Modify | Add `TemporalPaddleSubscriptionSyncWorkflow` constant + routing |
| `internal/integration/paddle/sync_service.go` | Modify | Three-branch `EnsureSubscriptionSynced`; catalog prices in `SyncInvoice`; `ProcessSubscriptionActivatedWebhook` |
| `internal/temporal/activities/paddle/subscription_sync_activities.go` | Create | `SyncSubscriptionToPaddle` + `CheckSubscriptionSyncStatus` activities |
| `internal/temporal/workflows/paddle_subscription_sync_workflow.go` | Create | `PaddleSubscriptionSyncWorkflow` |
| `internal/temporal/workflows/paddle_invoice_sync_workflow.go` | Modify | Add step 2.5 subscription sync guard |
| `internal/integration/paddle/webhook/types.go` | Modify | Add `EventSubscriptionActivated` constant |
| `internal/integration/paddle/webhook/handler.go` | Modify | Add `subscription.activated` handler case |
| `internal/temporal/service/service.go` | Modify | Add `TemporalPaddleSubscriptionSyncWorkflow` build/route cases |
| `internal/temporal/registration.go` | Modify | Register new workflow + activities |
| `internal/integration/events/dispatch.go` | Modify | Add `triggerPaddleSubscriptionSyncIfEnabled` + `subscriptionAlreadySynced` |
| `internal/integration/events/handler.go` | Modify | Register `WebhookEventSubscriptionCreated` processor |

---

## Task 1: Workflow input model + types

**Files:**
- Create: `internal/temporal/models/paddle_subscription_sync.go`
- Modify: `internal/integration/paddle/dto.go`
- Modify: `internal/types/temporal.go`

- [ ] **Step 1: Create the workflow input model**

```go
// internal/temporal/models/paddle_subscription_sync.go
package models

import ierr "github.com/flexprice/flexprice/internal/errors"

// PaddleSubscriptionSyncWorkflowInput is the input for PaddleSubscriptionSyncWorkflow.
type PaddleSubscriptionSyncWorkflowInput struct {
	SubscriptionID string `json:"subscription_id"`
	CustomerID     string `json:"customer_id"`
	TenantID       string `json:"tenant_id"`
	EnvironmentID  string `json:"environment_id"`
}

func (i *PaddleSubscriptionSyncWorkflowInput) Validate() error {
	if i.SubscriptionID == "" {
		return ierr.NewError("subscription_id is required").Mark(ierr.ErrValidation)
	}
	if i.TenantID == "" {
		return ierr.NewError("tenant_id is required").Mark(ierr.ErrValidation)
	}
	if i.EnvironmentID == "" {
		return ierr.NewError("environment_id is required").Mark(ierr.ErrValidation)
	}
	return nil
}
```

- [ ] **Step 2: Add `CheckoutURL` to `EnsureSubscriptionSyncedResponse` in `dto.go`**

In `internal/integration/paddle/dto.go`, replace the existing `EnsureSubscriptionSyncedResponse`:

```go
// EnsureSubscriptionSyncedResponse is returned by PaddleSyncService.EnsureSubscriptionSynced.
type EnsureSubscriptionSyncedResponse struct {
	PaddleSubscriptionID string `json:"paddle_subscription_id"`
	// CheckoutURL is the Paddle checkout URL for the bootstrap transaction.
	// Non-empty when the subscription sync has been initiated but the customer has not yet completed checkout.
	CheckoutURL string `json:"checkout_url"`
	// Created is true when the bootstrap transaction was newly created.
	Created bool `json:"created"`
}
```

- [ ] **Step 3: Add `TemporalPaddleSubscriptionSyncWorkflow` constant in `types/temporal.go`**

Locate the block containing `TemporalPaddleCustomerSyncWorkflow` and `TemporalPaddleInvoiceSyncWorkflow`. Add directly after them:

```go
TemporalPaddleSubscriptionSyncWorkflow TemporalWorkflowType = "PaddleSubscriptionSyncWorkflow"
```

- [ ] **Step 4: Add to `GetSupportedWorkflows()` in `types/temporal.go`**

Find the slice literal in `GetSupportedWorkflows()` that contains `TemporalPaddleInvoiceSyncWorkflow` and add the new constant on the next line:

```go
TemporalPaddleSubscriptionSyncWorkflow,
```

- [ ] **Step 5: Add to task queue routing in `types/temporal.go`**

There are **two** places to update:

**a.** In the `TaskQueue()` switch statement, find the long `case` line containing `TemporalPaddleInvoiceSyncWorkflow` and `TemporalPaddleCustomerSyncWorkflow`. Append `TemporalPaddleSubscriptionSyncWorkflow` to that same `case` clause.

**b.** In `GetWorkflowsForTaskQueue()`, find the `case TemporalTaskQueueTask:` slice and add:

```go
TemporalPaddleSubscriptionSyncWorkflow,
```

- [ ] **Step 6: Compile check**

```bash
go build ./internal/temporal/... ./internal/types/... ./internal/integration/paddle/...
```

Expected: no errors.

- [ ] **Step 7: Commit**

```bash
git add internal/temporal/models/paddle_subscription_sync.go \
        internal/integration/paddle/dto.go \
        internal/types/temporal.go
git commit -m "feat(paddle): add PaddleSubscriptionSync types and workflow input model"
```

---

## Task 2: Update `EnsureSubscriptionSynced` — three-branch guard

**Files:**
- Modify: `internal/integration/paddle/sync_service.go`
- Test: `internal/integration/paddle/sync_service_test.go`

- [ ] **Step 1: Write failing tests for the three branches**

Open `internal/integration/paddle/sync_service_test.go` and add:

```go
func TestEnsureSubscriptionSynced_AlreadyMapped(t *testing.T) {
	// mapping exists → return paddle sub ID without creating transaction
	svc, mocks := newTestSyncService(t)
	sub := &subscription.Subscription{ID: "sub_fp_1", CustomerID: "cust_1"}
	mocks.mappingService.EXPECT().
		GetEntityIntegrationMappings(gomock.Any(), gomock.Any()).
		Return(&dto.ListEntityIntegrationMappingsResponse{
			Items: []*dto.EntityIntegrationMappingResponse{{
				ProviderEntityID: "sub_paddle_1",
			}},
		}, nil)

	resp, err := svc.EnsureSubscriptionSynced(context.Background(), EnsureSubscriptionSyncedRequest{Subscription: sub})
	require.NoError(t, err)
	assert.Equal(t, "sub_paddle_1", resp.PaddleSubscriptionID)
	assert.False(t, resp.Created)
}

func TestEnsureSubscriptionSynced_TxnInMetadata(t *testing.T) {
	// no mapping but paddle_transaction_id in metadata → return checkout URL, no new transaction
	svc, mocks := newTestSyncService(t)
	sub := &subscription.Subscription{
		ID:         "sub_fp_2",
		CustomerID: "cust_1",
		Metadata:   types.Metadata{"paddle_transaction_id": "txn_existing", "paddle_checkout_url": "https://checkout.example.com?_ptxn=txn_existing"},
	}
	mocks.mappingService.EXPECT().
		GetEntityIntegrationMappings(gomock.Any(), gomock.Any()).
		Return(&dto.ListEntityIntegrationMappingsResponse{Items: nil}, nil)

	resp, err := svc.EnsureSubscriptionSynced(context.Background(), EnsureSubscriptionSyncedRequest{Subscription: sub})
	require.NoError(t, err)
	assert.Empty(t, resp.PaddleSubscriptionID)
	assert.False(t, resp.Created)
	assert.Contains(t, resp.CheckoutURL, "txn_existing")
}

func TestEnsureSubscriptionSynced_CreatesTransaction(t *testing.T) {
	// no mapping, no metadata → creates bootstrap transaction and stores in metadata
	svc, mocks := newTestSyncService(t)
	sub := &subscription.Subscription{
		ID: "sub_fp_3", CustomerID: "cust_1",
		Currency: "usd", BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1,
	}
	mocks.mappingService.EXPECT().GetEntityIntegrationMappings(gomock.Any(), gomock.Any()).Return(&dto.ListEntityIntegrationMappingsResponse{}, nil)
	mocks.customerRepo.EXPECT().Get(gomock.Any(), "cust_1").Return(&customer.Customer{ID: "cust_1", Email: "a@b.com", AddressCountry: "US"}, nil)
	// ... (customer sync mocks)
	mocks.client.EXPECT().CreateTransaction(gomock.Any(), gomock.Any()).Return(&paddlesdk.Transaction{
		ID:       "txn_new",
		Checkout: &paddlesdk.TransactionCheckout{URL: paddlesdk.PtrTo("https://checkout.test?_ptxn=txn_new")},
	}, nil)
	mocks.subscriptionRepo.EXPECT().Get(gomock.Any(), "sub_fp_3").Return(sub, nil)
	mocks.subscriptionRepo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)

	resp, err := svc.EnsureSubscriptionSynced(context.Background(), EnsureSubscriptionSyncedRequest{
		Subscription:       sub,
		PriceIDToProductID: map[string]string{"price_1": "prod_1"},
	})
	require.NoError(t, err)
	assert.True(t, resp.Created)
	assert.Contains(t, resp.CheckoutURL, "txn_new")
}
```

- [ ] **Step 2: Run tests — confirm they fail**

```bash
go test ./internal/integration/paddle/... -run "TestEnsureSubscriptionSynced" -v
```

Expected: FAIL (old implementation doesn't have three-branch logic).

- [ ] **Step 3: Replace `EnsureSubscriptionSynced` in `sync_service.go`**

Find the existing `EnsureSubscriptionSynced` function (starts around line 257) and replace its body with:

```go
func (s *PaddleSyncService) EnsureSubscriptionSynced(ctx context.Context, req EnsureSubscriptionSyncedRequest) (*EnsureSubscriptionSyncedResponse, error) {
	sub := req.Subscription
	if sub == nil || sub.ID == "" {
		return nil, ierr.NewError("subscription is required").Mark(ierr.ErrValidation)
	}

	// Guard 1: mapping exists → already fully activated.
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
		checkoutURL, _ := resp.Items[0].Metadata[MetaKeyPaddleCheckoutURL].(string)
		return &EnsureSubscriptionSyncedResponse{
			PaddleSubscriptionID: resp.Items[0].ProviderEntityID,
			CheckoutURL:          s.appendCheckoutToken(ctx, checkoutURL),
			Created:              false,
		}, nil
	}

	// Guard 2: bootstrap transaction already created → sync in progress, return existing checkout URL.
	if txnID, ok := sub.Metadata[MetaKeyPaddleTransactionID].(string); ok && txnID != "" {
		checkoutURL, _ := sub.Metadata[MetaKeyPaddleCheckoutURL].(string)
		s.logger.Infow("paddle subscription sync already initiated, returning existing checkout URL",
			"sub_id", sub.ID, "paddle_transaction_id", txnID)
		return &EnsureSubscriptionSyncedResponse{
			CheckoutURL: s.appendCheckoutToken(ctx, checkoutURL),
			Created:     false,
		}, nil
	}

	// Guard 3: ensure customer + address exist in Paddle.
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

	// Build $0 catalog prices with billing cycle for the bootstrap transaction.
	type bootstrapPair struct{ priceID, paddlePriceID string }
	pairs := make([]bootstrapPair, 0, len(req.PriceIDToProductID))
	for priceID, productID := range req.PriceIDToProductID {
		name := priceID
		for _, li := range sub.LineItems {
			if li != nil && li.PriceID == priceID && li.DisplayName != "" {
				name = li.DisplayName
				break
			}
		}
		catalogPrice, priceErr := s.client.CreatePrice(ctx, &paddlesdk.CreatePriceRequest{
			ProductID:    productID,
			Description:  name,
			Name:         paddlesdk.PtrTo(name),
			UnitPrice:    paddlesdk.Money{Amount: "0", CurrencyCode: paddlesdk.CurrencyCode(currency)},
			BillingCycle: billingCycle,
			TaxMode:      paddlesdk.PtrTo(paddlesdk.TaxModeAccountSetting),
			Quantity:     &paddlesdk.PriceQuantity{Minimum: 1, Maximum: 1},
		})
		if priceErr != nil {
			return nil, fmt.Errorf("creating bootstrap catalog price for product %s: %w", productID, priceErr)
		}
		pairs = append(pairs, bootstrapPair{priceID, catalogPrice.ID})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].paddlePriceID < pairs[j].paddlePriceID })

	items := make([]paddlesdk.CreateTransactionItems, 0, len(pairs))
	for _, p := range pairs {
		items = append(items, *paddlesdk.NewCreateTransactionItemsTransactionItemFromCatalog(
			&paddlesdk.TransactionItemFromCatalog{PriceID: p.paddlePriceID, Quantity: 1},
		))
	}
	if len(items) == 0 {
		return nil, ierr.NewError("no products to bootstrap subscription with").Mark(ierr.ErrValidation)
	}

	txn, err := s.client.CreateTransaction(ctx, &paddlesdk.CreateTransactionRequest{
		CustomerID:     paddlesdk.PtrTo(customerResp.PaddleCustomerID),
		AddressID:      paddlesdk.PtrTo(customerResp.PaddleAddressID),
		CollectionMode: paddlesdk.PtrTo(paddlesdk.CollectionModeAutomatic),
		Items:          items,
		CustomData: map[string]interface{}{
			"flexprice_subscription_id": sub.ID,
			"environment_id":            types.GetEnvironmentID(ctx),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("creating bootstrap transaction: %w", err)
	}

	checkoutURL := ""
	if txn.Checkout != nil {
		checkoutURL = lo.FromPtrOr(txn.Checkout.URL, "")
	}
	if checkoutURL == "" {
		if conn, connErr := s.client.GetConnection(ctx); connErr == nil && conn != nil && conn.Metadata != nil {
			if base, ok := conn.Metadata[ConnKeyCheckoutURL].(string); ok && base != "" {
				checkoutURL = base + "?_ptxn=" + txn.ID
			}
		}
	}

	// Persist bootstrap transaction ID + checkout URL in subscription metadata.
	freshSub, err := s.subscriptionRepo.Get(ctx, sub.ID)
	if err != nil {
		return nil, fmt.Errorf("re-fetching subscription for metadata update: %w", err)
	}
	if freshSub.Metadata == nil {
		freshSub.Metadata = make(types.Metadata)
	}
	freshSub.Metadata[MetaKeyPaddleTransactionID] = txn.ID
	if checkoutURL != "" {
		freshSub.Metadata[MetaKeyPaddleCheckoutURL] = checkoutURL
	}
	if err := s.subscriptionRepo.Update(ctx, freshSub); err != nil {
		s.logger.Warnw("failed to persist paddle transaction ID in sub metadata",
			"sub_id", sub.ID, "txn_id", txn.ID, "error", err)
	}

	return &EnsureSubscriptionSyncedResponse{
		CheckoutURL: s.appendCheckoutToken(ctx, checkoutURL),
		Created:     true,
	}, nil
}
```

- [ ] **Step 4: Run tests — confirm they pass**

```bash
go test ./internal/integration/paddle/... -run "TestEnsureSubscriptionSynced" -v
```

Expected: PASS.

- [ ] **Step 5: Compile check**

```bash
go build ./internal/integration/paddle/...
```

- [ ] **Step 6: Commit**

```bash
git add internal/integration/paddle/sync_service.go \
        internal/integration/paddle/sync_service_test.go \
        internal/integration/paddle/dto.go
git commit -m "feat(paddle): three-branch EnsureSubscriptionSynced with metadata guard"
```

---

## Task 3: Update `SyncInvoice` — catalog prices for charges

**Files:**
- Modify: `internal/integration/paddle/sync_service.go`
- Test: `internal/integration/paddle/sync_service_test.go`

- [ ] **Step 1: Add `getExistingSubscriptionMapping` helper in `sync_service.go`**

After the existing `getExistingInvoiceMapping` method (around line 477), add:

```go
// getExistingSubscriptionMapping returns the Paddle entity_integration_mapping for a FlexPrice subscription, or nil.
func (s *PaddleSyncService) getExistingSubscriptionMapping(ctx context.Context, subscriptionID string) (*apidto.EntityIntegrationMappingResponse, error) {
	filter := &types.EntityIntegrationMappingFilter{
		EntityType:    types.IntegrationEntityTypeSubscription,
		EntityID:      subscriptionID,
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
```

- [ ] **Step 2: Write failing test for catalog-price charge creation**

In `internal/integration/paddle/sync_service_test.go`:

```go
func TestSyncInvoice_UseCatalogPrices(t *testing.T) {
	// Verifies CreatePrice is called per line item and charge uses catalog priceId refs.
	svc, mocks := newTestSyncService(t)
	invoiceID := "inv_1"
	subID := "sub_fp_1"
	priceID := "price_1"
	productID := "prod_paddle_1"
	paddleSubID := "sub_paddle_1"
	catalogPriceID := "pri_new_1"

	inv := &invoice.Invoice{
		ID:             invoiceID,
		SubscriptionID: &subID,
		LineItems: []*invoice.InvoiceLineItem{{
			PriceID:     &priceID,
			Amount:      decimal.NewFromFloat(100),
			Currency:    "usd",
			DisplayName: lo.ToPtr("Pro Plan"),
		}},
	}
	mocks.invoiceRepo.EXPECT().Get(gomock.Any(), invoiceID).Return(inv, nil)
	// No existing invoice mapping
	mocks.mappingService.EXPECT().GetEntityIntegrationMappings(gomock.Any(), matchEntityType(types.IntegrationEntityTypeInvoice)).Return(emptyMappings(), nil)
	// Existing sub mapping (activated)
	mocks.mappingService.EXPECT().GetEntityIntegrationMappings(gomock.Any(), matchEntityType(types.IntegrationEntityTypeSubscription)).Return(mappingWith(paddleSubID), nil)
	// Product mapping
	mocks.mappingService.EXPECT().GetEntityIntegrationMappings(gomock.Any(), matchEntityType(types.IntegrationEntityTypePrice)).Return(mappingWith(productID), nil)
	// CreatePrice called with display name and actual amount
	mocks.client.EXPECT().CreatePrice(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, req *paddlesdk.CreatePriceRequest) (*paddlesdk.Price, error) {
			assert.Equal(t, "Pro Plan", lo.FromPtr(req.Name))
			assert.Equal(t, productID, req.ProductID)
			assert.Equal(t, "10000", req.UnitPrice.Amount) // 100 USD in cents
			assert.Nil(t, req.BillingCycle)               // no billing cycle = one-time
			return &paddlesdk.Price{ID: catalogPriceID}, nil
		},
	)
	// CreateSubscriptionCharge called with catalog price ref
	mocks.client.EXPECT().CreateSubscriptionCharge(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, req *paddlesdk.CreateSubscriptionChargeRequest) (*paddlesdk.Subscription, error) {
			require.Len(t, req.Items, 1)
			assert.NotNil(t, req.Items[0].SubscriptionChargeItemFromCatalog)
			assert.Equal(t, catalogPriceID, req.Items[0].SubscriptionChargeItemFromCatalog.PriceID)
			return &paddlesdk.Subscription{}, nil
		},
	)
	// ... list + get transaction mocks (return a txn with checkout URL)

	_, err := svc.SyncInvoice(ctx, SyncInvoiceRequest{InvoiceID: invoiceID})
	require.NoError(t, err)
}
```

- [ ] **Step 3: Run the test — confirm it fails**

```bash
go test ./internal/integration/paddle/... -run "TestSyncInvoice_UseCatalogPrices" -v
```

Expected: FAIL (current code uses inline prices).

- [ ] **Step 4: Replace charge-item construction in `SyncInvoice`**

Find the section in `SyncInvoice` that builds `chargeItems` (around the comment `// Step 6: Build charge items`). Replace it entirely:

```go
// Step 5b: Get Paddle subscription ID from the activated mapping.
subMapping, err := s.getExistingSubscriptionMapping(ctx, *flexInvoice.SubscriptionID)
if err != nil {
	return nil, fmt.Errorf("looking up subscription mapping: %w", err)
}
if subMapping == nil {
	return nil, ierr.NewError("paddle subscription mapping not found; subscription must be activated before invoice sync").
		WithHint("Run PaddleSubscriptionSyncWorkflow and wait for customer to complete checkout").
		WithReportableDetails(map[string]interface{}{"subscription_id": *flexInvoice.SubscriptionID}).
		Mark(ierr.ErrValidation)
}

// Step 6: Create per-line-item catalog prices and build charge items.
chargeItems := make([]paddlesdk.CreateSubscriptionChargeItems, 0, len(flexInvoice.LineItems))
for _, li := range flexInvoice.LineItems {
	if li == nil || lo.FromPtr(li.PriceID) == "" {
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

	catalogPrice, priceErr := s.client.CreatePrice(ctx, &paddlesdk.CreatePriceRequest{
		ProductID:   paddleProductID,
		Description: displayName,
		Name:        paddlesdk.PtrTo(displayName),
		UnitPrice: paddlesdk.Money{
			Amount:       fmt.Sprintf("%d", amountSmallest),
			CurrencyCode: paddlesdk.CurrencyCode(strings.ToUpper(li.Currency)),
		},
		TaxMode:  paddlesdk.PtrTo(paddlesdk.TaxModeAccountSetting),
		Quantity: &paddlesdk.PriceQuantity{Minimum: 1, Maximum: 100000},
	})
	if priceErr != nil {
		return nil, fmt.Errorf("creating catalog price for line item %s: %w", priceID, priceErr)
	}

	chargeItems = append(chargeItems, *paddlesdk.NewCreateSubscriptionChargeItemsSubscriptionChargeItemFromCatalog(
		&paddlesdk.SubscriptionChargeItemFromCatalog{
			PriceID:  catalogPrice.ID,
			Quantity: 1,
		},
	))
}
```

Also replace the `CreateSubscriptionCharge` call's `SubscriptionID` field — it previously used `subResp.PaddleSubscriptionID`; change to `subMapping.ProviderEntityID`.

Remove the old `// Step 5: Ensure subscription synced.` block (`subResp, err := s.EnsureSubscriptionSynced(...)`) as the mapping lookup above replaces it.

- [ ] **Step 5: Run test — confirm it passes**

```bash
go test ./internal/integration/paddle/... -run "TestSyncInvoice_UseCatalogPrices" -v
```

Expected: PASS.

- [ ] **Step 6: Compile check**

```bash
go build ./internal/integration/paddle/...
```

- [ ] **Step 7: Commit**

```bash
git add internal/integration/paddle/sync_service.go \
        internal/integration/paddle/sync_service_test.go
git commit -m "feat(paddle): SyncInvoice uses per-line-item catalog prices for display names"
```

---

## Task 4: New `SubscriptionSyncActivities`

**Files:**
- Create: `internal/temporal/activities/paddle/subscription_sync_activities.go`
- Test: `internal/temporal/activities/paddle/subscription_sync_activities_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/temporal/activities/paddle/subscription_sync_activities_test.go`:

```go
package paddle_test

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/integration"
	paddleActs "github.com/flexprice/flexprice/internal/temporal/activities/paddle"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
)

func TestSyncSubscriptionToPaddle_NoPaddleConnection(t *testing.T) {
	factory := integration.NewMockFactory() // returns ErrNotFound for paddle
	acts := paddleActs.NewSubscriptionSyncActivities(factory, nil)

	err := acts.SyncSubscriptionToPaddle(context.Background(), models.PaddleSubscriptionSyncWorkflowInput{
		SubscriptionID: "sub_1", TenantID: "t1", EnvironmentID: "e1",
	})

	require.Error(t, err)
	var appErr *temporal.ApplicationError
	require.ErrorAs(t, err, &appErr)
	assert.True(t, appErr.NonRetryable())
}

func TestCheckSubscriptionSyncStatus_NoMapping_ReturnsNotSynced(t *testing.T) {
	factory := integration.NewMockFactoryWithActivatedPaddle(t, "inv_1", "sub_fp_1", false)
	acts := paddleActs.NewSubscriptionSyncActivities(factory, nil)

	status, err := acts.CheckSubscriptionSyncStatus(context.Background(), models.PaddleInvoiceSyncWorkflowInput{
		InvoiceID: "inv_1", TenantID: "t1", EnvironmentID: "e1",
	})

	require.NoError(t, err)
	assert.Equal(t, "not_synced", status)
}

func TestCheckSubscriptionSyncStatus_MappingExists_ReturnsActivated(t *testing.T) {
	factory := integration.NewMockFactoryWithActivatedPaddle(t, "inv_1", "sub_fp_1", true)
	acts := paddleActs.NewSubscriptionSyncActivities(factory, nil)

	status, err := acts.CheckSubscriptionSyncStatus(context.Background(), models.PaddleInvoiceSyncWorkflowInput{
		InvoiceID: "inv_1", TenantID: "t1", EnvironmentID: "e1",
	})

	require.NoError(t, err)
	assert.Equal(t, "activated", status)
}
```

- [ ] **Step 2: Run tests — confirm they fail**

```bash
go test ./internal/temporal/activities/paddle/... -run "TestSyncSubscriptionToPaddle|TestCheckSubscriptionSyncStatus" -v
```

Expected: FAIL (file does not exist yet).

- [ ] **Step 3: Create `subscription_sync_activities.go`**

```go
// internal/temporal/activities/paddle/subscription_sync_activities.go
package paddle

import (
	"context"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integration"
	paddleint "github.com/flexprice/flexprice/internal/integration/paddle"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/temporal"
)

// SubscriptionSyncActivities handles Paddle subscription sync activities.
type SubscriptionSyncActivities struct {
	integrationFactory *integration.Factory
	logger             *logger.Logger
}

// NewSubscriptionSyncActivities creates a new SubscriptionSyncActivities.
func NewSubscriptionSyncActivities(
	integrationFactory *integration.Factory,
	log *logger.Logger,
) *SubscriptionSyncActivities {
	return &SubscriptionSyncActivities{
		integrationFactory: integrationFactory,
		logger:             log,
	}
}

// SyncSubscriptionToPaddle creates the $0 bootstrap Paddle transaction for a subscription.
// Non-retryable on validation errors (missing address, no line items, no Paddle connection).
func (a *SubscriptionSyncActivities) SyncSubscriptionToPaddle(
	ctx context.Context,
	input models.PaddleSubscriptionSyncWorkflowInput,
) error {
	if err := input.Validate(); err != nil {
		return temporal.NewNonRetryableApplicationError(err.Error(), "ValidationError", err)
	}

	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)

	paddleIntegration, err := a.integrationFactory.GetPaddleIntegration(ctx)
	if err != nil {
		if ierr.IsNotFound(err) {
			return temporal.NewNonRetryableApplicationError(
				"Paddle connection not configured",
				"ConnectionNotFound",
				err,
			)
		}
		return err
	}

	_, err = paddleIntegration.SyncSvc.EnsureSubscriptionSynced(ctx, paddleint.EnsureSubscriptionSyncedRequest{
		// Subscription is fetched inside EnsureSubscriptionSynced via subscriptionRepo.
		// We pass a minimal stub with only the ID; the method re-fetches via repo for guard 1/2,
		// but guard 3 needs the full object. So we load the full sub first.
		Subscription: &paddleint.SubscriptionStub{ID: input.SubscriptionID},
	})
	if err != nil {
		if ierr.IsValidation(err) {
			return temporal.NewNonRetryableApplicationError(err.Error(), "ValidationError", err)
		}
		return err
	}
	return nil
}

// CheckSubscriptionSyncStatus checks whether the FlexPrice subscription linked to the given
// invoice has an activated Paddle mapping. Returns "activated" or "not_synced".
func (a *SubscriptionSyncActivities) CheckSubscriptionSyncStatus(
	ctx context.Context,
	input models.PaddleInvoiceSyncWorkflowInput,
) (string, error) {
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)

	paddleIntegration, err := a.integrationFactory.GetPaddleIntegration(ctx)
	if err != nil {
		if ierr.IsNotFound(err) {
			return "not_synced", nil
		}
		return "", err
	}

	// Resolve subscription_id: prefer field on input, fall back to invoice lookup.
	subID := input.SubscriptionID
	if subID == "" {
		inv, invErr := paddleIntegration.SyncSvc.GetInvoiceByID(ctx, input.InvoiceID)
		if invErr != nil {
			return "", invErr
		}
		if inv.SubscriptionID == nil || *inv.SubscriptionID == "" {
			// Invoice is not subscription-linked; treat as no sub sync needed.
			return "activated", nil
		}
		subID = *inv.SubscriptionID
	}

	mapping, err := paddleIntegration.SyncSvc.GetSubscriptionMapping(ctx, subID)
	if err != nil {
		return "", err
	}
	if mapping == nil {
		return "not_synced", nil
	}
	return "activated", nil
}
```

> **Note:** `EnsureSubscriptionSynced` currently takes a full `Subscription` object. We need a thin loader path. In Task 4 step 4 below we add `GetInvoiceByID` and `GetSubscriptionMapping` helper methods to `PaddleSyncService`, and we pass the full sub loaded inside `SyncSubscriptionToPaddle` by fetching from `subscriptionRepo` directly.

- [ ] **Step 4: Add helper methods to `PaddleSyncService` in `sync_service.go`**

Add after `getExistingSubscriptionMapping`:

```go
// GetInvoiceByID fetches a FlexPrice invoice by ID. Used by CheckSubscriptionSyncStatus.
func (s *PaddleSyncService) GetInvoiceByID(ctx context.Context, invoiceID string) (*invoice.Invoice, error) {
	return s.invoiceRepo.Get(ctx, invoiceID)
}

// GetSubscriptionMapping returns the Paddle entity_integration_mapping for a subscription, or nil.
func (s *PaddleSyncService) GetSubscriptionMapping(ctx context.Context, subscriptionID string) (*apidto.EntityIntegrationMappingResponse, error) {
	return s.getExistingSubscriptionMapping(ctx, subscriptionID)
}
```

Also update `SyncSubscriptionToPaddle` in the activity to load the full subscription before calling `EnsureSubscriptionSynced`. Update the activity body:

```go
// Inside SyncSubscriptionToPaddle, replace the EnsureSubscriptionSynced call:
sub, fetchErr := paddleIntegration.SyncSvc.GetSubscriptionWithLineItems(ctx, input.SubscriptionID)
if fetchErr != nil {
	return fmt.Errorf("fetching subscription: %w", fetchErr)
}

// EnsureBulkProductSynced for all line item prices.
productItems := make([]paddleint.EnsureBulkProductSyncedItem, 0, len(sub.LineItems))
for _, li := range sub.LineItems {
	if li == nil || li.PriceID == "" {
		continue
	}
	productItems = append(productItems, paddleint.EnsureBulkProductSyncedItem{
		PriceID: li.PriceID,
		Name:    lo.FromPtrOr(&li.DisplayName, li.PriceID),
	})
}
productsResp, prodErr := paddleIntegration.SyncSvc.EnsureBulkProductSynced(ctx, paddleint.EnsureBulkProductSyncedRequest{Items: productItems})
if prodErr != nil {
	return fmt.Errorf("syncing products: %w", prodErr)
}

_, err = paddleIntegration.SyncSvc.EnsureSubscriptionSynced(ctx, paddleint.EnsureSubscriptionSyncedRequest{
	Subscription:       sub,
	PriceIDToProductID: productsResp.PriceIDToPaddleProductID,
})
```

Add `GetSubscriptionWithLineItems` to `PaddleSyncService`:

```go
// GetSubscriptionWithLineItems fetches a subscription including its line items.
func (s *PaddleSyncService) GetSubscriptionWithLineItems(ctx context.Context, subscriptionID string) (*subscription.Subscription, error) {
	return s.subscriptionRepo.GetWithLineItems(ctx, subscriptionID)
}
```

> If `GetWithLineItems` does not exist on the repo, use `s.subscriptionRepo.Get` — line items may already be loaded depending on the repo implementation. Verify with `grep -n "GetWithLineItems\|func.*Get\b" ./internal/repository/subscription.go`.

- [ ] **Step 5: Run tests — confirm they pass**

```bash
go test ./internal/temporal/activities/paddle/... -v
```

Expected: PASS.

- [ ] **Step 6: Compile check**

```bash
go build ./internal/temporal/activities/paddle/... ./internal/integration/paddle/...
```

- [ ] **Step 7: Commit**

```bash
git add internal/temporal/activities/paddle/subscription_sync_activities.go \
        internal/temporal/activities/paddle/subscription_sync_activities_test.go \
        internal/integration/paddle/sync_service.go
git commit -m "feat(paddle): SubscriptionSyncActivities — SyncSubscriptionToPaddle + CheckSubscriptionSyncStatus"
```

---

## Task 5: New `PaddleSubscriptionSyncWorkflow`

**Files:**
- Create: `internal/temporal/workflows/paddle_subscription_sync_workflow.go`

- [ ] **Step 1: Create the workflow file**

```go
// internal/temporal/workflows/paddle_subscription_sync_workflow.go
package workflows

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	WorkflowPaddleSubscriptionSync       = "PaddleSubscriptionSyncWorkflow"
	ActivitySyncSubscriptionToPaddle     = "SyncSubscriptionToPaddle"
	ActivityCheckSubscriptionSyncStatus  = "CheckSubscriptionSyncStatus"
)

// PaddleSubscriptionSyncWorkflow orchestrates creating the $0 Paddle bootstrap transaction
// for a FlexPrice subscription, enabling card capture via the checkout URL.
//
// Steps:
//  1. Sleep 2s — let subscription commit to DB.
//  2. EnsureCustomerSyncedToPaddle — create Paddle customer + address if absent.
//  3. SyncSubscriptionToPaddle — create $0 bootstrap transaction, store checkout URL in sub metadata.
func PaddleSubscriptionSyncWorkflow(ctx workflow.Context, input models.PaddleSubscriptionSyncWorkflowInput) error {
	logger := workflow.GetLogger(ctx)

	if err := input.Validate(); err != nil {
		return err
	}

	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	logger.Info("PaddleSubscriptionSyncWorkflow: step 1 — waiting for subscription to commit",
		"subscription_id", input.SubscriptionID)
	if err := workflow.Sleep(ctx, 2*time.Second); err != nil {
		return err
	}

	logger.Info("PaddleSubscriptionSyncWorkflow: step 2 — ensuring customer synced",
		"subscription_id", input.SubscriptionID)
	customerInput := models.PaddleCustomerSyncWorkflowInput{
		CustomerID:    input.CustomerID,
		TenantID:      input.TenantID,
		EnvironmentID: input.EnvironmentID,
	}
	if err := workflow.ExecuteActivity(ctx, ActivityEnsureCustomerSyncedToPaddle, customerInput).Get(ctx, nil); err != nil {
		logger.Error("PaddleSubscriptionSyncWorkflow: customer sync failed", "error", err)
		return err
	}

	logger.Info("PaddleSubscriptionSyncWorkflow: step 3 — syncing subscription to Paddle",
		"subscription_id", input.SubscriptionID)
	if err := workflow.ExecuteActivity(ctx, ActivitySyncSubscriptionToPaddle, input).Get(ctx, nil); err != nil {
		if ierr.IsValidation(errors.Unwrap(err)) {
			logger.Error("PaddleSubscriptionSyncWorkflow: non-retryable validation error", "error", err)
		}
		return err
	}

	logger.Info("PaddleSubscriptionSyncWorkflow: completed", "subscription_id", input.SubscriptionID)
	return nil
}
```

> Note: `ActivityEnsureCustomerSyncedToPaddle` is already defined in `paddle_invoice_sync_workflow.go` as `"EnsureCustomerSyncedToPaddle"`. Do not re-declare it — reference the same constant.

- [ ] **Step 2: Compile check**

```bash
go build ./internal/temporal/workflows/...
```

- [ ] **Step 3: Commit**

```bash
git add internal/temporal/workflows/paddle_subscription_sync_workflow.go
git commit -m "feat(paddle): PaddleSubscriptionSyncWorkflow Temporal workflow"
```

---

## Task 6: Updated `PaddleInvoiceSyncWorkflow` — subscription sync guard

**Files:**
- Modify: `internal/temporal/workflows/paddle_invoice_sync_workflow.go`

- [ ] **Step 1: Add subscription sync guard (step 2.5) to the workflow**

Open `internal/temporal/workflows/paddle_invoice_sync_workflow.go`. After the customer pre-check (`Step 2`) and before `Step 3` (invoice sync), add:

```go
// Step 2.5: Check if the subscription is synced to Paddle.
// If not, fire-and-forget PaddleSubscriptionSyncWorkflow and fail non-retryably.
// The operator must re-trigger this invoice sync workflow after the customer completes checkout.
logger.Info("Step 2.5: Checking subscription Paddle sync status", "invoice_id", input.InvoiceID)

var subSyncStatus string
if err := workflow.ExecuteActivity(ctx, ActivityCheckSubscriptionSyncStatus, input).Get(ctx, &subSyncStatus); err != nil {
    logger.Error("Failed to check subscription sync status", "error", err, "invoice_id", input.InvoiceID)
    return err
}

if subSyncStatus == "not_synced" {
    logger.Info("Subscription not synced to Paddle — triggering subscription sync workflow",
        "invoice_id", input.InvoiceID)

    childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
        WorkflowID:        WorkflowPaddleSubscriptionSync + "-" + input.InvoiceID,
        ParentClosePolicy: temporal.ParentClosePolicyAbandon,
    })
    // Fire-and-forget: do not call .Get() — we don't wait for it.
    workflow.ExecuteChildWorkflow(childCtx, PaddleSubscriptionSyncWorkflow, models.PaddleSubscriptionSyncWorkflowInput{
        SubscriptionID: input.SubscriptionID,
        CustomerID:     input.CustomerID,
        TenantID:       input.TenantID,
        EnvironmentID:  input.EnvironmentID,
    })

    return temporal.NewNonRetryableApplicationError(
        "paddle subscription sync triggered; re-run invoice sync after customer completes checkout",
        "SubscriptionNotSynced",
        nil,
    )
}

logger.Info("Step 2.5: Subscription is synced, proceeding to invoice sync", "invoice_id", input.InvoiceID)
```

Also add `ActivityCheckSubscriptionSyncStatus` to the constants block at the top of `paddle_invoice_sync_workflow.go` (alongside `ActivitySyncInvoiceToPaddle` and `ActivityEnsureCustomerSyncedToPaddle`):

```go
ActivityCheckSubscriptionSyncStatus = "CheckSubscriptionSyncStatus"
```

Remove the duplicate `ActivityCheckSubscriptionSyncStatus` and `ActivitySyncSubscriptionToPaddle` constants from `paddle_subscription_sync_workflow.go` — define them only in one file. Move the `PaddleSubscriptionSyncWorkflow`-specific constants to that file and remove from invoice sync file if any overlap. Keep `ActivityEnsureCustomerSyncedToPaddle` in `paddle_invoice_sync_workflow.go` as it was already there.

- [ ] **Step 2: Add `SubscriptionID` field to `PaddleInvoiceSyncWorkflowInput`**

In `internal/temporal/models/paddle_invoice_sync.go`, add the optional field:

```go
type PaddleInvoiceSyncWorkflowInput struct {
    InvoiceID      string `json:"invoice_id"`
    CustomerID     string `json:"customer_id"`
    SubscriptionID string `json:"subscription_id,omitempty"` // optional; activity falls back to invoice lookup
    TenantID       string `json:"tenant_id"`
    EnvironmentID  string `json:"environment_id"`
}
```

- [ ] **Step 3: Compile check**

```bash
go build ./internal/temporal/workflows/... ./internal/temporal/models/...
```

- [ ] **Step 4: Commit**

```bash
git add internal/temporal/workflows/paddle_invoice_sync_workflow.go \
        internal/temporal/workflows/paddle_subscription_sync_workflow.go \
        internal/temporal/models/paddle_invoice_sync.go
git commit -m "feat(paddle): PaddleInvoiceSyncWorkflow guards invoice sync on sub activation"
```

---

## Task 7: `subscription.activated` webhook handler

**Files:**
- Modify: `internal/integration/paddle/webhook/types.go`
- Modify: `internal/integration/paddle/webhook/handler.go`
- Modify: `internal/integration/paddle/sync_service.go`
- Modify: `internal/service/subscription.go` (update `ActivateIncompleteSubscription` for trialing)
- Test: `internal/integration/paddle/sync_service_test.go`

- [ ] **Step 1: Add event type constant to `webhook/types.go`**

```go
// EventSubscriptionActivated occurs when a Paddle subscription is activated
// (customer has completed checkout and payment method is saved).
EventSubscriptionActivated PaddleEventType = "subscription.activated"
```

- [ ] **Step 2: Write failing tests for `ProcessSubscriptionActivatedWebhook`**

In `internal/integration/paddle/sync_service_test.go`:

```go
func TestProcessSubscriptionActivatedWebhook_IncompleteToActive(t *testing.T) {
	svc, mocks := newTestSyncService(t)
	paddleSubID := "sub_paddle_1"
	flexSubID := "sub_fp_1"

	sub := &subscription.Subscription{
		ID:                 flexSubID,
		SubscriptionStatus: types.SubscriptionStatusIncomplete,
	}
	notification := &paddlenotification.SubscriptionActivated{
		Data: paddlenotification.SubscriptionNotification{
			ID:         paddleSubID,
			CustomData: paddlenotification.CustomData{"flexprice_subscription_id": flexSubID},
		},
	}

	// No existing mapping
	mocks.mappingService.EXPECT().GetEntityIntegrationMappings(gomock.Any(), gomock.Any()).Return(emptyMappings(), nil)
	mocks.mappingService.EXPECT().CreateEntityIntegrationMapping(gomock.Any(), gomock.Any()).Return(&dto.EntityIntegrationMappingResponse{}, nil)
	mocks.subscriptionRepo.EXPECT().Get(gomock.Any(), flexSubID).Return(sub, nil).Times(2)
	mocks.subscriptionRepo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
	// ActivateIncompleteSubscription internally calls SubRepo.Update
	mocks.subscriptionRepo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)

	err := svc.ProcessSubscriptionActivatedWebhook(context.Background(), &notification.Data, mockSubscriptionService(t))
	require.NoError(t, err)
}

func TestProcessSubscriptionActivatedWebhook_IncompleteToTrialing(t *testing.T) {
	svc, mocks := newTestSyncService(t)
	trialEnd := time.Now().Add(7 * 24 * time.Hour)
	sub := &subscription.Subscription{
		ID:                 "sub_fp_2",
		SubscriptionStatus: types.SubscriptionStatusIncomplete,
		TrialEnd:           &trialEnd,
	}
	// ... similar setup, expect status set to trialing not active
}

func TestProcessSubscriptionActivatedWebhook_MissingCustomData_NoOp(t *testing.T) {
	svc, _ := newTestSyncService(t)
	notification := &paddlenotification.SubscriptionNotification{
		ID:         "sub_paddle_1",
		CustomData: paddlenotification.CustomData{}, // no flexprice_subscription_id
	}
	err := svc.ProcessSubscriptionActivatedWebhook(context.Background(), notification, nil)
	require.NoError(t, err) // should be a no-op
}

func TestProcessSubscriptionActivatedWebhook_TrialingSubNoOp(t *testing.T) {
	svc, mocks := newTestSyncService(t)
	sub := &subscription.Subscription{
		ID:                 "sub_fp_3",
		SubscriptionStatus: types.SubscriptionStatusTrialing,
	}
	notification := &paddlenotification.SubscriptionNotification{
		ID:         "sub_paddle_3",
		CustomData: paddlenotification.CustomData{"flexprice_subscription_id": "sub_fp_3"},
	}
	mocks.mappingService.EXPECT().GetEntityIntegrationMappings(gomock.Any(), gomock.Any()).Return(emptyMappings(), nil)
	mocks.mappingService.EXPECT().CreateEntityIntegrationMapping(gomock.Any(), gomock.Any()).Return(&dto.EntityIntegrationMappingResponse{}, nil)
	mocks.subscriptionRepo.EXPECT().Get(gomock.Any(), "sub_fp_3").Return(sub, nil)
	mocks.subscriptionRepo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
	// No call to ActivateIncompleteSubscription expected.

	err := svc.ProcessSubscriptionActivatedWebhook(context.Background(), notification, nil)
	require.NoError(t, err)
}
```

- [ ] **Step 3: Run tests — confirm they fail**

```bash
go test ./internal/integration/paddle/... -run "TestProcessSubscriptionActivatedWebhook" -v
```

Expected: FAIL.

- [ ] **Step 4: Add `ProcessSubscriptionActivatedWebhook` to `sync_service.go`**

Add at the end of the file:

```go
// ProcessSubscriptionActivatedWebhook handles the Paddle subscription.activated event.
// It creates the entity_integration_mapping with the real Paddle subscription ID and
// transitions the FlexPrice subscription from incomplete to active (or trialing).
func (s *PaddleSyncService) ProcessSubscriptionActivatedWebhook(
	ctx context.Context,
	data *paddlenotification.SubscriptionNotification,
	subscriptionService interfaces.SubscriptionService,
) error {
	paddleSubID := data.ID

	flexSubID, _ := data.CustomData["flexprice_subscription_id"].(string)
	if flexSubID == "" {
		s.logger.Warnw("subscription.activated: no flexprice_subscription_id in custom_data — skipping",
			"paddle_sub_id", paddleSubID)
		return nil
	}

	// Idempotent: create mapping only if one does not already exist.
	existing, err := s.getExistingSubscriptionMapping(ctx, flexSubID)
	if err != nil {
		return fmt.Errorf("checking existing subscription mapping: %w", err)
	}
	if existing == nil {
		_, err = s.mappingService.CreateEntityIntegrationMapping(ctx, apidto.CreateEntityIntegrationMappingRequest{
			EntityID:         flexSubID,
			EntityType:       types.IntegrationEntityTypeSubscription,
			ProviderType:     string(types.SecretProviderPaddle),
			ProviderEntityID: paddleSubID,
			Metadata: map[string]interface{}{
				MetaKeyPaddleSubscriptionID: paddleSubID,
				MetaKeySyncedAt:             time.Now().UTC().Format(time.RFC3339),
			},
		})
		if err != nil {
			return fmt.Errorf("creating subscription mapping: %w", err)
		}
	}

	// Persist paddle_subscription_id in sub metadata.
	sub, err := s.subscriptionRepo.Get(ctx, flexSubID)
	if err != nil {
		return fmt.Errorf("fetching subscription: %w", err)
	}
	if sub.Metadata == nil {
		sub.Metadata = make(types.Metadata)
	}
	sub.Metadata[MetaKeyPaddleSubscriptionID] = paddleSubID
	if err := s.subscriptionRepo.Update(ctx, sub); err != nil {
		s.logger.Warnw("failed to update sub metadata with paddle_subscription_id",
			"sub_id", flexSubID, "error", err)
	}

	// Activate the FlexPrice subscription.
	switch sub.SubscriptionStatus {
	case types.SubscriptionStatusIncomplete:
		if sub.TrialEnd != nil && sub.TrialEnd.After(time.Now()) {
			sub.SubscriptionStatus = types.SubscriptionStatusTrialing
			if err := s.subscriptionRepo.Update(ctx, sub); err != nil {
				return fmt.Errorf("setting subscription to trialing: %w", err)
			}
			s.logger.Infow("subscription.activated: set incomplete→trialing",
				"sub_id", flexSubID, "paddle_sub_id", paddleSubID)
		} else {
			if subscriptionService == nil {
				return ierr.NewError("subscriptionService is required for ActivateIncompleteSubscription").Mark(ierr.ErrInternal)
			}
			if err := subscriptionService.ActivateIncompleteSubscription(ctx, flexSubID); err != nil {
				return fmt.Errorf("activating incomplete subscription: %w", err)
			}
			s.logger.Infow("subscription.activated: set incomplete→active",
				"sub_id", flexSubID, "paddle_sub_id", paddleSubID)
		}
	default:
		s.logger.Infow("subscription.activated: subscription already in non-incomplete state — no-op",
			"sub_id", flexSubID, "status", sub.SubscriptionStatus)
	}

	return nil
}
```

- [ ] **Step 5: Add `handleSubscriptionActivated` to `webhook/handler.go`**

In the `HandleWebhookEvent` switch:

```go
case string(EventSubscriptionActivated):
    return h.handleSubscriptionActivated(ctx, payload, services)
```

Add the handler method:

```go
func (h *Handler) handleSubscriptionActivated(ctx context.Context, payload []byte, services *ServiceDependencies) error {
    if services == nil || services.SubscriptionService == nil {
        h.logger.Errorw("subscription service not available for subscription.activated webhook")
        return nil
    }
    var event paddlesdk.SubscriptionActivatedEvent
    if err := json.Unmarshal(payload, &event); err != nil {
        h.logger.Errorw("failed to parse subscription.activated payload", "error", err)
        return nil
    }
    err := h.syncSvc.ProcessSubscriptionActivatedWebhook(ctx, &event.Data, services.SubscriptionService)
    if err != nil {
        h.logger.Errorw("failed to process subscription.activated webhook",
            "error", err, "paddle_sub_id", event.Data.ID)
    }
    return nil
}
```

Check `interfaces.ServiceDependencies` to confirm `SubscriptionService` is already a field. If not, add it:

```bash
grep -n "SubscriptionService\|ServiceDependencies" ./internal/interfaces/service.go | head -10
```

- [ ] **Step 6: Run tests — confirm they pass**

```bash
go test ./internal/integration/paddle/... -run "TestProcessSubscriptionActivatedWebhook" -v
```

Expected: PASS.

- [ ] **Step 7: Compile check**

```bash
go build ./internal/integration/paddle/...
```

- [ ] **Step 8: Commit**

```bash
git add internal/integration/paddle/webhook/types.go \
        internal/integration/paddle/webhook/handler.go \
        internal/integration/paddle/sync_service.go \
        internal/integration/paddle/sync_service_test.go
git commit -m "feat(paddle): subscription.activated webhook handler activates incomplete subscriptions"
```

---

## Task 8: Wiring — temporal service, registration, dispatch

**Files:**
- Modify: `internal/temporal/service/service.go`
- Modify: `internal/temporal/registration.go`
- Modify: `internal/integration/events/dispatch.go`
- Modify: `internal/integration/events/handler.go`

- [ ] **Step 1: Add `buildPaddleSubscriptionSyncInput` to `temporal/service/service.go`**

Add after `buildPaddleCustomerSyncInput` (around line 1071):

```go
func (s *temporalService) buildPaddleSubscriptionSyncInput(_ context.Context, tenantID, environmentID string, params interface{}) (interface{}, error) {
    if input, ok := params.(*models.PaddleSubscriptionSyncWorkflowInput); ok {
        input.TenantID = tenantID
        input.EnvironmentID = environmentID
        return *input, nil
    }
    if input, ok := params.(models.PaddleSubscriptionSyncWorkflowInput); ok {
        input.TenantID = tenantID
        input.EnvironmentID = environmentID
        return input, nil
    }
    return nil, errors.NewError("invalid input for Paddle subscription sync workflow").
        WithHint("Provide PaddleSubscriptionSyncWorkflowInput with subscription_id").
        Mark(errors.ErrValidation)
}
```

- [ ] **Step 2: Add the case in `buildWorkflowInput` switch in `service.go`**

Find the block with `case types.TemporalPaddleCustomerSyncWorkflow:` and add directly after it:

```go
case types.TemporalPaddleSubscriptionSyncWorkflow:
    return s.buildPaddleSubscriptionSyncInput(ctx, tenantID, environmentID, params)
```

- [ ] **Step 3: Add deterministic workflow ID in the `getWorkflowIdentifier` switch in `service.go`**

Find the block with `case types.TemporalPaddleCustomerSyncWorkflow:` in `getWorkflowIdentifier` and add:

```go
case types.TemporalPaddleSubscriptionSyncWorkflow:
    if input, ok := params.(models.PaddleSubscriptionSyncWorkflowInput); ok {
        return input.SubscriptionID
    }
```

- [ ] **Step 4: Register workflow + activities in `registration.go`**

Find the line that instantiates `paddleCustomerSyncActivities` (around line 179):

```go
paddleCustomerSyncActivities := paddleActivities.NewCustomerSyncActivities(...)
```

Add directly after:

```go
paddleSubscriptionSyncActivities := paddleActivities.NewSubscriptionSyncActivities(
    params.IntegrationFactory,
    params.Logger,
)
```

Find `buildWorkerConfig(...)` call (the long line around line 258) and add `paddleSubscriptionSyncActivities` to the argument list.

Find `buildWorkerConfig` function signature (around line 284) and add:

```go
paddleSubscriptionSyncActivities *paddleActivities.SubscriptionSyncActivities,
```

Find the `workerConfigs` block where `workflows.PaddleInvoiceSyncWorkflow` is registered and add the new workflow:

```go
workflows.PaddleSubscriptionSyncWorkflow,
```

Find the activity registration block and add:

```go
paddleSubscriptionSyncActivities.SyncSubscriptionToPaddle,
paddleSubscriptionSyncActivities.CheckSubscriptionSyncStatus,
```

- [ ] **Step 5: Add `subscriptionAlreadySynced` helper to `dispatch.go`**

After `invoiceAlreadySynced`:

```go
// subscriptionAlreadySynced returns true when an entity_integration_mapping already exists
// for (subscriptionID, subscription, provider). Prevents duplicate sub sync workflows.
func subscriptionAlreadySynced(ctx context.Context, eimRepo entityintegrationmapping.Repository, subscriptionID string, provider types.SecretProvider) bool {
    if eimRepo == nil {
        return false
    }
    filter := types.NewNoLimitEntityIntegrationMappingFilter()
    filter.EntityID = subscriptionID
    filter.EntityType = types.IntegrationEntityTypeSubscription
    filter.ProviderTypes = []string{string(provider)}
    count, err := eimRepo.Count(ctx, filter)
    return err == nil && count > 0
}
```

- [ ] **Step 6: Add `DispatchSubscriptionVendorSync` and `triggerPaddleSubscriptionSyncIfEnabled` to `dispatch.go`**

Add after `DispatchCustomerVendorSync`:

```go
type subscriptionVendorSyncInput struct {
    TenantID       string
    EnvironmentID  string
    UserID         string
    SubscriptionID string
    CustomerID     string
}

// DispatchSubscriptionVendorSync starts Paddle subscription sync workflow for subscriptions
// created with allow_incomplete payment behaviour.
func DispatchSubscriptionVendorSync(
    ctx context.Context,
    cfg *config.Configuration,
    connRepo connection.Repository,
    eimRepo entityintegrationmapping.Repository,
    log *logger.Logger,
    event *types.WebhookEvent,
    msgUUID string,
) error {
    if cfg != nil && !cfg.IntegrationEvents.Enabled {
        return nil
    }

    var pl struct {
        SubscriptionID  string `json:"subscription_id"`
        CustomerID      string `json:"customer_id"`
        PaymentBehavior string `json:"payment_behavior"`
        CollectionMethod string `json:"collection_method"`
    }
    if err := json.Unmarshal(event.Payload, &pl); err != nil || pl.SubscriptionID == "" {
        log.Errorw("integration_events: invalid subscription payload, dropping",
            "message_uuid", msgUUID, "error", err)
        return nil
    }

    // Only sync subscriptions that use allow_incomplete + charge_automatically.
    if pl.PaymentBehavior != string(types.PaymentBehaviorAllowIncomplete) {
        return nil
    }
    if pl.CollectionMethod != string(types.CollectionMethodChargeAutomatically) {
        return nil
    }

    in := subscriptionVendorSyncInput{
        TenantID:       event.TenantID,
        EnvironmentID:  event.EnvironmentID,
        UserID:         event.UserID,
        SubscriptionID: pl.SubscriptionID,
        CustomerID:     pl.CustomerID,
    }

    temporalSvc := temporalservice.GetGlobalTemporalService()
    if temporalSvc == nil {
        return errTemporalUnavailable
    }

    return triggerPaddleSubscriptionSyncIfEnabled(ctx, connRepo, eimRepo, temporalSvc, log, in)
}

func triggerPaddleSubscriptionSyncIfEnabled(
    ctx context.Context,
    connRepo connection.Repository,
    eimRepo entityintegrationmapping.Repository,
    temporalSvc temporalservice.TemporalService,
    log *logger.Logger,
    in subscriptionVendorSyncInput,
) error {
    conn, err := getConnectionIfExists(ctx, connRepo, types.SecretProviderPaddle)
    if err != nil {
        return err
    }
    if conn == nil || !conn.IsSubscriptionOutboundEnabled() {
        return nil
    }
    if subscriptionAlreadySynced(ctx, eimRepo, in.SubscriptionID, types.SecretProviderPaddle) {
        log.Infow("integration_events: subscription already synced to Paddle, skipping",
            "subscription_id", in.SubscriptionID)
        return nil
    }

    input := &temporalmodels.PaddleSubscriptionSyncWorkflowInput{
        SubscriptionID: in.SubscriptionID,
        CustomerID:     in.CustomerID,
        TenantID:       in.TenantID,
        EnvironmentID:  in.EnvironmentID,
    }
    workflowRun, err := temporalSvc.ExecuteWorkflow(ctx, types.TemporalPaddleSubscriptionSyncWorkflow, input)
    if err != nil {
        log.Errorw("integration_events: failed to start PaddleSubscriptionSyncWorkflow",
            "subscription_id", in.SubscriptionID, "error", err)
        return fmt.Errorf("paddle subscription sync workflow start failed: %w", err)
    }
    log.Infow("integration_events: PaddleSubscriptionSyncWorkflow started",
        "subscription_id", in.SubscriptionID,
        "workflow_id", workflowRun.GetID())
    return nil
}
```

- [ ] **Step 7: Register `WebhookEventSubscriptionCreated` processor in `handler.go`**

In the `h.processors` map inside `NewHandler`, add:

```go
types.WebhookEventSubscriptionCreated: func(ctx context.Context, event *types.WebhookEvent, msg *message.Message) error {
    return DispatchSubscriptionVendorSync(
        ctx,
        h.deps.Config,
        h.deps.ConnectionRepo,
        h.deps.EIMRepo,
        h.deps.Logger,
        event,
        msg.UUID,
    )
},
```

- [ ] **Step 8: Compile check — full project**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 9: Run all tests**

```bash
go test ./internal/... -race -count=1
```

Expected: all pass (or pre-existing failures only).

- [ ] **Step 10: Commit**

```bash
git add internal/temporal/service/service.go \
        internal/temporal/registration.go \
        internal/integration/events/dispatch.go \
        internal/integration/events/handler.go
git commit -m "feat(paddle): wire PaddleSubscriptionSyncWorkflow into temporal service, registration, and dispatch"
```

---

## End-to-End Smoke Test

After all tasks are complete, verify the full flow manually:

1. Create a FlexPrice subscription with `collection_method=charge_automatically`, `payment_behavior=allow_incomplete` for a tenant with a Paddle connection.
2. Confirm `PaddleSubscriptionSyncWorkflow` starts in the Temporal UI (http://localhost:8088).
3. Confirm subscription metadata contains `paddle_transaction_id` and `paddle_checkout_url`.
4. Trigger `PaddleInvoiceSyncWorkflow` for a related invoice — confirm it fails with `SubscriptionNotSynced` error.
5. Simulate `subscription.activated` webhook via Paddle sandbox or `curl` to the webhook endpoint.
6. Confirm entity_integration_mapping created with `provider_entity_id = sub_xxx`.
7. Confirm FlexPrice subscription status transitions to `active` (or `trialing`).
8. Re-trigger `PaddleInvoiceSyncWorkflow` — confirm it proceeds and creates a Paddle subscription charge with catalog prices.
9. Verify the charge transaction line items in Paddle show the correct display names.
