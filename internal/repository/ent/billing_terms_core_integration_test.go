package ent

import (
	"context"
	"errors"
	"testing"
	"time"

	invoicedb "github.com/flexprice/flexprice/ent/invoice"
	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	domaininvoice "github.com/flexprice/flexprice/internal/domain/invoice"
	core "github.com/flexprice/flexprice/internal/ee/service/billingterms"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type billingCoreOwner struct {
	snapshot domain.OwnerSnapshot
}

func (a *billingCoreOwner) Type() types.BillingTermOwnerType { return a.snapshot.Owner.Type }
func (a *billingCoreOwner) Snapshot(context.Context, domain.OwnerRef) (*domain.OwnerSnapshot, error) {
	copy := a.snapshot
	return &copy, nil
}

type billingCoreFixture struct {
	db      *billingPostgresFixture
	service core.Service
	store   domain.ExecutionStore
	owner   domain.OwnerSnapshot
}

func newBillingCoreFixture(t *testing.T, db *billingPostgresFixture, ctx context.Context) *billingCoreFixture {
	t.Helper()
	db.seedCustomer(t, ctx)
	start := time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC)
	owner := domain.OwnerSnapshot{
		Owner:    domain.OwnerRef{Type: "standalone_fee", ID: types.GenerateUUIDWithPrefix("fee")},
		TenantID: types.GetTenantID(ctx), EnvironmentID: types.GetEnvironmentID(ctx),
		OriginalTotal: decimal.NewFromInt(100), CollectionTarget: decimal.NewFromInt(100),
		StartDate: start, EndDate: start.AddDate(1, 0, 0), Timezone: "UTC", Currency: "usd",
		CustomerID: billingTestCustomerID(ctx), Delivery: types.BillingTermSeparateInvoice,
		CanEdit: true, CanCollect: true, Description: "Independent fixed fee", LifecycleKey: "active",
	}
	resolver, err := core.NewOwnerResolver(&billingCoreOwner{snapshot: owner})
	require.NoError(t, err)
	store := NewBillingTermsExecutionStore(db.client)
	return &billingCoreFixture{db: db, store: store, owner: owner, service: core.NewService(db.rules, db.items, store, resolver)}
}

func billingCoreOnce(amount int64, due time.Time, description string) core.RuleInput {
	value := decimal.NewFromInt(amount)
	return core.RuleInput{Kind: types.BillingTermOnce, ValueType: types.BillingTermFixed, Value: &value, BillOn: &due, Description: description}
}

func billingCoreInvoice(ctx context.Context, item *domain.ScheduleItem) (*domaininvoice.Invoice, *domaininvoice.InvoiceLineItem) {
	inv := domaininvoice.NewInvoiceBuilder(&domaininvoice.Invoice{
		ID: types.GenerateUUIDWithPrefix("inv"), CustomerID: item.CustomerID,
		InvoiceType: types.InvoiceTypeOneOff, InvoiceStatus: types.InvoiceStatusDraft,
		PaymentStatus: types.PaymentStatusPending, Currency: item.Currency,
		AmountDue: item.Amount, AmountRemaining: item.Amount, Subtotal: item.Amount, Total: item.Amount,
		EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx),
	}).Build()
	line := domaininvoice.NewInvoiceLineItemBuilder(&domaininvoice.InvoiceLineItem{
		ID: types.GenerateUUIDWithPrefix("invli"), InvoiceID: inv.ID, CustomerID: item.CustomerID,
		SubscriptionID: item.SubscriptionID, Amount: item.Amount, Quantity: decimal.NewFromInt(1), Currency: item.Currency,
		PeriodStart: &item.ServicePeriodStart, PeriodEnd: &item.ServicePeriodEnd,
		EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx),
	}).WithBillingScheduleItemID(&item.ID).Build()
	return domaininvoice.NewInvoiceBuilder(inv).WithIdempotencyKey(&inv.ID).WithLineItems([]*domaininvoice.InvoiceLineItem{line}).Build(), line
}

