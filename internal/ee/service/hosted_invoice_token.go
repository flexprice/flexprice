package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/hostedinvoicetoken"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// HostedInvoiceTokenService issues and revokes public hosted invoice tokens.
type HostedInvoiceTokenService interface {
	// Issue gives the invoice a token unless an integration hosts it; an invoice that already has one keeps it.
	Issue(ctx context.Context, inv *invoice.Invoice) error

	// Revoke revokes the invoice's current token if it has one.
	Revoke(ctx context.Context, invoiceID string) error
}

type hostedInvoiceTokenService struct {
	ServiceParams
}

func NewHostedInvoiceTokenService(params ServiceParams) HostedInvoiceTokenService {
	return &hostedInvoiceTokenService{ServiceParams: params}
}

func (s *hostedInvoiceTokenService) Issue(ctx context.Context, inv *invoice.Invoice) error {
	issue, err := s.shouldIssue(ctx, inv)
	if err != nil {
		return err
	}
	if !issue {
		s.Logger.Info(ctx, "skipped hosted invoice token, invoice syncs to an integration", "invoice_id", inv.ID)
		return nil
	}
	_, err = s.getOrCreate(ctx, inv)
	return err
}

func (s *hostedInvoiceTokenService) Revoke(ctx context.Context, invoiceID string) error {
	return s.HostedInvoiceTokenRepo.RevokeByInvoiceID(ctx, invoiceID)
}

func (s *hostedInvoiceTokenService) getOrCreate(ctx context.Context, inv *invoice.Invoice) (*hostedinvoicetoken.HostedInvoiceToken, error) {
	var token *hostedinvoicetoken.HostedInvoiceToken

	// The read-then-insert runs under an advisory lock keyed on the invoice, so a
	// concurrent issuer waits and then returns the committed token instead of hitting
	// the one-current-token index (which would abort the caller's transaction).
	err := s.DB.WithTx(ctx, func(ctx context.Context) error {
		if err := s.DB.LockWithWait(ctx, postgres.LockRequest{Key: hostedInvoiceTokenLockKey(ctx, inv.ID)}); err != nil {
			return ierr.WithError(err).
				WithHint("Failed to acquire hosted invoice token lock").
				Mark(ierr.ErrInternal)
		}

		existing, err := s.HostedInvoiceTokenRepo.GetCurrentByInvoiceID(ctx, inv.ID)
		if err == nil {
			token = existing
			return nil
		}
		if !ierr.IsNotFound(err) {
			return err
		}

		now := time.Now().UTC()
		token = &hostedinvoicetoken.HostedInvoiceToken{
			ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_HOSTED_INVOICE_TOKEN),
			EnvironmentID: types.GetEnvironmentID(ctx),
			InvoiceID:     inv.ID,
			ExpiresAt:     hostedinvoicetoken.CalculateExpiry(inv.DueDate, lo.FromPtrOr(inv.FinalizedAt, now), now),
			BaseModel:     types.GetDefaultBaseModel(ctx),
		}
		if err := s.HostedInvoiceTokenRepo.Create(ctx, token); err != nil {
			return err
		}
		s.Logger.Info(ctx, "issued hosted invoice token", "token_id", token.ID, "invoice_id", inv.ID)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return token, nil
}

// shouldIssue is the single place for integration rules on whether Flexprice hosts the invoice.
func (s *hostedInvoiceTokenService) shouldIssue(ctx context.Context, _ *invoice.Invoice) (bool, error) {
	conn, err := s.ConnectionRepo.GetByProvider(ctx, types.SecretProviderStripe)
	if ierr.IsNotFound(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return !conn.IsInvoiceOutboundEnabled(), nil
}

func hostedInvoiceTokenLockKey(ctx context.Context, invoiceID string) string {
	return "hosted_invoice_token:" + types.GetTenantID(ctx) + ":" + types.GetEnvironmentID(ctx) + ":" + invoiceID
}
