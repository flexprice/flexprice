# Wallet Auto-Topup Invoice Deduplication Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prevent duplicate auto-topup invoices by guarding `triggerAutoTopup` with a "pending invoice" check, and re-triggering the check after payment completes.

**Architecture:** Add a new `WALLET_AUTO_TOPUP` billing reason to identify auto-topup invoices; add `BillingReason` to `InvoiceFilter` so we can query for pending auto-topup invoices; add a guard in `triggerAutoTopup` that skips invoice creation when one already exists; call `PublishWalletBalanceAlertEvent` after payment completes to re-trigger the check.

**Tech Stack:** Go 1.23+, Ent ORM, in-memory test stores (`internal/testutil`), testify suites.

---

## File Map

| File | Change |
|---|---|
| `internal/types/invoice.go` | New `InvoiceBillingReasonWalletAutoTopup` constant + `BillingReason` field on `InvoiceFilter` |
| `internal/repository/ent/invoice.go` | Wire `BillingReason` filter in `applyEntityQueryOptions` |
| `internal/testutil/inmemory_invoice_store.go` | Wire `BillingReason` filter in `invoiceFilterFn` |
| `internal/api/dto/wallet.go` | Add `BillingReason` field to `TopUpWalletRequest` |
| `internal/service/wallet.go` | New `hasPendingAutoTopupInvoice` helper; update `triggerAutoTopup`; thread `BillingReason` through `handlePurchasedCreditInvoicedTransaction`; add re-trigger in `completePurchasedCreditTransaction` |
| `internal/service/wallet_test.go` | New test suite `WalletAutoTopupInvoiceSuite` |

---

### Task 1: Add `WALLET_AUTO_TOPUP` billing reason and `BillingReason` filter field

**Files:**
- Modify: `internal/types/invoice.go`

- [ ] **Step 1: Add the new constant after `InvoiceBillingReasonAutoInvoiceThreshold` (line 238)**

In `internal/types/invoice.go`, after the line:
```go
InvoiceBillingReasonAutoInvoiceThreshold InvoiceBillingReason = "AUTO_INVOICE_THRESHOLD"
```
add:
```go
// InvoiceBillingReasonWalletAutoTopup is generated when a wallet balance drops
// below the auto-topup threshold and invoiced top-up is enabled.
InvoiceBillingReasonWalletAutoTopup InvoiceBillingReason = "WALLET_AUTO_TOPUP"
```

- [ ] **Step 2: Add it to the `Validate()` allowed list**

In `internal/types/invoice.go`, the `Validate()` function has an `allowed` slice. Add the new constant:
```go
allowed := []InvoiceBillingReason{
    InvoiceBillingReasonSubscriptionCreate,
    InvoiceBillingReasonSubscriptionCycle,
    InvoiceBillingReasonSubscriptionUpdate,
    InvoiceBillingReasonSubscriptionTrialEnd,
    InvoiceBillingReasonSubscriptionTrialStart,
    InvoiceBillingReasonProration,
    InvoiceBillingReasonManual,
    InvoiceBillingReasonAutoInvoiceThreshold,
    InvoiceBillingReasonWalletAutoTopup,  // ← add this line
}
```

- [ ] **Step 3: Add `BillingReason` field to `InvoiceFilter`**

In `internal/types/invoice.go`, inside `InvoiceFilter` struct, add after the `PaymentStatus` field:
```go
// billing_reason filters invoices by why they were generated
BillingReason InvoiceBillingReason `json:"billing_reason,omitempty" form:"billing_reason"`
```

- [ ] **Step 4: Verify it compiles**

