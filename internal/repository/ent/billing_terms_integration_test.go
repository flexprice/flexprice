package ent

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/flexprice/flexprice/ent"
	itemdb "github.com/flexprice/flexprice/ent/billingscheduleitem"
	ruledb "github.com/flexprice/flexprice/ent/billingterm"
	customerdb "github.com/flexprice/flexprice/ent/customer"
	invoicedb "github.com/flexprice/flexprice/ent/invoice"
	linedb "github.com/flexprice/flexprice/ent/invoicelineitem"
	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	domaininvoice "github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/lib/pq"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type billingPostgresFixture struct {
	client postgres.IClient
	rules  domain.RuleRepository
	items  domain.ScheduleItemRepository
	lines  domaininvoice.LineItemRepository
	invs   domaininvoice.Repository
}

func newBillingPostgresFixture(t *testing.T) *billingPostgresFixture {
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, admin.PingContext(ctx))
	schema := strings.ToLower(strings.ReplaceAll(types.GenerateUUIDWithPrefix("billing_test"), "-", ""))
	_, err = admin.ExecContext(ctx, "CREATE SCHEMA "+pq.QuoteIdentifier(schema))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, cleanupErr := admin.ExecContext(context.Background(), "DROP SCHEMA "+pq.QuoteIdentifier(schema)+" CASCADE")
		require.NoError(t, cleanupErr)
	})
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	db, err := sql.Open("postgres", parsed.String())
	require.NoError(t, err)
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	require.NoError(t, client.Schema.Create(ctx))
	log := logger.NewNoopLogger()
	pg := postgres.NewClient(&postgres.EntClients{Writer: client, Reader: client}, log, nil)
	return &billingPostgresFixture{
		client: pg, rules: NewBillingTermRepository(pg), items: NewBillingScheduleItemRepository(pg),
		lines: NewInvoiceLineItemRepository(pg, log), invs: NewInvoiceRepository(pg, log),
	}
}

func billingTestContext(tenant, environment string) context.Context {
	ctx := types.SetTenantID(context.Background(), tenant)
	return types.SetEnvironmentID(ctx, environment)
}

func billingTestCustomerID(ctx context.Context) string {
	tenant, environment := types.GetTenantID(ctx), types.GetEnvironmentID(ctx)
	digest := sha256.Sum256([]byte(strconv.Itoa(len(tenant)) + ":" + tenant + environment))
	return "cust_" + hex.EncodeToString(digest[:20])
}

func (f *billingPostgresFixture) seedCustomer(t *testing.T, ctx context.Context) {
	t.Helper()
	id := billingTestCustomerID(ctx)
	exists, err := f.client.Writer(ctx).Customer.Query().Where(customerdb.ID(id), customerdb.TenantID(types.GetTenantID(ctx)), customerdb.EnvironmentID(types.GetEnvironmentID(ctx))).Exist(ctx)
	require.NoError(t, err)
	if exists {
		return
	}
	_, err = f.client.Writer(ctx).Customer.Create().SetID(id).SetTenantID(types.GetTenantID(ctx)).SetEnvironmentID(types.GetEnvironmentID(ctx)).
		SetExternalID(id).SetName("Billing terms fixture").SetStatus(string(types.StatusPublished)).Save(ctx)
	require.NoError(t, err)
}

func newBillingTestRule(ctx context.Context, owner domain.OwnerRef) *domain.Rule {
	amount := decimal.NewFromInt(100)
	due := time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC)
	return domain.NewRuleBuilder(&domain.Rule{
		ID: types.GenerateUUIDWithPrefix("bterm"), OwnerType: owner.Type, OwnerID: owner.ID,
		Kind: types.BillingTermOnce, ValueType: types.BillingTermFixed, Value: &amount, BillOn: &due,
		Description: "Contract payment", Metadata: types.Metadata{"source": "test"},
		EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx),
	}).Build()
}

