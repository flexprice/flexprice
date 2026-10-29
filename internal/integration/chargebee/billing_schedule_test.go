package chargebee

import (
	"context"
	"testing"
	"time"

	chargebeeSDK "github.com/chargebee/chargebee-go/v3"
	chargebeeInvoice "github.com/chargebee/chargebee-go/v3/models/invoice"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type scheduledChargeClient struct {
	ChargebeeClient
	request *chargebeeInvoice.CreateForChargeItemsAndChargesRequestParams
}

func (c *scheduledChargeClient) CreateInvoice(_ context.Context, req *chargebeeInvoice.CreateForChargeItemsAndChargesRequestParams) (*chargebeeSDK.Result, error) {
	c.request = req
	return &chargebeeSDK.Result{Invoice: &chargebeeInvoice.Invoice{Id: "provider_invoice", CustomerId: req.CustomerId}}, nil
}

func TestScheduledChargeUsesAmountAndServicePeriod(t *testing.T) {
	for _, test := range []struct {
		currency, amount string
		minor            int64
	}{{"usd", "123.45", 12345}, {"jpy", "123", 123}, {"kwd", "1.234", 1234}} {
		t.Run(test.currency, func(t *testing.T) {
			client := &scheduledChargeClient{}
			service := &InvoiceService{InvoiceServiceParams: InvoiceServiceParams{Client: client, Logger: logger.NewNoopLogger()}}
			start, end := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
			lines, err := service.buildLineItems(context.Background(), &invoice.Invoice{Currency: test.currency, LineItems: []*invoice.InvoiceLineItem{{
				ID: "line", Quantity: decimal.NewFromInt(1), BillingScheduleItemID: lo.ToPtr("source"), Amount: decimal.RequireFromString(test.amount), DisplayName: lo.ToPtr("Contract payment"), PeriodStart: &start, PeriodEnd: &end,
			}}})
			require.NoError(t, err)
			require.Len(t, lines, 1)
			lines = append(lines, InvoiceLineItem{ItemPriceID: "catalog_price", Quantity: 1, UnitAmount: 500})
			_, err = service.CreateInvoice(context.Background(), &InvoiceCreateRequest{CustomerID: "customer", Currency: test.currency, LineItems: lines})
			require.NoError(t, err)
			require.Len(t, client.request.Charges, 1)
			require.Len(t, client.request.ItemPrices, 1)
			require.Equal(t, test.minor, *client.request.Charges[0].Amount)
			require.Equal(t, start.Unix(), *client.request.Charges[0].DateFrom)
			require.Equal(t, end.Unix(), *client.request.Charges[0].DateTo)
			require.Equal(t, "Contract payment", client.request.Charges[0].Description)
		})
	}
}

func TestUnrelatedChargeStillRequiresPrice(t *testing.T) {
	service := &InvoiceService{InvoiceServiceParams: InvoiceServiceParams{Logger: logger.NewNoopLogger()}}
	_, err := service.buildLineItems(context.Background(), &invoice.Invoice{Currency: "usd", LineItems: []*invoice.InvoiceLineItem{{Amount: decimal.NewFromInt(100)}}})
	require.Error(t, err)
}
