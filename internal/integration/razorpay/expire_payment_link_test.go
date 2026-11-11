package razorpay

import (
	"context"
	"errors"
	"testing"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type expireClient struct {
	RazorpayClient
	cancelLinkIDs    []string
	cancelInvoiceIDs []string
	cancelErr        error
	fetched          map[string]interface{}
}

func (c *expireClient) CancelPaymentLink(_ context.Context, id string) (map[string]interface{}, error) {
	c.cancelLinkIDs = append(c.cancelLinkIDs, id)
	return map[string]interface{}{"status": "cancelled"}, c.cancelErr
}

func (c *expireClient) CancelInvoice(_ context.Context, id string) (map[string]interface{}, error) {
	c.cancelInvoiceIDs = append(c.cancelInvoiceIDs, id)
	return map[string]interface{}{"status": "cancelled"}, c.cancelErr
}

func (c *expireClient) FetchPaymentLink(context.Context, string) (map[string]interface{}, error) {
	return c.fetched, nil
}

func (c *expireClient) GetInvoice(context.Context, string) (map[string]interface{}, error) {
	return c.fetched, nil
}

func newExpireAdapter(c RazorpayClient) *CheckoutAdapter {
	return &CheckoutAdapter{Svc: &PaymentService{client: c, logger: logger.NewNoopLogger()}}
}

func TestExpirePaymentLink_RoutesPlinkToPaymentLinkCancel(t *testing.T) {
	c := &expireClient{}
	err := newExpireAdapter(c).ExpirePaymentLink(context.Background(), interfaces.ExpirePaymentLinkRequest{GatewayTrackingID: "plink_x"})
	require.NoError(t, err)
	assert.Equal(t, []string{"plink_x"}, c.cancelLinkIDs)
	assert.Empty(t, c.cancelInvoiceIDs)
}

func TestExpirePaymentLink_RoutesInvoiceToInvoiceCancel(t *testing.T) {
	c := &expireClient{}
	err := newExpireAdapter(c).ExpirePaymentLink(context.Background(), interfaces.ExpirePaymentLinkRequest{GatewayTrackingID: "inv_x"})
	require.NoError(t, err)
	assert.Equal(t, []string{"inv_x"}, c.cancelInvoiceIDs)
	assert.Empty(t, c.cancelLinkIDs)
}

func TestExpirePaymentLink_AlreadyClosedIsNil(t *testing.T) {
	for _, handle := range []string{"plink_x", "inv_x"} {
		for _, status := range []string{"cancelled", "expired", "paid"} {
			t.Run(handle+"/"+status, func(t *testing.T) {
				c := &expireClient{cancelErr: errors.New("boom"), fetched: map[string]interface{}{"status": status}}
				err := newExpireAdapter(c).ExpirePaymentLink(context.Background(), interfaces.ExpirePaymentLinkRequest{GatewayTrackingID: handle})
				assert.NoError(t, err)
			})
		}
	}
}

func TestExpirePaymentLink_StillOpenReturnsCancelError(t *testing.T) {
	c := &expireClient{cancelErr: errors.New("boom"), fetched: map[string]interface{}{"status": "created"}}
	err := newExpireAdapter(c).ExpirePaymentLink(context.Background(), interfaces.ExpirePaymentLinkRequest{GatewayTrackingID: "plink_x"})
	assert.Error(t, err)
}

func TestExpirePaymentLink_UnknownHandleNotImplemented(t *testing.T) {
	for _, handle := range []string{"order_x", "pay_x", ""} {
		err := newExpireAdapter(&expireClient{}).ExpirePaymentLink(context.Background(), interfaces.ExpirePaymentLinkRequest{GatewayTrackingID: handle})
		assert.True(t, ierr.IsNotImplemented(err), handle)
	}
}
