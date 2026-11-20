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
	for id, subID := range map[string]string{"inv_standalone": "subs_standalone", "inv_archived": "subs_archived", "inv_contract": "subs_contract"} {
		require.NoError(t, invoices.CreateWithLineItems(ctx, &invoice.Invoice{ID: id, CustomerID: "cust_1", SubscriptionID: lo.ToPtr(subID),
			InvoiceStatus: types.InvoiceStatusFinalized, Currency: "usd", Total: decimal.NewFromInt(100), FinalizedAt: lo.ToPtr(asOf.AddDate(0, -1, 0)),
			PeriodEnd: lo.ToPtr(asOf), EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx)}))
	}

	r := NewInvoiceRecognizer(invoices, testutil.NewInMemoryTaxAppliedStore(), contracts, nil, subs, testutil.NewInMemoryCustomerStore())
	due, err := r.Due(ctx, time.Time{}, asOf)
	require.NoError(t, err)
	require.Equal(t, []string{"inv_standalone"}, due, "a contract's invoices and an unreadable subscription's invoices are not due")
}