func newBillingTestItem(ctx context.Context, rule *domain.Rule) *domain.ScheduleItem {
	return domain.NewScheduleItemBuilder(&domain.ScheduleItem{
		ID: types.GenerateUUIDWithPrefix("bsitem"), OwnerType: rule.OwnerType, OwnerID: rule.OwnerID,
		BillingTermID: &rule.ID, CustomerID: billingTestCustomerID(ctx), Currency: "usd", Amount: *rule.Value,
		Kind: types.BillingScheduleCharge, BillTrigger: types.BillingScheduleOnDate, BillOn: rule.BillOn,
		ServicePeriodStart: *rule.BillOn, ServicePeriodEnd: rule.BillOn.AddDate(1, 0, 0),
		Description: rule.Description, Delivery: types.BillingTermSeparateInvoice,
		ScheduleStatus: types.BillingScheduleScheduled, Metadata: types.Metadata{"source": "test"},
		EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx),
	}).Build()
}

func (f *billingPostgresFixture) createSource(t *testing.T, ctx context.Context) (*domain.Rule, *domain.ScheduleItem) {
	t.Helper()
	f.seedCustomer(t, ctx)
	owner := domain.OwnerRef{Type: "future_fee", ID: types.GenerateUUIDWithPrefix("owner")}
	rule := newBillingTestRule(ctx, owner)
	require.NoError(t, f.rules.Create(ctx, rule))
	item := newBillingTestItem(ctx, rule)
	require.NoError(t, f.items.Create(ctx, item))
	return rule, item
}

func newBillingTestLine(ctx context.Context, inv *domaininvoice.Invoice, item *domain.ScheduleItem) *domaininvoice.InvoiceLineItem {
	var commitmentID *string
	if item.OwnerType == types.BillingTermOwnerCommitment {
		commitmentID = &item.OwnerID
	}
	return domaininvoice.NewInvoiceLineItemBuilder(&domaininvoice.InvoiceLineItem{
		ID: types.GenerateUUIDWithPrefix("invli"), InvoiceID: inv.ID, CustomerID: inv.CustomerID,
		SubscriptionID: item.SubscriptionID, CommitmentID: commitmentID,
		Amount: item.Amount, Quantity: decimal.NewFromInt(1), Currency: item.Currency,
		PeriodStart: &item.ServicePeriodStart, PeriodEnd: &item.ServicePeriodEnd,
		EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx),
	}).WithBillingScheduleItemID(&item.ID).Build()
}

func newBillingTestInvoice(ctx context.Context) *domaininvoice.Invoice {
	inv := newTestInvoice(ctx)
	inv.CustomerID = billingTestCustomerID(ctx)
	return domaininvoice.NewInvoiceBuilder(inv).WithIdempotencyKey(&inv.ID).Build()
}

