package events

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// fakeConnRepo embeds the interface (so only the methods under test need an implementation)
// and reports invoice-outbound-enabled connections for the providers in `enabled`.
type fakeConnRepo struct {
	connection.Repository
	enabled map[types.SecretProvider]bool
}

func (f *fakeConnRepo) enabledConn(provider types.SecretProvider) *connection.Connection {
	return &connection.Connection{
		ProviderType: provider,
		SyncConfig: &types.SyncConfig{
			Invoice: &types.EntitySyncConfig{Outbound: true},
		},
	}
}

func (f *fakeConnRepo) GetByProvider(_ context.Context, provider types.SecretProvider) (*connection.Connection, error) {
	if !f.enabled[provider] {
		return nil, ierr.NewError("not found").Mark(ierr.ErrNotFound)
	}
	return f.enabledConn(provider), nil
}

// List returns every enabled connection in one shot, matching how invoiceOutboundEnabledInOrder
// now batches its lookup instead of querying per provider.
func (f *fakeConnRepo) List(_ context.Context, _ *types.ConnectionFilter) ([]*connection.Connection, error) {
	var conns []*connection.Connection
	for provider, on := range f.enabled {
		if on {
			conns = append(conns, f.enabledConn(provider))
		}
	}
	return conns, nil
}

type fakeCustomerRepo struct {
	customer.Repository
	byID map[string]*customer.Customer
}

func (f *fakeCustomerRepo) Get(_ context.Context, id string) (*customer.Customer, error) {
	c, ok := f.byID[id]
	if !ok {
		return nil, ierr.NewError("not found").Mark(ierr.ErrNotFound)
	}
	return c, nil
}

type fakeInvoiceRepo struct {
	invoice.Repository
	byID map[string]*invoice.Invoice
}

func (f *fakeInvoiceRepo) Get(_ context.Context, id string) (*invoice.Invoice, error) {
	inv, ok := f.byID[id]
	if !ok {
		return nil, ierr.NewError("not found").Mark(ierr.ErrNotFound)
	}
	return inv, nil
}

func TestResolveInvoiceSyncTarget(t *testing.T) {
	tests := []struct {
		name           string
		allowed        []types.SecretProvider
		enabledInOrder []types.SecretProvider
		wantTarget     types.SecretProvider
		wantOK         bool
	}{
		{
			name:           "empty allow-list, one enabled ⇒ that one",
			allowed:        nil,
			enabledInOrder: []types.SecretProvider{types.SecretProviderRazorpay},
			wantTarget:     types.SecretProviderRazorpay,
			wantOK:         true,
		},
		{
			name:           "empty allow-list, several enabled ⇒ first by fixed order",
			allowed:        []types.SecretProvider{},
			enabledInOrder: []types.SecretProvider{types.SecretProviderStripe, types.SecretProviderRazorpay},
			wantTarget:     types.SecretProviderStripe,
			wantOK:         true,
		},
		{
			name:           "allow-list [razorpay], Stripe+Razorpay enabled ⇒ Razorpay",
			allowed:        []types.SecretProvider{types.SecretProviderRazorpay},
			enabledInOrder: []types.SecretProvider{types.SecretProviderStripe, types.SecretProviderRazorpay},
			wantTarget:     types.SecretProviderRazorpay,
			wantOK:         true,
		},
		{
			name:           "allow-list [quickbooks, razorpay], only Razorpay enabled ⇒ Razorpay (fallback)",
			allowed:        []types.SecretProvider{types.SecretProviderQuickBooks, types.SecretProviderRazorpay},
			enabledInOrder: []types.SecretProvider{types.SecretProviderRazorpay},
			wantTarget:     types.SecretProviderRazorpay,
			wantOK:         true,
		},
		{
			name:           "allow-list respects list priority over fixed order",
			allowed:        []types.SecretProvider{types.SecretProviderRazorpay, types.SecretProviderStripe},
			enabledInOrder: []types.SecretProvider{types.SecretProviderStripe, types.SecretProviderRazorpay},
			wantTarget:     types.SecretProviderRazorpay,
			wantOK:         true,
		},
		{
			name:           "allow-list [stripe], Stripe not enabled ⇒ (\"\", false)",
			allowed:        []types.SecretProvider{types.SecretProviderStripe},
			enabledInOrder: []types.SecretProvider{types.SecretProviderRazorpay},
			wantTarget:     "",
			wantOK:         false,
		},
		{
			name:           "allow-list with unknown provider ⇒ ignored, falls through",
			allowed:        []types.SecretProvider{"not-a-provider", types.SecretProviderStripe},
			enabledInOrder: []types.SecretProvider{types.SecretProviderStripe},
			wantTarget:     types.SecretProviderStripe,
			wantOK:         true,
		},
		{
			name:           "allow-list with only unknown provider ⇒ (\"\", false)",
			allowed:        []types.SecretProvider{"not-a-provider"},
			enabledInOrder: []types.SecretProvider{types.SecretProviderStripe},
			wantTarget:     "",
			wantOK:         false,
		},
		{
			name:           "nothing enabled, empty allow-list ⇒ (\"\", false)",
			allowed:        nil,
			enabledInOrder: nil,
			wantTarget:     "",
			wantOK:         false,
		},
		{
			name:           "nothing enabled, non-empty allow-list ⇒ (\"\", false)",
			allowed:        []types.SecretProvider{types.SecretProviderStripe},
			enabledInOrder: nil,
			wantTarget:     "",
			wantOK:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTarget, gotOK := ResolveInvoiceSyncTarget(tt.allowed, tt.enabledInOrder)
			if gotTarget != tt.wantTarget || gotOK != tt.wantOK {
				t.Errorf("ResolveInvoiceSyncTarget(%v, %v) = (%q, %v), want (%q, %v)",
					tt.allowed, tt.enabledInOrder, gotTarget, gotOK, tt.wantTarget, tt.wantOK)
			}
		})
	}
}

