//go:build pgintegration

package ent

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/billingterm"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type billingTermFixture struct {
	ctx         context.Context
	runID       string
	terms       billingterm.TermRepository
	instalments billingterm.InstalmentRepository
}

// newBillingTermFixture scopes a test to its own environment so reruns never collide.
func newBillingTermFixture(t *testing.T) *billingTermFixture {
	t.Helper()

	cfg, err := config.NewConfig()
	require.NoError(t, err)
	log, err := logger.NewLogger(cfg)
	require.NoError(t, err)
	clients, err := postgres.NewEntClients(cfg, log)
	require.NoError(t, err)
	client := postgres.NewClient(clients, log, nil)

	runID := types.GenerateUUID()
	ctx := types.SetUserID(
		types.SetEnvironmentID(types.SetTenantID(context.Background(), types.DefaultTenantID), "env_"+runID),
		"user_test",
	)

	return &billingTermFixture{
		ctx:         ctx,
		runID:       runID,
		terms:       NewBillingTermRepository(client, log),
		instalments: NewBillingInstalmentRepository(client, log),
	}
}

func (f *billingTermFixture) instalment(ctx context.Context, id, ownerID string, status types.InstalmentStatus, billOn time.Time) *billingterm.Instalment {
	return &billingterm.Instalment{
		ID:                 "binst_" + f.runID + "_" + id,
		OwnerType:          types.BillingTermOwnerCommitment,
		OwnerID:            ownerID,
		CustomerID:         "cust_" + f.runID,
		SubscriptionID:     lo.ToPtr("subs_" + f.runID),
		Currency:           "usd",
		Kind:               types.InstalmentKindInstalment,
		BillTrigger:        types.InstalmentBillTriggerDate,
		BillOn:             billOn,
		Amount:             decimal.NewFromInt(100),
		InvoiceLabel:       "Instalment " + id,
		ServicePeriodStart: billOn,
		ServicePeriodEnd:   billOn.AddDate(0, 1, 0),
		Delivery:           types.InstalmentDeliverySubscriptionInvoice,
		InstalmentStatus:   status,
		EnvironmentID:      types.GetEnvironmentID(ctx),
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
}

func instalmentIDs(rows []*billingterm.Instalment) []string {
	return lo.Map(rows, func(in *billingterm.Instalment, _ int) string { return in.ID })
}

// Run with:
//
//	go test -tags pgintegration ./internal/repository/ent -run 'TestInstalment|TestTermArchive' -v
func TestInstalmentClaim_OnlyScheduledRows(t *testing.T) {
	f := newBillingTermFixture(t)
	owner := "cmt_" + f.runID
	invoiceID := "inv_" + f.runID + "_1"
	start := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)

	first := f.instalment(f.ctx, "a", owner, types.InstalmentStatusScheduled, start)
	second := f.instalment(f.ctx, "b", owner, types.InstalmentStatusScheduled, start.AddDate(0, 1, 0))
	cancelled := f.instalment(f.ctx, "c", owner, types.InstalmentStatusCancelled, start.AddDate(0, 2, 0))
	_, err := f.instalments.CreateMany(f.ctx, []*billingterm.Instalment{first, second, cancelled})
	require.NoError(t, err)

	invoicedAt := start.Add(36 * time.Hour)
	claimed, err := f.instalments.Claim(f.ctx, invoiceID, invoicedAt, []string{first.ID, second.ID, cancelled.ID})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{first.ID, second.ID}, instalmentIDs(claimed))
	for _, in := range claimed {
		require.Equal(t, types.InstalmentStatusInvoiced, in.InstalmentStatus)
		require.Equal(t, invoiceID, lo.FromPtr(in.InvoiceID))
		require.True(t, invoicedAt.Equal(lo.FromPtr(in.InvoicedAt)))
	}

	again, err := f.instalments.Claim(f.ctx, invoiceID, time.Now(), []string{first.ID, second.ID, cancelled.ID})
	require.NoError(t, err)
	require.Empty(t, again)

	untouched, err := f.instalments.Get(f.ctx, cancelled.ID)
	require.NoError(t, err)
	require.Equal(t, types.InstalmentStatusCancelled, untouched.InstalmentStatus)
	require.Nil(t, untouched.InvoiceID)
	require.Nil(t, untouched.InvoicedAt)
}

