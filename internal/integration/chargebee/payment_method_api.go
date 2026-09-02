package chargebee

import (
	"context"

	"github.com/chargebee/chargebee-go/v3/actions/customer"
	"github.com/chargebee/chargebee-go/v3/actions/hostedpage"
	"github.com/chargebee/chargebee-go/v3/actions/paymentsource"
	"github.com/chargebee/chargebee-go/v3/enum"
	customerModel "github.com/chargebee/chargebee-go/v3/models/customer"
	hostedPageModel "github.com/chargebee/chargebee-go/v3/models/hostedpage"
)

// CreateManagePaymentSourcesPage returns Chargebee's own add-a-card page.
//
// Chargebee vaults the card itself, so no server call of ours is needed afterwards —
// we simply re-list payment_sources. gatewayAccountID is passed because this page
// otherwise defaults to a different gateway on a multi-gateway site.
func (c *Client) CreateManagePaymentSourcesPage(
	ctx context.Context,
	chargebeeCustomerID, redirectURL, gatewayAccountID string,
) (*hostedPageModel.HostedPage, error) {
	env, err := c.env(ctx)
	if err != nil {
		return nil, err
	}

	params := &hostedPageModel.ManagePaymentSourcesRequestParams{
		Customer:    &hostedPageModel.ManagePaymentSourcesCustomerParams{Id: chargebeeCustomerID},
		RedirectUrl: redirectURL,
	}
	if gatewayAccountID != "" {
		params.Card = &hostedPageModel.ManagePaymentSourcesCardParams{GatewayAccountId: gatewayAccountID}
	}

	res, err := hostedpage.ManagePaymentSources(params).RequestWithEnv(env)
	if err != nil {
		return nil, wrapAPIError(err, "Failed to create Chargebee add-payment-method page")
	}
	if res.HostedPage == nil {
		return nil, missingPayload("hosted page")
	}
	return res.HostedPage, nil
}

// AssignPaymentRole makes a vaulted source the customer's primary. The primary
// source is what off-session collection charges when no source is named, so this
// is how "change my auto-charge card" is implemented.
func (c *Client) AssignPaymentRole(ctx context.Context, chargebeeCustomerID, paymentSourceID string, role enum.Role) error {
	env, err := c.env(ctx)
	if err != nil {
		return err
	}

	_, err = customer.AssignPaymentRole(chargebeeCustomerID, &customerModel.AssignPaymentRoleRequestParams{
		PaymentSourceId: paymentSourceID,
		Role:            role,
	}).RequestWithEnv(env)
	if err != nil {
		return wrapAPIError(err, "Failed to assign Chargebee payment role")
	}
	return nil
}

// DeletePaymentSource removes a vaulted method. Chargebee scopes this by source id
// alone, with no customer in the path — callers must verify ownership first.
func (c *Client) DeletePaymentSource(ctx context.Context, paymentSourceID string) error {
	env, err := c.env(ctx)
	if err != nil {
		return err
	}

	if _, err := paymentsource.Delete(paymentSourceID).RequestWithEnv(env); err != nil {
		return wrapAPIError(err, "Failed to delete Chargebee payment source")
	}
	return nil
}
