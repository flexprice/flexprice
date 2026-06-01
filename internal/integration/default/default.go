package _default

import (
	"context"
	"fmt"
)

type Default struct {
}

func (*Default) PullAndUpdateInvoice(ctx context.Context, invoiceID string) error {
	return fmt.Errorf("invoice pull support unavailable for current integration")
}
