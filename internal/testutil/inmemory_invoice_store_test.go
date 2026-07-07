package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestInMemoryInvoiceStore_List_SubscriptionCustomerIDsFilter(t *testing.T) {
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), types.DefaultTenantID), "env_test")
	store := NewInMemoryInvoiceStore()
	lineStore := NewInMemoryInvoiceLineItemStore()
	store.SetLineItemStore(lineStore)

	base := types.GetDefaultBaseModel(ctx)

	mustCreate := func(inv *invoice.Invoice) {
		t.Helper()
		require.NoError(t, store.Create(ctx, inv))
	}

	invMatch := &invoice.Invoice{
		ID:                     "inv_subcust_match",
		CustomerID:             "cust_parent",
		SubscriptionCustomerID: lo.ToPtr("child_a"),
		InvoiceType:            types.InvoiceTypeSubscription,
		InvoiceStatus:          types.InvoiceStatusFinalized,
		PaymentStatus:          types.PaymentStatusPending,
		Currency:               "usd",
		AmountDue:              decimal.NewFromInt(10),
		AmountRemaining:        decimal.NewFromInt(10),
		EnvironmentID:          "env_test",
		BaseModel:              base,
	}
	invWrongChild := &invoice.Invoice{
		ID:                     "inv_subcust_other",
		CustomerID:             "cust_parent",
		SubscriptionCustomerID: lo.ToPtr("child_b"),
		InvoiceType:            types.InvoiceTypeSubscription,
		InvoiceStatus:          types.InvoiceStatusFinalized,
		PaymentStatus:          types.PaymentStatusPending,
		Currency:               "usd",
		AmountDue:              decimal.NewFromInt(20),
		AmountRemaining:        decimal.NewFromInt(20),
		EnvironmentID:          "env_test",
		BaseModel:              base,
	}
	invNilSubCust := &invoice.Invoice{
		ID:              "inv_subcust_nil",
		CustomerID:      "cust_parent",
		InvoiceType:     types.InvoiceTypeSubscription,
		InvoiceStatus:   types.InvoiceStatusFinalized,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "usd",
		AmountDue:       decimal.NewFromInt(30),
		AmountRemaining: decimal.NewFromInt(30),
		EnvironmentID:   "env_test",
		BaseModel:       base,
	}

	for _, inv := range []*invoice.Invoice{invMatch, invWrongChild, invNilSubCust} {
		mustCreate(inv)
	}

	filter := types.NewNoLimitInvoiceFilter()
	filter.QueryFilter.Status = lo.ToPtr(types.StatusPublished)
	filter.CustomerID = ""
	filter.SubscriptionCustomerIDs = []string{"child_a"}
	filter.InvoiceStatus = []types.InvoiceStatus{types.InvoiceStatusFinalized}

	got, err := store.List(ctx, filter)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "inv_subcust_match", got[0].ID)
}

// newInvoiceTestContext returns a context scoped to the default tenant and the
// "env_test" environment, matching the fixtures in this file.
func newInvoiceTestContext() context.Context {
	return types.SetEnvironmentID(types.SetTenantID(context.Background(), types.DefaultTenantID), "env_test")
}

