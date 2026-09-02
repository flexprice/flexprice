package chargebee

import (
	"context"

	"github.com/chargebee/chargebee-go/v3/actions/customer"
	"github.com/chargebee/chargebee-go/v3/actions/paymentsource"
	"github.com/chargebee/chargebee-go/v3/actions/transaction"
	"github.com/chargebee/chargebee-go/v3/filter"
	customerModel "github.com/chargebee/chargebee-go/v3/models/customer"
	paymentSourceModel "github.com/chargebee/chargebee-go/v3/models/paymentsource"
	transactionModel "github.com/chargebee/chargebee-go/v3/models/transaction"
	transactionEnum "github.com/chargebee/chargebee-go/v3/models/transaction/enum"
	"github.com/samber/lo"
)

// ListPaymentSources returns the customer's vaulted payment sources.
func (c *Client) ListPaymentSources(ctx context.Context, chargebeeCustomerID string) ([]*paymentSourceModel.PaymentSource, error) {
	env, err := c.env(ctx)
	if err != nil {
		return nil, err
	}

	res, err := paymentsource.List(&paymentSourceModel.ListRequestParams{
		CustomerId: &filter.StringFilter{Is: chargebeeCustomerID},
		// Chargebee pages at 10 by default; ask for more so the customer's primary
		// source cannot fall off the first page and get silently skipped.
		Limit: lo.ToPtr(int32(50)),
	}).ListRequestWithEnv(env)
	if err != nil {
		return nil, wrapAPIError(err, "Failed to list Chargebee payment sources")
	}

	out := make([]*paymentSourceModel.PaymentSource, 0, len(res.List))
	for _, entry := range res.List {
		if entry.PaymentSource != nil {
			out = append(out, entry.PaymentSource)
		}
	}
	return out, nil
}

// RetrieveCustomer fetches the Chargebee customer, chiefly for PrimaryPaymentSourceId.
func (c *Client) RetrieveCustomer(ctx context.Context, chargebeeCustomerID string) (*customerModel.Customer, error) {
	env, err := c.env(ctx)
	if err != nil {
		return nil, err
	}

	res, err := customer.Retrieve(chargebeeCustomerID).RequestWithEnv(env)
	if err != nil {
		return nil, wrapAPIError(err, "Failed to retrieve Chargebee customer")
	}
	if res.Customer == nil {
		return nil, missingPayload("customer")
	}
	return res.Customer, nil
}

// RetrieveTransaction fetches a transaction, chiefly to see what has already been
// refunded against it.
func (c *Client) RetrieveTransaction(ctx context.Context, transactionID string) (*transactionModel.Transaction, error) {
	env, err := c.env(ctx)
	if err != nil {
		return nil, err
	}

	res, err := transaction.Retrieve(transactionID).RequestWithEnv(env)
	if err != nil {
		return nil, wrapAPIError(err, "Failed to retrieve Chargebee transaction")
	}
	if res.Transaction == nil {
		return nil, missingPayload("transaction")
	}
	return res.Transaction, nil
}

// RefundTransaction refunds amountMinor against a settled transaction. Chargebee
// scopes the idempotency key globally, so callers must namespace it per operation.
func (c *Client) RefundTransaction(ctx context.Context, transactionID string, amountMinor int64, idempotencyKey string) (*transactionModel.Transaction, error) {
	env, err := c.env(ctx)
	if err != nil {
		return nil, err
	}

	req := transaction.Refund(transactionID, &transactionModel.RefundRequestParams{
		Amount: lo.ToPtr(amountMinor),
	})
	if idempotencyKey != "" {
		req = req.SetIdempotencyKey(idempotencyKey)
	}

	res, err := req.RequestWithEnv(env)
	if err != nil {
		return nil, wrapAPIError(err, "Failed to refund Chargebee transaction")
	}
	if res.Transaction == nil {
		return nil, missingPayload("transaction")
	}
	return res.Transaction, nil
}

// AmountRefunded totals the refunds already linked to a transaction. The API has
// no amount_unrefunded field on this model, so the guard against submitting a
// second refund is computed from linked_refunds.
func AmountRefunded(txn *transactionModel.Transaction) int64 {
	if txn == nil {
		return 0
	}
	var total int64
	for _, refund := range txn.LinkedRefunds {
		if refund == nil || refund.TxnStatus == transactionEnum.StatusFailure {
			continue
		}
		total += refund.TxnAmount
	}
	return total
}
