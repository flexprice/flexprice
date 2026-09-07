package chargebee

import (
	"context"
	"strings"
	"time"

	"github.com/chargebee/chargebee-go/v3/actions/hostedpage"
	hostedPageModel "github.com/chargebee/chargebee-go/v3/models/hostedpage"
	"github.com/samber/lo"
)

// AdHocCharge is one ad-hoc line on the invoice Chargebee creates. charges[amount][n]
// produces itemised entity_type:"adhoc" line items.
type AdHocCharge struct {
	AmountMinor int64
	Description string
	PeriodStart *time.Time
	PeriodEnd   *time.Time
}

func (c AdHocCharge) dateRange() (from, to *int64) {
	if c.PeriodStart != nil {
		from = lo.ToPtr(c.PeriodStart.Unix())
	}
	if c.PeriodEnd != nil {
		to = lo.ToPtr(c.PeriodEnd.Unix())
	}
	return from, to
}

type HostedCheckoutPageRequest struct {
	ChargebeeCustomerID string
	Currency            string
	Charges             []AdHocCharge
	RedirectURL         string
	GatewayAccountID    string
	// InvoiceNote lands in notes[] on the invoice this page creates and is echoed on
	// payment_succeeded — the only thread tying that webhook back to a Flexprice
	// entity, since the page's invoice is Chargebee's and no mapping of ours exists.
	InvoiceNote string
}

// CreateHostedCheckoutPage returns an invoice-scoped hosted checkout for an exact
// ad-hoc amount — unlike collect_now it charges only what we ask for.
//
// Requires One-Time Checkout enabled on the site (Checkout & Self Serve Portal →
// One time payments), else Chargebee returns one_time_checkout_not_enabled_in_hp.
// That same setting, not payment_method_save_policy, controls vaulting: this
// endpoint accepts the policy param but ignores it, leaving the card unsaved.
func (c *Client) CreateHostedCheckoutPage(
	ctx context.Context,
	req HostedCheckoutPageRequest,
) (*hostedPageModel.HostedPage, error) {
	env, err := c.env(ctx)
	if err != nil {
		return nil, err
	}

	charges := make([]*hostedPageModel.CheckoutOneTimeForItemsChargeParams, 0, len(req.Charges))
	for _, ch := range req.Charges {
		from, to := ch.dateRange()
		charges = append(charges, &hostedPageModel.CheckoutOneTimeForItemsChargeParams{
			Amount:      lo.ToPtr(ch.AmountMinor),
			Description: ch.Description,
			DateFrom:    from,
			DateTo:      to,
		})
	}

	params := &hostedPageModel.CheckoutOneTimeForItemsRequestParams{
		Customer:     &hostedPageModel.CheckoutOneTimeForItemsCustomerParams{Id: req.ChargebeeCustomerID},
		CurrencyCode: strings.ToUpper(req.Currency),
		Charges:      charges,
		RedirectUrl:  req.RedirectURL,
		InvoiceNote:  req.InvoiceNote,
	}
	if req.GatewayAccountID != "" {
		params.Card = &hostedPageModel.CheckoutOneTimeForItemsCardParams{GatewayAccountId: req.GatewayAccountID}
	}
	res, err := hostedpage.CheckoutOneTimeForItems(params).RequestWithEnv(env)
	if err != nil {
		return nil, wrapAPIError(err, "Failed to create Chargebee hosted checkout page")
	}
	if res.HostedPage == nil {
		return nil, missingPayload("hosted page")
	}
	return res.HostedPage, nil
}