func TestInstalmentReschedule_OnlyThatInvoice(t *testing.T) {
	f := newBillingTermFixture(t)
	owner := "cmt_" + f.runID
	firstInvoice := "inv_" + f.runID + "_1"
	secondInvoice := "inv_" + f.runID + "_2"
	start := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)

	onFirst := f.instalment(f.ctx, "a", owner, types.InstalmentStatusInvoiced, start)
	onFirst.InvoiceID, onFirst.InvoicedAt = &firstInvoice, &start
	alsoOnFirst := f.instalment(f.ctx, "b", owner, types.InstalmentStatusInvoiced, start.AddDate(0, 1, 0))
	alsoOnFirst.InvoiceID, alsoOnFirst.InvoicedAt = &firstInvoice, &start
	onSecond := f.instalment(f.ctx, "c", owner, types.InstalmentStatusInvoiced, start.AddDate(0, 2, 0))
	onSecond.InvoiceID, onSecond.InvoicedAt = &secondInvoice, &start
	cancelled := f.instalment(f.ctx, "d", owner, types.InstalmentStatusCancelled, start.AddDate(0, 3, 0))
	cancelled.InvoiceID = &firstInvoice
	_, err := f.instalments.CreateMany(f.ctx, []*billingterm.Instalment{onFirst, alsoOnFirst, onSecond, cancelled})
	require.NoError(t, err)

	rescheduled, err := f.instalments.Reschedule(f.ctx, firstInvoice)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{onFirst.ID, alsoOnFirst.ID}, instalmentIDs(rescheduled))
	for _, in := range rescheduled {
		require.Equal(t, types.InstalmentStatusScheduled, in.InstalmentStatus)
		require.Nil(t, in.InvoiceID)
		require.Nil(t, in.InvoicedAt)
	}

	other, err := f.instalments.Get(f.ctx, onSecond.ID)
	require.NoError(t, err)
	require.Equal(t, types.InstalmentStatusInvoiced, other.InstalmentStatus)
	require.Equal(t, secondInvoice, lo.FromPtr(other.InvoiceID))
	require.True(t, start.Equal(lo.FromPtr(other.InvoicedAt)))

	stillCancelled, err := f.instalments.Get(f.ctx, cancelled.ID)
	require.NoError(t, err)
	require.Equal(t, types.InstalmentStatusCancelled, stillCancelled.InstalmentStatus)
	require.Equal(t, firstInvoice, lo.FromPtr(stillCancelled.InvoiceID))
}

func TestInstalmentCancelInvoiced_OnlyGivenRowsOnThatInvoice(t *testing.T) {
	f := newBillingTermFixture(t)
	owner := "cmt_" + f.runID
	firstInvoice := "inv_" + f.runID + "_1"
	secondInvoice := "inv_" + f.runID + "_2"
	start := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)

	given := f.instalment(f.ctx, "a", owner, types.InstalmentStatusInvoiced, start)
	given.InvoiceID, given.InvoicedAt = &firstInvoice, &start
	kept := f.instalment(f.ctx, "b", owner, types.InstalmentStatusInvoiced, start.AddDate(0, 1, 0))
	kept.InvoiceID, kept.InvoicedAt = &firstInvoice, &start
	onSecond := f.instalment(f.ctx, "c", owner, types.InstalmentStatusInvoiced, start.AddDate(0, 2, 0))
	onSecond.InvoiceID, onSecond.InvoicedAt = &secondInvoice, &start
	scheduled := f.instalment(f.ctx, "d", owner, types.InstalmentStatusScheduled, start.AddDate(0, 3, 0))
	_, err := f.instalments.CreateMany(f.ctx, []*billingterm.Instalment{given, kept, onSecond, scheduled})
	require.NoError(t, err)

	cancelled, err := f.instalments.CancelInvoiced(f.ctx, firstInvoice, []string{given.ID, onSecond.ID, scheduled.ID})
	require.NoError(t, err)
	require.Equal(t, []string{given.ID}, instalmentIDs(cancelled))
	require.Equal(t, types.InstalmentStatusCancelled, cancelled[0].InstalmentStatus)
	require.Nil(t, cancelled[0].InvoiceID)
	require.Nil(t, cancelled[0].InvoicedAt)

	stillInvoiced, err := f.instalments.Get(f.ctx, kept.ID)
	require.NoError(t, err)
	require.Equal(t, types.InstalmentStatusInvoiced, stillInvoiced.InstalmentStatus)
	require.Equal(t, firstInvoice, lo.FromPtr(stillInvoiced.InvoiceID))

	other, err := f.instalments.Get(f.ctx, onSecond.ID)
	require.NoError(t, err)
	require.Equal(t, secondInvoice, lo.FromPtr(other.InvoiceID))

	untouched, err := f.instalments.Get(f.ctx, scheduled.ID)
	require.NoError(t, err)
	require.Equal(t, types.InstalmentStatusScheduled, untouched.InstalmentStatus)

	none, err := f.instalments.CancelInvoiced(f.ctx, firstInvoice, nil)
	require.NoError(t, err)
	require.Empty(t, none)
}

