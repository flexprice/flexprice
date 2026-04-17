package testutil

import (
	"context"
	"testing"

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
