package service

import (
	"context"
	"errors"
	"testing"
	"time"

	entcustomer "github.com/flexprice/flexprice/ent/customer"
	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	core "github.com/flexprice/flexprice/internal/ee/service/billingterms"
	"github.com/flexprice/flexprice/internal/postgres"
	repository "github.com/flexprice/flexprice/internal/repository/ent"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type invoiceCollectionFixture struct {
	ctx     context.Context
	service *invoiceService
	invoice *invoice.Invoice
	items   domain.ScheduleItemRepository
	itemID  string
}

func seedBillingScheduleCustomer(t *testing.T, ctx context.Context, db postgres.IClient, id string) {
	t.Helper()
	exists, err := db.Writer(ctx).Customer.Query().Where(entcustomer.ID(id), entcustomer.TenantID(types.GetTenantID(ctx)), entcustomer.EnvironmentID(types.GetEnvironmentID(ctx))).Exist(ctx)
	require.NoError(t, err)
	if exists {
		return
	}
	_, err = db.Writer(ctx).Customer.Create().SetID(id).SetTenantID(types.GetTenantID(ctx)).SetEnvironmentID(types.GetEnvironmentID(ctx)).
		SetExternalID(id).SetName("Billing terms fixture").SetStatus(string(types.StatusPublished)).Save(ctx)
	require.NoError(t, err)
}

func newInvoiceCollectionFixture(t *testing.T, db postgres.IClient) *invoiceCollectionFixture {
	t.Helper()
	suite := &InvoiceServiceSuite{}
	suite.SetT(t)
	suite.SetupSuite()
	suite.SetupTest()
	t.Cleanup(suite.TearDownTest)
	suite.testData.now = suite.testData.now.Truncate(time.Microsecond)
	ctx := suite.GetContext()
	inv := suite.computedCycleDraft(types.GenerateUUIDWithPrefix("sub"), "usd", decimal.NewFromInt(40))
	params := suite.service.(*invoiceService).ServiceParams
	sub, err := params.SubRepo.Get(ctx, *inv.SubscriptionID)
	require.NoError(t, err)
	sub.EnvironmentID = types.GetEnvironmentID(ctx)
	sub.Timezone = "UTC"
	sub.SubscriptionType = types.SubscriptionTypeStandalone
	sub.BillingCadence = types.BILLING_CADENCE_RECURRING
	sub.BillingCycle = types.BillingCycleAnniversary
	require.NoError(t, params.SubRepo.Update(ctx, sub))
	params.DB = db
	seedBillingScheduleCustomer(t, ctx, db, sub.CustomerID)
	require.NoError(t, repository.NewSubscriptionRepository(db, params.Logger, testutil.NewInMemoryRedis()).Create(ctx, subscription.NewSubscriptionBuilder(sub).Build()))
	params.InvoiceRepo = repository.NewInvoiceRepository(db, params.Logger)
	params.InvoiceLineItemRepo = repository.NewInvoiceLineItemRepository(db, params.Logger)
	require.NoError(t, params.InvoiceRepo.CreateWithLineItems(ctx, inv))
	params.CommitmentRepo = testutil.NewInMemoryCommitmentStore()
	owner := &commitment.Commitment{ID: types.GenerateUUIDWithPrefix("comm"), SubscriptionID: sub.ID, CustomerID: sub.CustomerID,
		Name: "Annual capacity", Currency: "usd", StartDate: sub.StartDate, EndDate: sub.CurrentPeriodEnd, Timezone: "UTC", Binding: true,
		CollectionMethod: lo.ToPtr(types.CommitmentCollectionBillingTerms), TotalAmount: lo.ToPtr(decimal.NewFromInt(100)),
		CommitmentStatus: types.CommitmentStatusActive, EnvironmentID: sub.EnvironmentID, BaseModel: types.GetDefaultBaseModel(ctx),
	}
	_, err = params.CommitmentRepo.Create(ctx, owner)
	require.NoError(t, err)
	rules, items := repository.NewBillingTermRepository(db), repository.NewBillingScheduleItemRepository(db)
	store := repository.NewBillingTermsExecutionStore(db)
	resolver, err := core.NewOwnerResolver(NewCommitmentTermsOwner(params.CommitmentRepo, params.SubRepo))
	require.NoError(t, err)
	params.BillingTerms = core.NewService(rules, items, store, resolver)
	params.BillingTermsStore = store
	state, err := params.BillingTerms.Initialize(ctx, core.InitializeRequest{Owner: domain.OwnerRef{Type: types.BillingTermOwnerCommitment, ID: owner.ID}, Rows: []core.RuleInput{{Kind: types.BillingTermOnce, ValueType: types.BillingTermRemainder, BillOn: &sub.StartDate}}})
	require.NoError(t, err)
	return &invoiceCollectionFixture{ctx: ctx, service: NewInvoiceService(params).(*invoiceService), invoice: inv, items: items, itemID: state.Items()[0].ID}
}

type failCollectionInvoiceUpdate struct{ invoice.Repository }

func (r *failCollectionInvoiceUpdate) Update(context.Context, *invoice.Invoice) error {
	return errors.New("injected total persistence failure")
}

func TestInvoiceBillingCollectionPostgres(t *testing.T) {
	db := newInvoiceVoidPostgresClient(t)
	t.Run("subscription claims once and recomputes retained principal", func(t *testing.T) {
		f := newInvoiceCollectionFixture(t, db)
		var originalLineID string
		for range 2 {
			inv, skipped, err := f.service.ComputeInvoice(f.ctx, f.invoice.ID, nil)
			require.NoError(t, err)
			require.False(t, skipped)
			require.Equal(t, "140", inv.Subtotal.String())
			require.Len(t, inv.LineItems, 2)
			item, err := f.items.Get(f.ctx, f.itemID)
			require.NoError(t, err)
			require.Equal(t, types.BillingScheduleInvoiced, item.ScheduleStatus)
			require.Equal(t, inv.ID, lo.FromPtr(item.InvoiceID))
			if originalLineID != "" {
				require.Equal(t, originalLineID, lo.FromPtr(item.InvoiceLineItemID))
			}
			originalLineID = lo.FromPtr(item.InvoiceLineItemID)
			line, err := f.service.InvoiceLineItemRepo.Get(f.ctx, originalLineID)
			require.NoError(t, err)
			require.Nil(t, line.PriceID)
			require.Equal(t, "100", line.Amount.String())
		}
	})
	t.Run("line and claim roll back when totals fail", func(t *testing.T) {
		f := newInvoiceCollectionFixture(t, db)
		f.service.InvoiceRepo = &failCollectionInvoiceUpdate{f.service.InvoiceRepo}
		_, _, err := f.service.ComputeInvoice(f.ctx, f.invoice.ID, nil)
		require.ErrorContains(t, err, "injected total persistence failure")
		item, err := f.items.Get(f.ctx, f.itemID)
		require.NoError(t, err)
		require.Equal(t, types.BillingScheduleScheduled, item.ScheduleStatus)
		require.Nil(t, item.InvoiceID)
		inv, err := f.service.InvoiceRepo.Get(f.ctx, f.invoice.ID)
		require.NoError(t, err)
		require.Len(t, inv.LineItems, 1)
		require.Equal(t, "40", inv.Subtotal.String())
	})
	t.Run("paused collection waits and catches up after resume", func(t *testing.T) {
		f := newInvoiceCollectionFixture(t, db)
		sub, err := f.service.SubRepo.Get(f.ctx, *f.invoice.SubscriptionID)
		require.NoError(t, err)
		sub.SubscriptionStatus = types.SubscriptionStatusPaused
		require.NoError(t, f.service.SubRepo.Update(f.ctx, sub))
		inv, _, err := f.service.ComputeInvoice(f.ctx, f.invoice.ID, nil)
		require.NoError(t, err)
		require.Equal(t, "40", inv.Subtotal.String())
		item, err := f.items.Get(f.ctx, f.itemID)
		require.NoError(t, err)
		require.Nil(t, item.InvoiceID)
		originalDate := *item.BillOn
		sub.SubscriptionStatus = types.SubscriptionStatusActive
		require.NoError(t, f.service.SubRepo.Update(f.ctx, sub))
		inv, _, err = f.service.ComputeInvoice(f.ctx, f.invoice.ID, nil)
		require.NoError(t, err)
		require.Equal(t, "140", inv.Subtotal.String())
		item, err = f.items.Get(f.ctx, f.itemID)
		require.NoError(t, err)
		require.True(t, item.BillOn.Equal(originalDate))
	})
}

func TestInvoiceCollectionBoundary(t *testing.T) {
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	for _, reason := range []types.InvoiceBillingReason{types.InvoiceBillingReasonSubscriptionCreate, types.InvoiceBillingReasonSubscriptionTrialEnd, types.InvoiceBillingReasonSubscriptionResume, types.InvoiceBillingReasonSubscriptionCycle, types.InvoiceBillingReasonProration} {
		boundary, err := invoiceCollectionCutoff(&invoice.Invoice{BillingReason: string(reason), PeriodStart: &start, PeriodEnd: &end})
		require.NoError(t, err)
		expected := start
		if reason == types.InvoiceBillingReasonSubscriptionCycle || reason == types.InvoiceBillingReasonProration {
			expected = end
		}
		require.True(t, expected.Equal(boundary))
	}
}

type signaledCollectionStore struct {
	domain.ExecutionStore
	entered chan struct{}
}

func (s *signaledCollectionStore) LockEligibility(ctx context.Context, scopes []domain.EligibilityScope) error {
	select {
	case s.entered <- struct{}{}:
	default:
	}
	return s.ExecutionStore.LockEligibility(ctx, scopes)
}

func TestInvoiceBillingCollectionPauseRacePostgres(t *testing.T) {
	db := newInvoiceVoidPostgresClient(t)
	f := newInvoiceCollectionFixture(t, db)
	entered := make(chan struct{}, 1)
	store := &signaledCollectionStore{ExecutionStore: f.service.BillingTermsStore, entered: entered}
	resolver, err := core.NewOwnerResolver(NewCommitmentTermsOwner(f.service.CommitmentRepo, f.service.SubRepo))
	require.NoError(t, err)
	f.service.BillingTerms = core.NewService(repository.NewBillingTermRepository(db), f.items, store, resolver)
	finished := make(chan error, 1)
	require.NoError(t, db.WithTx(f.ctx, func(tx context.Context) error {
		scope := domain.EligibilityScope{Type: "subscription", ID: *f.invoice.SubscriptionID}
		if err := f.service.BillingTermsStore.LockEligibility(tx, []domain.EligibilityScope{scope}); err != nil {
			return err
		}
		go func() { _, _, err := f.service.ComputeInvoice(f.ctx, f.invoice.ID, nil); finished <- err }()
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			return errors.New("invoice never reached eligibility guard")
		}
		sub, err := f.service.SubRepo.Get(f.ctx, *f.invoice.SubscriptionID)
		if err != nil {
			return err
		}
		sub.SubscriptionStatus = types.SubscriptionStatusPaused
		return f.service.SubRepo.Update(f.ctx, sub)
	}))
	select {
	case err := <-finished:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("collection blocked after pause committed")
	}
	item, err := f.items.Get(f.ctx, f.itemID)
	require.NoError(t, err)
	require.Equal(t, types.BillingScheduleScheduled, item.ScheduleStatus)
	require.Nil(t, item.InvoiceID)
}

