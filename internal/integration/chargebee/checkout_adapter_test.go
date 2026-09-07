package chargebee

import (
	"context"
	"testing"
	"time"

	invoiceModel "github.com/chargebee/chargebee-go/v3/models/invoice"
	invoiceEnum "github.com/chargebee/chargebee-go/v3/models/invoice/enum"
	transactionEnum "github.com/chargebee/chargebee-go/v3/models/transaction/enum"
	customerDomain "github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type stubCheckoutClient struct {
	ChargebeeClient
	gotInvoiceReq AdHocInvoiceRequest
	invoice       *invoiceModel.Invoice
	voidedIDs     []string
}

func (c *stubCheckoutClient) CreateAdHocInvoice(_ context.Context, req AdHocInvoiceRequest) (*invoiceModel.Invoice, error) {
	c.gotInvoiceReq = req
	return c.invoice, nil
}

func (c *stubCheckoutClient) VoidInvoice(_ context.Context, chargebeeInvoiceID, _ string) error {
	c.voidedIDs = append(c.voidedIDs, chargebeeInvoiceID)
	return nil
}

type stubCustomerSvc struct{ ChargebeeCustomerService }

func (s *stubCustomerSvc) EnsureCustomerSyncedToChargebee(_ context.Context, _ string) (*customerDomain.Customer, error) {
	return &customerDomain.Customer{Metadata: map[string]string{"chargebee_customer_id": "cb_cust_1"}}, nil
}

type stubInvoiceSvc struct{ ChargebeeInvoiceService }

func (s *stubInvoiceSvc) LinkInvoiceMapping(_ context.Context, _, _ string) error { return nil }

func newTestAdapter(client ChargebeeClient) *CheckoutAdapter {
	return &CheckoutAdapter{
		Client:      client,
		CustomerSvc: &stubCustomerSvc{},
		InvoiceSvc:  &stubInvoiceSvc{},
		Logger:      logger.NewNoopLogger(),
	}
}

func line(desc, amount string, from, to *time.Time) interfaces.CheckoutLineItem {
	return interfaces.CheckoutLineItem{
		Description: desc,
		Amount:      decimal.RequireFromString(amount),
		Quantity:    decimal.NewFromInt(1),
		PeriodStart: from,
		PeriodEnd:   to,
	}
}

func TestGetLineItems(t *testing.T) {
	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	adapter := newTestAdapter(&stubCheckoutClient{})

	t.Run("multi-line carries minor units and periods", func(t *testing.T) {
		charges, err := adapter.getLineItems(context.Background(), []interfaces.CheckoutLineItem{
			line("Pro Plan", "4999.00", &from, &to),
			line("API Calls (Qty: 12500)", "1250.50", &from, &to),
			line("Storage GB (Qty: 340)", "340.00", nil, nil),
		}, decimal.RequireFromString("6589.50"), "INR", "inv_1")

		require.NoError(t, err)
		require.Len(t, charges, 3)
		require.Equal(t, int64(499900), charges[0].AmountMinor)
		require.Equal(t, int64(125050), charges[1].AmountMinor)
		require.Equal(t, int64(34000), charges[2].AmountMinor)
		require.Equal(t, "API Calls (Qty: 12500)", charges[1].Description)
		require.Equal(t, &from, charges[0].PeriodStart)
		require.Equal(t, &to, charges[0].PeriodEnd)
		require.Nil(t, charges[2].PeriodStart, "a line with no period must not invent one")
	})

	// JPY has zero precision: a /100 anywhere in the chain undercharges 100x.
	t.Run("zero-precision currency is not divided", func(t *testing.T) {
		charges, err := adapter.getLineItems(context.Background(),
			[]interfaces.CheckoutLineItem{line("Pro Plan", "5000", nil, nil)},
			decimal.RequireFromString("5000"), "JPY", "inv_jpy")

		require.NoError(t, err)
		require.Equal(t, int64(5000), charges[0].AmountMinor)
	})

	// The lines are what the customer agreed to; a drift is reported, never papered
	// over by silently substituting a different charge.
	t.Run("sum mismatch still sends our lines", func(t *testing.T) {
		charges, err := adapter.getLineItems(context.Background(), []interfaces.CheckoutLineItem{
			line("Pro Plan", "4999.00", nil, nil),
			line("Discount adjustment", "0.005", nil, nil),
		}, decimal.RequireFromString("4999.00"), "INR", "inv_drift")

		require.NoError(t, err)
		require.Len(t, charges, 2, "no collapse to a single charge")
		require.Equal(t, int64(499900), charges[0].AmountMinor)
	})

	t.Run("no line items is an error", func(t *testing.T) {
		_, err := adapter.getLineItems(context.Background(), nil,
			decimal.RequireFromString("100"), "INR", "inv_empty")
		require.Error(t, err)
	})
}

