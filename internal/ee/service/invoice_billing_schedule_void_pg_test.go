package service

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/api/dto"
	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	core "github.com/flexprice/flexprice/internal/ee/service/billingterms"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	repository "github.com/flexprice/flexprice/internal/repository/ent"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/lib/pq"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type invoiceVoidPostgresOwner struct{ snapshot domain.OwnerSnapshot }

func (o *invoiceVoidPostgresOwner) Type() types.BillingTermOwnerType { return o.snapshot.Owner.Type }
func (o *invoiceVoidPostgresOwner) Snapshot(context.Context, domain.OwnerRef) (*domain.OwnerSnapshot, error) {
	copy := o.snapshot
	return &copy, nil
}

type invoiceVoidPostgresFixture struct {
	ctx       context.Context
	params    ServiceParams
	rules     domain.RuleRepository
	items     domain.ScheduleItemRepository
	store     domain.ExecutionStore
	resolver  core.OwnerResolver
	terms     core.Service
	owner     domain.OwnerSnapshot
	invoice   *invoice.Invoice
	item      *domain.ScheduleItem
	line      *invoice.InvoiceLineItem
	wallet    *wallet.Wallet
	publisher *testutil.InMemoryWebhookPublisher
}

func newInvoiceVoidPostgresClient(t *testing.T) postgres.IClient {
	t.Helper()
	dsn := os.Getenv("FLEXPRICE_BILLING_TERMS_TEST_DSN")
	if dsn == "" {
		t.Skip("set FLEXPRICE_BILLING_TERMS_TEST_DSN to run isolated PostgreSQL integration tests")
	}
	parsed, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Contains(t, []string{"postgres", "postgresql"}, parsed.Scheme)
	admin, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = admin.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	require.NoError(t, admin.PingContext(ctx))
	schema := strings.ToLower(strings.ReplaceAll(types.GenerateUUIDWithPrefix("void_test"), "-", ""))
	_, err = admin.ExecContext(ctx, "CREATE SCHEMA "+pq.QuoteIdentifier(schema))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := admin.ExecContext(context.Background(), "DROP SCHEMA "+pq.QuoteIdentifier(schema)+" CASCADE")
		require.NoError(t, err)
	})
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	db, err := sql.Open("postgres", parsed.String())
	require.NoError(t, err)
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	require.NoError(t, client.Schema.Create(ctx))
	return postgres.NewClient(&postgres.EntClients{Writer: client, Reader: client}, logger.NewNoopLogger(), nil)
}