// TestInMemoryInvoiceStore_FieldFidelity verifies that every field written on
// Create/Update survives a Get round-trip — copyInvoice used to silently drop
// TotalTax and IssueDate (the real Ent repo persists and returns both:
// internal/repository/ent/invoice.go SetTotalTax/SetNillableIssueDate on
// create/update and domain FromEnt mapping).
func TestInMemoryInvoiceStore_FieldFidelity(t *testing.T) {
	ctx := newInvoiceTestContext()
	now := time.Now().UTC()
	issueDate := now.Add(-24 * time.Hour)
	updatedIssueDate := now.Add(-2 * time.Hour)

	testCases := []struct {
		name             string
		update           bool // when true, mutate + Update before the final Get
		expectedTotalTax decimal.Decimal
		expectedIssue    time.Time
	}{
		{
			name:             "total_tax_and_issue_date_survive_create_get_round_trip",
			update:           false,
			expectedTotalTax: decimal.RequireFromString("12.34"),
			expectedIssue:    issueDate,
		},
		{
			name:             "total_tax_and_issue_date_survive_update_get_round_trip",
			update:           true,
			expectedTotalTax: decimal.RequireFromString("99.99"),
			expectedIssue:    updatedIssueDate,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			store := NewInMemoryInvoiceStore()
			lineStore := NewInMemoryInvoiceLineItemStore()
			store.SetLineItemStore(lineStore)

			inv := &invoice.Invoice{
				ID:              "inv_fidelity",
				CustomerID:      "cust_fidelity",
				InvoiceType:     types.InvoiceTypeOneOff,
				InvoiceStatus:   types.InvoiceStatusFinalized,
				PaymentStatus:   types.PaymentStatusPending,
				Currency:        "usd",
				AmountDue:       decimal.NewFromInt(112),
				AmountRemaining: decimal.NewFromInt(112),
				Subtotal:        decimal.NewFromInt(100),
				Total:           decimal.NewFromInt(112),
				TotalTax:        decimal.RequireFromString("12.34"),
				IssueDate:       lo.ToPtr(issueDate),
				EnvironmentID:   "env_test",
				BaseModel:       types.GetDefaultBaseModel(ctx),
			}
			require.NoError(t, store.Create(ctx, inv))

			if tc.update {
				stored, err := store.Get(ctx, inv.ID)
				require.NoError(t, err)
				stored.TotalTax = decimal.RequireFromString("99.99")
				stored.IssueDate = lo.ToPtr(updatedIssueDate)
				require.NoError(t, store.Update(ctx, stored))
			}

			got, err := store.Get(ctx, inv.ID)
			require.NoError(t, err)
			require.True(t, got.TotalTax.Equal(tc.expectedTotalTax),
				"TotalTax: expected %s, got %s", tc.expectedTotalTax, got.TotalTax)
			require.NotNil(t, got.IssueDate)
			require.True(t, got.IssueDate.Equal(tc.expectedIssue),
				"IssueDate: expected %s, got %s", tc.expectedIssue, got.IssueDate)
		})
	}
}