func (f *billingCoreFixture) claim(ctx context.Context, item *domain.ScheduleItem) (*domaininvoice.Invoice, error) {
	inv, line := billingCoreInvoice(ctx, item)
	err := f.store.WithTx(ctx, func(tx context.Context) error {
		if err := f.store.LockOwner(tx, f.owner.Owner); err != nil {
			return err
		}
		if err := f.db.invs.CreateWithLineItems(tx, inv); err != nil {
			return err
		}
		claimed, err := f.db.items.Claim(tx, item.ID, inv.ID, line.ID)
		if err != nil {
			return err
		}
		if !claimed {
			return ierr.NewError("candidate changed before collection").Mark(ierr.ErrVersionConflict)
		}
		return nil
	})
	return inv, err
}

func TestBillingTermsCoreReplacePostgres(t *testing.T) {
	db := newBillingPostgresFixture(t)
	ctx := billingTestContext("replace_tenant", "replace_env")
	f := newBillingCoreFixture(t, db, ctx)
	initialRows := []core.RuleInput{billingCoreOnce(25, f.owner.StartDate, "Deposit"), billingCoreOnce(75, f.owner.StartDate.AddDate(0, 1, 0), "Balance")}
	initial, err := f.service.Initialize(ctx, core.InitializeRequest{Owner: f.owner.Owner, Rows: initialRows})
	require.NoError(t, err)
	require.Len(t, initial.Items(), 2)
	require.True(t, initial.Summary().ScheduledAmount.Equal(decimal.NewFromInt(100)))
	var deposit *domain.ScheduleItem
	for _, item := range initial.Items() {
		if item.Amount.Equal(decimal.NewFromInt(25)) {
			deposit = item
		}
	}
	require.NotNil(t, deposit)
	inv, err := f.claim(ctx, deposit)
	require.NoError(t, err)
	assigned, err := f.service.Get(ctx, f.owner.Owner)
	require.NoError(t, err)
	require.NotEqual(t, initial.StateToken(), assigned.StateToken())
	require.True(t, assigned.Summary().AssignedAmount.Equal(decimal.NewFromInt(25)))
	noop, err := f.service.Replace(ctx, core.ReplaceRequest{Owner: f.owner.Owner, Rows: initialRows, ExpectedState: initial.StateToken()})
	require.NoError(t, err)
	require.True(t, noop.Noop())
	require.Equal(t, assigned.StateToken(), noop.StateToken())
	_, err = f.service.Replace(ctx, core.ReplaceRequest{Owner: f.owner.Owner, Rows: []core.RuleInput{billingCoreOnce(75, f.owner.StartDate.AddDate(0, 2, 0), "Revised balance")}, ExpectedState: initial.StateToken()})
	require.True(t, ierr.IsVersionConflict(err))
	replaced, err := f.service.Replace(ctx, core.ReplaceRequest{Owner: f.owner.Owner, Rows: []core.RuleInput{billingCoreOnce(75, f.owner.StartDate.AddDate(0, 2, 0), "Revised balance")}, ExpectedState: assigned.StateToken(), Reason: "Correct collection dates"})
	require.NoError(t, err)
	require.Len(t, replaced.Rules(), 1)
	require.Len(t, replaced.Items(), 2)
	require.True(t, replaced.Summary().AssignedAmount.Equal(decimal.NewFromInt(25)))
	require.True(t, replaced.Summary().ScheduledAmount.Equal(decimal.NewFromInt(75)))
	retained, err := db.items.Get(ctx, deposit.ID)
	require.NoError(t, err)
	require.Equal(t, types.StatusArchived, retained.Status)
	require.Equal(t, inv.ID, *retained.InvoiceID)
	require.True(t, retained.Amount.Equal(deposit.Amount))
	history, err := db.items.List(ctx, &domain.ScheduleItemFilter{Owner: &f.owner.Owner, IncludeArchived: true})
	require.NoError(t, err)
	require.Len(t, history, 3)
	for _, item := range history {
		if item.ID != deposit.ID && item.Status == types.StatusArchived {
			require.Equal(t, types.BillingScheduleCanceled, item.ScheduleStatus)
			require.Equal(t, types.BillingScheduleTermsAmended, *item.CancellationReason)
		}
	}
	_, err = db.client.Writer(ctx).Invoice.Update().Where(invoicedb.ID(inv.ID), invoicedb.TenantID(types.GetTenantID(ctx)), invoicedb.EnvironmentID(types.GetEnvironmentID(ctx))).SetPaymentStatus(types.PaymentStatusSucceeded).Save(ctx)
	require.NoError(t, err)
	afterPayment, err := f.service.Get(ctx, f.owner.Owner)
	require.NoError(t, err)
	require.Equal(t, replaced.StateToken(), afterPayment.StateToken(), "payment does not change principal allocation")
	err = f.store.WithTx(ctx, func(tx context.Context) error {
		_, err := db.invs.GetForUpdate(tx, inv.ID)
		if err != nil {
			return err
		}
		if err := f.store.LockOwner(tx, f.owner.Owner); err != nil {
			return err
		}
		_, err = db.client.Writer(tx).Invoice.Update().Where(invoicedb.ID(inv.ID), invoicedb.TenantID(types.GetTenantID(tx)), invoicedb.EnvironmentID(types.GetEnvironmentID(tx))).SetInvoiceStatus(types.InvoiceStatusVoided).Save(tx)
		if err != nil {
			return err
		}
		changed, err := db.items.Cancel(tx, deposit.ID, types.BillingScheduleInvoiceVoided)
		if err == nil && !changed {
			return errors.New("void did not release its retained source")
		}
		return err
	})
	require.NoError(t, err)
	released, err := f.service.Get(ctx, f.owner.Owner)
	require.NoError(t, err)
	require.True(t, released.Summary().AssignedAmount.IsZero())
	require.True(t, released.Summary().UnallocatedAmount.Equal(decimal.NewFromInt(25)))
	require.True(t, released.Summary().RequiresReplan)
	require.NotEqual(t, replaced.StateToken(), released.StateToken())
}