func TestInvoiceBillingCollectionCompetingInvoicesPostgres(t *testing.T) {
	db := newInvoiceVoidPostgresClient(t)
	f := newInvoiceCollectionFixture(t, db)
	otherID := types.GenerateUUIDWithPrefix("inv")
	other := invoice.NewInvoiceBuilder(f.invoice).WithID(otherID).WithIdempotencyKey(&otherID).WithLineItems(nil).Build()
	require.NoError(t, f.service.InvoiceRepo.CreateWithLineItems(f.ctx, other))
	completed := make(chan error, 2)
	for _, id := range []string{f.invoice.ID, otherID} {
		go func(id string) { _, _, err := f.service.ComputeInvoice(f.ctx, id, nil); completed <- err }(id)
	}
	for range 2 {
		select {
		case err := <-completed:
			require.NoError(t, err)
		case <-time.After(10 * time.Second):
			t.Fatal("concurrent collection did not finish")
		}
	}
	count := 0
	for _, id := range []string{f.invoice.ID, otherID} {
		inv, err := f.service.InvoiceRepo.Get(f.ctx, id)
		require.NoError(t, err)
		for _, line := range inv.LineItems {
			if line.BillingScheduleItemID != nil {
				count++
				require.Equal(t, f.itemID, *line.BillingScheduleItemID)
			}
		}
	}
	require.Equal(t, 1, count)
}