```bash
go build ./internal/types/...
```
Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add internal/types/invoice.go
git commit -m "feat: add WALLET_AUTO_TOPUP billing reason and BillingReason filter to InvoiceFilter"
```

---

### Task 2: Wire `BillingReason` filter in ent repo and in-memory store

**Files:**
- Modify: `internal/repository/ent/invoice.go`
- Modify: `internal/testutil/inmemory_invoice_store.go`

- [ ] **Step 1: Add predicate in ent repo `applyEntityQueryOptions`**

In `internal/repository/ent/invoice.go`, inside `func (o InvoiceQueryOptions) applyEntityQueryOptions` (around line 1096 after the `PaymentStatus` block), add:
```go
if f.BillingReason != "" {
    query = query.Where(invoice.BillingReasonEQ(string(f.BillingReason)))
}
```

Place it after:
```go
if len(f.PaymentStatus) > 0 {
    query = query.Where(invoice.PaymentStatusIn(f.PaymentStatus...))
}
```

- [ ] **Step 2: Add filter in in-memory store `invoiceFilterFn`**

In `internal/testutil/inmemory_invoice_store.go`, inside the `invoiceFilterFn` function, add after the payment status block (after line `if len(f.PaymentStatus) > 0 && !lo.Contains(f.PaymentStatus, inv.PaymentStatus)`):
```go
// Filter by billing reason
if f.BillingReason != "" && inv.BillingReason != string(f.BillingReason) {
    return false
}
```

- [ ] **Step 3: Verify it compiles**

```bash
go build ./internal/repository/... ./internal/testutil/...
```
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add internal/repository/ent/invoice.go internal/testutil/inmemory_invoice_store.go
git commit -m "feat: wire BillingReason filter in invoice ent repo and in-memory store"
```

---

### Task 3: Add `BillingReason` to `TopUpWalletRequest` and thread through to invoice creation

**Files:**
- Modify: `internal/api/dto/wallet.go`
- Modify: `internal/service/wallet.go`

- [ ] **Step 1: Add `BillingReason` field to `TopUpWalletRequest`**

In `internal/api/dto/wallet.go`, inside `TopUpWalletRequest` struct, add after the `Metadata` field:
```go
// billing_reason indicates why this top-up was triggered (e.g. WALLET_AUTO_TOPUP).
// When set, it is stamped on the invoice created for PURCHASED_CREDIT_INVOICED transactions.
BillingReason types.InvoiceBillingReason `json:"billing_reason,omitempty"`
```

- [ ] **Step 2: Thread `BillingReason` into `invReq` inside `handlePurchasedCreditInvoicedTransaction`**

In `internal/service/wallet.go`, find the `invReq := dto.CreateInvoiceRequest{` block (around line 814). Add `BillingReason` to it:
```go
invReq := dto.CreateInvoiceRequest{
    CustomerID:     w.CustomerID,
    AmountDue:      amount,
    AmountPaid:     amountPaid,
    Subtotal:       amount,
    Total:          amount,
    Currency:       w.Currency,
    InvoiceType:    types.InvoiceTypeOneOff,
    DueDate:        lo.ToPtr(time.Now().UTC()),
    IdempotencyKey: idempotencyKey,
    LineItems: []dto.CreateInvoiceLineItemRequest{
        {
            Amount:      amount,
            Quantity:    decimal.NewFromInt(1),
            DisplayName: lo.ToPtr(fmt.Sprintf("Purchase %s Credits", req.CreditsToAdd.String())),
        },
    },
    PaymentStatus: lo.ToPtr(paymentStatus),
    Metadata:      invoiceMetadata,
    BillingReason: req.BillingReason, // ← add this line
}
```

- [ ] **Step 3: Verify it compiles**

