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

type billingNilContext struct{ context.Context }

type billingVoidFailingItems struct {
	domain.ScheduleItemRepository
	failure error
}

func (r *billingVoidFailingItems) Cancel(context.Context, string, types.BillingScheduleCancellationReason) (bool, error) {
	return false, r.failure
}

func newBillingVoidFixture(t *testing.T, db *billingPostgresFixture, ctx context.Context) (*billingCoreFixture, *domain.ScheduleItem, *domaininvoice.Invoice, core.State) {
	t.Helper()
	f := newBillingCoreFixture(t, db, ctx)
	initial, err := f.service.Initialize(ctx, core.InitializeRequest{Owner: f.owner.Owner, Rows: []core.RuleInput{
		billingCoreOnce(25, f.owner.StartDate, "Deposit"),
		billingCoreOnce(75, f.owner.StartDate.AddDate(0, 1, 0), "Balance"),
	}})
	require.NoError(t, err)
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
	current, err := f.service.Replace(ctx, core.ReplaceRequest{
		Owner: f.owner.Owner, ExpectedState: assigned.StateToken(), Reason: "Correct the balance collection date",
		Rows: []core.RuleInput{billingCoreOnce(75, f.owner.StartDate.AddDate(0, 2, 0), "Revised balance")},
	})
	require.NoError(t, err)
	archived, err := db.items.Get(ctx, deposit.ID)
	require.NoError(t, err)
	require.Equal(t, types.StatusArchived, archived.Status)
	require.Equal(t, types.BillingScheduleInvoiced, archived.ScheduleStatus)
	return f, archived, inv, current
}

func billingVoidRequest(item *domain.ScheduleItem) core.InvoiceVoidRequest {
	return core.InvoiceVoidRequest{InvoiceID: *item.InvoiceID, Sources: []core.InvoiceSource{{ItemID: item.ID, InvoiceLineItemID: *item.InvoiceLineItemID}}}
}

func billingPersistVoided(ctx context.Context, f *billingCoreFixture, invoiceID string) error {
	_, err := f.db.client.Writer(ctx).Invoice.Update().Where(
		invoicedb.TenantID(types.GetTenantID(ctx)), invoicedb.EnvironmentID(types.GetEnvironmentID(ctx)), invoicedb.ID(invoiceID),
	).SetInvoiceStatus(types.InvoiceStatusVoided).Save(ctx)
	return err
}

func billingWithInvoiceVoid(ctx context.Context, f *billingCoreFixture, req core.InvoiceVoidRequest, callback func(context.Context) error) error {
	return f.store.WithTx(ctx, func(tx context.Context) error {
		if _, err := f.db.invs.GetForUpdate(tx, req.InvoiceID); err != nil {
			return err
		}
		return f.service.WithInvoiceVoid(tx, req, callback)
	})
}

func TestBillingTermsInvoiceVoidAtomicPostgres(t *testing.T) {
	db := newBillingPostgresFixture(t)
	ctx := billingTestContext("void_tenant", "void_environment")
	f, source, inv, before := newBillingVoidFixture(t, db, ctx)
	req := billingVoidRequest(source)

	require.False(t, f.store.InTransaction(nil))
	var typedNil *billingNilContext
	require.False(t, f.store.InTransaction(typedNil))
	require.False(t, f.store.InTransaction(ctx))
	require.NoError(t, f.store.WithTx(ctx, func(tx context.Context) error {
		require.True(t, f.store.InTransaction(tx))
		return nil
	}))
	called := false
	err := f.service.WithInvoiceVoid(ctx, req, func(context.Context) error { called = true; return nil })
	require.Error(t, err)
	require.False(t, called, "void requires the caller invoice transaction")

	failure := errors.New("later invoice mutation failed")
	err = billingWithInvoiceVoid(ctx, f, req, func(tx context.Context) error {
		if err := billingPersistVoided(tx, f, inv.ID); err != nil {
			return err
		}
		return failure
	})
	require.ErrorIs(t, err, failure)
	unchangedInvoice, err := db.invs.Get(ctx, inv.ID)
	require.NoError(t, err)
	require.Equal(t, types.InvoiceStatusDraft, unchangedInvoice.InvoiceStatus)
	unchangedSource, err := db.items.Get(ctx, source.ID)
	require.NoError(t, err)
	require.Equal(t, types.BillingScheduleInvoiced, unchangedSource.ScheduleStatus)
	afterRollback, err := f.service.Get(ctx, f.owner.Owner)
	require.NoError(t, err)
	require.Equal(t, before.StateToken(), afterRollback.StateToken())

	resolver, err := core.NewOwnerResolver(&billingCoreOwner{snapshot: f.owner})
	require.NoError(t, err)
	cancelFailure := errors.New("source cancellation failed after invoice update")
	faulty := *f
	faulty.service = core.NewService(db.rules, &billingVoidFailingItems{ScheduleItemRepository: db.items, failure: cancelFailure}, f.store, resolver)
	err = billingWithInvoiceVoid(ctx, &faulty, req, func(tx context.Context) error { return billingPersistVoided(tx, f, inv.ID) })
	require.ErrorIs(t, err, cancelFailure)
	unchangedInvoice, err = db.invs.Get(ctx, inv.ID)
	require.NoError(t, err)
	require.Equal(t, types.InvoiceStatusDraft, unchangedInvoice.InvoiceStatus)
	afterRollback, err = f.service.Get(ctx, f.owner.Owner)
	require.NoError(t, err)
	require.Equal(t, before.StateToken(), afterRollback.StateToken())

	called = false
	err = billingWithInvoiceVoid(ctx, f, req, func(tx context.Context) error {
		called = true
		return billingPersistVoided(tx, f, inv.ID)
	})
	require.NoError(t, err)
	require.True(t, called)
	released, err := f.service.Get(ctx, f.owner.Owner)
	require.NoError(t, err)
	require.True(t, released.Summary().AssignedAmount.IsZero())
	require.True(t, released.Summary().ScheduledAmount.Equal(decimal.NewFromInt(75)))
	require.True(t, released.Summary().UnallocatedAmount.Equal(decimal.NewFromInt(25)))
	require.True(t, released.Summary().RequiresReplan)
	require.NotEqual(t, before.StateToken(), released.StateToken())
	canceled, err := db.items.Get(ctx, source.ID)
	require.NoError(t, err)
	require.Equal(t, types.StatusArchived, canceled.Status)
	require.Equal(t, types.BillingScheduleCanceled, canceled.ScheduleStatus)
	require.Equal(t, types.BillingScheduleInvoiceVoided, *canceled.CancellationReason)
	require.Equal(t, source.InvoiceID, canceled.InvoiceID)
	require.Equal(t, source.InvoiceLineItemID, canceled.InvoiceLineItemID)
	require.Equal(t, source.BillingTermID, canceled.BillingTermID)
	require.True(t, source.Amount.Equal(canceled.Amount))
	require.Equal(t, source.BillOn, canceled.BillOn)

	called = false
	err = billingWithInvoiceVoid(ctx, f, req, func(tx context.Context) error {
		called = true
		return billingPersistVoided(tx, f, inv.ID)
	})
	require.NoError(t, err)
	require.False(t, called, "an already completed void skips the invoice callback")
	retried, err := f.service.Get(ctx, f.owner.Owner)
	require.NoError(t, err)
	require.Equal(t, released.StateToken(), retried.StateToken())
	claimed, err := db.items.Claim(ctx, source.ID, inv.ID, *source.InvoiceLineItemID)
	require.True(t, err != nil || !claimed, "canceled historical sources cannot become collectible")
	canceled, err = db.items.Get(ctx, source.ID)
	require.NoError(t, err)
	require.Equal(t, types.BillingScheduleCanceled, canceled.ScheduleStatus)
}