type billingFailingItems struct {
	domain.ScheduleItemRepository
	failure error
}

func (r *billingFailingItems) Create(context.Context, *domain.ScheduleItem) error { return r.failure }

func TestBillingTermsCoreRollbackPostgres(t *testing.T) {
	db := newBillingPostgresFixture(t)
	ctx := billingTestContext("rollback_tenant", "rollback_env")
	f := newBillingCoreFixture(t, db, ctx)
	initial, err := f.service.Initialize(ctx, core.InitializeRequest{Owner: f.owner.Owner, Rows: []core.RuleInput{billingCoreOnce(100, f.owner.StartDate, "Original")}})
	require.NoError(t, err)
	resolver, err := core.NewOwnerResolver(&billingCoreOwner{snapshot: f.owner})
	require.NoError(t, err)
	failure := errors.New("item insert failed after rule archive and replacement insert")
	faulty := core.NewService(db.rules, &billingFailingItems{ScheduleItemRepository: db.items, failure: failure}, f.store, resolver)
	_, err = faulty.Replace(ctx, core.ReplaceRequest{Owner: f.owner.Owner, Rows: []core.RuleInput{billingCoreOnce(100, f.owner.StartDate.AddDate(0, 1, 0), "Replacement")}, ExpectedState: initial.StateToken()})
	require.ErrorIs(t, err, failure)
	after, err := f.service.Get(ctx, f.owner.Owner)
	require.NoError(t, err)
	require.Equal(t, initial.StateToken(), after.StateToken())
	require.Equal(t, initial.Items()[0].ID, after.Items()[0].ID)
	history, err := db.rules.List(ctx, &domain.RuleFilter{Owner: &f.owner.Owner, IncludeArchived: true})
	require.NoError(t, err)
	require.Len(t, history, 1)
	items, err := db.items.List(ctx, &domain.ScheduleItemFilter{Owner: &f.owner.Owner, IncludeArchived: true})
	require.NoError(t, err)
	require.Len(t, items, 1)
}

