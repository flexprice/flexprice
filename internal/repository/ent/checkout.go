package ent

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/ent"
	entcheckout "github.com/flexprice/flexprice/ent/checkout"
	domainCheckout "github.com/flexprice/flexprice/internal/domain/checkout"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type checkoutRepository struct {
	client postgres.IClient
	logger *logger.Logger
}

// NewCheckoutRepository creates a new checkout repository.
func NewCheckoutRepository(client postgres.IClient, logger *logger.Logger) domainCheckout.Repository {
	return &checkoutRepository{client: client, logger: logger}
}

func (r *checkoutRepository) Create(ctx context.Context, c *domainCheckout.Checkout) error {
	client := r.client.Writer(ctx)

	span := StartRepositorySpan(ctx, "checkout", "create", map[string]interface{}{
		"checkout_id": c.ID,
		"entity_type": c.EntityType,
		"entity_id":   c.EntityID,
	})
	defer FinishSpan(span)

	configMap, err := c.GetConfigurationMap()
	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to serialize checkout configuration").
			Mark(ierr.ErrValidation)
	}

	builder := client.Checkout.Create().
		SetID(c.ID).
		SetCustomerID(c.CustomerID).
		SetEntityType(c.EntityType).
		SetEntityID(c.EntityID).
		SetCheckoutAction(c.CheckoutAction).
		SetMode(c.Mode).
		SetCheckoutStatus(c.Status).
		SetNillableAmount(c.Amount).
		SetCurrency(c.Currency).
		SetProvider(c.Provider).
		SetNillableProviderSessionID(c.ProviderSessionID).
		SetNillableCheckoutURL(c.CheckoutURL).
		SetNillableSuccessURL(c.SuccessURL).
		SetNillableCancelURL(c.CancelURL).
		SetExpiresAt(c.ExpiresAt).
		SetTenantID(c.TenantID).
		SetEnvironmentID(c.EnvironmentID).
		SetCreatedBy(c.CreatedBy).
		SetUpdatedBy(c.UpdatedBy)

	if configMap != nil {
		builder.SetConfiguration(configMap)
	}

	if _, err := builder.Save(ctx); err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to create checkout").
			Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return nil
}

func (r *checkoutRepository) Get(ctx context.Context, id string) (*domainCheckout.Checkout, error) {
	client := r.client.Reader(ctx)

	span := StartRepositorySpan(ctx, "checkout", "get", map[string]interface{}{"checkout_id": id})
	defer FinishSpan(span)

	entity, err := client.Checkout.Query().
		Where(
			entcheckout.IDEQ(id),
			entcheckout.TenantIDEQ(types.GetTenantID(ctx)),
			entcheckout.EnvironmentIDEQ(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)
	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("Checkout with ID %s was not found", id).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get checkout").
			Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return domainCheckout.FromEnt(entity), nil
}

func (r *checkoutRepository) Update(ctx context.Context, c *domainCheckout.Checkout) error {
	client := r.client.Writer(ctx)

	span := StartRepositorySpan(ctx, "checkout", "update", map[string]interface{}{
		"checkout_id": c.ID,
		"status":      c.Status,
	})
	defer FinishSpan(span)

	builder := client.Checkout.UpdateOneID(c.ID).
		SetCheckoutStatus(c.Status).
		SetUpdatedBy(c.UpdatedBy).
		SetNillableProviderSessionID(c.ProviderSessionID).
		SetNillableCheckoutURL(c.CheckoutURL).
		SetNillableCompletedAt(c.CompletedAt).
		SetNillableCancelledAt(c.CancelledAt).
		SetNillableFailureMessage(c.FailureMessage)

	if _, err := builder.Save(ctx); err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to update checkout").
			Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return nil
}

func (r *checkoutRepository) GetPendingByEntity(
	ctx context.Context,
	params domainCheckout.GetPendingByEntityParams,
) (*domainCheckout.Checkout, error) {
	client := r.client.Reader(ctx)

	span := StartRepositorySpan(ctx, "checkout", "get_pending_by_entity", map[string]interface{}{
		"entity_type": params.EntityType,
		"entity_id":   params.EntityID,
		"mode":        params.Mode,
	})
	defer FinishSpan(span)

	entity, err := client.Checkout.Query().
		Where(
			entcheckout.EntityTypeEQ(params.EntityType),
			entcheckout.EntityIDEQ(params.EntityID),
			entcheckout.ModeEQ(params.Mode),
			entcheckout.CheckoutStatusEQ(types.CheckoutStatusPending),
			entcheckout.TenantIDEQ(types.GetTenantID(ctx)),
			entcheckout.EnvironmentIDEQ(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			SetSpanSuccess(span)
			return nil, nil
		}
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to get pending checkout by entity").
			Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return domainCheckout.FromEnt(entity), nil
}

// ListPendingExpired performs a system-wide (cross-tenant/cross-environment) sweep
// of pending, expired checkouts for the cleanup cron. It is intentionally NOT
// tenant/environment scoped — the expiry worker is a maintenance job.
func (r *checkoutRepository) ListPendingExpired(ctx context.Context, cutoff time.Time, filter *types.QueryFilter) ([]*domainCheckout.Checkout, error) {
	client := r.client.Reader(ctx)

	span := StartRepositorySpan(ctx, "checkout", "list_pending_expired", nil)
	defer FinishSpan(span)

	q := client.Checkout.Query().
		Where(
			entcheckout.CheckoutStatusEQ(types.CheckoutStatusPending),
			entcheckout.ExpiresAtLT(cutoff),
		)

	if filter != nil && !filter.IsUnlimited() {
		q = q.Limit(filter.GetLimit()).Offset(filter.GetOffset())
	}

	entities, err := q.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list pending expired checkouts").
			Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return domainCheckout.FromEntList(entities), nil
}