// TestInMemoryInvoiceStore_LineItemsSingleSourceOfTruth verifies that Get/List
// assemble line items from the wired line-item store, mirroring the real Ent
// repo where Get loads them via LineItemRepository.ListByInvoiceID and List
// edge-loads them (published only) from the invoice_line_items table.
func TestInMemoryInvoiceStore_LineItemsSingleSourceOfTruth(t *testing.T) {
	ctx := newInvoiceTestContext()
	base := types.GetDefaultBaseModel(ctx)

	newLineItem := func(id, invoiceID string, amount int64) *invoice.InvoiceLineItem {
		return &invoice.InvoiceLineItem{
			ID:         id,
			InvoiceID:  invoiceID,
			CustomerID: "cust_lisrc",
			Amount:     decimal.NewFromInt(amount),
			Quantity:   decimal.NewFromInt(1),
			Currency:   "usd",
			BaseModel:  base,
		}
	}
	newInvoice := func(id string, items ...*invoice.InvoiceLineItem) *invoice.Invoice {
		return &invoice.Invoice{
			ID:              id,
			CustomerID:      "cust_lisrc",
			InvoiceType:     types.InvoiceTypeOneOff,
			InvoiceStatus:   types.InvoiceStatusFinalized,
			PaymentStatus:   types.PaymentStatusPending,
			Currency:        "usd",
			AmountDue:       decimal.NewFromInt(10),
			AmountRemaining: decimal.NewFromInt(10),
			EnvironmentID:   "env_test",
			LineItems:       items,
			BaseModel:       base,
		}
	}

	t.Run("embedded_items_on_create_are_visible_on_get", func(t *testing.T) {
		store := NewInMemoryInvoiceStore()
		lineStore := NewInMemoryInvoiceLineItemStore()
		store.SetLineItemStore(lineStore)

		inv := newInvoice("inv_li_embed", newLineItem("li_embed_1", "inv_li_embed", 10))
		require.NoError(t, store.Create(ctx, inv))

		got, err := store.Get(ctx, inv.ID)
		require.NoError(t, err)
		require.Len(t, got.LineItems, 1)
		require.Equal(t, "li_embed_1", got.LineItems[0].ID)

		// The embedded items must also be persisted in the line-item store
		// (mirrors the real CreateWithLineItems transaction).
		items, err := lineStore.ListByInvoiceID(ctx, inv.ID)
		require.NoError(t, err)
		require.Len(t, items, 1)
	})

	t.Run("items_added_via_line_item_repo_appear_on_get", func(t *testing.T) {
		store := NewInMemoryInvoiceStore()
		lineStore := NewInMemoryInvoiceLineItemStore()
		store.SetLineItemStore(lineStore)

		inv := newInvoice("inv_li_direct")
		require.NoError(t, store.Create(ctx, inv))

		// Add a line item directly through the line-item repository — this is
		// what reconcileLineItems does via InvoiceLineItemRepo.
		require.NoError(t, lineStore.Create(ctx, newLineItem("li_direct_1", inv.ID, 25)))

		got, err := store.Get(ctx, inv.ID)
		require.NoError(t, err)
		require.Len(t, got.LineItems, 1)
		require.Equal(t, "li_direct_1", got.LineItems[0].ID)
		require.True(t, got.LineItems[0].Amount.Equal(decimal.NewFromInt(25)))
	})

	t.Run("items_added_via_line_item_repo_appear_on_list", func(t *testing.T) {
		store := NewInMemoryInvoiceStore()
		lineStore := NewInMemoryInvoiceLineItemStore()
		store.SetLineItemStore(lineStore)

		inv := newInvoice("inv_li_list")
		require.NoError(t, store.Create(ctx, inv))
		require.NoError(t, lineStore.Create(ctx, newLineItem("li_list_1", inv.ID, 5)))

		filter := types.NewNoLimitInvoiceFilter()
		filter.CustomerID = "cust_lisrc"
		got, err := store.List(ctx, filter)
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Len(t, got[0].LineItems, 1)
		require.Equal(t, "li_list_1", got[0].LineItems[0].ID)
	})

	t.Run("updates_via_line_item_repo_are_visible_on_get", func(t *testing.T) {
		store := NewInMemoryInvoiceStore()
		lineStore := NewInMemoryInvoiceLineItemStore()
		store.SetLineItemStore(lineStore)

		inv := newInvoice("inv_li_upd", newLineItem("li_upd_1", "inv_li_upd", 10))
		require.NoError(t, store.Create(ctx, inv))

		item, err := lineStore.Get(ctx, "li_upd_1")
		require.NoError(t, err)
		item.Amount = decimal.NewFromInt(77)
		require.NoError(t, lineStore.Update(ctx, item))

		got, err := store.Get(ctx, inv.ID)
		require.NoError(t, err)
		require.Len(t, got.LineItems, 1)
		require.True(t, got.LineItems[0].Amount.Equal(decimal.NewFromInt(77)),
			"stale embedded copy served instead of line-item store state")
	})

	t.Run("soft_deleted_items_disappear_from_get", func(t *testing.T) {
		store := NewInMemoryInvoiceStore()
		lineStore := NewInMemoryInvoiceLineItemStore()
		store.SetLineItemStore(lineStore)

		inv := newInvoice("inv_li_del",
			newLineItem("li_del_1", "inv_li_del", 10),
			newLineItem("li_del_2", "inv_li_del", 20))
		require.NoError(t, store.Create(ctx, inv))
		require.NoError(t, store.RemoveLineItems(ctx, inv.ID, []string{"li_del_1"}))

		got, err := store.Get(ctx, inv.ID)
		require.NoError(t, err)
		require.Len(t, got.LineItems, 1)
		require.Equal(t, "li_del_2", got.LineItems[0].ID)
	})

	t.Run("skip_line_items_filter_omits_line_items_on_list", func(t *testing.T) {
		store := NewInMemoryInvoiceStore()
		lineStore := NewInMemoryInvoiceLineItemStore()
		store.SetLineItemStore(lineStore)

		inv := newInvoice("inv_li_skip", newLineItem("li_skip_1", "inv_li_skip", 10))
		require.NoError(t, store.Create(ctx, inv))

		filter := types.NewNoLimitInvoiceFilter()
		filter.SkipLineItems = true
		got, err := store.List(ctx, filter)
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Empty(t, got[0].LineItems)
	})
}

