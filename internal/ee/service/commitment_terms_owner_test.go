package service

import (
	"context"
	"math"
	"testing"
	"time"

	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	core "github.com/flexprice/flexprice/internal/ee/service/billingterms"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type commitmentTermsFixture struct {
	ctx         context.Context
	owner       domain.OwnerRef
	commitment  *commitment.Commitment
	sub         *subscription.Subscription
	commitments *testutil.InMemoryCommitmentStore
	subs        *testutil.InMemorySubscriptionStore
	adapter     domain.OwnerAdapter
}

func newCommitmentTermsFixture(t *testing.T) *commitmentTermsFixture {
	t.Helper()
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), "tenant_terms"), "env_terms")
	start := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	c := &commitment.Commitment{
		ID: "comm_terms", SubscriptionID: "sub_terms", CustomerID: "customer_terms", Name: "Annual commitment", InvoiceLabel: "Contract payment",
		Currency: "usd", Type: types.COMMITMENT_TYPE_AMOUNT, Value: decimal.NewFromInt(100), Measurement: types.CommitmentMeasurementBillingPeriod,
		Binding: true, CollectionMethod: lo.ToPtr(types.CommitmentCollectionBillingTerms), TotalAmount: lo.ToPtr(decimal.NewFromInt(1200)),
		StartDate: start.AddDate(0, 0, 14), EndDate: start.AddDate(0, 9, 14), Timezone: "UTC", CommitmentStatus: types.CommitmentStatusActive,
		EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx),
	}
	sub := &subscription.Subscription{
		ID: c.SubscriptionID, CustomerID: c.CustomerID, InvoicingCustomerID: lo.ToPtr("customer_invoice"), Currency: "usd",
		SubscriptionStatus: types.SubscriptionStatusActive, PauseStatus: types.PauseStatusNone,
		StartDate: start, EndDate: lo.ToPtr(start.AddDate(1, 0, 0)), BillingAnchor: start,
		BillingCadence: types.BILLING_CADENCE_RECURRING, BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1, Timezone: "UTC",
		EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx),
	}
	commitments, subs := testutil.NewInMemoryCommitmentStore(), testutil.NewInMemorySubscriptionStore()
	_, err := commitments.Create(ctx, c)
	require.NoError(t, err)
	require.NoError(t, subs.Create(ctx, sub))
	return &commitmentTermsFixture{ctx: ctx, owner: domain.OwnerRef{Type: types.BillingTermOwnerCommitment, ID: c.ID}, commitment: c, sub: sub, commitments: commitments, subs: subs, adapter: NewCommitmentTermsOwner(commitments, subs)}
}

func TestCommitmentTermsOwner_Snapshot(t *testing.T) {
	for _, delivery := range []*string{nil, lo.ToPtr(string(types.BillingTermSeparateInvoice))} {
		name := "subscription delivery default"
		if delivery != nil {
			name = *delivery
		}
		t.Run(name, func(t *testing.T) {
			f := newCommitmentTermsFixture(t)
			f.commitment.InstalmentInvoicing = delivery
			got, err := f.adapter.Snapshot(f.ctx, f.owner)
			require.NoError(t, err)
			require.Equal(t, f.owner, got.Owner)
			require.Equal(t, "tenant_terms", got.TenantID)
			require.Equal(t, "env_terms", got.EnvironmentID)
			require.Equal(t, "customer_invoice", got.CustomerID)
			require.Equal(t, f.sub.ID, *got.SubscriptionID)
			require.Equal(t, []domain.EligibilityScope{{Type: "subscription", ID: f.sub.ID}}, got.EligibilityScopes)
			require.True(t, got.OriginalTotal.Equal(decimal.NewFromInt(1200)))
			require.True(t, got.CollectionTarget.Equal(got.OriginalTotal))
			require.True(t, got.AssignedAmount.IsZero())
			require.Zero(t, got.RetainedItemCount)
			require.Equal(t, f.commitment.StartDate, got.StartDate)
			require.Equal(t, f.commitment.EndDate, got.EndDate)
			require.Equal(t, f.sub.BillingAnchor, got.CalendarAnchor)
			require.Equal(t, f.sub.BillingPeriod, got.DefaultInterval)
			require.Equal(t, f.sub.BillingPeriodCount, got.DefaultIntervalCount)
			require.Equal(t, "Contract payment", got.Description)
			require.True(t, got.CanEdit)
			require.True(t, got.CanCollect)
			require.Empty(t, got.DeferredReason)
			wantDelivery := types.BillingTermSubscriptionInvoice
			if delivery != nil {
				wantDelivery = types.BillingTermDelivery(*delivery)
			}
			require.Equal(t, wantDelivery, got.Delivery)
			got.EligibilityScopes[0].ID = "changed"
			fresh, err := f.adapter.Snapshot(f.ctx, f.owner)
			require.NoError(t, err)
			require.Equal(t, []domain.EligibilityScope{{Type: "subscription", ID: f.sub.ID}}, fresh.EligibilityScopes)
		})
	}
}

