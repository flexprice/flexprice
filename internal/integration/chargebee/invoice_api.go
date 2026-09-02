package chargebee

import (
	"context"
	"strings"

	"github.com/chargebee/chargebee-go/v3/actions/invoice"
	"github.com/chargebee/chargebee-go/v3/enum"
	invoiceModel "github.com/chargebee/chargebee-go/v3/models/invoice"
	"github.com/samber/lo"
)

// CreateAdHocInvoice creates a Chargebee invoice carrying a single ad-hoc charge.
// This is the "mirror" of the Flexprice draft invoice. Ad-hoc is required because
// a wallet top-up has no Price entity, so there is no item_price to reference —
// the existing catalog-based sync path rejects such line items outright.
//
// poNumber is the correlation key the hosted page also carries, so a webhook
// resolves the same way whichever path created the invoice.
func (c *Client) CreateAdHocInvoice(
	ctx context.Context,
	chargebeeCustomerID, currency string,
	amountMinor int64,
	description, poNumber, idempotencyKey string,
	autoCollect, customerPresent bool,
) (*invoiceModel.Invoice, error) {
	env, err := c.env(ctx)
	if err != nil {
		return nil, err
	}

	req := invoice.CreateForChargeItemsAndCharges(&invoiceModel.CreateForChargeItemsAndChargesRequestParams{
		CustomerId:   chargebeeCustomerID,
		CurrencyCode: strings.ToUpper(currency),
		Charges: []*invoiceModel.CreateForChargeItemsAndChargesChargeParams{{
			Amount:      lo.ToPtr(amountMinor),
			Description: description,
		}},
		PoNumber: poNumber,
		// autoCollect charges the customer's primary source as part of this call,
		// which Chargebee books as merchant-initiated. Off leaves the invoice as the
		// allocation target for an explicit collect_payment, the only way to declare
		// the charge customer-initiated — and the caller must then collect, or nothing
		// charges at all.
		AutoCollection: lo.Ternary(autoCollect, enum.AutoCollectionOn, enum.AutoCollectionOff),
		PaymentInitiator: lo.Ternary(customerPresent,
			enum.PaymentInitiatorCustomer, enum.PaymentInitiatorMerchant),
	})
	if idempotencyKey != "" {
		req = req.SetIdempotencyKey(idempotencyKey)
	}

	res, err := req.RequestWithEnv(env)
	if err != nil {
		return nil, wrapAPIError(err, "Failed to create Chargebee ad-hoc invoice")
	}
	if res.Invoice == nil {
		return nil, missingPayload("invoice")
	}
	return res.Invoice, nil
}

// VoidInvoice abandons an invoice at Chargebee. An ad-hoc invoice created with
// auto-collection on is a live receivable: left unpaid it can be dunned and charged
// later, long after the caller gave up on it.
func (c *Client) VoidInvoice(ctx context.Context, chargebeeInvoiceID, comment string) error {
	env, err := c.env(ctx)
	if err != nil {
		return err
	}

	_, err = invoice.VoidInvoice(chargebeeInvoiceID, &invoiceModel.VoidInvoiceRequestParams{
		Comment: comment,
	}).RequestWithEnv(env)
	if err != nil {
		return wrapAPIError(err, "Failed to void Chargebee invoice")
	}
	return nil
}