func TestBillingTermsCoreConcurrentEditsPostgres(t *testing.T) {
	db := newBillingPostgresFixture(t)
	ctx, cancel := context.WithTimeout(billingTestContext("edit_tenant", "edit_env"), 10*time.Second)
	defer cancel()
	f := newBillingCoreFixture(t, db, ctx)
	initial, err := f.service.Initialize(ctx, core.InitializeRequest{Owner: f.owner.Owner, Rows: []core.RuleInput{billingCoreOnce(100, f.owner.StartDate, "Original")}})
	require.NoError(t, err)
	start, outcomes := make(chan struct{}), make(chan error, 2)
	for _, months := range []int{1, 2} {
		go func(months int) {
			<-start
			_, err := f.service.Replace(ctx, core.ReplaceRequest{Owner: f.owner.Owner, Rows: []core.RuleInput{billingCoreOnce(100, f.owner.StartDate.AddDate(0, months, 0), "New date")}, ExpectedState: initial.StateToken()})
			outcomes <- err
		}(months)
	}
	close(start)
	first, second := <-outcomes, <-outcomes
	require.True(t, first == nil && ierr.IsVersionConflict(second) || second == nil && ierr.IsVersionConflict(first), "one reviewed state can authorize only one different replacement: %v / %v", first, second)
	after, err := f.service.Get(ctx, f.owner.Owner)
	require.NoError(t, err)
	require.Len(t, after.Items(), 1)
	require.True(t, after.Summary().ScheduledAmount.Equal(decimal.NewFromInt(100)))
	history, err := db.rules.List(ctx, &domain.RuleFilter{Owner: &f.owner.Owner, IncludeArchived: true})
	require.NoError(t, err)
	require.Len(t, history, 2, "losing edit must not create another revision")
}

func TestBillingTermsCoreTriggerRacePostgres(t *testing.T) {
	db := newBillingPostgresFixture(t)
	ctx, cancel := context.WithTimeout(billingTestContext("trigger_tenant", "trigger_env"), 10*time.Second)
	defer cancel()
	f := newBillingCoreFixture(t, db, ctx)
	amount := decimal.NewFromInt(100)
	rows := []core.RuleInput{{Kind: types.BillingTermOnce, ValueType: types.BillingTermFixed, Value: &amount, OnTrigger: true, Description: "Acceptance milestone"}}
	initial, err := f.service.Initialize(ctx, core.InitializeRequest{Owner: f.owner.Owner, Rows: rows})
	require.NoError(t, err)
	itemID := initial.Items()[0].ID
	start, outcomes := make(chan struct{}), make(chan error, 2)
	for _, days := range []int{1, 2} {
		go func(days int) {
			<-start
			due := f.owner.StartDate.AddDate(0, 0, days)
			_, err := f.service.Trigger(ctx, core.TriggerRequest{Owner: f.owner.Owner, ItemID: itemID, ExpectedState: initial.StateToken(), BillOn: &due})
			outcomes <- err
		}(days)
	}
	close(start)
	first, second := <-outcomes, <-outcomes
	require.True(t, first == nil && ierr.IsVersionConflict(second) || second == nil && ierr.IsVersionConflict(first), "only the first trigger date wins: %v / %v", first, second)
	triggered, err := f.service.Get(ctx, f.owner.Owner)
	require.NoError(t, err)
	require.NotEqual(t, initial.StateToken(), triggered.StateToken())
	require.NotNil(t, triggered.Items()[0].BillOn)
	replayed, err := f.service.Trigger(ctx, core.TriggerRequest{Owner: f.owner.Owner, ItemID: itemID, ExpectedState: initial.StateToken()})
	require.NoError(t, err)
	require.True(t, replayed.Noop())
	require.Equal(t, triggered.StateToken(), replayed.StateToken())
	noop, err := f.service.Replace(ctx, core.ReplaceRequest{Owner: f.owner.Owner, Rows: rows, ExpectedState: initial.StateToken()})
	require.NoError(t, err)
	require.True(t, noop.Noop())
	require.Equal(t, itemID, noop.Items()[0].ID)
	require.Equal(t, triggered.StateToken(), noop.StateToken(), "an identical save cannot re-arm a triggered milestone")
	replaced, err := f.service.Replace(ctx, core.ReplaceRequest{Owner: f.owner.Owner, Rows: []core.RuleInput{billingCoreOnce(100, f.owner.StartDate.AddDate(0, 2, 0), "Different terms")}, ExpectedState: triggered.StateToken()})
	require.NoError(t, err)
	_, err = f.service.Trigger(ctx, core.TriggerRequest{Owner: f.owner.Owner, ItemID: itemID, ExpectedState: replaced.StateToken()})
	require.Error(t, err, "an archived trigger cannot re-arm")
}