type nilCommitmentTermsContext struct{ context.Context }

func TestCommitmentTermsOwner_Context(t *testing.T) {
	f := newCommitmentTermsFixture(t)
	for _, ctx := range []context.Context{nil, (*nilCommitmentTermsContext)(nil)} {
		_, err := f.adapter.Snapshot(ctx, f.owner)
		require.Error(t, err)
	}
	ctx, cancel := context.WithCancel(f.ctx)
	cancel()
	_, err := f.adapter.Snapshot(ctx, f.owner)
	require.ErrorIs(t, err, context.Canceled)
	var missing *commitmentTermsOwner
	_, err = missing.Snapshot(f.ctx, f.owner)
	require.Error(t, err)
}

func TestCommitmentTermsOwner_CollectionDeferral(t *testing.T) {
	tests := []struct {
		name   string
		status types.SubscriptionStatus
		pause  types.PauseStatus
		reason string
	}{
		{"active", types.SubscriptionStatusActive, types.PauseStatusNone, ""},
		{"paused", types.SubscriptionStatusPaused, types.PauseStatusActive, "subscription_paused"},
		{"active pause", types.SubscriptionStatusActive, types.PauseStatusActive, "subscription_paused"},
		{"scheduled pause", types.SubscriptionStatusActive, types.PauseStatusScheduled, ""},
		{"trialing", types.SubscriptionStatusTrialing, types.PauseStatusNone, "subscription_trialing"},
		{"incomplete", types.SubscriptionStatusIncomplete, types.PauseStatusNone, "subscription_pending"},
		{"draft", types.SubscriptionStatusDraft, types.PauseStatusNone, "subscription_pending"},
		{"pause takes precedence", types.SubscriptionStatusTrialing, types.PauseStatusActive, "subscription_paused"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newCommitmentTermsFixture(t)
			f.sub.SubscriptionStatus, f.sub.PauseStatus = tt.status, tt.pause
			f.sub.TrialEnd = lo.ToPtr(time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC))
			for _, delivery := range []types.BillingTermDelivery{types.BillingTermSubscriptionInvoice, types.BillingTermSeparateInvoice} {
				f.commitment.InstalmentInvoicing = lo.ToPtr(string(delivery))
				got, err := f.adapter.Snapshot(f.ctx, f.owner)
				require.NoError(t, err)
				require.True(t, got.CanEdit)
				require.Equal(t, tt.reason == "", got.CanCollect)
				require.Equal(t, tt.reason, got.DeferredReason)
			}
		})
	}
}

