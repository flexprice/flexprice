package billingterms

import (
	"context"
	"errors"
	"testing"
	"time"

	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func collectionRequest(db *memoryExecution) CollectionRequest {
	return CollectionRequest{InvoiceID: "invoice", CustomerID: db.owner.CustomerID, Currency: db.owner.Currency,
		Delivery: db.owner.Delivery, Owners: []domain.OwnerRef{db.owner.Owner},
		Cutoff: db.owner.StartDate.AddDate(0, 2, 0), AsOf: db.owner.StartDate.AddDate(0, 4, 0)}
}

func persistCollection(db *memoryExecution, req CollectionRequest, items []*domain.ScheduleItem) []InvoiceSource {
	bindings := make([]InvoiceSource, len(items))
	for i, item := range items {
		lineID := "line-" + item.ID
		bindings[i] = InvoiceSource{ItemID: item.ID, InvoiceLineItemID: lineID}
		db.bindings[item.ID] = domain.BindingState{ItemID: item.ID, InvoiceID: req.InvoiceID, InvoiceLineItemID: lineID,
			TenantID: db.owner.TenantID, EnvironmentID: db.owner.EnvironmentID, InvoiceStatus: types.InvoiceStatusDraft}
	}
	return bindings
}

func TestCollectionClaimsOnlyEligibleDueItems(t *testing.T) {
	ctx, core, db := newCore(t)
	initial, err := core.Initialize(ctx, InitializeRequest{Owner: db.owner.Owner, Rows: annualRows()})
	require.NoError(t, err)
	req := collectionRequest(db)
	for attempt := 0; attempt < 2; attempt++ {
		require.NoError(t, db.WithTx(ctx, func(tx context.Context) error {
			return core.WithCollection(tx, req, func(_ context.Context, items []*domain.ScheduleItem) ([]InvoiceSource, error) {
				if attempt == 0 {
					require.Len(t, items, 3)
					require.Equal(t, "450", sumItems(items).String())
				} else {
					require.Empty(t, items)
				}
				return persistCollection(db, req, items), nil
			})
		}))
	}
	state, err := core.Get(ctx, db.owner.Owner)
	require.NoError(t, err)
	require.Equal(t, "450", state.Summary().AssignedAmount.String())
	require.Equal(t, "0", state.Summary().InvoicedAmount.String(), "draft assignments are not finalized invoices")
	require.Len(t, state.Items(), len(initial.Items()))
}

func TestCollectionFreshEligibilityAndCutoff(t *testing.T) {
	for _, deferred := range []bool{false, true} {
		t.Run(map[bool]string{true: "pause wins", false: "clock bounds cutoff"}[deferred], func(t *testing.T) {
			ctx, core, db := newCore(t)
			_, err := core.Initialize(ctx, InitializeRequest{Owner: db.owner.Owner, Rows: annualRows()})
			require.NoError(t, err)
			req := collectionRequest(db)
			req.AsOf = db.owner.StartDate.Add(time.Hour)
			if deferred {
				db.afterEligibility = func() { db.owner.CanCollect = false }
			}
			require.NoError(t, db.WithTx(ctx, func(tx context.Context) error {
				return core.WithCollection(tx, req, func(_ context.Context, items []*domain.ScheduleItem) ([]InvoiceSource, error) {
					if deferred {
						require.Empty(t, items)
					} else {
						require.Len(t, items, 1)
						require.Equal(t, "300", items[0].Amount.String())
					}
					return persistCollection(db, req, items), nil
				})
			}))
		})
	}
}

func TestCollectionFailureRollsBackClaims(t *testing.T) {
	for _, failure := range []string{"callback", "missing binding", "duplicate binding", "wrong invoice", "claim race"} {
		t.Run(failure, func(t *testing.T) {
			ctx, core, db := newCore(t)
			initial, err := core.Initialize(ctx, InitializeRequest{Owner: db.owner.Owner, Rows: annualRows()})
			require.NoError(t, err)
			req := collectionRequest(db)
			err = db.WithTx(ctx, func(tx context.Context) error {
				return core.WithCollection(tx, req, func(_ context.Context, items []*domain.ScheduleItem) ([]InvoiceSource, error) {
					bindings := persistCollection(db, req, items)
					switch failure {
					case "callback":
						return nil, errors.New("invoice persistence failed")
					case "missing binding":
						return bindings[:len(bindings)-1], nil
					case "duplicate binding":
						bindings[1] = bindings[0]
					case "wrong invoice":
						state := db.bindings[items[0].ID]
						state.InvoiceID = "other"
						db.bindings[items[0].ID] = state
					case "claim race":
						db.items[items[1].ID].ScheduleStatus = types.BillingScheduleCanceled
					}
					return bindings, nil
				})
			})
			require.Error(t, err)
			for _, item := range initial.Items() {
				require.Equal(t, types.BillingScheduleScheduled, db.items[item.ID].ScheduleStatus)
				require.Nil(t, db.items[item.ID].InvoiceID)
			}
		})
	}
}

func TestCollectionSelectedSourcesMustRemainEligible(t *testing.T) {
	ctx, core, db := newCore(t)
	_, err := core.Initialize(ctx, InitializeRequest{Owner: db.owner.Owner, Rows: annualRows()})
	require.NoError(t, err)
	req := collectionRequest(db)
	req.SelectedItemIDs = []string{"missing"}
	err = db.WithTx(ctx, func(tx context.Context) error {
		return core.WithCollection(tx, req, func(context.Context, []*domain.ScheduleItem) ([]InvoiceSource, error) {
			t.Fatal("must not persist stale reviewed selection")
			return nil, nil
		})
	})
	require.True(t, ierr.IsVersionConflict(err), "%v", err)
	require.Error(t, core.WithCollection(ctx, req, func(context.Context, []*domain.ScheduleItem) ([]InvoiceSource, error) { return nil, nil }))
}
