package chargebee

import (
	"context"
	"strings"

	"github.com/chargebee/chargebee-go/v3/actions/invoice"
	"github.com/chargebee/chargebee-go/v3/enum"
	downloadModel "github.com/chargebee/chargebee-go/v3/models/download"
	invoiceModel "github.com/chargebee/chargebee-go/v3/models/invoice"
	"github.com/samber/lo"
)

type AdHocInvoiceRequest struct {
	ChargebeeCustomerID string
	Currency            string
	Charges             []AdHocCharge
	// InvoiceNote is the correlation carrier the hosted page also uses, so a webhook
	// resolves the same way whichever path created the invoice.
	InvoiceNote    string
	IdempotencyKey string
	// AutoCollect charges the primary source as part of the create call, which
	// Chargebee books as merchant-initiated. Off leaves the invoice as the allocation
	// target for an explicit collect_payment, the only way to declare the charge
	// customer-initiated — and the caller must then collect, or nothing charges at all.
	AutoCollect     bool
	CustomerPresent bool
}

// CreateAdHocInvoice mirrors a Flexprice draft invoice. Ad-hoc is required because
// a wallet top-up has no Price entity, so there is no item_price to reference — the
// catalog-based sync path rejects such line items outright.
func (c *Client) CreateAdHocInvoice(
	ctx context.Context,
	adHocReq AdHocInvoiceRequest,
) (*invoiceModel.Invoice, error) {
	env, err := c.env(ctx)
	if err != nil {
		return nil, err
	}

	charges := make([]*invoiceModel.CreateForChargeItemsAndChargesChargeParams, 0, len(adHocReq.Charges))
	for _, ch := range adHocReq.Charges {
		from, to := ch.dateRange()
		charges = append(charges, &invoiceModel.CreateForChargeItemsAndChargesChargeParams{
			Amount:      lo.ToPtr(ch.AmountMinor),
			Description: ch.Description,
			DateFrom:    from,
			DateTo:      to,
		})
	}

	req := invoice.CreateForChargeItemsAndCharges(&invoiceModel.CreateForChargeItemsAndChargesRequestParams{
		CustomerId:     adHocReq.ChargebeeCustomerID,
		CurrencyCode:   strings.ToUpper(adHocReq.Currency),
		Charges:        charges,
		InvoiceNote:    adHocReq.InvoiceNote,
		AutoCollection: lo.Ternary(adHocReq.AutoCollect, enum.AutoCollectionOn, enum.AutoCollectionOff),
		PaymentInitiator: lo.Ternary(adHocReq.CustomerPresent,
			enum.PaymentInitiatorCustomer, enum.PaymentInitiatorMerchant),
	})
	if adHocReq.IdempotencyKey != "" {
		req = req.SetIdempotencyKey(adHocReq.IdempotencyKey)
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

// RetrieveInvoicePDF returns a signed link to Chargebee's rendering.
func (c *Client) RetrieveInvoicePDF(ctx context.Context, chargebeeInvoiceID string) (*downloadModel.Download, error) {
	env, err := c.env(ctx)
	if err != nil {
		return nil, err
	}

	res, err := invoice.Pdf(chargebeeInvoiceID, &invoiceModel.PdfRequestParams{
		DispositionType: enum.DispositionTypeAttachment,
	}).RequestWithEnv(env)
	if err != nil {
		return nil, wrapAPIError(err, "Failed to retrieve Chargebee invoice PDF")
	}
	if res.Download == nil || res.Download.DownloadUrl == "" {
		return nil, missingPayload("invoice pdf download")
	}

	return res.Download, nil
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