// TestInMemoryInvoiceStore_PeriodFilters verifies PeriodStartLTE / PeriodEndGTE
// comparison semantics against the real Ent repo (invoice.PeriodStartLTE /
// invoice.PeriodEndGTE in applyEntityQueryOptions — inclusive comparisons, and
// NULL periods never match). These filters drive the wallet credit-expiry skip
// logic (shouldSkipCreditExpiryDueToActiveSubscriptionOrInvoice).
func TestInMemoryInvoiceStore_PeriodFilters(t *testing.T) {
	ctx := newInvoiceTestContext()
	base := types.GetDefaultBaseModel(ctx)
	anchor := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)

	seed := func(t *testing.T, store *InMemoryInvoiceStore, id string, periodStart, periodEnd *time.Time) {
		t.Helper()
		require.NoError(t, store.Create(ctx, &invoice.Invoice{
			ID:              id,
			CustomerID:      "cust_period",
			InvoiceType:     types.InvoiceTypeSubscription,
			InvoiceStatus:   types.InvoiceStatusFinalized,
			PaymentStatus:   types.PaymentStatusPending,
			Currency:        "usd",
			AmountDue:       decimal.NewFromInt(10),
			AmountRemaining: decimal.NewFromInt(10),
			BillingPeriod:   lo.ToPtr("MONTHLY"),
			PeriodStart:     periodStart,
			PeriodEnd:       periodEnd,
			EnvironmentID:   "env_test",
			BaseModel:       base,
		}))
	}

	testCases := []struct {
		name           string
		periodStartLTE *time.Time
		periodEndGTE   *time.Time
		expectedIDs    []string
	}{
		{
			name:           "period_start_lte_keeps_periods_starting_at_or_before_value",
			periodStartLTE: lo.ToPtr(anchor),
			// starts_before(-10d) and starts_at_anchor (inclusive) match; starts_after(+10d) and nil period do not
			expectedIDs: []string{"inv_starts_before", "inv_starts_at_anchor"},
		},
		{
			name:         "period_end_gte_keeps_periods_ending_at_or_after_value",
			periodEndGTE: lo.ToPtr(anchor),
			// ends_after(+40d), starts_at_anchor(ends +30d) and starts_before(ends at anchor, inclusive) match
			expectedIDs: []string{"inv_starts_before", "inv_starts_at_anchor", "inv_starts_after"},
		},
		{
			name:           "combined_filters_keep_periods_containing_the_anchor",
			periodStartLTE: lo.ToPtr(anchor),
			periodEndGTE:   lo.ToPtr(anchor),
			// only periods that contain the anchor (grant created_at semantics in wallet expiry)
			expectedIDs: []string{"inv_starts_before", "inv_starts_at_anchor"},
		},
		{
			name:           "nil_periods_never_match_period_filters",
			periodStartLTE: lo.ToPtr(anchor.Add(365 * 24 * time.Hour)),
			expectedIDs:    []string{"inv_starts_before", "inv_starts_at_anchor", "inv_starts_after"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			store := NewInMemoryInvoiceStore()
			store.SetLineItemStore(NewInMemoryInvoiceLineItemStore())

			// inv_starts_before:    [-30d, anchor]
			// inv_starts_at_anchor: [anchor, +30d]
			// inv_starts_after:     [+10d, +40d]
			// inv_nil_period:       no period at all
			seed(t, store, "inv_starts_before", lo.ToPtr(anchor.Add(-30*24*time.Hour)), lo.ToPtr(anchor))
			seed(t, store, "inv_starts_at_anchor", lo.ToPtr(anchor), lo.ToPtr(anchor.Add(30*24*time.Hour)))
			seed(t, store, "inv_starts_after", lo.ToPtr(anchor.Add(10*24*time.Hour)), lo.ToPtr(anchor.Add(40*24*time.Hour)))
			seed(t, store, "inv_nil_period", nil, nil)

			filter := types.NewNoLimitInvoiceFilter()
			filter.PeriodStartLTE = tc.periodStartLTE
			filter.PeriodEndGTE = tc.periodEndGTE

			got, err := store.List(ctx, filter)
			require.NoError(t, err)
			gotIDs := lo.Map(got, func(inv *invoice.Invoice, _ int) string { return inv.ID })
			require.ElementsMatch(t, tc.expectedIDs, gotIDs)
		})
	}
}