func TestBillingTermsInvoiceVoidEditRacePostgres(t *testing.T) {
	db := newBillingPostgresFixture(t)
	for _, editFirst := range []bool{false, true} {
		name := "void_first"
		if editFirst {
			name = "edit_first"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(billingTestContext("void_edit_tenant", "void_edit_environment"), 10*time.Second)
			defer cancel()
			f, source, inv, before := newBillingVoidFixture(t, db, ctx)
			req := billingVoidRequest(source)
			replacement := core.ReplaceRequest{
				Owner: f.owner.Owner, ExpectedState: before.StateToken(), Reason: "Revise remaining terms",
				Rows: []core.RuleInput{billingCoreOnce(75, f.owner.StartDate.AddDate(0, 3, 0), "New balance")},
			}
			mutated, release := make(chan struct{}), make(chan struct{})
			firstDone, secondDone := make(chan error, 1), make(chan error, 1)
			waitForRelease := func() error {
				close(mutated)
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			go func() {
				if editFirst {
					firstDone <- f.store.WithTx(ctx, func(tx context.Context) error {
						if _, err := f.service.Replace(tx, replacement); err != nil {
							return err
						}
						return waitForRelease()
					})
					return
				}
				firstDone <- billingWithInvoiceVoid(ctx, f, req, func(tx context.Context) error {
					if err := waitForRelease(); err != nil {
						return err
					}
					return billingPersistVoided(tx, f, inv.ID)
				})
			}()
			select {
			case <-mutated:
			case err := <-firstDone:
				t.Fatalf("first mutation failed: %v", err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			go func() {
				if editFirst {
					secondDone <- billingWithInvoiceVoid(ctx, f, req, func(tx context.Context) error { return billingPersistVoided(tx, f, inv.ID) })
					return
				}
				_, err := f.service.Replace(ctx, replacement)
				secondDone <- err
			}()
			close(release)
			require.NoError(t, <-firstDone)
			secondErr := <-secondDone
			if editFirst {
				require.NoError(t, secondErr, "void revalidates the retained source after an edit")
			} else {
				require.True(t, ierr.IsVersionConflict(secondErr), "void invalidates the old edit preview: %v", secondErr)
			}
			after, err := f.service.Get(ctx, f.owner.Owner)
			require.NoError(t, err)
			require.True(t, after.Summary().AssignedAmount.IsZero())
			require.True(t, after.Summary().ScheduledAmount.Equal(decimal.NewFromInt(75)))
			require.True(t, after.Summary().UnallocatedAmount.Equal(decimal.NewFromInt(25)))
			require.Len(t, after.Rules(), 1)
			if editFirst {
				require.Equal(t, "New balance", after.Rules()[0].Description)
			} else {
				require.Equal(t, "Revised balance", after.Rules()[0].Description)
			}
			canceled, err := db.items.Get(ctx, source.ID)
			require.NoError(t, err)
			require.Equal(t, types.BillingScheduleCanceled, canceled.ScheduleStatus)
			require.Equal(t, source.InvoiceLineItemID, canceled.InvoiceLineItemID)
		})
	}
}