func newInvoiceVoidPostgresFixture(t *testing.T, db postgres.IClient) *invoiceVoidPostgresFixture {
	t.Helper()
	suite := &InvoiceVoidRecalculateSuite{}
	suite.SetT(t)
	suite.SetupSuite()
	suite.SetupTest()
	t.Cleanup(suite.TearDownTest)
	ctx := suite.GetContext()
	params := suite.service.(*invoiceService).ServiceParams
	customer := *suite.testData.customer
	customer.ID = types.GenerateUUIDWithPrefix("customer")
	customer.ExternalID = customer.ID
	require.NoError(t, params.CustomerRepo.Create(ctx, &customer))
	params.DB = db
	params.InvoiceRepo = repository.NewInvoiceRepository(db, params.Logger)
	params.InvoiceLineItemRepo = repository.NewInvoiceLineItemRepository(db, params.Logger)
	params.RefundRepo = repository.NewRefundRepository(db, params.Logger)
	cache := testutil.NewInMemoryRedis()
	params.WalletRepo = repository.NewWalletRepository(db, params.Logger, cache)
	params.CreditGrantRepo = repository.NewCreditGrantRepository(db, params.Logger, cache)
	params.CreditGrantApplicationRepo = repository.NewCreditGrantApplicationRepository(db, params.Logger, cache)
	rules, items := repository.NewBillingTermRepository(db), repository.NewBillingScheduleItemRepository(db)
	store := repository.NewBillingTermsExecutionStore(db)
	start := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	owner := domain.OwnerSnapshot{
		Owner: domain.OwnerRef{Type: "standalone_fee", ID: types.GenerateUUIDWithPrefix("fee")}, TenantID: types.GetTenantID(ctx), EnvironmentID: types.GetEnvironmentID(ctx),
		OriginalTotal: decimal.NewFromInt(100), CollectionTarget: decimal.NewFromInt(100), Currency: "usd", Timezone: "UTC", StartDate: start, EndDate: start.AddDate(1, 0, 0),
		CustomerID: customer.ID, Delivery: types.BillingTermSeparateInvoice, CanCollect: true, CanEdit: true,
	}
	resolver, err := core.NewOwnerResolver(&invoiceVoidPostgresOwner{snapshot: owner})
	require.NoError(t, err)
	terms := core.NewService(rules, items, store, resolver)
	params.BillingTerms = terms
	f := &invoiceVoidPostgresFixture{ctx: ctx, params: params, rules: rules, items: items, store: store, resolver: resolver, terms: terms, owner: owner, publisher: suite.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher)}
	state, err := terms.Initialize(ctx, core.InitializeRequest{Owner: owner.Owner, Rows: []core.RuleInput{{Kind: types.BillingTermOnce, ValueType: types.BillingTermRemainder, BillOn: &start}}})
	require.NoError(t, err)
	f.item = state.Items()[0]
	f.invoice = invoice.NewInvoiceBuilder(&invoice.Invoice{
		ID: types.GenerateUUIDWithPrefix("inv"), CustomerID: owner.CustomerID, InvoiceType: types.InvoiceTypeOneOff,
		InvoiceStatus: types.InvoiceStatusFinalized, PaymentStatus: types.PaymentStatusSucceeded, Currency: "usd",
		Subtotal: f.item.Amount, Total: f.item.Amount, AmountDue: f.item.Amount, AmountPaid: f.item.Amount,
		FinalizedAt: &start, EnvironmentID: owner.EnvironmentID, BaseModel: types.GetDefaultBaseModel(ctx),
	}).Build()
	f.line = invoice.NewInvoiceLineItemBuilder(&invoice.InvoiceLineItem{
		ID: types.GenerateUUIDWithPrefix("invli"), InvoiceID: f.invoice.ID, CustomerID: owner.CustomerID,
		Amount: f.item.Amount, Quantity: decimal.NewFromInt(1), Currency: "usd", PriceType: lo.ToPtr(string(types.PRICE_TYPE_FIXED)),
		PeriodStart: &f.item.ServicePeriodStart, PeriodEnd: &f.item.ServicePeriodEnd,
		EnvironmentID: owner.EnvironmentID, BaseModel: types.GetDefaultBaseModel(ctx),
	}).WithBillingScheduleItemID(&f.item.ID).Build()
	f.invoice = invoice.NewInvoiceBuilder(f.invoice).WithIdempotencyKey(&f.invoice.ID).WithLineItems([]*invoice.InvoiceLineItem{f.line}).Build()
	require.NoError(t, db.WithTx(ctx, func(tx context.Context) error {
		if err := store.LockOwner(tx, owner.Owner); err != nil {
			return err
		}
		if err := params.InvoiceRepo.CreateWithLineItems(tx, f.invoice); err != nil {
			return err
		}
		claimed, err := items.Claim(tx, f.item.ID, f.invoice.ID, f.line.ID)
		if err != nil {
			return err
		}
		if !claimed {
			return errors.New("fixture source was not claimed")
		}
		if err := items.Archive(tx, owner.Owner); err != nil {
			return err
		}
		return rules.Archive(tx, owner.Owner)
	}))
	f.wallet = &wallet.Wallet{
		ID: types.GenerateUUIDWithPrefix("wallet"), CustomerID: owner.CustomerID, Currency: "usd", Name: "Void refund wallet",
		WalletType: types.WalletTypePrePaid, WalletStatus: types.WalletStatusActive, ConversionRate: decimal.NewFromInt(1), TopupConversionRate: decimal.NewFromInt(1),
		EnvironmentID: owner.EnvironmentID, BaseModel: types.GetDefaultBaseModel(ctx),
	}
	require.NoError(t, params.WalletRepo.CreateWallet(ctx, f.wallet))
	f.publisher.Reset()
	return f
}