func TestInstalmentCancelScheduled_OnlyGivenScheduledRows(t *testing.T) {
	f := newBillingTermFixture(t)
	owner := "cmt_" + f.runID
	invoiceID := "inv_" + f.runID + "_1"
	start := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)

	given := f.instalment(f.ctx, "a", owner, types.InstalmentStatusScheduled, start)
	kept := f.instalment(f.ctx, "b", owner, types.InstalmentStatusScheduled, start.AddDate(0, 1, 0))
	invoiced := f.instalment(f.ctx, "c", owner, types.InstalmentStatusInvoiced, start.AddDate(0, 2, 0))
	invoiced.InvoiceID, invoiced.InvoicedAt = &invoiceID, &start
	_, err := f.instalments.CreateMany(f.ctx, []*billingterm.Instalment{given, kept, invoiced})
	require.NoError(t, err)

	cancelled, err := f.instalments.CancelScheduled(f.ctx, []string{given.ID, invoiced.ID})
	require.NoError(t, err)
	require.Equal(t, []string{given.ID}, instalmentIDs(cancelled))
	require.Equal(t, types.InstalmentStatusCancelled, cancelled[0].InstalmentStatus)

	stillScheduled, err := f.instalments.Get(f.ctx, kept.ID)
	require.NoError(t, err)
	require.Equal(t, types.InstalmentStatusScheduled, stillScheduled.InstalmentStatus)

	stillInvoiced, err := f.instalments.Get(f.ctx, invoiced.ID)
	require.NoError(t, err)
	require.Equal(t, types.InstalmentStatusInvoiced, stillInvoiced.InstalmentStatus)
	require.Equal(t, invoiceID, lo.FromPtr(stillInvoiced.InvoiceID))

	again, err := f.instalments.CancelScheduled(f.ctx, []string{given.ID})
	require.NoError(t, err)
	require.Empty(t, again)

	none, err := f.instalments.CancelScheduled(f.ctx, nil)
	require.NoError(t, err)
	require.Empty(t, none)
}

func TestInstalmentReinstate_OnlyGivenCancelledRows(t *testing.T) {
	f := newBillingTermFixture(t)
	owner := "cmt_" + f.runID
	start := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)

	given := f.instalment(f.ctx, "a", owner, types.InstalmentStatusCancelled, start)
	kept := f.instalment(f.ctx, "b", owner, types.InstalmentStatusCancelled, start.AddDate(0, 1, 0))
	scheduled := f.instalment(f.ctx, "c", owner, types.InstalmentStatusScheduled, start.AddDate(0, 2, 0))
	_, err := f.instalments.CreateMany(f.ctx, []*billingterm.Instalment{given, kept, scheduled})
	require.NoError(t, err)

	reinstated, err := f.instalments.Reinstate(f.ctx, []string{given.ID, scheduled.ID})
	require.NoError(t, err)
	require.Equal(t, []string{given.ID}, instalmentIDs(reinstated))
	require.Equal(t, types.InstalmentStatusScheduled, reinstated[0].InstalmentStatus)
	require.Nil(t, reinstated[0].InvoiceID)

	stillCancelled, err := f.instalments.Get(f.ctx, kept.ID)
	require.NoError(t, err)
	require.Equal(t, types.InstalmentStatusCancelled, stillCancelled.InstalmentStatus)

	none, err := f.instalments.Reinstate(f.ctx, nil)
	require.NoError(t, err)
	require.Empty(t, none)
}

