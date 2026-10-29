package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/api/dto"
	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	core "github.com/flexprice/flexprice/internal/ee/service/billingterms"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type correctionSourceStore struct {
	domain.ExecutionStore
	batchSizes []int
	modify     func([]domain.BindingState) []domain.BindingState
}

func (*correctionSourceStore) InTransaction(context.Context) bool { return false }

func (s *correctionSourceStore) BindingStates(ctx context.Context, items []*domain.ScheduleItem) ([]domain.BindingState, error) {
	s.batchSizes = append(s.batchSizes, len(items))
	states := make([]domain.BindingState, len(items))
	for index, item := range items {
		states[index] = domain.BindingState{ItemID: item.ID, InvoiceID: lo.FromPtr(item.InvoiceID), InvoiceLineItemID: lo.FromPtr(item.InvoiceLineItemID),
			TenantID: types.GetTenantID(ctx), EnvironmentID: types.GetEnvironmentID(ctx), InvoiceStatus: types.InvoiceStatusVoided}
	}
	if s.modify != nil {
		states = s.modify(states)
	}
	return states, nil
}

type correctionSourceItems struct {
	domain.ScheduleItemRepository
	rows     []*domain.ScheduleItem
	filters  []*domain.ScheduleItemFilter
	selected *domain.ScheduleItem
}

func (r *correctionSourceItems) Get(_ context.Context, id string) (*domain.ScheduleItem, error) {
	if r.selected != nil && r.selected.ID == id {
		return r.selected, nil
	}
	for _, item := range r.rows {
		if item.ID == id {
			return item, nil
		}
	}
	return nil, errors.New("missing correction source")
}

func (r *correctionSourceItems) List(_ context.Context, filter *domain.ScheduleItemFilter) ([]*domain.ScheduleItem, error) {
	copy := *filter
	r.filters = append(r.filters, &copy)
	var page []*domain.ScheduleItem
	for _, item := range r.rows {
		if filter.After != nil && (item.CreatedAt.Before(filter.After.CreatedAt) || item.CreatedAt.Equal(filter.After.CreatedAt) && item.ID <= filter.After.ID) {
			continue
		}
		page = append(page, item)
		if len(page) == filter.Limit {
			break
		}
	}
	return page, nil
}

type correctionSourceCore struct {
	core.Service
	state core.State
}

func (s *correctionSourceCore) Get(context.Context, domain.OwnerRef) (core.State, error) {
	return s.state, nil
}

type correctionSourceState struct {
	owner   *domain.OwnerSnapshot
	summary domain.CollectionSummary
}

func (s *correctionSourceState) Owner() *domain.OwnerSnapshot       { return s.owner }
func (s *correctionSourceState) Rules() []*domain.Rule              { return nil }
func (s *correctionSourceState) Items() []*domain.ScheduleItem      { return nil }
func (s *correctionSourceState) Summary() *domain.CollectionSummary { copy := s.summary; return &copy }
func (s *correctionSourceState) StateToken() string                 { return "correction_state" }
func (s *correctionSourceState) Noop() bool                         { return false }

type correctionSourceDB struct{ postgres.IClient }

func (*correctionSourceDB) TxFromContext(context.Context) *ent.Tx { return nil }

func newCorrectionSources(t *testing.T, count int) (context.Context, *invoice.Invoice, *correctionSourceItems, *correctionSourceStore) {
	t.Helper()
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), "tenant_correction"), "env_correction")
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	inv := &invoice.Invoice{ID: "inv_original", InvoiceType: types.InvoiceTypeSubscription, InvoiceStatus: types.InvoiceStatusVoided,
		CustomerID: "cus_recipient", SubscriptionID: lo.ToPtr("sub_parent"), PeriodStart: &start, PeriodEnd: &end,
		Currency: "usd", EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx)}
	items := &correctionSourceItems{}
	for index := range count {
		item := &domain.ScheduleItem{ID: fmt.Sprintf("bsi_%04d", index), OwnerType: types.BillingTermOwnerCommitment, OwnerID: "comm_first",
			InvoiceID: &inv.ID, InvoiceLineItemID: lo.ToPtr(fmt.Sprintf("ili_%04d", index)), CustomerID: inv.CustomerID,
			SubscriptionID: inv.SubscriptionID, Currency: inv.Currency, Delivery: types.BillingTermSubscriptionInvoice,
			EnvironmentID: inv.EnvironmentID, BaseModel: types.GetDefaultBaseModel(ctx)}
		item.Status = types.StatusArchived
		item.CreatedAt = start
		items.rows = append(items.rows, item)
		inv.LineItems = append(inv.LineItems, &invoice.InvoiceLineItem{ID: *item.InvoiceLineItemID, BillingScheduleItemID: &item.ID})
	}
	return ctx, inv, items, &correctionSourceStore{}
}

