package recognition

import (
	"testing"
	"time"

	domainContract "github.com/flexprice/flexprice/internal/domain/contract"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestInvoiceRecognizerDue(t *testing.T) {
	ctx := testutil.SetupContext()
	asOf := time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC)
	invoices := testutil.NewInMemoryInvoiceStore()
	subs := testutil.NewInMemorySubscriptionStore()
	contracts := testutil.NewInMemoryContractStore()

	for _, id := range []string{"subs_standalone", "subs_contract"} {
		require.NoError(t, subs.Create(ctx, &subscription.Subscription{ID: id, BaseModel: types.GetDefaultBaseModel(ctx)}))
	}
	require.NoError(t, contracts.CreateWithAssociation(ctx,
		domainContract.NewBuilder(nil).WithID("contract_1").WithContractStatus(types.ContractStatusActive).WithStatus(types.StatusPublished).Build(),
		domainContract.NewAssociationBuilder(nil).WithID("assoc_1").WithEntityType(types.RevRecEntityTypeSubscription).WithEntityID("subs_contract").
			WithStatus(types.StatusPublished).Build()))
	newInvoice := func(id, subID, customerID string, periodEnd time.Time) *invoice.Invoice {
		return &invoice.Invoice{ID: id, CustomerID: customerID, SubscriptionID: lo.ToPtr(subID),
			InvoiceStatus: types.InvoiceStatusFinalized, Currency: "usd", Total: decimal.NewFromInt(100), FinalizedAt: lo.ToPtr(asOf.AddDate(-1, 0, 0)),
			PeriodEnd: lo.ToPtr(periodEnd), EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx)}
	}
	for _, inv := range []*invoice.Invoice{
		newInvoice("inv_standalone", "subs_standalone", "cust_1", asOf),
		newInvoice("inv_archived", "subs_archived", "cust_1", asOf),
		newInvoice("inv_contract", "subs_contract", "cust_1", asOf),
		// Ended long ago and unchanged since the run's window: due only for a customer run.
		newInvoice("inv_old", "subs_standalone", "cust_1", asOf.AddDate(-1, 0, 0)),
		newInvoice("inv_other_customer", "subs_standalone", "cust_2", asOf),
	} {
		require.NoError(t, invoices.CreateWithLineItems(ctx, inv))
	}

	r := NewInvoiceRecognizer(invoices, testutil.NewInMemoryTaxAppliedStore(), contracts, nil, subs, testutil.NewInMemoryCustomerStore())
	// After every invoice's last update, so only the recent-period rule selects.
	unchangedSince := time.Now().Add(time.Hour)
	tests := []struct {
		name  string
		since time.Time
		scope Scope
		want  []string
	}{
		{name: "environment: recent standalone invoices; not a contract's, nor an unreadable subscription's", since: unchangedSince, want: []string{"inv_other_customer", "inv_standalone"}},
		{name: "customer: all of the customer's standalone invoices", since: unchangedSince, scope: Scope{CustomerID: "cust_1"}, want: []string{"inv_old", "inv_standalone"}},
		{name: "contract: no standalone invoices", scope: Scope{ContractID: "contract_1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			due, err := r.Due(ctx, tt.since, asOf, tt.scope)
			require.NoError(t, err)
			require.Equal(t, tt.want, due)
		})
	}
}

func TestContractRecognizerDueScope(t *testing.T) {
	ctx := testutil.SetupContext()
	contracts := testutil.NewInMemoryContractStore()
	for id, customerID := range map[string]string{"contract_a": "cust_1", "contract_b": "cust_1", "contract_c": "cust_2"} {
		require.NoError(t, contracts.Create(ctx, domainContract.NewBuilder(nil).WithID(id).WithCustomerID(customerID).
			WithContractStatus(types.ContractStatusActive).WithStatus(types.StatusPublished).Build()))
	}
	r := NewContractRecognizer(contracts, nil, nil, nil, testutil.NewInMemoryInvoiceStore(), nil, nil)

	due, err := r.Due(ctx, time.Time{}, time.Now(), Scope{ContractID: "contract_c"})
	require.NoError(t, err)
	require.Equal(t, []string{"contract_c"}, due)

	due, err = r.Due(ctx, time.Time{}, time.Now(), Scope{CustomerID: "cust_1"})
	require.NoError(t, err)
	require.Equal(t, []string{"contract_a", "contract_b"}, due, "every contract of the customer")
}
