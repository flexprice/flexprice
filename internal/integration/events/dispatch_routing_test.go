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

// fakeConnRepo embeds the interface (so only GetByProvider needs an implementation) and
// reports invoice-outbound-enabled connections for the providers in `enabled`.
type fakeConnRepo struct {
	connection.Repository
	enabled map[types.SecretProvider]bool
}

func (f *fakeConnRepo) GetByProvider(_ context.Context, provider types.SecretProvider) (*connection.Connection, error) {
	if !f.enabled[provider] {
		return nil, ierr.NewError("not found").Mark(ierr.ErrNotFound)
	}
	return &connection.Connection{
		ProviderType: provider,
		SyncConfig: &types.SyncConfig{
			Invoice: &types.EntitySyncConfig{Outbound: true},
		},
	}, nil
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

func TestInvoiceOutboundEnabledInOrder(t *testing.T) {
	// Razorpay + QuickBooks enabled ⇒ returned in fixed code order (Razorpay before QuickBooks).
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
			"cus_1": {ID: "cus_1", AllowedIntegrationProviders: []string{
				string(types.SecretProviderQuickBooks), string(types.SecretProviderRazorpay),
			}},
		}}

		enabledInOrder, err := invoiceOutboundEnabledInOrder(ctx, connRepo)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		allowed, err := resolveAllowedProviders(ctx, custRepo, nil, "cus_1", "inv_1", testLogger())
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

		enabledInOrder, err := invoiceOutboundEnabledInOrder(ctx, connRepo)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		allowed, err := resolveAllowedProviders(ctx, custRepo, nil, "cus_2", "inv_2", testLogger())
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

	cust := &customer.Customer{ID: "cus_1", AllowedIntegrationProviders: []string{string(types.SecretProviderRazorpay)}}
	custRepo := &fakeCustomerRepo{byID: map[string]*customer.Customer{"cus_1": cust}}
	invRepo := &fakeInvoiceRepo{byID: map[string]*invoice.Invoice{
		"inv_1": {ID: "inv_1", CustomerID: "cus_1"},
	}}

	t.Run("customer_id present ⇒ returns allow-list without invoice read", func(t *testing.T) {
		got, err := resolveAllowedProviders(ctx, custRepo, nil, "cus_1", "inv_1", log)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 || got[0] != string(types.SecretProviderRazorpay) {
			t.Fatalf("got %v, want [razorpay]", got)
		}
	})

	t.Run("customer_id absent ⇒ guarded invoice read recovers customer", func(t *testing.T) {
		got, err := resolveAllowedProviders(ctx, custRepo, invRepo, "", "inv_1", log)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 || got[0] != string(types.SecretProviderRazorpay) {
			t.Fatalf("got %v, want [razorpay]", got)
		}
	})

	t.Run("customer_id absent and invoice missing ⇒ nil (fixed order)", func(t *testing.T) {
		got, err := resolveAllowedProviders(ctx, custRepo, invRepo, "", "inv_missing", log)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != nil {
			t.Fatalf("got %v, want nil", got)
		}
	})

	t.Run("customer missing ⇒ nil (fixed order)", func(t *testing.T) {
		got, err := resolveAllowedProviders(ctx, custRepo, invRepo, "cus_missing", "inv_1", log)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != nil {
			t.Fatalf("got %v, want nil", got)
		}
	})
}