type failInvoiceVoidCancellation struct {
	domain.ScheduleItemRepository
	fixture      *invoiceVoidPostgresFixture
	failure      error
	callbackSeen bool
}

func (r *failInvoiceVoidCancellation) Cancel(ctx context.Context, _ string, _ types.BillingScheduleCancellationReason) (bool, error) {
	f := r.fixture
	inv, err := f.params.InvoiceRepo.Get(ctx, f.invoice.ID)
	if err != nil {
		return false, err
	}
	refunds, err := f.params.RefundRepo.ListByInvoice(ctx, inv.ID)
	if err != nil {
		return false, err
	}
	w, err := f.params.WalletRepo.GetWalletByID(ctx, f.wallet.ID)
	if err != nil {
		return false, err
	}
	r.callbackSeen = inv.InvoiceStatus == types.InvoiceStatusVoided && inv.RefundedAmount.Equal(f.item.Amount) && len(refunds) == 1 && refunds[0].RefundStatus == types.RefundStatusSucceeded && w.CreditBalance.Equal(f.item.Amount)
	return false, r.failure
}

func TestInvoiceBillingScheduleVoidPostgres(t *testing.T) {
	db := newInvoiceVoidPostgresClient(t)
	t.Run("archived source cancels and releases principal once", func(t *testing.T) {
		f := newInvoiceVoidPostgresFixture(t, db)
		service := NewInvoiceService(f.params)
		got, err := service.VoidInvoice(f.ctx, f.invoice.ID, dto.InvoiceVoidRequest{})
		require.NoError(t, err)
		require.Equal(t, types.InvoiceStatusVoided, got.InvoiceStatus)
		require.Equal(t, types.PaymentStatusRefunded, got.PaymentStatus)
		require.True(t, got.RefundedAmount.Equal(f.item.Amount))
		item, err := f.items.Get(f.ctx, f.item.ID)
		require.NoError(t, err)
		require.Equal(t, types.StatusArchived, item.Status)
		require.Equal(t, types.BillingScheduleCanceled, item.ScheduleStatus)
		require.Equal(t, types.BillingScheduleInvoiceVoided, *item.CancellationReason)
		require.Equal(t, f.invoice.ID, *item.InvoiceID)
		require.Equal(t, f.line.ID, *item.InvoiceLineItemID)
		require.Equal(t, *f.item.BillingTermID, *item.BillingTermID)
		require.True(t, item.Amount.Equal(f.item.Amount))
		state, err := f.terms.Get(f.ctx, f.owner.Owner)
		require.NoError(t, err)
		require.True(t, state.Summary().AssignedAmount.IsZero())
		require.True(t, state.Summary().UnallocatedAmount.Equal(f.item.Amount))
		require.True(t, state.Summary().RequiresReplan)
		w, err := f.params.WalletRepo.GetWalletByID(f.ctx, f.wallet.ID)
		require.NoError(t, err)
		require.True(t, w.CreditBalance.Equal(f.item.Amount))
		eventCount := len(f.publisher.Events())
		require.Positive(t, eventCount)
		_, err = service.VoidInvoice(f.ctx, f.invoice.ID, dto.InvoiceVoidRequest{})
		require.NoError(t, err)
		require.Len(t, f.publisher.Events(), eventCount)
		refunds, err := f.params.RefundRepo.ListByInvoice(f.ctx, f.invoice.ID)
		require.NoError(t, err)
		require.Len(t, refunds, 1)
	})
	t.Run("cancellation failure rolls back real invoice refund and wallet", func(t *testing.T) {
		f := newInvoiceVoidPostgresFixture(t, db)
		failure := errors.New("injected schedule cancellation failure")
		failing := &failInvoiceVoidCancellation{ScheduleItemRepository: f.items, fixture: f, failure: failure}
		params := f.params
		params.BillingTerms = core.NewService(f.rules, failing, f.store, f.resolver)
		_, err := NewInvoiceService(params).VoidInvoice(f.ctx, f.invoice.ID, dto.InvoiceVoidRequest{})
		require.ErrorIs(t, err, failure)
		require.True(t, failing.callbackSeen, "actual refund and wallet updates must precede the injected cancellation failure")
		stored, err := f.params.InvoiceRepo.Get(f.ctx, f.invoice.ID)
		require.NoError(t, err)
		require.Equal(t, types.InvoiceStatusFinalized, stored.InvoiceStatus)
		require.Equal(t, types.PaymentStatusSucceeded, stored.PaymentStatus)
		require.Nil(t, stored.VoidedAt)
		require.True(t, stored.RefundedAmount.IsZero())
		item, err := f.items.Get(f.ctx, f.item.ID)
		require.NoError(t, err)
		require.Equal(t, types.BillingScheduleInvoiced, item.ScheduleStatus)
		require.Equal(t, f.invoice.ID, *item.InvoiceID)
		require.Equal(t, f.line.ID, *item.InvoiceLineItemID)
		require.Nil(t, item.CancellationReason)
		refunds, err := f.params.RefundRepo.ListByInvoice(f.ctx, f.invoice.ID)
		require.NoError(t, err)
		require.Empty(t, refunds)
		w, err := f.params.WalletRepo.GetWalletByID(types.WithForceWriter(f.ctx), f.wallet.ID)
		require.NoError(t, err)
		require.True(t, w.CreditBalance.IsZero())
		filter := types.NewNoLimitWalletTransactionFilter()
		filter.WalletID = &f.wallet.ID
		txns, err := f.params.WalletRepo.ListAllWalletTransactions(f.ctx, filter)
		require.NoError(t, err)
		require.Empty(t, txns)
		require.Empty(t, f.publisher.Events())
		state, err := f.terms.Get(f.ctx, f.owner.Owner)
		require.NoError(t, err)
		require.True(t, state.Summary().AssignedAmount.Equal(f.item.Amount))
		require.False(t, state.Summary().RequiresReplan)
	})
	t.Run("outer rollback discards a successful nested void and its notifications", func(t *testing.T) {
		f := newInvoiceVoidPostgresFixture(t, db)
		failure := errors.New("outer operation failed")
		err := db.WithTx(f.ctx, func(tx context.Context) error {
			got, err := NewInvoiceService(f.params).VoidInvoice(tx, f.invoice.ID, dto.InvoiceVoidRequest{})
			if err != nil {
				return err
			}
			require.Equal(t, types.InvoiceStatusVoided, got.InvoiceStatus)
			item, err := f.items.Get(tx, f.item.ID)
			if err != nil {
				return err
			}
			require.Equal(t, types.BillingScheduleCanceled, item.ScheduleStatus)
			require.Empty(t, f.publisher.Events(), "nested success cannot announce an uncommitted void or refund")
			return failure
		})
		require.ErrorIs(t, err, failure)
		stored, err := f.params.InvoiceRepo.Get(f.ctx, f.invoice.ID)
		require.NoError(t, err)
		require.Equal(t, types.InvoiceStatusFinalized, stored.InvoiceStatus)
		require.True(t, stored.RefundedAmount.IsZero())
		item, err := f.items.Get(f.ctx, f.item.ID)
		require.NoError(t, err)
		require.Equal(t, types.BillingScheduleInvoiced, item.ScheduleStatus)
		refunds, err := f.params.RefundRepo.ListByInvoice(f.ctx, f.invoice.ID)
		require.NoError(t, err)
		require.Empty(t, refunds)
		w, err := f.params.WalletRepo.GetWalletByID(types.WithForceWriter(f.ctx), f.wallet.ID)
		require.NoError(t, err)
		require.True(t, w.CreditBalance.IsZero())
		require.Empty(t, f.publisher.Events())
	})
}