func TestInvoiceOutboundEnabledInOrder(t *testing.T) {
	// Razorpay + QuickBooks enabled ⇒ returned in fixed code order (Razorpay before QuickBooks),
	// regardless of the order List happens to yield them.
	connRepo := &fakeConnRepo{enabled: map[types.SecretProvider]bool{
		types.SecretProviderQuickBooks: true,
		types.SecretProviderRazorpay:   true,
	}}

	got, err := invoiceOutboundEnabledInOrder(context.Background(), connRepo)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []types.SecretProvider{types.SecretProviderRazorpay, types.SecretProviderQuickBooks}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("invoiceOutboundEnabledInOrder = %v, want %v", got, want)
	}
}

// TestDispatchResolution_EndToEnd wires the two dispatcher helpers to the pure resolver to
// mirror the spec's dispatch-level scenarios without needing the Temporal service.
func TestDispatchResolution_EndToEnd(t *testing.T) {
	ctx := context.Background()

	t.Run("allow-list [quickbooks, razorpay] with both enabled ⇒ QuickBooks wins", func(t *testing.T) {
		connRepo := &fakeConnRepo{enabled: map[types.SecretProvider]bool{
			types.SecretProviderRazorpay:   true,
			types.SecretProviderQuickBooks: true,
		}}
		custRepo := &fakeCustomerRepo{byID: map[string]*customer.Customer{
			"cus_1": {ID: "cus_1", AllowedIntegrationProviders: []types.SecretProvider{
				types.SecretProviderQuickBooks, types.SecretProviderRazorpay,
			}},
		}}
		invRepo := &fakeInvoiceRepo{byID: map[string]*invoice.Invoice{
			"inv_1": {ID: "inv_1", CustomerID: "cus_1"},
		}}

		enabledInOrder, err := invoiceOutboundEnabledInOrder(ctx, connRepo)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		allowed, err := resolveAllowedProviders(ctx, custRepo, invRepo, "inv_1", testLogger())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		target, ok := ResolveInvoiceSyncTarget(allowed, enabledInOrder)
		if !ok || target != types.SecretProviderQuickBooks {
			t.Fatalf("resolved (%q, %v), want (quickbooks, true)", target, ok)
		}
	})

	t.Run("empty allow-list with Stripe & Razorpay enabled ⇒ Stripe (fixed order)", func(t *testing.T) {
		connRepo := &fakeConnRepo{enabled: map[types.SecretProvider]bool{
			types.SecretProviderStripe:   true,
			types.SecretProviderRazorpay: true,
		}}
		custRepo := &fakeCustomerRepo{byID: map[string]*customer.Customer{
			"cus_2": {ID: "cus_2"}, // no allow-list
		}}
		invRepo := &fakeInvoiceRepo{byID: map[string]*invoice.Invoice{
			"inv_2": {ID: "inv_2", CustomerID: "cus_2"},
		}}

		enabledInOrder, err := invoiceOutboundEnabledInOrder(ctx, connRepo)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		allowed, err := resolveAllowedProviders(ctx, custRepo, invRepo, "inv_2", testLogger())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		target, ok := ResolveInvoiceSyncTarget(allowed, enabledInOrder)
		if !ok || target != types.SecretProviderStripe {
			t.Fatalf("resolved (%q, %v), want (stripe, true)", target, ok)
		}
	})
}

func TestResolveAllowedProviders(t *testing.T) {
	ctx := context.Background()
	log := testLogger()

	cust := &customer.Customer{ID: "cus_1", AllowedIntegrationProviders: []types.SecretProvider{types.SecretProviderRazorpay}}
	custRepo := &fakeCustomerRepo{byID: map[string]*customer.Customer{"cus_1": cust}}
	invRepo := &fakeInvoiceRepo{byID: map[string]*invoice.Invoice{
		"inv_1":       {ID: "inv_1", CustomerID: "cus_1"},
		"inv_no_cust": {ID: "inv_no_cust", CustomerID: "cus_missing"},
	}}

	t.Run("invoice → customer ⇒ returns allow-list", func(t *testing.T) {
		got, err := resolveAllowedProviders(ctx, custRepo, invRepo, "inv_1", log)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 || got[0] != types.SecretProviderRazorpay {
			t.Fatalf("got %v, want [razorpay]", got)
		}
	})

	t.Run("invoice missing ⇒ nil (fixed order)", func(t *testing.T) {
		got, err := resolveAllowedProviders(ctx, custRepo, invRepo, "inv_missing", log)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != nil {
			t.Fatalf("got %v, want nil", got)
		}
	})

	t.Run("customer missing ⇒ nil (fixed order)", func(t *testing.T) {
		got, err := resolveAllowedProviders(ctx, custRepo, invRepo, "inv_no_cust", log)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != nil {
			t.Fatalf("got %v, want nil", got)
		}
	})

	t.Run("nil invoiceRepo ⇒ nil (fixed order)", func(t *testing.T) {
		got, err := resolveAllowedProviders(ctx, custRepo, nil, "inv_1", log)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != nil {
			t.Fatalf("got %v, want nil", got)
		}
	})
}
