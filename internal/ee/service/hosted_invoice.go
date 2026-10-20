package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/auth"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/idempotency"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// HostedInvoiceService serves the public hosted invoice page.
type HostedInvoiceService interface {
	// Resolve validates a hosted invoice token and returns the context scoped to its invoice.
	Resolve(ctx context.Context, token string) (context.Context, error)

	// GetInvoice returns what the hosted invoice page shows.
	GetInvoice(ctx context.Context) (*dto.HostedInvoiceResponse, error)

	// GetInvoicePDFUrl returns a short-lived download link for the invoice PDF.
	GetInvoicePDFUrl(ctx context.Context) (string, error)

	// PayInvoice starts a payment for the amount remaining through the default gateway.
	PayInvoice(ctx context.Context, req *dto.HostedInvoicePayRequest) (*dto.HostedInvoicePayResponse, error)
}

type hostedInvoiceService struct {
	ServiceParams
}

func NewHostedInvoiceService(params ServiceParams) HostedInvoiceService {
	return &hostedInvoiceService{ServiceParams: params}
}

func (s *hostedInvoiceService) Resolve(ctx context.Context, rawToken string) (context.Context, error) {
	tokenID, err := auth.NewHostedInvoiceSigner(s.Config).Verify(rawToken)
	if err != nil {
		return nil, errHostedInvoiceLinkNotValid()
	}

	// The only read without tenant filters; everything after it runs in the token's scope.
	token, err := s.HostedInvoiceTokenRepo.GetUnscoped(ctx, tokenID)
	if ierr.IsNotFound(err) {
		return nil, errHostedInvoiceLinkNotValid()
	}
	if err != nil {
		return nil, err
	}

	ctx = context.WithValue(ctx, types.CtxTenantID, token.TenantID)
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, token.EnvironmentID)

	t, err := s.TenantRepo.GetByID(ctx, token.TenantID)
	if ierr.IsNotFound(err) {
		return nil, errHostedInvoiceLinkNotValid()
	}
	if err != nil {
		return nil, err
	}

	inv, err := s.InvoiceRepo.Get(ctx, token.InvoiceID)
	if ierr.IsNotFound(err) {
		return nil, errHostedInvoiceLinkNotValid()
	}
	if err != nil {
		return nil, err
	}

	if !token.IsActive(time.Now().UTC()) ||
		inv.Metadata[types.InvoiceMetadataKeyStripeHostedInvoiceURL] != "" ||
		t.InternalStatus == types.TenantInternalStatusSuspended {
		s.Logger.Info(ctx, "hosted invoice link no longer valid", "token_id", token.ID, "invoice_id", inv.ID)
		return nil, ierr.NewError("hosted invoice link no longer valid").
			WithHintf("This link is no longer valid, contact %s.", t.Name).
			Mark(ierr.ErrNotFound)
	}

	ctx = context.WithValue(ctx, types.CtxHostedInvoiceID, inv.ID)
	ctx = context.WithValue(ctx, types.CtxHostedInvoiceTokenID, token.ID)
	ctx = types.SetActor(ctx, types.Actor{Type: types.ActorTypeCustomer, ID: inv.CustomerID}, types.SourceHostedInvoice)
	return ctx, nil
}

func (s *hostedInvoiceService) GetInvoice(ctx context.Context) (*dto.HostedInvoiceResponse, error) {
	inv, err := s.invoice(ctx)
	if err != nil {
		return nil, err
	}

	t, err := s.TenantRepo.GetByID(ctx, types.GetTenantID(ctx))
	if err != nil {
		return nil, err
	}

	filter := types.NewNoLimitPaymentFilter()
	filter.DestinationType = lo.ToPtr(string(types.PaymentDestinationTypeInvoice))
	filter.DestinationID = lo.ToPtr(inv.ID)
	payments, err := NewPaymentService(s.ServiceParams).ListPayments(ctx, filter)
	if err != nil {
		return nil, err
	}

	_, payable, err := s.payGateway(ctx, inv)
	if err != nil {
		return nil, err
	}

	return &dto.HostedInvoiceResponse{
		Tenant:  dto.HostedInvoiceTenant{Name: t.Name, BillingDetails: t.BillingDetails},
		Invoice: inv,
		Payments: lo.Map(payments.Items, func(p *dto.PaymentResponse, _ int) *dto.HostedInvoicePayment {
			return &dto.HostedInvoicePayment{
				ID:                p.ID,
				Amount:            p.Amount,
				Currency:          p.Currency,
				PaymentStatus:     p.PaymentStatus,
				PaymentGateway:    p.PaymentGateway,
				PaymentMethodType: p.PaymentMethodType,
				CreatedAt:         p.CreatedAt,
				SucceededAt:       p.SucceededAt,
				FailedAt:          p.FailedAt,
			}
		}),
		Pay: dto.HostedInvoicePay{Enabled: payable},
	}, nil
}