func TestInvoiceTrialEndBillingCollectionPostgres(t *testing.T) {
	db := newInvoiceVoidPostgresClient(t)
	for _, authorized := range []bool{false, true} {
		name := "ordinary incomplete remains deferred"
		if authorized {
			name = "trusted trial end includes due payments"
		}
		t.Run(name, func(t *testing.T) {
			f := newInvoiceCollectionFixture(t, db)
			sub, err := f.service.SubRepo.Get(f.ctx, *f.invoice.SubscriptionID)
			require.NoError(t, err)
			trialEnd := sub.StartDate.AddDate(0, 0, 10)
			require.True(t, trialEnd.Before(time.Now()))
			sub.TrialStart, sub.TrialEnd = lo.ToPtr(sub.StartDate), &trialEnd
			sub.CurrentPeriodStart = trialEnd
			sub.SubscriptionStatus = types.SubscriptionStatusIncomplete
			require.NoError(t, f.service.SubRepo.Update(f.ctx, sub))
			f.invoice.PeriodStart = &trialEnd
			f.invoice.BillingReason = string(types.InvoiceBillingReasonSubscriptionTrialEnd)
			_, err = db.Writer(f.ctx).ExecContext(f.ctx,
				"UPDATE invoices SET period_start = $1, billing_reason = $2 WHERE id = $3 AND tenant_id = $4 AND environment_id = $5",
				trialEnd, f.invoice.BillingReason, f.invoice.ID, f.invoice.TenantID, f.invoice.EnvironmentID)
			require.NoError(t, err)
			ctx := f.ctx
			if authorized {
				ctx = bindTrialEndCollection(withTrialEndCollection(ctx, sub, time.Now().UTC()), f.invoice)
			}
			inv, _, err := f.service.ComputeInvoice(ctx, f.invoice.ID, nil)
			require.NoError(t, err)
			item, err := f.items.Get(f.ctx, f.itemID)
			require.NoError(t, err)
			if authorized {
				require.Equal(t, f.invoice.ID, lo.FromPtr(item.InvoiceID))
				require.Equal(t, "100", inv.Subtotal.String())
			} else {
				require.Nil(t, item.InvoiceID)
				require.Equal(t, "0", inv.Subtotal.String())
			}
		})
	}
}

func TestInvoiceBillingCollectionRejectsCrossCurrencyPostgres(t *testing.T) {
	db := newInvoiceVoidPostgresClient(t)
	f := newInvoiceCollectionFixture(t, db)
	recipient, err := f.service.CustomerRepo.Get(f.ctx, f.invoice.CustomerID)
	require.NoError(t, err)
	recipient.BillingCurrency = lo.ToPtr("eur")
	require.NoError(t, f.service.CustomerRepo.Update(f.ctx, recipient))
	_, _, err = f.service.ComputeInvoice(f.ctx, f.invoice.ID, nil)
	require.ErrorContains(t, err, "billing currency to match")
	item, err := f.items.Get(f.ctx, f.itemID)
	require.NoError(t, err)
	require.Nil(t, item.InvoiceID)
	inv, err := f.service.InvoiceRepo.Get(f.ctx, f.invoice.ID)
	require.NoError(t, err)
	require.Len(t, inv.LineItems, 1)
	require.Equal(t, "40", inv.Subtotal.String())
}