```bash
go build ./internal/api/dto/... ./internal/service/...
```
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add internal/api/dto/wallet.go internal/service/wallet.go
git commit -m "feat: add BillingReason to TopUpWalletRequest and thread through to invoice creation"
```

---

### Task 4: Add `hasPendingAutoTopupInvoice` helper and update `triggerAutoTopup`

**Files:**
- Modify: `internal/service/wallet.go`

- [ ] **Step 1: Add `hasPendingAutoTopupInvoice` helper method**

In `internal/service/wallet.go`, add this new method directly before `triggerAutoTopup` (before line 3096):

```go
// hasPendingAutoTopupInvoice returns true if there is already a FINALIZED, unpaid
// auto-topup invoice for this customer. Used to prevent duplicate invoices while
// waiting for the customer to pay.
func (s *walletService) hasPendingAutoTopupInvoice(ctx context.Context, customerID string) (bool, error) {
	filter := types.NewNoLimitInvoiceFilter()
	filter.CustomerID = customerID
	filter.BillingReason = types.InvoiceBillingReasonWalletAutoTopup
	filter.PaymentStatus = []types.PaymentStatus{types.PaymentStatusPending}
	filter.InvoiceStatus = []types.InvoiceStatus{types.InvoiceStatusFinalized}
	filter.SkipLineItems = true

	invoices, err := s.InvoiceRepo.List(ctx, filter)
	if err != nil {
		return false, ierr.WithError(err).
			WithHint("Failed to check for pending auto-topup invoices").
			WithReportableDetails(map[string]interface{}{
				"customer_id": customerID,
			}).
			Mark(ierr.ErrDatabase)
	}
	return len(invoices) > 0, nil
}
```

- [ ] **Step 2: Update `triggerAutoTopup` to guard invoiced-mode and set `BillingReason`**

Replace the existing `triggerAutoTopup` function (lines 3096–3139) with:

```go
// triggerAutoTopup checks if auto top-up is enabled and triggers it if needed
func (s *walletService) triggerAutoTopup(ctx context.Context, w *wallet.Wallet, ongoingBalance decimal.Decimal) error {

	if w.AutoTopup == nil || w.AutoTopup.Enabled == nil || !*w.AutoTopup.Enabled {
		s.Logger.DebugwCtx(ctx, "auto top-up not enabled, skipping",
			"wallet_id", w.ID,
		)
		return nil
	}

	// Check if ongoing balance is below threshold
	if ongoingBalance.LessThanOrEqual(*w.AutoTopup.Threshold) {

		isInvoiced := w.AutoTopup.Invoicing != nil && *w.AutoTopup.Invoicing

		// Guard: for invoiced mode, skip if there is already a pending auto-topup invoice.
		// This prevents flooding the customer with invoices while they wait to pay.
		if isInvoiced {
			hasPending, err := s.hasPendingAutoTopupInvoice(ctx, w.CustomerID)
			if err != nil {
				s.Logger.ErrorwCtx(ctx, "failed to check for pending auto-topup invoice",
					"error", err,
					"wallet_id", w.ID,
					"customer_id", w.CustomerID,
				)
				return err
			}
			if hasPending {
				s.Logger.InfowCtx(ctx, "pending auto-topup invoice exists, skipping",
					"wallet_id", w.ID,
					"customer_id", w.CustomerID,
					"auto_topup_threshold", *w.AutoTopup.Threshold,
				)
				return nil
			}
		}

		transactionReason := lo.Ternary(isInvoiced,
			types.TransactionReasonPurchasedCreditInvoiced,
			types.TransactionReasonPurchasedCreditDirect,
		)
		billingReason := lo.Ternary(isInvoiced,
			types.InvoiceBillingReasonWalletAutoTopup,
			types.InvoiceBillingReason(""),
		)

		_, err := s.TopUpWallet(ctx, w.ID, &dto.TopUpWalletRequest{
			CreditsToAdd:      *w.AutoTopup.Amount,
			Amount:            *w.AutoTopup.Amount,
			TransactionReason: transactionReason,
			BillingReason:     billingReason,
			IdempotencyKey:    lo.ToPtr(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_TRANSACTION)),
			Description:       "Auto top-up triggered for low ongoing balance",
			Metadata:          types.Metadata{"auto_topup": "true"},
		})
		if err != nil {
			s.Logger.ErrorwCtx(ctx, "failed to top up wallet for auto top-up",
				"error", err,
				"wallet_id", w.ID,
				"auto_topup_threshold", *w.AutoTopup.Threshold,
				"auto_topup_amount", *w.AutoTopup.Amount,
			)
			return err
		}
		s.Logger.DebugwCtx(ctx, "auto top-up triggered",
			"wallet_id", w.ID,
			"auto_topup_threshold", *w.AutoTopup.Threshold,
			"auto_topup_amount", *w.AutoTopup.Amount,
			"invoiced", isInvoiced,
		)
	}

	s.Logger.InfowCtx(ctx, "auto top-up check completed",
		"wallet_id", w.ID,
		"ongoing_balance", ongoingBalance,
		"auto_topup_threshold", *w.AutoTopup.Threshold,
	)

	return nil
}
```

- [ ] **Step 3: Verify it compiles**

```bash
go build ./internal/service/...
```
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add internal/service/wallet.go
git commit -m "feat: add hasPendingAutoTopupInvoice guard to triggerAutoTopup for invoiced mode"
```

---

### Task 5: Re-trigger balance check after payment completes

**Files:**
- Modify: `internal/service/wallet.go`

- [ ] **Step 1: Add `PublishWalletBalanceAlertEvent` call at the end of `completePurchasedCreditTransaction`**

In `internal/service/wallet.go`, find `completePurchasedCreditTransaction` (around line 926). The function currently ends with:

