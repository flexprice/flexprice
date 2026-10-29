package billingterms

import (
	"context"
	"errors"
	"testing"
	"time"

	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestSettlementDatePolicy(t *testing.T) {
	for _, test := range []struct {
		name   string
		date   func(*domain.OwnerSnapshot) time.Time
		target int64
		valid  bool
	}{
		{"pre-service charge rejected", func(owner *domain.OwnerSnapshot) time.Time { return owner.StartDate.Add(-time.Hour) }, 100, false},
		{"pre-service zero closure", func(owner *domain.OwnerSnapshot) time.Time { return owner.StartDate.Add(-time.Hour) }, 0, true},
		{"service start", func(owner *domain.OwnerSnapshot) time.Time { return owner.StartDate }, 100, true},
		{"service end", func(owner *domain.OwnerSnapshot) time.Time { return owner.EndDate }, 100, true},
		{"late terminal collection", func(owner *domain.OwnerSnapshot) time.Time { return owner.EndDate.Add(time.Hour) }, 100, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, core, db := newCore(t)
			initial, err := core.Initialize(ctx, InitializeRequest{Owner: db.owner.Owner, Rows: annualRows()})
			require.NoError(t, err)
			billOn := test.date(&db.owner)
			settled, err := core.WithSettlement(ctx, SettlementRequest{Owner: db.owner.Owner, BillOn: billOn}, func(context.Context, State) (bool, error) {
				db.owner.CollectionTarget = decimal.NewFromInt(test.target)
				return true, nil
			})
			if !test.valid {
				require.True(t, ierr.IsValidation(err), "%v", err)
				current, err := core.Get(ctx, db.owner.Owner)
				require.NoError(t, err)
				require.Equal(t, initial.StateToken(), current.StateToken())
				require.Equal(t, sourceIDs(initial), sourceIDs(current))
				return
			}
			require.NoError(t, err)
			if test.target == 0 {
				require.Empty(t, settled.Items())
				return
			}
			require.Len(t, settled.Items(), 1)
			require.True(t, settled.Items()[0].BillOn.Equal(billOn))
		})
	}
}

func TestSettlementRetainsPrincipalAndReplays(t *testing.T) {
	ctx, core, db := newCore(t)
	initial, err := core.Initialize(ctx, InitializeRequest{Owner: db.owner.Owner, Rows: annualRows()})
	require.NoError(t, err)
	claimed := initial.Items()[0]
	claimMemory(db, claimed, types.InvoiceStatusFinalized)
	retained := db.items[claimed.ID].Amount
	request := SettlementRequest{Owner: db.owner.Owner, BillOn: db.owner.StartDate.AddDate(0, 3, 0)}
	settled, err := core.WithSettlement(ctx, request, func(_ context.Context, current State) (bool, error) {
		require.True(t, current.Summary().AssignedAmount.Equal(retained))
		db.owner.CollectionTarget = retained.Add(decimal.NewFromInt(50))
		db.owner.LifecycleKey = "terminated"
		db.owner.DatedOnceOnly = true
		return true, nil
	})
	require.NoError(t, err)
	require.Empty(t, settled.Rules())
	require.True(t, settled.Summary().AssignedAmount.Equal(retained))
	require.Equal(t, "50", settled.Summary().ScheduledAmount.String())
	require.False(t, settled.Summary().RequiresReplan)
	var settlement *domain.ScheduleItem
	for _, item := range settled.Items() {
		if item.ScheduleStatus == types.BillingScheduleInvoiced {
			require.Equal(t, claimed.ID, item.ID)
			require.Equal(t, types.StatusArchived, item.Status)
			continue
		}
		settlement = item
		require.Equal(t, types.BillingScheduleSettlement, item.Kind)
		require.Nil(t, item.BillingTermID)
		require.Equal(t, request.BillOn, *item.BillOn)
		require.Equal(t, db.owner.StartDate, item.ServicePeriodStart)
		require.Equal(t, db.owner.EndDate, item.ServicePeriodEnd)
	}
	require.NotNil(t, settlement)
	replay := func(context.Context, State) (bool, error) { return false, nil }
	retried, err := core.WithSettlement(ctx, request, replay)
	require.NoError(t, err)
	require.True(t, retried.Noop())
	require.Equal(t, sourceIDs(settled), sourceIDs(retried))
	claimMemory(db, settlement, types.InvoiceStatusDraft)
	retainedSettlement := db.items[settlement.ID]
	db.items[settlement.ID] = domain.NewScheduleItemBuilder(retainedSettlement).WithStatus(types.StatusArchived).
		WithScheduleStatus(types.BillingScheduleCanceled).WithCancellationReason(ptr(types.BillingScheduleInvoiceVoided)).Build()
	retried, err = core.WithSettlement(ctx, request, replay)
	require.NoError(t, err)
	require.True(t, retried.Noop())
	require.True(t, retried.Summary().RequiresReplan)
	require.Equal(t, "50", retried.Summary().UnallocatedAmount.String())
	require.Equal(t, "0", retried.Summary().ScheduledAmount.String())
}

