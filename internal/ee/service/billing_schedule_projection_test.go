package service

import (
	"context"
	"testing"
	"time"

	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	core "github.com/flexprice/flexprice/internal/ee/service/billingterms"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type billingProjectionEngine struct {
	core.Service
	state core.State
}

func (s *billingProjectionEngine) Get(context.Context, domain.OwnerRef) (core.State, error) {
	return s.state, nil
}

func TestBillingScheduleProjection_EligibilityAndArchivedSources(t *testing.T) {
	for _, tc := range []struct {
		name     string
		collect  bool
		gap      bool
		future   bool
		archived bool
		assigned bool
		wantDue  int
	}{
		{name: "eligible", collect: true, wantDue: 1},
		{name: "future", collect: true, future: true},
		{name: "paused"},
		{name: "trial"},
		{name: "gap", collect: true, gap: true},
		{name: "archived unassigned", collect: true, archived: true},
		{name: "archived assigned", archived: true, assigned: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newCommitmentTermsFixture(t)
			now := time.Now().UTC()
			owner := &domain.OwnerSnapshot{Owner: f.owner, TenantID: types.GetTenantID(f.ctx), EnvironmentID: types.GetEnvironmentID(f.ctx), CustomerID: f.sub.GetInvoicingCustomerID(), Currency: "usd", CanCollect: tc.collect}
			item := &domain.ScheduleItem{ID: "schedule", OwnerID: f.owner.ID, OwnerType: f.owner.Type, CustomerID: owner.CustomerID, Currency: owner.Currency, Amount: decimal.NewFromInt(100), BillOn: lo.ToPtr(now.Add(-time.Hour)),
				ScheduleStatus: types.BillingScheduleScheduled, EnvironmentID: owner.EnvironmentID, BaseModel: types.GetDefaultBaseModel(f.ctx)}
			if tc.future {
				item.BillOn = lo.ToPtr(now.Add(time.Hour))
			}
			if tc.archived {
				item.Status = types.StatusArchived
			}
			if tc.assigned {
				item.ScheduleStatus = types.BillingScheduleInvoiced
			}
			params := ServiceParams{BillingTerms: &billingProjectionEngine{state: &billingAPIState{owner: owner, items: []*domain.ScheduleItem{item}, summary: domain.CollectionSummary{RequiresReplan: tc.gap}}}}
			projection, err := projectBillingSchedule(f.ctx, params, []domain.OwnerRef{f.owner}, owner.CustomerID, owner.Currency, now)
			require.NoError(t, err)
			require.Len(t, projection.due, tc.wantDue)
			require.Len(t, projection.assigned, lo.Ternary(tc.assigned, 1, 0))
		})
	}
}
