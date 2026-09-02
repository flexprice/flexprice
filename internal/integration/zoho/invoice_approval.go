package zoho

import (
	"context"
	"time"
)

const (
	approvalWait     = 5 * time.Second
	approvalMaxPolls = 3
)

// awaitingApproval reports whether the invoice is still moving through the approval flow —
// either not yet submitted, or submitted and queued for an approver.
func awaitingApproval(status string) bool {
	switch NormalizeInvoiceStatus(status) {
	case InvoiceStatusDraft, InvoiceStatusPendingApproval:
		return true
	default:
		return false
	}
}

// ensureApprovedForPayment takes a synced Zoho invoice out of draft so a payment can be
// recorded against it. It returns the freshest invoice read, or nil when the invoice did
// not become payable. Callers must settle against the returned invoice: polling can span
// several seconds, during which Zoho's balance may have moved.
func (s *InvoiceService) ensureApprovedForPayment(ctx context.Context, flexpriceInvoiceID string, zohoInv *InvoiceResponse) (*InvoiceResponse, error) {
	return s.ensureApprovedForPaymentWithin(ctx, flexpriceInvoiceID, zohoInv, approvalWait, approvalMaxPolls)
}

func (s *InvoiceService) ensureApprovedForPaymentWithin(
	ctx context.Context,
	flexpriceInvoiceID string,
	zohoInv *InvoiceResponse,
	wait time.Duration,
	maxPolls int,
) (*InvoiceResponse, error) {
	if zohoInv == nil {
		return nil, nil
	}

	settings, err := s.getInvoiceSyncSettings(ctx)
	if err != nil {
		return nil, err
	}
	if !settings.IsSubmitForApprovalEnabled() {
		return zohoInv, nil
	}

	switch NormalizeInvoiceStatus(zohoInv.Status) {
	case InvoiceStatusDraft:
		if err := s.client.SubmitInvoiceForApproval(ctx, zohoInv.InvoiceID); err != nil {
			return nil, err
		}
		s.logger.Info(ctx, "submitted Zoho invoice for approval",
			"invoice_id", flexpriceInvoiceID,
			"zoho_invoice_id", zohoInv.InvoiceID)
	case InvoiceStatusPendingApproval:
		// Already queued; re-submitting would make Zoho error. Fall through and wait.
	case InvoiceStatusRejected:
		return nil, s.logRejected(ctx, flexpriceInvoiceID, zohoInv.InvoiceID)
	default:
		// Approved, sent, partially paid, paid: nothing blocks a payment.
		return zohoInv, nil
	}

	latest := zohoInv
	for i := 0; i < maxPolls; i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}

		current, err := s.client.GetInvoice(ctx, zohoInv.InvoiceID)
		if err != nil {
			return nil, err
		}
		if current == nil {
			continue
		}

		latest = current
		if NormalizeInvoiceStatus(latest.Status) == InvoiceStatusRejected {
			return nil, s.logRejected(ctx, flexpriceInvoiceID, zohoInv.InvoiceID)
		}
		if !awaitingApproval(latest.Status) {
			return latest, nil
		}
	}

	s.logger.Info(ctx, "Zoho invoice still awaiting approval after wait, skipping mark-paid",
		"invoice_id", flexpriceInvoiceID,
		"zoho_invoice_id", zohoInv.InvoiceID,
		"zoho_status", latest.Status,
		"polls", maxPolls,
		"wait_per_poll", wait.String())
	return nil, nil
}

// logRejected records the terminal case where an approver turned the invoice down. It
// returns a nil error so the caller skips mark-paid without failing the sync.
func (s *InvoiceService) logRejected(ctx context.Context, flexpriceInvoiceID, zohoInvoiceID string) error {
	s.logger.Info(ctx, "Zoho invoice was rejected in approval, skipping mark-paid",
		"invoice_id", flexpriceInvoiceID,
		"zoho_invoice_id", zohoInvoiceID)
	return nil
}
