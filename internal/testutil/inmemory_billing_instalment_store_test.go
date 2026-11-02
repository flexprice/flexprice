package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/billingterm"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type instalmentFixture struct {
	ctx   context.Context
	store *InMemoryBillingInstalmentStore
	start time.Time
}

func newInstalmentFixture(t *testing.T) *instalmentFixture {
	t.Helper()

	ctx := types.SetUserID(
		types.SetEnvironmentID(types.SetTenantID(context.Background(), "tenant_1"), "env_1"),
		"user_1",
	)

	return &instalmentFixture{
		ctx:   ctx,
		store: NewInMemoryBillingInstalmentStore(),
		start: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func (f *instalmentFixture) instalment(id, ownerID string, status types.InstalmentStatus, billOn time.Time) *billingterm.Instalment {
	return &billingterm.Instalment{
		ID:                 id,
		OwnerType:          types.BillingTermOwnerCommitment,
		OwnerID:            ownerID,
		CustomerID:         "cust_1",
		SubscriptionID:     "subs_1",
		Currency:           "usd",
		Kind:               types.InstalmentKindInstalment,
		BillTrigger:        types.InstalmentBillTriggerDate,
		BillOn:             billOn,
		Amount:             decimal.NewFromInt(100),
		Description:        "Instalment " + id,
		ServicePeriodStart: billOn,
		ServicePeriodEnd:   billOn.AddDate(0, 1, 0),
		Delivery:           types.InstalmentDeliverySubscriptionInvoice,
		InstalmentStatus:   status,
		EnvironmentID:      "env_1",
		BaseModel:          types.BaseModel{TenantID: "tenant_1", Status: types.StatusPublished},
	}
}

func (f *instalmentFixture) seed(t *testing.T, rows ...*billingterm.Instalment) {
	t.Helper()

	_, err := f.store.CreateMany(f.ctx, rows)
	require.NoError(t, err)
}

func (f *instalmentFixture) get(t *testing.T, id string) *billingterm.Instalment {
	t.Helper()

	row, err := f.store.Get(f.ctx, id)
	require.NoError(t, err)
	return row
}

func instalmentIDs(rows []*billingterm.Instalment) []string {
	return lo.Map(rows, func(in *billingterm.Instalment, _ int) string { return in.ID })
}

func TestInMemoryBillingInstalmentStore_ClaimRescheduleCancel(t *testing.T) {
	t.Run("claim moves only scheduled rows", func(t *testing.T) {
		f := newInstalmentFixture(t)
		first := f.instalment("binst_a", "cmt_1", types.InstalmentStatusScheduled, f.start)
		second := f.instalment("binst_b", "cmt_1", types.InstalmentStatusScheduled, f.start.AddDate(0, 1, 0))
		cancelled := f.instalment("binst_c", "cmt_1", types.InstalmentStatusCancelled, f.start.AddDate(0, 2, 0))
		f.seed(t, first, second, cancelled)

		claimed, err := f.store.Claim(f.ctx, "inv_1", []string{first.ID, second.ID, cancelled.ID})
		require.NoError(t, err)
		require.ElementsMatch(t, []string{first.ID, second.ID}, instalmentIDs(claimed))
		for _, in := range claimed {
			require.Equal(t, types.InstalmentStatusInvoiced, in.InstalmentStatus)
			require.Equal(t, "inv_1", lo.FromPtr(in.InvoiceID))
		}

		again, err := f.store.Claim(f.ctx, "inv_1", []string{first.ID, second.ID, cancelled.ID})
		require.NoError(t, err)
		require.Empty(t, again)

		untouched := f.get(t, cancelled.ID)
		require.Equal(t, types.InstalmentStatusCancelled, untouched.InstalmentStatus)
		require.Nil(t, untouched.InvoiceID)

		none, err := f.store.Claim(f.ctx, "inv_1", nil)
		require.NoError(t, err)
		require.Empty(t, none)
	})

	t.Run("reschedule moves only that invoice's invoiced rows", func(t *testing.T) {
		f := newInstalmentFixture(t)
		onFirst := f.instalment("binst_a", "cmt_1", types.InstalmentStatusInvoiced, f.start)
		onFirst.InvoiceID = lo.ToPtr("inv_1")
		alsoOnFirst := f.instalment("binst_b", "cmt_1", types.InstalmentStatusInvoiced, f.start.AddDate(0, 1, 0))
		alsoOnFirst.InvoiceID = lo.ToPtr("inv_1")
		onSecond := f.instalment("binst_c", "cmt_1", types.InstalmentStatusInvoiced, f.start.AddDate(0, 2, 0))
		onSecond.InvoiceID = lo.ToPtr("inv_2")
		cancelled := f.instalment("binst_d", "cmt_1", types.InstalmentStatusCancelled, f.start.AddDate(0, 3, 0))
		cancelled.InvoiceID = lo.ToPtr("inv_1")
		f.seed(t, onFirst, alsoOnFirst, onSecond, cancelled)

		rescheduled, err := f.store.Reschedule(f.ctx, "inv_1")
		require.NoError(t, err)
		require.ElementsMatch(t, []string{onFirst.ID, alsoOnFirst.ID}, instalmentIDs(rescheduled))
		for _, in := range rescheduled {
			require.Equal(t, types.InstalmentStatusScheduled, in.InstalmentStatus)
			require.Nil(t, in.InvoiceID)
		}

		other := f.get(t, onSecond.ID)
		require.Equal(t, types.InstalmentStatusInvoiced, other.InstalmentStatus)
		require.Equal(t, "inv_2", lo.FromPtr(other.InvoiceID))

		stillCancelled := f.get(t, cancelled.ID)
		require.Equal(t, types.InstalmentStatusCancelled, stillCancelled.InstalmentStatus)
		require.Equal(t, "inv_1", lo.FromPtr(stillCancelled.InvoiceID))

		again, err := f.store.Reschedule(f.ctx, "inv_1")
		require.NoError(t, err)
		require.Empty(t, again)
	})

	t.Run("cancel by owner and status keeps the invoice", func(t *testing.T) {
		f := newInstalmentFixture(t)
		scheduled := f.instalment("binst_a", "cmt_1", types.InstalmentStatusScheduled, f.start)
		invoiced := f.instalment("binst_b", "cmt_1", types.InstalmentStatusInvoiced, f.start.AddDate(0, 1, 0))
		invoiced.InvoiceID = lo.ToPtr("inv_1")
		otherScheduled := f.instalment("binst_c", "cmt_2", types.InstalmentStatusScheduled, f.start)
		f.seed(t, scheduled, invoiced, otherScheduled)

		cancelled, err := f.store.Cancel(f.ctx, types.BillingTermOwnerCommitment, "cmt_1", []types.InstalmentStatus{types.InstalmentStatusScheduled})
		require.NoError(t, err)
		require.Equal(t, []string{scheduled.ID}, instalmentIDs(cancelled))
		require.Equal(t, types.InstalmentStatusCancelled, cancelled[0].InstalmentStatus)

		require.Equal(t, types.InstalmentStatusInvoiced, f.get(t, invoiced.ID).InstalmentStatus)

		rest, err := f.store.Cancel(f.ctx, types.BillingTermOwnerCommitment, "cmt_1",
			[]types.InstalmentStatus{types.InstalmentStatusScheduled, types.InstalmentStatusInvoiced})
		require.NoError(t, err)
		require.Equal(t, []string{invoiced.ID}, instalmentIDs(rest))
		require.Equal(t, types.InstalmentStatusCancelled, rest[0].InstalmentStatus)
		require.Equal(t, "inv_1", lo.FromPtr(rest[0].InvoiceID))

		require.Equal(t, types.InstalmentStatusScheduled, f.get(t, otherScheduled.ID).InstalmentStatus)

		_, err = f.store.Cancel(f.ctx, types.BillingTermOwnerCommitment, "cmt_1", nil)
		require.Error(t, err)
		require.True(t, ierr.IsValidation(err))
	})

	t.Run("writes stay inside the tenant and environment", func(t *testing.T) {
		f := newInstalmentFixture(t)
		row := f.instalment("binst_a", "cmt_1", types.InstalmentStatusScheduled, f.start)
		f.seed(t, row)

		otherCtx := types.SetEnvironmentID(f.ctx, "env_2")
		claimed, err := f.store.Claim(otherCtx, "inv_1", []string{row.ID})
		require.NoError(t, err)
		require.Empty(t, claimed)

		cancelled, err := f.store.Cancel(otherCtx, types.BillingTermOwnerCommitment, "cmt_1", []types.InstalmentStatus{types.InstalmentStatusScheduled})
		require.NoError(t, err)
		require.Empty(t, cancelled)

		require.Equal(t, types.InstalmentStatusScheduled, f.get(t, row.ID).InstalmentStatus)
	})
}

func TestInMemoryBillingInstalmentStore_ListFiltersAndOrder(t *testing.T) {
	f := newInstalmentFixture(t)
	nextDay := f.start.Add(24 * time.Hour)
	dayAfter := f.start.Add(48 * time.Hour)

	first := f.instalment("binst_a", "cmt_1", types.InstalmentStatusScheduled, f.start)
	second := f.instalment("binst_b", "cmt_1", types.InstalmentStatusInvoiced, nextDay)
	second.InvoiceID = lo.ToPtr("inv_1")
	third := f.instalment("binst_c", "cmt_1", types.InstalmentStatusScheduled, nextDay)
	third.SubscriptionID = "subs_other"
	third.Delivery = types.InstalmentDeliverySeparateInvoice
	fourth := f.instalment("binst_d", "cmt_1", types.InstalmentStatusScheduled, dayAfter)
	fourth.Delivery = types.InstalmentDeliverySeparateInvoice
	otherOwner := f.instalment("binst_e", "cmt_2", types.InstalmentStatusScheduled, f.start)
	elsewhere := f.instalment("binst_f", "cmt_1", types.InstalmentStatusScheduled, f.start)
	elsewhere.EnvironmentID = "env_2"

	f.seed(t, fourth, third, first, second, otherOwner, elsewhere)

	tests := []struct {
		name   string
		filter *billingterm.InstalmentFilter
		want   []string
	}{
		{
			name:   "nil filter lists the environment ordered by bill_on then id",
			filter: nil,
			want:   []string{"binst_a", "binst_e", "binst_b", "binst_c", "binst_d"},
		},
		{
			name:   "owner ordered by bill_on then id",
			filter: &billingterm.InstalmentFilter{OwnerType: types.BillingTermOwnerCommitment, OwnerIDs: []string{"cmt_1"}},
			want:   []string{"binst_a", "binst_b", "binst_c", "binst_d"},
		},
		{
			name:   "bill_on bound is inclusive",
			filter: &billingterm.InstalmentFilter{OwnerIDs: []string{"cmt_1"}, BillOnOrBefore: &nextDay},
			want:   []string{"binst_a", "binst_b", "binst_c"},
		},
		{
			name:   "statuses",
			filter: &billingterm.InstalmentFilter{OwnerIDs: []string{"cmt_1"}, Statuses: []types.InstalmentStatus{types.InstalmentStatusInvoiced}},
			want:   []string{"binst_b"},
		},
		{
			name:   "deliveries",
			filter: &billingterm.InstalmentFilter{OwnerIDs: []string{"cmt_1"}, Deliveries: []types.InstalmentDelivery{types.InstalmentDeliverySeparateInvoice}},
			want:   []string{"binst_c", "binst_d"},
		},
		{
			name:   "subscriptions",
			filter: &billingterm.InstalmentFilter{SubscriptionIDs: []string{"subs_other"}},
			want:   []string{"binst_c"},
		},
		{
			name:   "invoices",
			filter: &billingterm.InstalmentFilter{InvoiceIDs: []string{"inv_1"}},
			want:   []string{"binst_b"},
		},
		{
			name:   "ids",
			filter: &billingterm.InstalmentFilter{IDs: []string{"binst_d", "binst_a", "binst_f"}},
			want:   []string{"binst_a", "binst_d"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, err := f.store.List(f.ctx, tt.filter)
			require.NoError(t, err)
			require.Equal(t, tt.want, instalmentIDs(rows))
		})
	}

	t.Run("get is scoped to the environment", func(t *testing.T) {
		_, err := f.store.Get(f.ctx, elsewhere.ID)
		require.Error(t, err)
		require.True(t, ierr.IsNotFound(err))
	})
}

func TestInMemoryBillingInstalmentStore_Update(t *testing.T) {
	f := newInstalmentFixture(t)
	row := f.instalment("binst_a", "cmt_1", types.InstalmentStatusScheduled, f.start)
	row.TriggeredAt = lo.ToPtr(f.start)
	f.seed(t, row)

	moved := f.start.AddDate(0, 0, 7)
	updated, err := f.store.Update(f.ctx, billingterm.NewInstalmentBuilder(row).
		WithAmount(decimal.NewFromInt(250)).
		WithBillOn(moved).
		Build())
	require.NoError(t, err)
	require.True(t, decimal.NewFromInt(250).Equal(updated.Amount))
	require.Equal(t, moved, updated.BillOn)
	require.Equal(t, types.InstalmentStatusScheduled, updated.InstalmentStatus)
	require.Equal(t, "user_1", updated.UpdatedBy)

	cleared := billingterm.NewInstalmentBuilder(updated).Build()
	cleared.TriggeredAt = nil
	updated, err = f.store.Update(f.ctx, cleared)
	require.NoError(t, err)
	require.Nil(t, updated.TriggeredAt)
	require.Equal(t, row.Description, updated.Description, "fields outside the update set are kept")

	_, err = f.store.Update(types.SetEnvironmentID(f.ctx, "env_2"), updated)
	require.Error(t, err)
	require.True(t, ierr.IsNotFound(err))

	missing := billingterm.NewInstalmentBuilder(updated).Build()
	missing.ID = "binst_missing"
	_, err = f.store.Update(f.ctx, missing)
	require.Error(t, err)
	require.True(t, ierr.IsNotFound(err))

	cancelled, err := f.store.Update(f.ctx, billingterm.NewInstalmentBuilder(updated).WithStatus(types.InstalmentStatusCancelled).Build())
	require.NoError(t, err)
	require.Equal(t, types.InstalmentStatusCancelled, cancelled.InstalmentStatus)
}

func TestInMemoryBillingInstalmentStore_UpdateOnlyScheduled(t *testing.T) {
	for _, status := range []types.InstalmentStatus{types.InstalmentStatusInvoiced, types.InstalmentStatusCancelled} {
		t.Run(string(status), func(t *testing.T) {
			f := newInstalmentFixture(t)
			row := f.instalment("binst_a", "cmt_1", status, f.start)
			f.seed(t, row)
			before := f.get(t, row.ID)

			_, err := f.store.Update(f.ctx, billingterm.NewInstalmentBuilder(row).
				WithAmount(decimal.NewFromInt(250)).
				WithBillOn(f.start.AddDate(0, 0, 7)).
				WithStatus(types.InstalmentStatusScheduled).
				Build())
			require.Error(t, err)
			require.True(t, ierr.IsInvalidOperation(err), "got %v", err)
			require.Equal(t, before, f.get(t, row.ID), "the row is left as it was")
		})
	}
}