func TestInvoiceCorrectionSources(t *testing.T) {
	t.Run("later grouped owner remains selectable", func(t *testing.T) {
		ctx, inv, items, store := newCorrectionSources(t, core.MaxItems+1)
		last := items.rows[len(items.rows)-1]
		last.OwnerID = "comm_last"
		items.selected = domain.NewScheduleItemBuilder(last).WithID("bsi_replacement").Build()
		items.selected.InvoiceID, items.selected.InvoiceLineItemID = nil, nil
		items.selected.Status = types.StatusPublished
		items.selected.ScheduleStatus = types.BillingScheduleScheduled
		items.selected.BillOn = inv.PeriodStart
		repo := testutil.NewInMemoryInvoiceStore()
		require.NoError(t, repo.Create(ctx, inv))
		s := &invoiceService{ServiceParams: ServiceParams{InvoiceRepo: repo, BillingScheduleItems: items, BillingTermsStore: store, DB: &correctionSourceDB{},
			BillingTerms: &correctionSourceCore{state: &correctionSourceState{owner: &domain.OwnerSnapshot{CanCollect: true, CustomerID: inv.CustomerID},
				summary: domain.CollectionSummary{TargetAmount: decimal.NewFromInt(100)}}}}}
		plan, err := s.loadInvoiceCorrection(ctx, inv.ID, dto.RecalculateInvoiceRequest{BillingScheduleItemIDs: []string{items.selected.ID}})
		require.NoError(t, err)
		require.Equal(t, []domain.OwnerRef{{Type: types.BillingTermOwnerCommitment, ID: "comm_last"}}, plan.owners)
		require.Equal(t, []int{core.MaxItems, 1}, store.batchSizes)
		require.Len(t, items.filters, 2)
		require.Equal(t, 2, items.filters[1].Limit)
		require.True(t, items.filters[1].IncludeArchived)
	})
	for _, tc := range []struct {
		name   string
		modify func(*invoice.Invoice, *correctionSourceItems, *correctionSourceStore)
	}{
		{"missing source", func(_ *invoice.Invoice, items *correctionSourceItems, _ *correctionSourceStore) {
			items.rows = items.rows[:1]
		}},
		{"extra reverse binding", func(inv *invoice.Invoice, _ *correctionSourceItems, _ *correctionSourceStore) {
			inv.LineItems = inv.LineItems[:1]
		}},
		{"wrong source line", func(_ *invoice.Invoice, items *correctionSourceItems, _ *correctionSourceStore) {
			items.rows[0].InvoiceLineItemID = lo.ToPtr("other_line")
		}},
		{"duplicate source", func(_ *invoice.Invoice, items *correctionSourceItems, _ *correctionSourceStore) {
			items.rows[1] = items.rows[0]
		}},
		{"wrong binding scope", func(_ *invoice.Invoice, _ *correctionSourceItems, store *correctionSourceStore) {
			store.modify = func(states []domain.BindingState) []domain.BindingState {
				states[0].EnvironmentID = "foreign"
				return states
			}
		}},
		{"duplicate binding", func(_ *invoice.Invoice, _ *correctionSourceItems, store *correctionSourceStore) {
			store.modify = func(states []domain.BindingState) []domain.BindingState { states[1] = states[0]; return states }
		}},
		{"wrong invoice status", func(_ *invoice.Invoice, _ *correctionSourceItems, store *correctionSourceStore) {
			store.modify = func(states []domain.BindingState) []domain.BindingState {
				states[0].InvoiceStatus = types.InvoiceStatusFinalized
				return states
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, inv, items, store := newCorrectionSources(t, 2)
			tc.modify(inv, items, store)
			s := &invoiceService{ServiceParams: ServiceParams{BillingScheduleItems: items, BillingTermsStore: store}}
			_, err := s.invoiceCorrectionSources(ctx, inv)
			require.ErrorContains(t, err, "invoice source history does not match")
		})
	}
}
