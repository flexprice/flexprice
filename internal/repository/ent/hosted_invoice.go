package ent

import (
	"context"
	"errors"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/hostedinvoicetoken"
	"github.com/flexprice/flexprice/ent/schema"
	domainHostedInvoiceToken "github.com/flexprice/flexprice/internal/domain/hostedinvoicetoken"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/lib/pq"
)

type hostedInvoiceTokenRepository struct {
	client postgres.IClient
	log    *logger.Logger
}

// Hosted invoice tokens are not cached: a public request must see a revoke at once.
func NewHostedInvoiceTokenRepository(client postgres.IClient, log *logger.Logger) domainHostedInvoiceToken.Repository {
	return &hostedInvoiceTokenRepository{client: client, log: log}
}

func (r *hostedInvoiceTokenRepository) Create(ctx context.Context, l *domainHostedInvoiceToken.HostedInvoiceToken) error {
	span := StartRepositorySpan(ctx, "hosted_invoice_token", "create", map[string]interface{}{
		"token_id":   l.ID,
		"invoice_id": l.InvoiceID,
	})
	defer FinishSpan(span)

	if l.EnvironmentID == "" {
		l.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	_, err := r.client.Writer(ctx).HostedInvoiceToken.Create().
		SetID(l.ID).
		SetInvoiceID(l.InvoiceID).
		SetExpiresAt(l.ExpiresAt).
		SetNillableRevokedAt(l.RevokedAt).
		SetStatus(string(l.Status)).
		SetTenantID(l.TenantID).
		SetEnvironmentID(l.EnvironmentID).
		SetCreatedBy(l.CreatedBy).
		SetUpdatedBy(l.UpdatedBy).
		Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		var pqErr *pq.Error
		if ent.IsConstraintError(err) && errors.As(err, &pqErr) && pqErr.Constraint == schema.Idx_hosted_invoice_token_invoice_live {
			return ierr.WithError(err).
				WithHint("The invoice already has a current hosted token").
				WithReportableDetails(map[string]any{"invoice_id": l.InvoiceID}).
				Mark(ierr.ErrAlreadyExists)
		}
		r.log.Error(ctx, "error creating hosted invoice token", "error", err, "token_id", l.ID, "invoice_id", l.InvoiceID)
		return ierr.WithError(err).
			WithHint("Failed to create hosted invoice token").
			Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return nil
}

func (r *hostedInvoiceTokenRepository) GetUnscoped(ctx context.Context, id string) (*domainHostedInvoiceToken.HostedInvoiceToken, error) {
	span := StartRepositorySpan(ctx, "hosted_invoice_token", "get_unscoped", map[string]interface{}{"token_id": id})
	defer FinishSpan(span)

	// Deliberately no tenant/environment filter: the public request learns its tenant from this row.
	token, err := r.client.Reader(ctx).HostedInvoiceToken.Query().
		Where(hostedinvoicetoken.ID(id)).
		Only(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, r.notFoundOrDatabase(err, "Hosted invoice token was not found", map[string]any{"token_id": id})
	}
	SetSpanSuccess(span)
	return domainHostedInvoiceToken.FromEnt(token), nil
}

func (r *hostedInvoiceTokenRepository) GetCurrentByInvoiceID(ctx context.Context, invoiceID string) (*domainHostedInvoiceToken.HostedInvoiceToken, error) {
	span := StartRepositorySpan(ctx, "hosted_invoice_token", "get_by_invoice", map[string]interface{}{"invoice_id": invoiceID})
	defer FinishSpan(span)

	token, err := r.client.Reader(ctx).HostedInvoiceToken.Query().
		Where(
			hostedinvoicetoken.TenantID(types.GetTenantID(ctx)),
			hostedinvoicetoken.EnvironmentID(types.GetEnvironmentID(ctx)),
			hostedinvoicetoken.InvoiceID(invoiceID),
			hostedinvoicetoken.RevokedAtIsNil(),
		).
		Only(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, r.notFoundOrDatabase(err, "The invoice has no current hosted token", map[string]any{"invoice_id": invoiceID})
	}
	SetSpanSuccess(span)
	return domainHostedInvoiceToken.FromEnt(token), nil
}

func (r *hostedInvoiceTokenRepository) ListCurrentByInvoiceIDs(ctx context.Context, invoiceIDs []string) ([]*domainHostedInvoiceToken.HostedInvoiceToken, error) {
	if len(invoiceIDs) == 0 {
		return []*domainHostedInvoiceToken.HostedInvoiceToken{}, nil
	}

	span := StartRepositorySpan(ctx, "hosted_invoice_token", "list_by_invoices", map[string]interface{}{"invoice_count": len(invoiceIDs)})
	defer FinishSpan(span)

	tokens, err := r.client.Reader(ctx).HostedInvoiceToken.Query().
		Where(
			hostedinvoicetoken.TenantID(types.GetTenantID(ctx)),
			hostedinvoicetoken.EnvironmentID(types.GetEnvironmentID(ctx)),
			hostedinvoicetoken.InvoiceIDIn(invoiceIDs...),
			hostedinvoicetoken.RevokedAtIsNil(),
		).
		All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).WithHint("Failed to list hosted invoice tokens").Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return domainHostedInvoiceToken.FromEntList(tokens), nil
}