func TestInstalmentListInvoiced_ByInvoicedDate(t *testing.T) {
	f := newBillingTermFixture(t)
	owner, otherOwner := "cmt_"+f.runID, "cmt_"+f.runID+"_2"
	jan := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	feb := jan.AddDate(0, 1, 0)
	invoiced := func(id, ownerID string, at time.Time) *billingterm.Instalment {
		in := f.instalment(f.ctx, id, ownerID, types.InstalmentStatusInvoiced, jan)
		in.InvoiceID, in.InvoicedAt = lo.ToPtr("inv_"+f.runID+"_"+id), lo.ToPtr(at)
		return in
	}

	late := invoiced("late", owner, jan.AddDate(0, 0, 20))
	early := invoiced("early", owner, jan)
	atEnd := invoiced("at_end", owner, feb)
	other := invoiced("other", otherOwner, jan.Add(time.Hour))
	scheduled := f.instalment(f.ctx, "scheduled", owner, types.InstalmentStatusScheduled, jan)
	_, err := f.instalments.CreateMany(f.ctx, []*billingterm.Instalment{late, early, atEnd, other, scheduled})
	require.NoError(t, err)

	january := types.Period{Start: jan, End: feb}
	got, err := f.instalments.ListInvoiced(f.ctx, types.BillingTermOwnerCommitment, []string{owner}, january)
	require.NoError(t, err)
	require.Equal(t, []string{early.ID, late.ID}, instalmentIDs(got))

	got, err = f.instalments.ListInvoiced(f.ctx, types.BillingTermOwnerCommitment, []string{owner, otherOwner}, january)
	require.NoError(t, err)
	require.Equal(t, []string{early.ID, other.ID, late.ID}, instalmentIDs(got))
}

func TestInstalmentCancel_ByOwnerAndStatus(t *testing.T) {
	f := newBillingTermFixture(t)
	owner := "cmt_" + f.runID + "_1"
	otherOwner := "cmt_" + f.runID + "_2"
	invoiceID := "inv_" + f.runID + "_1"
	start := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)

	scheduled := f.instalment(f.ctx, "a", owner, types.InstalmentStatusScheduled, start)
	invoiced := f.instalment(f.ctx, "b", owner, types.InstalmentStatusInvoiced, start.AddDate(0, 1, 0))
	invoiced.InvoiceID = &invoiceID
	otherScheduled := f.instalment(f.ctx, "c", otherOwner, types.InstalmentStatusScheduled, start)
	_, err := f.instalments.CreateMany(f.ctx, []*billingterm.Instalment{scheduled, invoiced, otherScheduled})
	require.NoError(t, err)

	cancelled, err := f.instalments.Cancel(f.ctx, types.BillingTermOwnerCommitment, owner, []types.InstalmentStatus{types.InstalmentStatusScheduled})
	require.NoError(t, err)
	require.Equal(t, []string{scheduled.ID}, instalmentIDs(cancelled))
	require.Equal(t, types.InstalmentStatusCancelled, cancelled[0].InstalmentStatus)

	stillInvoiced, err := f.instalments.Get(f.ctx, invoiced.ID)
	require.NoError(t, err)
	require.Equal(t, types.InstalmentStatusInvoiced, stillInvoiced.InstalmentStatus)

	rest, err := f.instalments.Cancel(f.ctx, types.BillingTermOwnerCommitment, owner,
		[]types.InstalmentStatus{types.InstalmentStatusScheduled, types.InstalmentStatusInvoiced})
	require.NoError(t, err)
	require.Equal(t, []string{invoiced.ID}, instalmentIDs(rest))
	require.Equal(t, types.InstalmentStatusCancelled, rest[0].InstalmentStatus)
	require.Equal(t, invoiceID, lo.FromPtr(rest[0].InvoiceID))

	other, err := f.instalments.Get(f.ctx, otherScheduled.ID)
	require.NoError(t, err)
	require.Equal(t, types.InstalmentStatusScheduled, other.InstalmentStatus)

	_, err = f.instalments.Cancel(f.ctx, types.BillingTermOwnerCommitment, owner, nil)
	require.Error(t, err)
	require.True(t, ierr.IsValidation(err))
}