```go
	// Log credit balance alert after transaction completes
	if err := s.logCreditBalanceAlert(ctx, w, w.CreditBalance.Add(tx.CreditAmount)); err != nil {
		// Don't fail the transaction if alert logging fails
		s.Logger.ErrorwCtx(ctx, "failed to log credit balance alert after completing purchased credit transaction",
			"error", err,
			"wallet_id", w.ID,
		)
	}

	return nil
}
```

Replace that block with:

```go
	// Log credit balance alert after transaction completes
	if err := s.logCreditBalanceAlert(ctx, w, w.CreditBalance.Add(tx.CreditAmount)); err != nil {
		// Don't fail the transaction if alert logging fails
		s.Logger.ErrorwCtx(ctx, "failed to log credit balance alert after completing purchased credit transaction",
			"error", err,
			"wallet_id", w.ID,
		)
	}

	// Re-trigger wallet balance alert so triggerAutoTopup can fire a new invoice if
	// the balance is still below threshold after payment. The previous invoice is now
	// SUCCEEDED so the guard in triggerAutoTopup will allow a fresh one.
	// This is async (Kafka) and non-fatal.
	s.PublishWalletBalanceAlertEvent(ctx, tx.CustomerID, true, tx.WalletID)

	return nil
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/service/...
```
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/service/wallet.go
git commit -m "feat: re-trigger wallet balance alert after purchased credit payment completes"
```

---

### Task 6: Tests

**Files:**
- Modify: `internal/service/wallet_test.go`

- [ ] **Step 1: Write the failing tests**

Add a new test suite at the bottom of `internal/service/wallet_test.go`:

```go
// ── Auto-topup invoice deduplication tests ────────────────────────────────

type WalletAutoTopupInvoiceSuite struct {
	testutil.BaseServiceTestSuite
	service  WalletService
	customer *customer.Customer
	wallet   *wallet.Wallet
}

func TestWalletAutoTopupInvoice(t *testing.T) {
	suite.Run(t, new(WalletAutoTopupInvoiceSuite))
}

func (s *WalletAutoTopupInvoiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()

	stores := s.GetStores()
	pubsub := testutil.NewInMemoryPubSub()
	s.service = NewWalletService(ServiceParams{
		Logger:                   s.GetLogger(),
		Config:                   s.GetConfig(),
		DB:                       s.GetDB(),
		WalletRepo:               stores.WalletRepo,
		SubRepo:                  stores.SubscriptionRepo,
		SubscriptionLineItemRepo: stores.SubscriptionLineItemRepo,
		PlanRepo:                 stores.PlanRepo,
		PriceRepo:                stores.PriceRepo,
		EventRepo:                stores.EventRepo,
		MeterRepo:                stores.MeterRepo,
		CustomerRepo:             stores.CustomerRepo,
		InvoiceRepo:              stores.InvoiceRepo,
		EntitlementRepo:          stores.EntitlementRepo,
		FeatureRepo:              stores.FeatureRepo,
		AddonAssociationRepo:     stores.AddonAssociationRepo,
		SettingsRepo:             stores.SettingsRepo,
		AlertLogsRepo:            stores.AlertLogsRepo,
		FeatureUsageRepo:         stores.FeatureUsageRepo,
		EventPublisher:           s.GetPublisher(),
		WebhookPublisher:         s.GetWebhookPublisher(),
		WalletBalanceAlertPubSub: types.WalletBalanceAlertPubSub{PubSub: pubsub},
	})

	ctx := s.GetContext()

	// Customer
	s.customer = &customer.Customer{
		ID:        "cust_autotopup",
		ExternalID: "ext_autotopup",
		Name:      "AutoTopup Customer",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(stores.CustomerRepo.Create(ctx, s.customer))

	// Wallet with auto-topup enabled, invoicing=true, threshold=5, amount=10
	threshold := decimal.NewFromInt(5)
	amount := decimal.NewFromInt(10)
	invoicing := true
	enabled := true
	s.wallet = &wallet.Wallet{
		ID:           "wallet_autotopup",
		CustomerID:   s.customer.ID,
		Currency:     "USD",
		WalletStatus: types.WalletStatusActive,
		WalletType:   types.WalletTypePrePaid,
		CreditBalance: decimal.NewFromInt(3), // below threshold
		Balance:      decimal.NewFromFloat(3.0),
		ConversionRate:     decimal.NewFromInt(1),
		TopupConversionRate: decimal.NewFromInt(1),
		AutoTopup: &types.AutoTopup{
			Enabled:   &enabled,
			Threshold: &threshold,
			Amount:    &amount,
			Invoicing: &invoicing,
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(stores.WalletRepo.CreateWallet(ctx, s.wallet))
}

func (s *WalletAutoTopupInvoiceSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
	s.BaseServiceTestSuite.ClearStores()
}

func (s *WalletAutoTopupInvoiceSuite) GetContext() context.Context {
	return types.SetEnvironmentID(s.BaseServiceTestSuite.GetContext(), "env_test")
}

// hasPendingAutoTopupInvoice is accessible because tests are in package service.
func (s *WalletAutoTopupInvoiceSuite) svc() *walletService {
	return s.service.(*walletService)
}

// TestHasPendingAutoTopupInvoice_NoneExist — returns false when no invoices present
func (s *WalletAutoTopupInvoiceSuite) TestHasPendingAutoTopupInvoice_NoneExist() {
	ctx := s.GetContext()
	has, err := s.svc().hasPendingAutoTopupInvoice(ctx, s.customer.ID)
	s.NoError(err)
	s.False(has, "should be false when no invoices exist")
}

// TestHasPendingAutoTopupInvoice_PendingExists — returns true for FINALIZED + PENDING invoice
func (s *WalletAutoTopupInvoiceSuite) TestHasPendingAutoTopupInvoice_PendingExists() {
	ctx := s.GetContext()
	stores := s.GetStores()

	inv := &invoice.Invoice{
		ID:            "inv_autotopup_1",
		CustomerID:    s.customer.ID,
		InvoiceStatus: types.InvoiceStatusFinalized,
		PaymentStatus: types.PaymentStatusPending,
		BillingReason: string(types.InvoiceBillingReasonWalletAutoTopup),
		Currency:      "USD",
		AmountDue:     decimal.NewFromInt(10),
		InvoiceType:   types.InvoiceTypeOneOff,
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
	s.NoError(stores.InvoiceRepo.Create(ctx, inv))

	has, err := s.svc().hasPendingAutoTopupInvoice(ctx, s.customer.ID)
	s.NoError(err)
	s.True(has, "should be true when a FINALIZED+PENDING WALLET_AUTO_TOPUP invoice exists")
}

// TestHasPendingAutoTopupInvoice_PaidDoesNotBlock — returns false when invoice is SUCCEEDED
func (s *WalletAutoTopupInvoiceSuite) TestHasPendingAutoTopupInvoice_PaidDoesNotBlock() {
	ctx := s.GetContext()
	stores := s.GetStores()

	inv := &invoice.Invoice{
		ID:            "inv_autotopup_paid",
		CustomerID:    s.customer.ID,
		InvoiceStatus: types.InvoiceStatusFinalized,
		PaymentStatus: types.PaymentStatusSucceeded,
		BillingReason: string(types.InvoiceBillingReasonWalletAutoTopup),
		Currency:      "USD",
		AmountDue:     decimal.NewFromInt(10),
		InvoiceType:   types.InvoiceTypeOneOff,
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
	s.NoError(stores.InvoiceRepo.Create(ctx, inv))

	has, err := s.svc().hasPendingAutoTopupInvoice(ctx, s.customer.ID)
	s.NoError(err)
	s.False(has, "paid invoice should not block a new auto-topup")
}

// TestTriggerAutoTopup_GuardPreventsSecondInvoice — calling triggerAutoTopup twice
// while invoice is pending must not create a second invoice.
func (s *WalletAutoTopupInvoiceSuite) TestTriggerAutoTopup_GuardPreventsSecondInvoice() {
	ctx := s.GetContext()
	stores := s.GetStores()
	svc := s.svc()

	ongoingBalance := decimal.NewFromInt(3) // below threshold of 5

	// First call — should create one invoice
	err := svc.triggerAutoTopup(ctx, s.wallet, ongoingBalance)
	s.NoError(err)

	// Count invoices with WALLET_AUTO_TOPUP billing reason
	filter := types.NewNoLimitInvoiceFilter()
	filter.CustomerID = s.customer.ID
	filter.BillingReason = types.InvoiceBillingReasonWalletAutoTopup
	filter.SkipLineItems = true
	invoices, err := stores.InvoiceRepo.List(ctx, filter)
	s.NoError(err)
	s.Len(invoices, 1, "first call should create exactly one auto-topup invoice")

	// Second call while invoice is still pending — guard must block it
	err = svc.triggerAutoTopup(ctx, s.wallet, ongoingBalance)
	s.NoError(err)

	invoices, err = stores.InvoiceRepo.List(ctx, filter)
	s.NoError(err)
	s.Len(invoices, 1, "second call while invoice is pending must not create a new invoice")
}

// TestTriggerAutoTopup_AllowsNewInvoiceAfterPayment — once the pending invoice is paid,
// triggerAutoTopup must create a new invoice when balance is still below threshold.
func (s *WalletAutoTopupInvoiceSuite) TestTriggerAutoTopup_AllowsNewInvoiceAfterPayment() {
	ctx := s.GetContext()
	stores := s.GetStores()
	svc := s.svc()

	ongoingBalance := decimal.NewFromInt(3) // below threshold of 5

	// Create the first invoice (simulates first triggerAutoTopup call)
	err := svc.triggerAutoTopup(ctx, s.wallet, ongoingBalance)
	s.NoError(err)

	filter := types.NewNoLimitInvoiceFilter()
	filter.CustomerID = s.customer.ID
	filter.BillingReason = types.InvoiceBillingReasonWalletAutoTopup
	filter.SkipLineItems = true
	invoices, err := stores.InvoiceRepo.List(ctx, filter)
	s.NoError(err)
	s.Require().Len(invoices, 1)

	// Simulate payment: mark invoice as SUCCEEDED
	invoices[0].PaymentStatus = types.PaymentStatusSucceeded
	s.NoError(stores.InvoiceRepo.Update(ctx, invoices[0]))

	// Third call — previous invoice is paid, balance still below threshold → new invoice
	err = svc.triggerAutoTopup(ctx, s.wallet, ongoingBalance)
	s.NoError(err)

	// Filter for all WALLET_AUTO_TOPUP invoices — expect 2 now
	allFilter := types.NewNoLimitInvoiceFilter()
	allFilter.CustomerID = s.customer.ID
	allFilter.BillingReason = types.InvoiceBillingReasonWalletAutoTopup
	allFilter.SkipLineItems = true
	// Clear payment status filter to see all invoices
	allInvoices, err := stores.InvoiceRepo.List(ctx, allFilter)
	s.NoError(err)
	s.Len(allInvoices, 2, "should create a new invoice after the previous one is paid")
}
```

- [ ] **Step 2: Run the tests — expect failures (red)**

```bash
go test -v -race ./internal/service/... -run TestWalletAutoTopupInvoice 2>&1 | tail -30
```
Expected: compilation errors or test failures because the new methods/fields don't exist yet. If the previous tasks are complete, expect test failures with meaningful error messages, not compilation errors.

- [ ] **Step 3: Run tests — expect green after all tasks are complete**

```bash
go test -v -race ./internal/service/... -run TestWalletAutoTopupInvoice
```
Expected output (all PASS):
```
--- PASS: TestWalletAutoTopupInvoice/TestHasPendingAutoTopupInvoice_NoneExist
--- PASS: TestWalletAutoTopupInvoice/TestHasPendingAutoTopupInvoice_PendingExists
--- PASS: TestWalletAutoTopupInvoice/TestHasPendingAutoTopupInvoice_PaidDoesNotBlock
--- PASS: TestWalletAutoTopupInvoice/TestTriggerAutoTopup_GuardPreventsSecondInvoice
--- PASS: TestWalletAutoTopupInvoice/TestTriggerAutoTopup_AllowsNewInvoiceAfterPayment
PASS
```

- [ ] **Step 4: Run the full wallet test suite to confirm no regressions**

```bash
go test -v -race ./internal/service/... -run "TestWalletService|TestWalletAutoTopupInvoice" 2>&1 | tail -20
```
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/service/wallet_test.go
git commit -m "test: add WalletAutoTopupInvoiceSuite for guard and re-trigger behaviour"
```

---

### Task 7: Full regression check and vet

- [ ] **Step 1: Vet all changed packages**

```bash
go vet ./internal/types/... ./internal/api/dto/... ./internal/repository/ent/... ./internal/testutil/... ./internal/service/...
```
Expected: no output (no errors).

- [ ] **Step 2: Run the full service test suite**

```bash
go test -race ./internal/service/... 2>&1 | tail -20
```
Expected: `ok  github.com/flexprice/flexprice/internal/service`.

- [ ] **Step 3: Commit if any last fixes were needed**

```bash
git add -A
git commit -m "fix: address any vet or test issues from full regression run"
```
(Skip this commit if nothing changed.)
