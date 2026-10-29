package quickbooks

import (
	"context"
	"errors"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type scheduledItemClient struct {
	QuickBooksClient
	created  []*ItemCreateRequest
	queries  []string
	existing *ItemResponse
	fail     bool
}

func (c *scheduledItemClient) QueryItemByName(_ context.Context, name string) (*ItemResponse, error) {
	c.queries = append(c.queries, name)
	if c.existing != nil {
		return c.existing, nil
	}
	return nil, ierr.NewError("not found").Mark(ierr.ErrNotFound)
}
func (c *scheduledItemClient) GetConnection(context.Context) (*connection.Connection, error) {
	return nil, errors.New("use existing default income account")
}
func (c *scheduledItemClient) CreateItem(_ context.Context, req *ItemCreateRequest) (*ItemResponse, error) {
	c.created = append(c.created, req)
	if c.fail {
		return nil, errors.New("provider creation failed")
	}
	c.existing = &ItemResponse{ID: "qb_service_item", Name: req.Name}
	return c.existing, nil
}

func TestScheduledPaymentExportUsesExactAmountWithoutPrice(t *testing.T) {
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), "tenant"), "env")
	client := &scheduledItemClient{}
	service := &InvoiceService{InvoiceServiceParams: InvoiceServiceParams{Client: client, Logger: logger.NewNoopLogger()}}
	inv := &invoice.Invoice{LineItems: []*invoice.InvoiceLineItem{
		{ID: "first", Quantity: decimal.NewFromInt(1), BillingScheduleItemID: lo.ToPtr("schedule_first"), Amount: decimal.RequireFromString("123.45"), DisplayName: lo.ToPtr("Upfront payment")},
		{ID: "second", Quantity: decimal.NewFromInt(1), BillingScheduleItemID: lo.ToPtr("schedule_second"), Amount: decimal.NewFromInt(50)},
	}}
	lines, err := service.buildLineItems(ctx, inv)
	require.NoError(t, err)
	require.Len(t, lines, 2)
	require.Equal(t, "123.45", lines[0].Amount.String())
	require.Equal(t, "50", lines[1].Amount.String())
	require.Equal(t, "Upfront payment", lines[0].Description)
	require.Equal(t, "qb_service_item", lines[0].SalesItemLineDetail.ItemRef.Value)
	require.Len(t, client.created, 1)
	require.Len(t, client.queries, 1)
	require.Equal(t, string(ItemTypeService), client.created[0].Type)
	require.Equal(t, "79", client.created[0].IncomeAccountRef.Value)
	_, err = service.buildLineItems(ctx, inv)
	require.NoError(t, err)
	require.Len(t, client.created, 1, "retry reuses the scoped provider item")
}

func TestScheduledPaymentExportDoesNotDropFailedCharges(t *testing.T) {
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), "tenant"), "env")
	client := &scheduledItemClient{fail: true}
	service := &InvoiceService{InvoiceServiceParams: InvoiceServiceParams{Client: client, Logger: logger.NewNoopLogger()}}
	lines, err := service.buildLineItems(ctx, &invoice.Invoice{LineItems: []*invoice.InvoiceLineItem{{BillingScheduleItemID: lo.ToPtr("source"), Amount: decimal.NewFromInt(100)}}})
	require.ErrorContains(t, err, "provider creation failed")
	require.Nil(t, lines)
}