func TestInstalmentUpdate_OnlyScheduledRows(t *testing.T) {
	f := newBillingTermFixture(t)
	owner := "cmt_" + f.runID
	invoiceID := "inv_" + f.runID + "_1"
	start := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)

	scheduled := f.instalment(f.ctx, "a", owner, types.InstalmentStatusScheduled, start)
	invoiced := f.instalment(f.ctx, "b", owner, types.InstalmentStatusInvoiced, start.AddDate(0, 1, 0))
	invoiced.InvoiceID = &invoiceID
	cancelled := f.instalment(f.ctx, "c", owner, types.InstalmentStatusCancelled, start.AddDate(0, 2, 0))
	_, err := f.instalments.CreateMany(f.ctx, []*billingterm.Instalment{scheduled, invoiced, cancelled})
	require.NoError(t, err)

	moved := start.AddDate(0, 0, 7)
	for _, row := range []*billingterm.Instalment{invoiced, cancelled} {
		_, err := f.instalments.Update(f.ctx, billingterm.NewInstalmentBuilder(row).WithBillOn(moved).Build())
		require.Error(t, err)
		require.True(t, ierr.IsInvalidOperation(err), "got %v", err)

		unchanged, err := f.instalments.Get(f.ctx, row.ID)
		require.NoError(t, err)
		require.Equal(t, row.InstalmentStatus, unchanged.InstalmentStatus)
		require.True(t, row.BillOn.Equal(unchanged.BillOn))
		require.Equal(t, row.InvoiceID, unchanged.InvoiceID)
	}

	missing := billingterm.NewInstalmentBuilder(scheduled).Build()
	missing.ID = "binst_" + f.runID + "_missing"
	_, err = f.instalments.Update(f.ctx, missing)
	require.Error(t, err)
	require.True(t, ierr.IsNotFound(err), "got %v", err)

	changed := billingterm.NewInstalmentBuilder(scheduled).WithBillOn(moved).WithTriggeredAt(moved).Build()
	changed.Amount = decimal.NewFromInt(50)
	changed.InstalmentStatus = types.InstalmentStatusCancelled
	updated, err := f.instalments.Update(f.ctx, changed)
	require.NoError(t, err)
	require.True(t, moved.Equal(updated.BillOn))
	require.True(t, moved.Equal(lo.FromPtr(updated.TriggeredAt)))
	require.Equal(t, types.InstalmentStatusScheduled, updated.InstalmentStatus, "only bill_on and triggered_at are written")
	require.True(t, scheduled.Amount.Equal(updated.Amount))
}

func TestInstalmentList_FiltersAndOrder(t *testing.T) {
	f := newBillingTermFixture(t)
	owner := "cmt_" + f.runID
	firstInvoice := "inv_" + f.runID + "_1"
	start := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	nextDay := start.Add(24 * time.Hour)
	dayAfter := start.Add(48 * time.Hour)

	first := f.instalment(f.ctx, "a", owner, types.InstalmentStatusScheduled, start)
	second := f.instalment(f.ctx, "b", owner, types.InstalmentStatusInvoiced, nextDay)
	second.InvoiceID = &firstInvoice
	third := f.instalment(f.ctx, "c", owner, types.InstalmentStatusScheduled, nextDay)
	third.SubscriptionID = lo.ToPtr("subs_" + f.runID + "_other")
	third.Delivery = types.InstalmentDeliverySeparateInvoice
	fourth := f.instalment(f.ctx, "d", owner, types.InstalmentStatusScheduled, dayAfter)
	fourth.Delivery = types.InstalmentDeliverySeparateInvoice

	otherCtx := types.SetEnvironmentID(f.ctx, "env_"+f.runID+"_other")
	elsewhere := f.instalment(otherCtx, "e", owner, types.InstalmentStatusScheduled, start)

	_, err := f.instalments.CreateMany(f.ctx, []*billingterm.Instalment{fourth, third, first, second})
	require.NoError(t, err)
	_, err = f.instalments.CreateMany(otherCtx, []*billingterm.Instalment{elsewhere})
	require.NoError(t, err)

	tests := []struct {
		name   string
		filter billingterm.InstalmentFilter
		want   []string
	}{
		{
			name:   "owner only, ordered by bill_on then id",
			filter: billingterm.InstalmentFilter{OwnerType: types.BillingTermOwnerCommitment, OwnerIDs: []string{owner}},
			want:   []string{first.ID, second.ID, third.ID, fourth.ID},
		},
		{
			name: "bill_on bound is inclusive",
			filter: billingterm.InstalmentFilter{
				OwnerType:      types.BillingTermOwnerCommitment,
				OwnerIDs:       []string{owner},
				BillOnOrBefore: &nextDay,
			},
			want: []string{first.ID, second.ID, third.ID},
		},
		{
			name: "status and bill_on bound",
			filter: billingterm.InstalmentFilter{
				OwnerIDs:       []string{owner},
				Statuses:       []types.InstalmentStatus{types.InstalmentStatusScheduled},
				BillOnOrBefore: &nextDay,
			},
			want: []string{first.ID, third.ID},
		},
		{
			name: "delivery",
			filter: billingterm.InstalmentFilter{
				OwnerIDs:   []string{owner},
				Deliveries: []types.InstalmentDelivery{types.InstalmentDeliverySeparateInvoice},
			},
			want: []string{third.ID, fourth.ID},
		},
		{
			name:   "subscription",
			filter: billingterm.InstalmentFilter{SubscriptionIDs: []string{*third.SubscriptionID}},
			want:   []string{third.ID},
		},
		{
			name:   "invoice",
			filter: billingterm.InstalmentFilter{InvoiceIDs: []string{firstInvoice}},
			want:   []string{second.ID},
		},
		{
			name:   "ids",
			filter: billingterm.InstalmentFilter{IDs: []string{fourth.ID, first.ID}},
			want:   []string{first.ID, fourth.ID},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := f.instalments.List(f.ctx, &tt.filter)
			require.NoError(t, err)
			require.Equal(t, tt.want, instalmentIDs(got))
		})
	}

	t.Run("other environment is isolated", func(t *testing.T) {
		filter := &billingterm.InstalmentFilter{OwnerIDs: []string{owner}}

		got, err := f.instalments.List(otherCtx, filter)
		require.NoError(t, err)
		require.Equal(t, []string{elsewhere.ID}, instalmentIDs(got))

		_, err = f.instalments.Get(f.ctx, elsewhere.ID)
		require.Error(t, err)
		require.True(t, ierr.IsNotFound(err))
	})
}