func TestCommitmentTermsOwner_RejectsInvalidOwners(t *testing.T) {
	tests := []struct {
		name   string
		change func(*commitmentTermsFixture)
	}{
		{"missing scope", func(f *commitmentTermsFixture) { f.ctx = context.Background() }},
		{"wrong owner type", func(f *commitmentTermsFixture) { f.owner.Type = "fixed_fee" }},
		{"wrong owner ID", func(f *commitmentTermsFixture) { f.owner.ID = "comm_other" }},
		{"foreign tenant", func(f *commitmentTermsFixture) { f.ctx = types.SetTenantID(f.ctx, "other") }},
		{"foreign environment", func(f *commitmentTermsFixture) { f.ctx = types.SetEnvironmentID(f.ctx, "other") }},
		{"archived owner", func(f *commitmentTermsFixture) { f.commitment.Status = types.StatusArchived }},
		{"nonbinding", func(f *commitmentTermsFixture) { f.commitment.Binding = false }},
		{"legacy collection", func(f *commitmentTermsFixture) { f.commitment.CollectionMethod = nil }},
		{"period collection", func(f *commitmentTermsFixture) {
			f.commitment.CollectionMethod = lo.ToPtr(types.CommitmentCollectionPerPeriod)
		}},
		{"missing total", func(f *commitmentTermsFixture) { f.commitment.TotalAmount = nil }},
		{"zero total", func(f *commitmentTermsFixture) { f.commitment.TotalAmount = lo.ToPtr(decimal.Zero) }},
		{"negative total", func(f *commitmentTermsFixture) { f.commitment.TotalAmount = lo.ToPtr(decimal.NewFromInt(-1)) }},
		{"unrounded total", func(f *commitmentTermsFixture) {
			f.commitment.TotalAmount = lo.ToPtr(decimal.RequireFromString("10.001"))
		}},
		{"storage overflow", func(f *commitmentTermsFixture) { f.commitment.TotalAmount = lo.ToPtr(decimal.New(1, 12)) }},
		{"extreme total exponent", func(f *commitmentTermsFixture) { f.commitment.TotalAmount = lo.ToPtr(decimal.New(1, math.MinInt32)) }},
		{"invalid currency", func(f *commitmentTermsFixture) { f.commitment.Currency = "invalid" }},
		{"currency mismatch", func(f *commitmentTermsFixture) { f.sub.Currency = "eur" }},
		{"invalid timezone", func(f *commitmentTermsFixture) { f.commitment.Timezone = "Local" }},
		{"missing subscription timezone", func(f *commitmentTermsFixture) { f.sub.Timezone = "" }},
		{"invalid owner dates", func(f *commitmentTermsFixture) { f.commitment.EndDate = f.commitment.StartDate }},
		{"missing anchor", func(f *commitmentTermsFixture) { f.sub.BillingAnchor = time.Time{} }},
		{"before subscription", func(f *commitmentTermsFixture) { f.commitment.StartDate = f.sub.StartDate.AddDate(0, 0, -1) }},
		{"after subscription", func(f *commitmentTermsFixture) { f.commitment.EndDate = f.sub.EndDate.AddDate(0, 0, 1) }},
		{"foreign subscription tenant", func(f *commitmentTermsFixture) { f.sub.TenantID = "other" }},
		{"foreign subscription environment", func(f *commitmentTermsFixture) { f.sub.EnvironmentID = "other" }},
		{"archived subscription", func(f *commitmentTermsFixture) { f.sub.Status = types.StatusArchived }},
		{"customer mismatch", func(f *commitmentTermsFixture) { f.sub.CustomerID = "other" }},
		{"invalid delivery", func(f *commitmentTermsFixture) { f.commitment.InstalmentInvoicing = lo.ToPtr("immediate") }},
		{"nonrecurring subscription", func(f *commitmentTermsFixture) { f.sub.BillingCadence = "" }},
		{"invalid period count", func(f *commitmentTermsFixture) { f.sub.BillingPeriodCount = 0 }},
		{"unknown lifecycle", func(f *commitmentTermsFixture) { f.sub.SubscriptionStatus = "unknown" }},
		{"malformed protected settlement", func(f *commitmentTermsFixture) { f.commitment.Metadata = types.Metadata{commitmentSettlementKey: "{}"} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newCommitmentTermsFixture(t)
			tt.change(f)
			got, err := f.adapter.Snapshot(f.ctx, f.owner)
			require.Error(t, err)
			require.Nil(t, got)
		})
	}
	var adapter *commitmentTermsOwner
	_, err := adapter.Snapshot(context.Background(), domain.OwnerRef{})
	require.Error(t, err)
	_, err = NewCommitmentTermsOwner(nil, nil).Snapshot(types.SetEnvironmentID(types.SetTenantID(context.Background(), "tenant"), "env"), domain.OwnerRef{Type: types.BillingTermOwnerCommitment, ID: "comm"})
	require.Error(t, err)
}