func linkedPayment(id string, status transactionEnum.Status) *invoiceModel.LinkedPayment {
	return &invoiceModel.LinkedPayment{TxnId: id, TxnStatus: status}
}

// ACH and SEPA return in_progress from the collection call itself. Voiding there
// cancels a collection that is still live.
func TestTryAutoCharging_PendingIsNotVoided(t *testing.T) {
	client := &stubCheckoutClient{invoice: &invoiceModel.Invoice{
		Id:             "cb_inv_pending",
		Status:         invoiceEnum.StatusPaymentDue,
		LinkedPayments: []*invoiceModel.LinkedPayment{linkedPayment("txn_ach", transactionEnum.StatusInProgress)},
	}}
	adapter := newTestAdapter(client)

	resp, charged, err := adapter.TryAutoChargingSavedMethod(context.Background(), interfaces.AuthorizationLinkRequest{
		CustomerID: "cust_1",
		InvoiceID:  "inv_1",
		PaymentID:  "pay_1",
		Amount:     decimal.RequireFromString("100.00"),
		Currency:   "USD",
		LineItems:  []interfaces.CheckoutLineItem{line("Pro Plan", "100.00", nil, nil)},
	})

	require.NoError(t, err)
	require.True(t, charged, "a live collection must not be reported as uncharged")
	require.Empty(t, client.voidedIDs, "an in-flight collection must never be voided")
	require.Equal(t, "txn_ach", resp.ProviderPaymentIntentID)
	require.Equal(t, string(transactionEnum.StatusInProgress), resp.ProviderMetadata["transaction_status"])
}

// Nothing was collected at all, so the receivable would otherwise be dunned later.
func TestTryAutoCharging_NoLinkedPaymentIsVoided(t *testing.T) {
	client := &stubCheckoutClient{invoice: &invoiceModel.Invoice{
		Id:     "cb_inv_empty",
		Status: invoiceEnum.StatusPaymentDue,
	}}
	adapter := newTestAdapter(client)

	_, charged, err := adapter.TryAutoChargingSavedMethod(context.Background(), interfaces.AuthorizationLinkRequest{
		CustomerID: "cust_1",
		InvoiceID:  "inv_1",
		PaymentID:  "pay_1",
		Amount:     decimal.RequireFromString("100.00"),
		Currency:   "USD",
		LineItems:  []interfaces.CheckoutLineItem{line("Pro Plan", "100.00", nil, nil)},
	})

	require.NoError(t, err)
	require.False(t, charged)
	require.Equal(t, []string{"cb_inv_empty"}, client.voidedIDs)
}

// The invoice note is the correlation carrier and must reach the client on the
// auto-charge path too, not only the hosted page.
func TestTryAutoCharging_SendsInvoiceNoteAndLines(t *testing.T) {
	client := &stubCheckoutClient{invoice: &invoiceModel.Invoice{
		Id:             "cb_inv_paid",
		Status:         invoiceEnum.StatusPaid,
		LinkedPayments: []*invoiceModel.LinkedPayment{linkedPayment("txn_ok", transactionEnum.StatusSuccess)},
	}}
	adapter := newTestAdapter(client)

	_, charged, err := adapter.TryAutoChargingSavedMethod(context.Background(), interfaces.AuthorizationLinkRequest{
		CustomerID: "cust_1",
		InvoiceID:  "inv_1",
		PaymentID:  "pay_abc",
		Amount:     decimal.RequireFromString("62.50"),
		Currency:   "USD",
		LineItems: []interfaces.CheckoutLineItem{
			line("Pro Plan", "50.00", nil, nil),
			line("Overage", "12.50", nil, nil),
		},
	})

	require.NoError(t, err)
	require.True(t, charged)
	require.Equal(t, "Flexprice payment: pay_abc", client.gotInvoiceReq.InvoiceNote)
	require.Len(t, client.gotInvoiceReq.Charges, 2)
	require.Equal(t, int64(5000), client.gotInvoiceReq.Charges[0].AmountMinor)
	require.Equal(t, int64(1250), client.gotInvoiceReq.Charges[1].AmountMinor)
}
