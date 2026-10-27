package selfbilling

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/samber/lo"
)

func (s *selfBillingService) Summary(ctx context.Context, tenantID string) (_ *dto.SelfBillingSummaryResponse, err error) {
	defer func() { s.logRemoteFailure(ctx, "summary", tenantID, err) }()

	if !s.enabled() {
		return nil, errDisabled
	}

	t, err := s.TenantRepo.GetByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	remote, err := s.client.Summary(ctx, s.extID(tenantID))
	if err != nil {
		return nil, err
	}

	// The billing page also reconciles, so a payment whose checkout poll never ran (closed tab, portal payment) still activates.
	status, err := s.updateTenantStatus(ctx, t, remote.subscription)
	if err != nil {
		return nil, err
	}

	out := &dto.SelfBillingSummaryResponse{
		Status:   status,
		Invoices: lo.Map(remote.invoices, toSelfBillingInvoice),
		Usage:    remote.usage,
	}
	if remote.subscription != nil {
		out.SubscriptionStatus = remote.subscription.status
		out.TrialEndsAt = remote.subscription.trialEnd
	}
	return out, nil
}

// PortalSession derives the customer from the caller's tenant, never from the request.
func (s *selfBillingService) PortalSession(ctx context.Context, tenantID string) (_ *dto.SelfBillingPortalResponse, err error) {
	defer func() { s.logRemoteFailure(ctx, "portal_session", tenantID, err) }()

	if !s.enabled() {
		return nil, errDisabled
	}

	p, err := s.client.PortalSession(ctx, s.extID(tenantID))
	if err != nil {
		return nil, err
	}

	return &dto.SelfBillingPortalResponse{
		URL:       p.url,
		ExpiresAt: p.expiresAt,
	}, nil
}

func toSelfBillingInvoice(inv remoteInvoice, _ int) dto.SelfBillingInvoice {
	return dto.SelfBillingInvoice{
		ID:            inv.id,
		Number:        inv.number,
		Status:        inv.status,
		PaymentStatus: inv.paymentStatus,
		Total:         inv.total,
		Currency:      inv.currency,
		DueDate:       inv.dueDate,
		PDFURL:        inv.pdfURL,
	}
}