func TestCommitmentTermsOwner_Calendar(t *testing.T) {
	tests := []struct {
		name       string
		start, end string
		interval   types.BillingPeriod
		count      int
		want       string
	}{
		{"partial owner start", "2027-01-15", "2027-10-15", types.BILLING_PERIOD_MONTHLY, 1, "2027-02-01"},
		{"partial owner end", "2027-10-01", "2027-10-15", types.BILLING_PERIOD_MONTHLY, 1, "2027-10-15"},
		{"whole cadence multiple", "2027-01-15", "2027-10-15", types.BILLING_PERIOD_QUARTER, 1, "2027-04-01"},
		{"interior start misaligned", "2027-02-02", "2027-10-15", types.BILLING_PERIOD_MONTHLY, 1, ""},
		{"interior end misaligned", "2027-02-01", "2027-10-02", types.BILLING_PERIOD_MONTHLY, 1, ""},
		{"shorter cadence", "2027-01-15", "2027-10-15", types.BILLING_PERIOD_WEEKLY, 1, ""},
		{"large count clips to owner edge", "2027-01-15", "2027-10-15", types.BILLING_PERIOD_MONTHLY, math.MaxInt32, "2027-10-15"},
	}
	parse := func(value string) time.Time {
		date, err := time.Parse("2006-01-02", value)
		require.NoError(t, err)
		return date
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newCommitmentTermsFixture(t)
			got, err := f.adapter.Snapshot(f.ctx, f.owner)
			require.NoError(t, err)
			next, err := got.Calendar.Next(parse(tt.start), parse(tt.end), tt.interval, tt.count)
			if tt.want == "" {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, parse(tt.want), next)
		})
	}
	t.Run("shortened owner term expands partial steps", func(t *testing.T) {
		f := newCommitmentTermsFixture(t)
		f.commitment.EndDate = parse("2027-02-15")
		snapshot, err := f.adapter.Snapshot(f.ctx, f.owner)
		require.NoError(t, err)
		plan, err := core.NewPlanner().Plan(f.ctx, core.PlanRequest{Owner: *snapshot, Calendar: snapshot.Calendar, Rows: []core.RuleInput{{Kind: types.BillingTermRecurring, ValueType: types.BillingTermRemainder, BillAt: lo.ToPtr(types.BillingTermArrear)}}})
		require.NoError(t, err)
		require.Len(t, plan.Items(), 2)
		require.Equal(t, parse("2027-02-01"), plan.Items()[0].ServicePeriodEnd)
		require.Equal(t, parse("2027-02-15"), plan.Items()[1].ServicePeriodEnd)
		require.True(t, plan.Items()[0].Amount.Add(plan.Items()[1].Amount).Equal(snapshot.OriginalTotal))
	})
	t.Run("subscription cadence count", func(t *testing.T) {
		f := newCommitmentTermsFixture(t)
		f.sub.BillingPeriodCount = 2
		snapshot, err := f.adapter.Snapshot(f.ctx, f.owner)
		require.NoError(t, err)
		require.Equal(t, 2, snapshot.DefaultIntervalCount)
		_, err = snapshot.Calendar.Next(snapshot.StartDate, snapshot.EndDate, types.BILLING_PERIOD_MONTHLY, 1)
		require.Error(t, err)
		_, err = snapshot.Calendar.Next(snapshot.StartDate, snapshot.EndDate, types.BILLING_PERIOD_QUARTER, 1)
		require.Error(t, err)
		next, err := snapshot.Calendar.Next(snapshot.StartDate, snapshot.EndDate, types.BILLING_PERIOD_MONTHLY, 4)
		require.NoError(t, err)
		require.Equal(t, parse("2027-05-01"), next)
	})
	t.Run("naturally ended corrections", func(t *testing.T) {
		f := newCommitmentTermsFixture(t)
		f.commitment.CommitmentStatus = types.CommitmentStatusEnded
		snapshot, err := f.adapter.Snapshot(f.ctx, f.owner)
		require.NoError(t, err)
		require.True(t, snapshot.CanEdit)
		_, err = snapshot.Calendar.Next(snapshot.StartDate, snapshot.EndDate, types.BILLING_PERIOD_MONTHLY, 1)
		require.ErrorContains(t, err, "dated once")
	})
}