func TestBillingTermsPostgres(t *testing.T) {
	f := newBillingPostgresFixture(t)
	ctx := billingTestContext("billing_tenant_a", "billing_env_a")

	t.Run("scope_is_required_and_sources_cannot_cross_owner_or_environment", func(t *testing.T) {
		rule, item := f.createSource(t, ctx)
		var typedNil *struct{ context.Context }
		for _, bad := range []context.Context{nil, typedNil, context.Background(), billingTestContext("billing_tenant_a", ""), billingTestContext("", "billing_env_a")} {
			_, err := f.rules.Get(bad, rule.ID)
			require.Error(t, err)
			_, err = f.items.Get(bad, item.ID)
			require.Error(t, err)
			require.Error(t, f.rules.Archive(bad, domain.OwnerRef{Type: rule.OwnerType, ID: rule.OwnerID}))
			_, err = f.items.ResolveBillOn(bad, item.ID, time.Now())
			require.Error(t, err)
		}
		for _, other := range []context.Context{billingTestContext("billing_tenant_b", "billing_env_a"), billingTestContext("billing_tenant_a", "billing_env_b")} {
			_, err := f.rules.Get(other, rule.ID)
			require.Error(t, err)
			_, err = f.items.Get(other, item.ID)
			require.Error(t, err)
			rows, err := f.items.List(other, &domain.ScheduleItemFilter{IDs: []string{item.ID}, IncludeArchived: true})
			require.NoError(t, err)
			require.Empty(t, rows)
			cross := domain.NewScheduleItemBuilder(item).WithID(types.GenerateUUIDWithPrefix("bsitem")).Build()
			cross.TenantID, cross.EnvironmentID = types.GetTenantID(other), types.GetEnvironmentID(other)
			require.Error(t, f.items.Create(other, cross))
			require.Error(t, f.rules.Create(other, rule))
			owner := domain.OwnerRef{Type: rule.OwnerType, ID: rule.OwnerID}
			require.NoError(t, f.rules.Archive(other, owner))
			require.NoError(t, f.items.Archive(other, owner))
		}
		wrongOwner := domain.NewScheduleItemBuilder(item).WithID(types.GenerateUUIDWithPrefix("bsitem")).Build()
		wrongOwner.OwnerID = "different_owner"
		require.Error(t, f.items.Create(ctx, wrongOwner))
		got, err := f.rules.Get(ctx, rule.ID)
		require.NoError(t, err)
		require.Equal(t, types.StatusPublished, got.Status)
		require.Equal(t, rule.Metadata, got.Metadata)
	})

	t.Run("skipped_invoices_cannot_claim_principal", func(t *testing.T) {
		_, item := f.createSource(t, ctx)
		inv := domaininvoice.NewInvoiceBuilder(newBillingTestInvoice(ctx)).WithInvoiceStatus(types.InvoiceStatusSkipped).Build()
		line := newBillingTestLine(ctx, inv, item)
		require.NoError(t, f.invs.Create(ctx, inv))
		require.NoError(t, f.lines.Create(ctx, line))
		claimed, err := f.items.Claim(ctx, item.ID, inv.ID, line.ID)
		require.Error(t, err)
		require.False(t, claimed)
		unassigned, err := f.items.Get(ctx, item.ID)
		require.NoError(t, err)
		require.Equal(t, types.BillingScheduleScheduled, unassigned.ScheduleStatus)
		require.Nil(t, unassigned.InvoiceID)
		require.Nil(t, unassigned.InvoiceLineItemID)
	})

	t.Run("archive_retains_history_and_blocks_claims", func(t *testing.T) {
		rule, item := f.createSource(t, ctx)
		owner := domain.OwnerRef{Type: rule.OwnerType, ID: rule.OwnerID}
		inv := newBillingTestInvoice(ctx)
		line := newBillingTestLine(ctx, inv, item)
		require.NoError(t, f.invs.Create(ctx, inv))
		require.NoError(t, f.lines.Create(ctx, line))
		require.NoError(t, f.rules.Archive(ctx, owner))
		claimed, err := f.items.Claim(ctx, item.ID, inv.ID, line.ID)
		require.NoError(t, err)
		require.False(t, claimed)
		require.NoError(t, f.items.Archive(ctx, owner))
		current, err := f.items.List(ctx, &domain.ScheduleItemFilter{Owner: &owner})
		require.NoError(t, err)
		require.Empty(t, current)
		history, err := f.items.List(ctx, &domain.ScheduleItemFilter{Owner: &owner, IncludeArchived: true})
		require.NoError(t, err)
		require.Len(t, history, 1)
		require.Equal(t, types.StatusArchived, history[0].Status)
		gotRule, err := f.rules.Get(ctx, rule.ID)
		require.NoError(t, err)
		require.Equal(t, types.StatusArchived, gotRule.Status)
		gotLine, err := f.lines.Get(ctx, line.ID)
		require.NoError(t, err)
		require.Equal(t, item.ID, *gotLine.BillingScheduleItemID)
		_, err = f.client.Writer(ctx).BillingTerm.Delete().Where(ruledb.ID(rule.ID), ruledb.TenantID(types.GetTenantID(ctx)), ruledb.EnvironmentID(types.GetEnvironmentID(ctx))).Exec(ctx)
		require.Error(t, err, "historical schedule source cannot be deleted")
		_, err = f.client.Writer(ctx).BillingScheduleItem.Delete().Where(itemdb.ID(item.ID), itemdb.TenantID(types.GetTenantID(ctx)), itemdb.EnvironmentID(types.GetEnvironmentID(ctx))).Exec(ctx)
		require.Error(t, err, "historical invoice source cannot be deleted")
	})

	t.Run("manual_resolution_and_invoice_claim_are_compare_and_set", func(t *testing.T) {
		rule, item := f.createSource(t, ctx)
		manual := domain.NewScheduleItemBuilder(item).WithID(types.GenerateUUIDWithPrefix("bsitem")).WithBillOn(nil).Build()
		manual.BillTrigger = types.BillingScheduleManual
		require.NoError(t, f.items.Create(ctx, manual))
		before, err := f.items.List(ctx, &domain.ScheduleItemFilter{IDs: []string{manual.ID}, DueBefore: rule.BillOn})
		require.NoError(t, err)
		require.Empty(t, before)
		raceBillingOperations(t, func() (bool, error) { return f.items.ResolveBillOn(ctx, manual.ID, *rule.BillOn) })
		resolved, err := f.items.Get(ctx, manual.ID)
		require.NoError(t, err)
		require.NotNil(t, resolved.BillOn)
		inv := newBillingTestInvoice(ctx)
		line := newBillingTestLine(ctx, inv, manual)
		require.NoError(t, f.invs.Create(ctx, inv))
		require.NoError(t, f.lines.Create(ctx, line))
		claimed, err := f.items.Claim(ctx, item.ID, inv.ID, line.ID)
		require.Error(t, err, "an unrelated source line cannot claim an item")
		require.False(t, claimed)
		raceBillingOperations(t, func() (bool, error) { return f.items.Claim(ctx, manual.ID, inv.ID, line.ID) })
		got, err := f.items.Get(ctx, manual.ID)
		require.NoError(t, err)
		require.Equal(t, types.BillingScheduleInvoiced, got.ScheduleStatus)
		require.Equal(t, inv.ID, *got.InvoiceID)
		require.Equal(t, line.ID, *got.InvoiceLineItemID)
	})

	t.Run("invoice_line_and_claim_roll_back_together", func(t *testing.T) {
		_, item := f.createSource(t, ctx)
		inv := newBillingTestInvoice(ctx)
		line := newBillingTestLine(ctx, inv, item)
		abort := errors.New("abort invoice creation")
		err := f.client.WithTx(ctx, func(txCtx context.Context) error {
			if err := f.invs.Create(txCtx, inv); err != nil {
				return err
			}
			if err := f.lines.Create(txCtx, line); err != nil {
				return err
			}
			claimed, err := f.items.Claim(txCtx, item.ID, inv.ID, line.ID)
			if err != nil {
				return err
			}
			require.True(t, claimed)
			return abort
		})
		require.ErrorIs(t, err, abort)
		got, err := f.items.Get(ctx, item.ID)
		require.NoError(t, err)
		require.Equal(t, types.BillingScheduleScheduled, got.ScheduleStatus)
		require.Nil(t, got.InvoiceID)
		_, err = f.invs.Get(ctx, inv.ID)
		require.Error(t, err)
		_, err = f.lines.Get(ctx, line.ID)
		require.Error(t, err)
		err = f.client.WithTx(ctx, func(txCtx context.Context) error {
			if err := f.invs.Create(txCtx, inv); err != nil {
				return err
			}
			if err := f.lines.CreateBulk(txCtx, []*domaininvoice.InvoiceLineItem{line}); err != nil {
				return err
			}
			claimed, err := f.items.Claim(txCtx, item.ID, inv.ID, line.ID)
			if err != nil {
				return err
			}
			require.True(t, claimed)
			return nil
		})
		require.NoError(t, err)
		gotLine, err := f.lines.Get(ctx, line.ID)
		require.NoError(t, err)
		require.Equal(t, item.ID, *gotLine.BillingScheduleItemID)
		require.Nil(t, gotLine.PriceID, "scheduled principal does not require a synthetic Price")
	})

	t.Run("only_void_releases_an_assignment_and_the_source_never_reopens", func(t *testing.T) {
		rule, item := f.createSource(t, ctx)
		canceled, err := f.items.Cancel(ctx, item.ID, types.BillingScheduleInvoiceVoided)
		require.NoError(t, err)
		require.False(t, canceled)
		inv := newBillingTestInvoice(ctx)
		line := newBillingTestLine(ctx, inv, item)
		inv.LineItems = []*domaininvoice.InvoiceLineItem{line}
		require.NoError(t, f.invs.CreateWithLineItems(ctx, inv))
		claimed, err := f.items.Claim(ctx, item.ID, inv.ID, line.ID)
		require.NoError(t, err)
		require.True(t, claimed)
		for _, reason := range []types.BillingScheduleCancellationReason{types.BillingScheduleTermination, types.BillingScheduleTermsAmended, types.BillingScheduleInvoiceVoided} {
			canceled, err = f.items.Cancel(ctx, item.ID, reason)
			require.NoError(t, err)
			require.False(t, canceled, "a live invoice retains its principal")
		}
		owner := domain.OwnerRef{Type: rule.OwnerType, ID: rule.OwnerID}
		require.NoError(t, f.rules.Archive(ctx, owner))
		require.NoError(t, f.items.Archive(ctx, owner))
		_, err = f.client.Writer(ctx).Invoice.Update().Where(invoicedb.ID(inv.ID), invoicedb.TenantID(types.GetTenantID(ctx)), invoicedb.EnvironmentID(types.GetEnvironmentID(ctx))).SetInvoiceStatus(types.InvoiceStatusVoided).Save(ctx)
		require.NoError(t, err)
		canceled, err = f.items.Cancel(ctx, item.ID, types.BillingScheduleInvoiceVoided)
		require.NoError(t, err)
		require.True(t, canceled)
		canceled, err = f.items.Cancel(ctx, item.ID, types.BillingScheduleInvoiceVoided)
		require.NoError(t, err)
		require.False(t, canceled)
		got, err := f.items.Get(ctx, item.ID)
		require.NoError(t, err)
		require.Equal(t, types.BillingScheduleCanceled, got.ScheduleStatus)
		require.Equal(t, types.StatusArchived, got.Status)
		require.Equal(t, types.BillingScheduleInvoiceVoided, *got.CancellationReason)
		require.Equal(t, line.ID, *got.InvoiceLineItemID)
		require.Equal(t, inv.ID, *got.InvoiceID)
		_, err = f.client.Writer(ctx).InvoiceLineItem.Update().Where(linedb.ID(line.ID), linedb.TenantID(types.GetTenantID(ctx)), linedb.EnvironmentID(types.GetEnvironmentID(ctx))).SetStatus(string(types.StatusArchived)).Save(ctx)
		require.NoError(t, err)
		replacement := newBillingTestInvoice(ctx)
		require.NoError(t, f.invs.Create(ctx, replacement))
		duplicate := newBillingTestLine(ctx, replacement, item)
		require.Error(t, f.lines.Create(ctx, duplicate), "source uniqueness includes archived invoice lines")
	})

	t.Run("pagination_is_stable_and_bounded", func(t *testing.T) {
		owner := domain.OwnerRef{Type: "future_fee", ID: types.GenerateUUIDWithPrefix("owner")}
		created := time.Date(2026, time.October, 28, 0, 0, 0, 0, time.UTC)
		var ids []string
		for range 3 {
			rule := newBillingTestRule(ctx, owner)
			rule.CreatedAt = created
			require.NoError(t, f.rules.Create(ctx, rule))
			ids = append(ids, rule.ID)
		}
		var gotIDs []string
		filter := &domain.RuleFilter{Owner: &owner, Limit: 1}
		for {
			page, err := f.rules.List(ctx, filter)
			require.NoError(t, err)
			if len(page) == 0 {
				break
			}
			gotIDs = append(gotIDs, page[0].ID)
			filter.After = &domain.Cursor{CreatedAt: page[0].CreatedAt, ID: page[0].ID}
		}
		require.ElementsMatch(t, ids, gotIDs)
		require.Len(t, gotIDs, 3)
		_, err := f.rules.List(ctx, &domain.RuleFilter{Limit: 501})
		require.Error(t, err)
		_, err = f.items.List(ctx, &domain.ScheduleItemFilter{After: &domain.Cursor{ID: "missing_date"}})
		require.Error(t, err)
		statuses := make([]types.BillingScheduleStatus, 500)
		for index := range statuses {
			statuses[index] = types.BillingScheduleScheduled
		}
		_, err = f.items.List(ctx, &domain.ScheduleItemFilter{Owner: &owner, ScheduleStatuses: statuses})
		require.NoError(t, err)
		_, err = f.items.List(ctx, &domain.ScheduleItemFilter{ScheduleStatuses: append(statuses, types.BillingScheduleScheduled)})
		require.Error(t, err)
	})

	t.Run("database_guards_invalid_shapes_and_execution_states", func(t *testing.T) {
		rule, item := f.createSource(t, ctx)
		badRule := newBillingTestRule(ctx, domain.OwnerRef{Type: rule.OwnerType, ID: rule.OwnerID})
		badRule.BillOn = nil
		require.Error(t, f.rules.Create(ctx, badRule))
		for name, change := range map[string]func(*domain.ScheduleItem){
			"zero_principal":                             func(i *domain.ScheduleItem) { i.Amount = decimal.Zero },
			"negative_principal":                         func(i *domain.ScheduleItem) { i.Amount = decimal.NewFromInt(-1) },
			"reversed_period":                            func(i *domain.ScheduleItem) { i.ServicePeriodEnd = i.ServicePeriodStart },
			"dated_trigger_without_date":                 func(i *domain.ScheduleItem) { i.BillOn = nil },
			"subscription_delivery_without_subscription": func(i *domain.ScheduleItem) { i.Delivery = types.BillingTermSubscriptionInvoice },
		} {
			t.Run(name, func(t *testing.T) {
				bad := domain.NewScheduleItemBuilder(item).WithID(types.GenerateUUIDWithPrefix("bsitem")).Build()
				change(bad)
				require.Error(t, f.items.Create(ctx, bad))
			})
		}
		_, err := f.client.Writer(ctx).BillingScheduleItem.Update().Where(itemdb.ID(item.ID), itemdb.TenantID(types.GetTenantID(ctx)), itemdb.EnvironmentID(types.GetEnvironmentID(ctx))).SetScheduleStatus(types.BillingScheduleInvoiced).Save(ctx)
		require.Error(t, err)
		_, err = f.client.Writer(ctx).BillingScheduleItem.Update().Where(itemdb.ID(item.ID), itemdb.TenantID(types.GetTenantID(ctx)), itemdb.EnvironmentID(types.GetEnvironmentID(ctx))).SetScheduleStatus(types.BillingScheduleCanceled).Save(ctx)
		require.Error(t, err)
		canceled, err := f.items.Cancel(ctx, item.ID, types.BillingScheduleTermsAmended)
		require.NoError(t, err)
		require.True(t, canceled)
	})
}

func raceBillingOperations(t *testing.T, operation func() (bool, error)) {
	t.Helper()
	var wg sync.WaitGroup
	results := make(chan bool, 2)
	errors := make(chan error, 2)
	start := make(chan struct{})
	for range 2 {
		wg.Go(func() {
			<-start
			changed, err := operation()
			results <- changed
			errors <- err
		})
	}
	close(start)
	wg.Wait()
	close(results)
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	winners := 0
	for changed := range results {
		if changed {
			winners++
		}
	}
	require.Equal(t, 1, winners)
}
