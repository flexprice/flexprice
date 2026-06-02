package integration

import "context"

type Base interface {
	PullAndUpdateInvoice(ctx context.Context, invoiceID string) error
}