func TestCommitmentTermsOwner_PauseState(t *testing.T) {
	for _, invalid := range []string{"", "tenant", "environment", "subscription", "missing"} {
		t.Run("pause scope "+invalid, func(t *testing.T) {
			f := newCommitmentTermsFixture(t)
			pause := &subscription.SubscriptionPause{ID: "pause_terms", SubscriptionID: f.sub.ID, PauseStatus: types.PauseStatusScheduled, PauseStart: f.commitment.StartDate, EnvironmentID: f.sub.EnvironmentID, BaseModel: types.GetDefaultBaseModel(f.ctx)}
			require.NoError(t, f.subs.CreatePause(f.ctx, pause))
			f.sub.ActivePauseID, f.sub.PauseStatus = lo.ToPtr(pause.ID), types.PauseStatusScheduled
			switch invalid {
			case "tenant":
				pause.TenantID = "other"
			case "environment":
				pause.EnvironmentID = "other"
			case "subscription":
				pause.SubscriptionID = "other"
			case "missing":
				f.sub.ActivePauseID = lo.ToPtr("missing")
			}
			before, err := f.adapter.Snapshot(f.ctx, f.owner)
			if invalid != "" {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.True(t, before.CanCollect)
			pause.PauseStart = pause.PauseStart.Add(time.Hour)
			after, err := f.adapter.Snapshot(f.ctx, f.owner)
			require.NoError(t, err)
			require.True(t, after.CanCollect)
			require.NotEqual(t, before.LifecycleKey, after.LifecycleKey)
		})
	}
}

func TestCommitmentTermsOwner_LifecycleKey(t *testing.T) {
	for _, tt := range []struct {
		name    string
		change  func(*commitmentTermsFixture)
		changed bool
	}{
		{"stable repeated snapshot", func(*commitmentTermsFixture) {}, false},
		{"unrelated metadata", func(f *commitmentTermsFixture) {
			f.commitment.Metadata = types.Metadata{"note": "updated"}
			f.sub.Metadata = types.Metadata{"note": "updated"}
		}, false},
		{"log timestamps", func(f *commitmentTermsFixture) {
			f.sub.UpdatedAt = f.sub.UpdatedAt.Add(time.Hour)
			f.commitment.UpdatedAt = f.commitment.UpdatedAt.Add(time.Hour)
		}, false},
		{"cadence count", func(f *commitmentTermsFixture) { f.sub.BillingPeriodCount = 2 }, true},
		{"cadence period", func(f *commitmentTermsFixture) { f.sub.BillingPeriod = types.BILLING_PERIOD_QUARTER }, true},
		{"calendar anchor", func(f *commitmentTermsFixture) { f.sub.BillingAnchor = f.sub.BillingAnchor.Add(time.Hour) }, true},
		{"calendar timezone", func(f *commitmentTermsFixture) { f.sub.Timezone = "America/New_York" }, true},
		{"scheduled cancellation", func(f *commitmentTermsFixture) {
			f.sub.CancelAt = lo.ToPtr(f.commitment.EndDate)
			f.sub.CancelAtPeriodEnd = true
		}, true},
		{"trial end", func(f *commitmentTermsFixture) { f.sub.TrialEnd = lo.ToPtr(f.commitment.StartDate) }, true},
		{"paused state", func(f *commitmentTermsFixture) { f.sub.PauseStatus = types.PauseStatusActive }, true},
		{"grouped destination", func(f *commitmentTermsFixture) { f.sub.ParentSubscriptionID = lo.ToPtr("sub_parent") }, true},
		{"naturally ended", func(f *commitmentTermsFixture) { f.commitment.CommitmentStatus = types.CommitmentStatusEnded }, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newCommitmentTermsFixture(t)
			before, err := f.adapter.Snapshot(f.ctx, f.owner)
			require.NoError(t, err)
			tt.change(f)
			after, err := f.adapter.Snapshot(f.ctx, f.owner)
			require.NoError(t, err)
			require.Len(t, before.LifecycleKey, 64)
			require.Equal(t, tt.changed, before.LifecycleKey != after.LifecycleKey)
		})
	}
}
