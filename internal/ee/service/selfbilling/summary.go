package selfbilling

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/samber/lo"
)

func (s *selfBillingService) Summary(ctx context.Context, tenantID string) (*dto.SelfBillingSummaryResponse, error) {
	if !s.Enabled() {
		return nil, errDisabled
	}
	t, err := s.TenantRepo.GetByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	rs, err := s.client.Summary(ctx, externalID(s.region(), tenantID))
	if err != nil {
		return nil, err
	}
	out := &dto.SelfBillingSummaryResponse{
		Status: t.InternalStatus,
		Usage:  rs.usage,
		Invoices: lo.Map(rs.invoices, func(i remoteInvoice, _ int) dto.SelfBillingInvoice {
			return dto.SelfBillingInvoice{ID: i.id, Number: i.number, Status: i.status, PaymentStatus: i.paymentStatus,
				Total: i.total, Currency: i.currency, DueDate: i.dueDate, PDFURL: i.pdfURL}
		}),
	}
	if rs.subscription != nil {
		out.SubscriptionStatus = rs.subscription.status
		out.TrialEndsAt = rs.subscription.trialEnd
	}
	return out, nil
}

func (s *selfBillingService) PortalSession(ctx context.Context, tenantID string) (*dto.SelfBillingPortalResponse, error) {
	if !s.Enabled() {
		return nil, errDisabled
	}
	p, err := s.client.PortalSession(ctx, externalID(s.region(), tenantID))
	if err != nil {
		return nil, err
	}
	return &dto.SelfBillingPortalResponse{URL: p.url, ExpiresAt: p.expiresAt}, nil
}