func TestTermArchive_HidesArchived(t *testing.T) {
	f := newBillingTermFixture(t)
	owner := "cmt_" + f.runID + "_1"
	otherOwner := "cmt_" + f.runID + "_2"
	startDate := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)

	newTerm := func(id, ownerID string) *billingterm.Term {
		return &billingterm.Term{
			ID:             "bterm_" + f.runID + "_" + id,
			OwnerType:      types.BillingTermOwnerCommitment,
			OwnerID:        ownerID,
			Kind:           types.BillingTermKindRecurring,
			ValueType:      types.BillingTermValueFixed,
			Value:          lo.ToPtr(decimal.NewFromInt(1200)),
			StartDate:      &startDate,
			PeriodCount:    lo.ToPtr(12),
			InvoiceCadence: lo.ToPtr(types.InvoiceCadenceArrear),
			Split:          lo.ToPtr(types.BillingTermSplitEven),
			EnvironmentID:  types.GetEnvironmentID(f.ctx),
			BaseModel:      types.GetDefaultBaseModel(f.ctx),
		}
	}

	archivable := []*billingterm.Term{newTerm("a", owner), newTerm("b", owner)}
	kept := newTerm("c", otherOwner)
	created, err := f.terms.CreateMany(f.ctx, append(archivable, kept))
	require.NoError(t, err)
	require.Len(t, created, 3)

	listed, err := f.terms.List(f.ctx, types.BillingTermOwnerCommitment, owner)
	require.NoError(t, err)
	require.Len(t, listed, 2)
	require.Equal(t, archivable[0].ID, listed[0].ID)
	require.True(t, archivable[0].Value.Equal(lo.FromPtr(listed[0].Value)))

	archived, err := f.terms.Archive(f.ctx, types.BillingTermOwnerCommitment, owner)
	require.NoError(t, err)
	require.Len(t, archived, 2)
	for _, term := range archived {
		require.Equal(t, types.StatusArchived, term.Status)
	}

	listed, err = f.terms.List(f.ctx, types.BillingTermOwnerCommitment, owner)
	require.NoError(t, err)
	require.Empty(t, listed)

	listed, err = f.terms.List(f.ctx, types.BillingTermOwnerCommitment, otherOwner)
	require.NoError(t, err)
	require.Len(t, listed, 1)

	again, err := f.terms.Archive(f.ctx, types.BillingTermOwnerCommitment, owner)
	require.NoError(t, err)
	require.Empty(t, again)
}
