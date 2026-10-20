package service

import (
	"context"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/auth"
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

	// RecalculateExpiry resets the current token's expiry from the invoice's due date; no current token is a no-op.
	RecalculateExpiry(ctx context.Context, inv *invoice.Invoice) error

	// HostedInvoiceURLs returns each invoice's customer-facing link, keyed by invoice id: the Stripe
	// hosted URL if the invoice synced to Stripe, else the Flexprice page link for its active token
	// when hosted_invoice.base_url is set. Invoices with neither are absent.
	HostedInvoiceURLs(ctx context.Context, invoices []*invoice.Invoice) (map[string]string, error)
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

func (s *hostedInvoiceTokenService) RecalculateExpiry(ctx context.Context, inv *invoice.Invoice) error {
	token, err := s.HostedInvoiceTokenRepo.GetCurrentByInvoiceID(ctx, inv.ID)
	if ierr.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	expiresAt := hostedinvoicetoken.CalculateExpiry(inv.DueDate, lo.FromPtrOr(inv.FinalizedAt, token.CreatedAt), token.CreatedAt)
	if expiresAt.Equal(token.ExpiresAt) {
		return nil
	}
	// Not found here means the token was revoked after the read (Stripe switch), so there is nothing to update.
	if err := s.HostedInvoiceTokenRepo.UpdateExpiresAt(ctx, inv.ID, expiresAt); err != nil {
		if ierr.IsNotFound(err) {
			return nil
		}
		return err
	}
	s.Logger.Info(ctx, "recalculated hosted invoice token expiry", "token_id", token.ID, "invoice_id", inv.ID)
	return nil
}

func (s *hostedInvoiceTokenService) HostedInvoiceURLs(ctx context.Context, invoices []*invoice.Invoice) (map[string]string, error) {
	urls := make(map[string]string, len(invoices))
	pending := make([]string, 0, len(invoices))
	for _, inv := range invoices {
		if url := inv.Metadata[types.InvoiceMetadataKeyStripeHostedInvoiceURL]; url != "" {
			urls[inv.ID] = url
			continue
		}
		pending = append(pending, inv.ID)
	}

	// An empty base URL means this deployment does not serve the hosted invoice page.
	baseURL := strings.TrimRight(s.Config.HostedInvoice.BaseURL, "/")
	if baseURL == "" || len(pending) == 0 {
		return urls, nil
	}

	tokens, err := s.HostedInvoiceTokenRepo.ListCurrentByInvoiceIDs(ctx, pending)
	if err != nil {
		return nil, err
	}
	signer := auth.NewHostedInvoiceSigner(s.Config)
	now := time.Now().UTC()
	for _, token := range tokens {
		if token.IsActive(now) {
			urls[token.InvoiceID] = baseURL + "/" + signer.Sign(token.ID)
		}
	}
	return urls, nil
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