func TestBillingTermsCoreClaimEditRacePostgres(t *testing.T) {
	db := newBillingPostgresFixture(t)
	for _, editFirst := range []bool{false, true} {
		name := "claim_first"
		if editFirst {
			name = "edit_first"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(billingTestContext("claim_edit_tenant", "claim_edit_env"), 10*time.Second)
			defer cancel()
			f := newBillingCoreFixture(t, db, ctx)
			initial, err := f.service.Initialize(ctx, core.InitializeRequest{Owner: f.owner.Owner, Rows: []core.RuleInput{billingCoreOnce(100, f.owner.StartDate, "Original")}})
			require.NoError(t, err)
			original := initial.Items()[0]
			replacement := core.ReplaceRequest{Owner: f.owner.Owner, Rows: []core.RuleInput{billingCoreOnce(100, f.owner.StartDate.AddDate(0, 1, 0), "Edited")}, ExpectedState: initial.StateToken()}
			mutated, release, firstDone, secondDone := make(chan struct{}), make(chan struct{}), make(chan error, 1), make(chan error, 1)
			go func() {
				firstDone <- f.store.WithTx(ctx, func(tx context.Context) error {
					if err := f.store.LockOwner(tx, f.owner.Owner); err != nil {
						return err
					}
					if editFirst {
						_, err = f.service.Replace(tx, replacement)
					} else {
						_, err = f.claim(tx, original)
					}
					if err != nil {
						return err
					}
					close(mutated)
					select {
					case <-release:
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				})
			}()
			select {
			case <-mutated:
			case err := <-firstDone:
				t.Fatalf("first mutation failed: %v", err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			var losingInvoiceID string
			go func() {
				if editFirst {
					inv, err := f.claim(ctx, original)
					losingInvoiceID = inv.ID
					secondDone <- err
				} else {
					_, err := f.service.Replace(ctx, replacement)
					secondDone <- err
				}
			}()
			close(release)
			require.NoError(t, <-firstDone)
			losingErr := <-secondDone
			require.True(t, ierr.IsVersionConflict(losingErr), "losing mutation must conflict: %v", losingErr)
			after, err := f.service.Get(ctx, f.owner.Owner)
			require.NoError(t, err)
			require.Len(t, after.Items(), 1)
			if editFirst {
				require.NotEqual(t, original.ID, after.Items()[0].ID)
				require.True(t, after.Summary().AssignedAmount.IsZero())
				require.True(t, after.Summary().ScheduledAmount.Equal(decimal.NewFromInt(100)))
				_, err := db.invs.Get(ctx, losingInvoiceID)
				require.Error(t, err, "losing claim must roll back its invoice and source line")
			} else {
				require.Equal(t, original.ID, after.Items()[0].ID)
				require.True(t, after.Summary().AssignedAmount.Equal(decimal.NewFromInt(100)))
				require.True(t, after.Summary().ScheduledAmount.IsZero())
			}
		})
	}
}

func TestBillingTermsCoreTimestampPrecisionPostgres(t *testing.T) {
	db := newBillingPostgresFixture(t)
	ctx := billingTestContext("precision_tenant", "precision_env")
	f := newBillingCoreFixture(t, db, ctx)
	due := f.owner.StartDate.Add(123456789 * time.Nanosecond)
	rows := []core.RuleInput{billingCoreOnce(100, due, "Precise configured date")}
	initial, err := f.service.Initialize(ctx, core.InitializeRequest{Owner: f.owner.Owner, Rows: rows})
	require.NoError(t, err)
	require.True(t, initial.Items()[0].BillOn.Equal(due.Truncate(time.Microsecond)))
	retried, err := f.service.Replace(ctx, core.ReplaceRequest{Owner: f.owner.Owner, Rows: rows, ExpectedState: initial.StateToken()})
	require.NoError(t, err)
	require.True(t, retried.Noop())
	require.Equal(t, initial.StateToken(), retried.StateToken())

	manual := newBillingCoreFixture(t, db, ctx)
	amount := decimal.NewFromInt(100)
	initial, err = manual.service.Initialize(ctx, core.InitializeRequest{Owner: manual.owner.Owner, Rows: []core.RuleInput{{Kind: types.BillingTermOnce, ValueType: types.BillingTermFixed, Value: &amount, OnTrigger: true}}})
	require.NoError(t, err)
	request := core.TriggerRequest{Owner: manual.owner.Owner, ItemID: initial.Items()[0].ID, ExpectedState: initial.StateToken(), BillOn: &due}
	triggered, err := manual.service.Trigger(ctx, request)
	require.NoError(t, err)
	retried, err = manual.service.Trigger(ctx, request)
	require.NoError(t, err)
	require.True(t, retried.Noop())
	require.Equal(t, triggered.StateToken(), retried.StateToken())
	require.True(t, retried.Items()[0].BillOn.Equal(due.Truncate(time.Microsecond)))
}

func TestBillingTermsExecutionTransactionsPostgres(t *testing.T) {
	f := newBillingPostgresFixture(t)
	ctx := billingTestContext("core_tenant", "core_environment")
	f.seedCustomer(t, ctx)
	store := NewBillingTermsExecutionStore(f.client)
	owner := domain.OwnerRef{Type: "standalone_fee", ID: "contract_transaction"}
	rule := newBillingTestRule(ctx, owner)
	item := newBillingTestItem(ctx, rule)
	failure := errors.New("rollback whole collection plan")
	err := store.WithTx(ctx, func(txCtx context.Context) error {
		require.NoError(t, store.LockOwner(txCtx, owner))
		require.NoError(t, f.rules.Create(txCtx, rule))
		return store.WithTx(txCtx, func(nestedCtx context.Context) error {
			require.Same(t, f.client.TxFromContext(txCtx), f.client.TxFromContext(nestedCtx))
			require.NoError(t, store.LockOwner(nestedCtx, owner))
			require.NoError(t, f.items.Create(nestedCtx, item))
			return failure
		})
	})
	require.ErrorIs(t, err, failure)
	_, err = f.rules.Get(ctx, rule.ID)
	require.Error(t, err)
	_, err = f.items.Get(ctx, item.ID)
	require.Error(t, err)
	require.Error(t, store.LockOwner(ctx, owner))
	require.Error(t, store.WithTx(ctx, nil))
	for _, missingScope := range []context.Context{nil, context.Background(), billingTestContext("core_tenant", "")} {
		require.Error(t, store.WithTx(missingScope, func(context.Context) error { return nil }))
		require.Error(t, store.LockOwner(missingScope, owner))
	}
}

func TestBillingTermsOwnerLocksPostgres(t *testing.T) {
	f := newBillingPostgresFixture(t)
	ctx, cancel := context.WithTimeout(billingTestContext("lock_tenant", "lock_environment"), 10*time.Second)
	defer cancel()
	store := NewBillingTermsExecutionStore(f.client)
	owner := domain.OwnerRef{Type: "standalone_fee", ID: "contract_lock"}
	locked, release, firstDone := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		firstDone <- store.WithTx(ctx, func(txCtx context.Context) error {
			if err := store.LockOwner(txCtx, owner); err != nil {
				return err
			}
			close(locked)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	select {
	case <-locked:
	case err := <-firstDone:
		t.Fatalf("initial owner lock failed: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	for _, different := range []struct {
		ctx   context.Context
		owner domain.OwnerRef
	}{
		{billingTestContext("another_tenant", "lock_environment"), owner},
		{billingTestContext("lock_tenant", "another_environment"), owner},
		{ctx, domain.OwnerRef{Type: "another_fee", ID: owner.ID}},
	} {
		otherCtx, otherCancel := context.WithTimeout(different.ctx, time.Second)
		err := store.WithTx(otherCtx, func(txCtx context.Context) error { return store.LockOwner(txCtx, different.owner) })
		otherCancel()
		require.NoError(t, err, "tenant, environment and owner type must participate in the lock")
	}
	secondAcquired, secondDone := make(chan struct{}), make(chan error, 1)
	go func() {
		secondDone <- store.WithTx(ctx, func(txCtx context.Context) error {
			if err := store.LockOwner(txCtx, owner); err != nil {
				return err
			}
			close(secondAcquired)
			return nil
		})
	}()
	select {
	case <-secondAcquired:
		t.Fatal("same owner entered while its transaction lock was held")
	case err := <-secondDone:
		t.Fatalf("second owner lock failed before release: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	released = true
	require.NoError(t, <-firstDone)
	require.NoError(t, <-secondDone)
	<-secondAcquired

	a := billingOwnerLockKey("a:b", "c", domain.OwnerRef{Type: types.BillingTermOwnerCommitment, ID: "d"})
	b := billingOwnerLockKey("a", "b:c", domain.OwnerRef{Type: types.BillingTermOwnerCommitment, ID: "d"})
	require.NotEqual(t, a, b, "delimiter-bearing identifiers cannot alias another scoped owner")
}
