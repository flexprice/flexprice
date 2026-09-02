package chargebee

import (
	"context"
	"strings"

	"github.com/chargebee/chargebee-go/v3/actions/hostedpage"
	hostedPageModel "github.com/chargebee/chargebee-go/v3/models/hostedpage"
	"github.com/samber/lo"
)

// CreateCheckoutOneTimePage returns an INVOICE-SCOPED hosted checkout for an exact
// ad-hoc amount. This is the correct page type for a top-up — unlike collect_now it
// charges only what we ask for.
//
// Requires One-Time Checkout enabled on the Chargebee site:
// Settings → Configure Chargebee → Checkout & Self Serve Portal → Configuration →
// One time payments. Without it Chargebee returns one_time_checkout_not_enabled_in_hp.
//
// poNumber stamps a caller-chosen reference onto the invoice this page creates.
// Verified live: it reaches that invoice AND Chargebee echoes it on the
// payment_succeeded webhook, which is the only thread tying the webhook back to a
// Flexprice entity — the page's invoice is created by Chargebee, so no mapping of
// ours exists for it.
//
// This page does NOT vault the card. payment_method_save_policy is a collect_now
// parameter — measured here: a bogus value is rejected on collect_now but accepted
// on this endpoint, and a paid page left a zero-source customer with none. Vaulting
// is a site setting (One time payments → Save customers' payment method).
func (c *Client) CreateCheckoutOneTimePage(
	ctx context.Context,
	chargebeeCustomerID, currency string,
	amountMinor int64,
	description, redirectURL, gatewayAccountID, poNumber string,
) (*hostedPageModel.HostedPage, error) {
	env, err := c.env(ctx)
	if err != nil {
		return nil, err
	}

	params := &hostedPageModel.CheckoutOneTimeForItemsRequestParams{
		Customer:     &hostedPageModel.CheckoutOneTimeForItemsCustomerParams{Id: chargebeeCustomerID},
		CurrencyCode: strings.ToUpper(currency),
		Charges: []*hostedPageModel.CheckoutOneTimeForItemsChargeParams{{
			Amount:      lo.ToPtr(amountMinor),
			Description: description,
		}},
		RedirectUrl: redirectURL,
	}
	if gatewayAccountID != "" {
		params.Card = &hostedPageModel.CheckoutOneTimeForItemsCardParams{GatewayAccountId: gatewayAccountID}
	}
	if poNumber != "" {
		params.Invoice = &hostedPageModel.CheckoutOneTimeForItemsInvoiceParams{PoNumber: poNumber}
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