func (s *hostedInvoiceService) GetInvoicePDFUrl(ctx context.Context) (string, error) {
	return NewInvoiceService(s.ServiceParams).GetInvoicePDFUrl(ctx, dto.InvoicePDFRequest{InvoiceID: types.GetHostedInvoiceID(ctx)})
}

func (s *hostedInvoiceService) PayInvoice(ctx context.Context, req *dto.HostedInvoicePayRequest) (*dto.HostedInvoicePayResponse, error) {
	inv, err := s.invoice(ctx)
	if err != nil {
		return nil, err
	}

	gateway, payable, err := s.payGateway(ctx, inv)
	if err != nil {
		return nil, err
	}
	if !payable {
		return nil, ierr.NewError("hosted invoice not payable").
			WithHint("This invoice can't be paid right now.").
			Mark(ierr.ErrInvalidOperation)
	}

	// The gateway returns the customer to the hosted invoice page whatever the outcome.
	returnURL := hostedInvoiceURL(s.Config, types.GetHostedInvoiceTokenID(ctx))
	// The page's key is scoped to this invoice, so a key can never return another invoice's payment.
	idempotencyKey := idempotency.NewGenerator().GenerateKey(idempotency.ScopePayment, map[string]interface{}{
		"invoice_id":             inv.ID,
		"hosted_idempotency_key": req.IdempotencyKey,
	})
	payResp, err := NewPaymentService(s.ServiceParams).GetOrCreateInvoicePaymentLink(ctx, &dto.CreatePaymentRequest{
		IdempotencyKey:    idempotencyKey,
		DestinationType:   types.PaymentDestinationTypeInvoice,
		DestinationID:     inv.ID,
		PaymentMethodType: types.PaymentMethodTypePaymentLink,
		PaymentGateway:    lo.ToPtr(gateway),
		Amount:            inv.AmountRemaining,
		Currency:          inv.Currency,
		SuccessURL:        returnURL,
		CancelURL:         returnURL,
		ProcessPayment:    true,
	})
	if err != nil {
		return nil, err
	}

	return &dto.HostedInvoicePayResponse{
		PaymentID:     payResp.ID,
		InvoiceID:     inv.ID,
		Status:        payResp.PaymentStatus,
		Amount:        payResp.Amount,
		Currency:      payResp.Currency,
		PaymentAction: paymentActionFrom(payResp),
	}, nil
}

func (s *hostedInvoiceService) invoice(ctx context.Context) (*dto.InvoiceResponse, error) {
	return NewInvoiceService(s.ServiceParams).GetInvoiceWithBreakdown(ctx, dto.GetInvoiceWithBreakdownRequest{ID: types.GetHostedInvoiceID(ctx)})
}

// payGateway returns the gateway Pay uses: the first, alphabetically, that can create payment links.
// It reports false when the invoice cannot be paid now or the deployment does not serve the page.
func (s *hostedInvoiceService) payGateway(ctx context.Context, inv *dto.InvoiceResponse) (types.PaymentGatewayType, bool, error) {
	if err := NewInvoiceService(s.ServiceParams).CheckInvoicePayable(ctx, inv); err != nil {
		if ierr.IsInvalidOperation(err) {
			return "", false, nil
		}
		return "", false, err
	}

	if hostedInvoiceURL(s.Config, types.GetHostedInvoiceTokenID(ctx)) == "" {
		return "", false, nil
	}

	providers, err := NewPaymentProviderResolver(s.ServiceParams).ListProviders(ctx, inv.CustomerID)
	if err != nil {
		return "", false, err
	}
	provider, found := lo.Find(providers, func(p ProviderCapabilities) bool {
		return lo.ContainsBy(p.Capabilities, func(c types.IntegrationCapability) bool {
			return c.Type == types.IntegrationCapabilityPaymentLink
		})
	})
	if !found {
		return "", false, nil
	}
	return provider.Gateway, true, nil
}

func errHostedInvoiceLinkNotValid() error {
	return ierr.NewError("hosted invoice link not valid").
		WithHint("This link is not valid.").
		Mark(ierr.ErrNotFound)
}