func TestSettlementZeroClosureAndRollback(t *testing.T) {
	for _, scenario := range []string{"zero closure", "create failure", "callback failure", "target below assigned"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, core, db := newCore(t)
			initial, err := core.Initialize(ctx, InitializeRequest{Owner: db.owner.Owner, Rows: annualRows()})
			require.NoError(t, err)
			if scenario == "target below assigned" {
				claimMemory(db, initial.Items()[0], types.InvoiceStatusDraft)
			}
			before, err := core.Get(ctx, db.owner.Owner)
			require.NoError(t, err)
			request := SettlementRequest{Owner: db.owner.Owner, BillOn: db.owner.StartDate}
			_, err = core.WithSettlement(ctx, request, func(context.Context, State) (bool, error) {
				db.owner.CollectionTarget = decimal.Zero
				if scenario == "create failure" {
					db.owner.CollectionTarget = decimal.NewFromInt(10)
					db.failCreate = true
				}
				if scenario == "callback failure" {
					return false, errors.New("owner persistence failed")
				}
				return true, nil
			})
			if scenario == "zero closure" {
				require.NoError(t, err)
				closed, err := core.Get(ctx, db.owner.Owner)
				require.NoError(t, err)
				require.Empty(t, closed.Items())
				require.Empty(t, closed.Rules())
				require.False(t, closed.Summary().RequiresReplan)
				return
			}
			require.Error(t, err)
			db.failCreate = false
			after, err := core.Get(ctx, db.owner.Owner)
			require.NoError(t, err)
			require.Equal(t, before.StateToken(), after.StateToken())
			require.Equal(t, sourceIDs(before), sourceIDs(after))
		})
	}
}

func TestSettlementDatedCorrectionsOnly(t *testing.T) {
	ctx, core, db := newCore(t)
	initial, err := core.Initialize(ctx, InitializeRequest{Owner: db.owner.Owner, Rows: annualRows()})
	require.NoError(t, err)
	db.owner.DatedOnceOnly = true
	_, err = core.Replace(ctx, ReplaceRequest{Owner: db.owner.Owner, Rows: annualRows(), ExpectedState: initial.StateToken()})
	require.NoError(t, err, "identical installed rules remain a no-op")
	current, err := core.Get(ctx, db.owner.Owner)
	require.NoError(t, err)
	for _, rows := range [][]RuleInput{replacementRows(db.owner), {{Kind: types.BillingTermOnce, ValueType: types.BillingTermRemainder, OnTrigger: true}}} {
		_, err := core.Replace(ctx, ReplaceRequest{Owner: db.owner.Owner, Rows: rows, ExpectedState: current.StateToken()})
		require.True(t, ierr.IsValidation(err), "%+v", err)
	}
	_, err = core.Replace(ctx, ReplaceRequest{Owner: db.owner.Owner, Rows: []RuleInput{{Kind: types.BillingTermOnce, ValueType: types.BillingTermRemainder, BillOn: ptr(db.owner.EndDate)}}, ExpectedState: current.StateToken()})
	require.NoError(t, err)
}