func (r *hostedInvoiceTokenRepository) UpdateExpiresAt(ctx context.Context, invoiceID string, expiresAt time.Time) error {
	span := StartRepositorySpan(ctx, "hosted_invoice_token", "update_expires_at", map[string]interface{}{"invoice_id": invoiceID})
	defer FinishSpan(span)

	n, err := r.client.Writer(ctx).HostedInvoiceToken.Update().
		Where(
			hostedinvoicetoken.TenantID(types.GetTenantID(ctx)),
			hostedinvoicetoken.EnvironmentID(types.GetEnvironmentID(ctx)),
			hostedinvoicetoken.InvoiceID(invoiceID),
			hostedinvoicetoken.RevokedAtIsNil(),
		).
		SetExpiresAt(expiresAt).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).WithHint("Failed to update hosted invoice token expiry").Mark(ierr.ErrDatabase)
	}
	if n == 0 {
		return ierr.NewError("hosted invoice token not found").
			WithHint("The invoice has no current hosted token").
			WithReportableDetails(map[string]any{"invoice_id": invoiceID}).
			Mark(ierr.ErrNotFound)
	}
	SetSpanSuccess(span)
	return nil
}

func (r *hostedInvoiceTokenRepository) Revoke(ctx context.Context, id string) error {
	span := StartRepositorySpan(ctx, "hosted_invoice_token", "revoke", map[string]interface{}{"token_id": id})
	defer FinishSpan(span)

	now := time.Now().UTC()
	client := r.client.Writer(ctx)
	n, err := client.HostedInvoiceToken.Update().
		Where(
			hostedinvoicetoken.ID(id),
			hostedinvoicetoken.TenantID(types.GetTenantID(ctx)),
			hostedinvoicetoken.EnvironmentID(types.GetEnvironmentID(ctx)),
			hostedinvoicetoken.RevokedAtIsNil(),
		).
		SetRevokedAt(now).
		SetUpdatedAt(now).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).WithHint("Failed to revoke hosted invoice token").Mark(ierr.ErrDatabase)
	}
	if n > 0 {
		SetSpanSuccess(span)
		return nil
	}

	// Nothing updated: either already revoked (no-op) or unknown in this tenant.
	exists, err := client.HostedInvoiceToken.Query().
		Where(
			hostedinvoicetoken.ID(id),
			hostedinvoicetoken.TenantID(types.GetTenantID(ctx)),
			hostedinvoicetoken.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Exist(ctx)
	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).WithHint("Failed to revoke hosted invoice token").Mark(ierr.ErrDatabase)
	}
	if !exists {
		return ierr.NewError("hosted invoice token not found").
			WithHint("Hosted invoice token was not found").
			WithReportableDetails(map[string]any{"token_id": id}).
			Mark(ierr.ErrNotFound)
	}
	SetSpanSuccess(span)
	return nil
}

func (r *hostedInvoiceTokenRepository) RevokeByInvoiceID(ctx context.Context, invoiceID string) error {
	span := StartRepositorySpan(ctx, "hosted_invoice_token", "revoke_by_invoice", map[string]interface{}{"invoice_id": invoiceID})
	defer FinishSpan(span)

	now := time.Now().UTC()
	_, err := r.client.Writer(ctx).HostedInvoiceToken.Update().
		Where(
			hostedinvoicetoken.TenantID(types.GetTenantID(ctx)),
			hostedinvoicetoken.EnvironmentID(types.GetEnvironmentID(ctx)),
			hostedinvoicetoken.InvoiceID(invoiceID),
			hostedinvoicetoken.RevokedAtIsNil(),
		).
		SetRevokedAt(now).
		SetUpdatedAt(now).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).WithHint("Failed to revoke hosted invoice token").Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return nil
}

func (r *hostedInvoiceTokenRepository) notFoundOrDatabase(err error, hint string, details map[string]any) error {
	if ent.IsNotFound(err) {
		return ierr.WithError(err).WithHint(hint).WithReportableDetails(details).Mark(ierr.ErrNotFound)
	}
	return ierr.WithError(err).WithHint("Failed to get hosted invoice token").Mark(ierr.ErrDatabase)
}
