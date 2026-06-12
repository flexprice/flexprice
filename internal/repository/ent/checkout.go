package ent

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/ent"
	entcheckout "github.com/flexprice/flexprice/ent/checkout"
	domainCheckout "github.com/flexprice/flexprice/internal/domain/checkout"
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
		return fmt.Errorf("failed to serialize configuration: %w", err)
	}

	builder := client.Checkout.Create().
		SetID(c.ID).
		SetCustomerID(c.CustomerID).
		SetEntityType(c.EntityType).
		SetEntityID(c.EntityID).
		SetCheckoutType(c.CheckoutType).
		SetObjective(c.Objective).
		SetCheckoutStatus(c.Status).
		SetAmount(c.Amount).
		SetCurrency(c.Currency).
		SetProvider(c.Provider).
		SetExpiresAt(c.ExpiresAt).
		SetTenantID(c.TenantID).
		SetEnvironmentID(c.EnvironmentID).
		SetCreatedBy(c.CreatedBy).
		SetUpdatedBy(c.UpdatedBy)

	if c.SourceSubscriptionID != nil {
		builder.SetSourceSubscriptionID(*c.SourceSubscriptionID)
	}
	if c.ProviderSessionID != nil {
		builder.SetProviderSessionID(*c.ProviderSessionID)
	}
	if c.CheckoutURL != nil {
		builder.SetCheckoutURL(*c.CheckoutURL)
	}
	if c.SuccessURL != nil {
		builder.SetSuccessURL(*c.SuccessURL)
	}
	if c.CancelURL != nil {
		builder.SetCancelURL(*c.CancelURL)
	}
	if configMap != nil {
		builder.SetConfiguration(configMap)
	}

	if _, err := builder.Save(ctx); err != nil {
		SetSpanError(span, err)
		return fmt.Errorf("failed to create checkout: %w", err)
	}
	SetSpanSuccess(span)
	return nil
}

func (r *checkoutRepository) Get(ctx context.Context, id string) (*domainCheckout.Checkout, error) {
	client := r.client.Reader(ctx)

	span := StartRepositorySpan(ctx, "checkout", "get", map[string]interface{}{"checkout_id": id})
	defer FinishSpan(span)

	entity, err := client.Checkout.Query().
		Where(entcheckout.IDEQ(id)).
		Only(ctx)
	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("checkout not found: %w", err)
		}
		return nil, fmt.Errorf("failed to get checkout: %w", err)
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
		SetUpdatedBy(c.UpdatedBy)

	if c.ProviderSessionID != nil {
		builder.SetProviderSessionID(*c.ProviderSessionID)
	}
	if c.CheckoutURL != nil {
		builder.SetCheckoutURL(*c.CheckoutURL)
	}
	if c.CompletedAt != nil {
		builder.SetCompletedAt(*c.CompletedAt)
	}
	if c.CancelledAt != nil {
		builder.SetCancelledAt(*c.CancelledAt)
	}
	if c.ErrorMessage != nil {
		builder.SetErrorMessage(*c.ErrorMessage)
	}

	if _, err := builder.Save(ctx); err != nil {
		SetSpanError(span, err)
		return fmt.Errorf("failed to update checkout: %w", err)
	}
	SetSpanSuccess(span)
	return nil
}

func (r *checkoutRepository) GetPendingByEntity(
	ctx context.Context,
	entityType types.CheckoutEntityType,
	entityID string,
	objective types.CheckoutObjective,
) (*domainCheckout.Checkout, error) {
	client := r.client.Reader(ctx)

	span := StartRepositorySpan(ctx, "checkout", "get_pending_by_entity", map[string]interface{}{
		"entity_type": entityType,
		"entity_id":   entityID,
		"objective":   objective,
	})
	defer FinishSpan(span)

	entity, err := client.Checkout.Query().
		Where(
			entcheckout.EntityTypeEQ(entityType),
			entcheckout.EntityIDEQ(entityID),
			entcheckout.ObjectiveEQ(objective),
			entcheckout.CheckoutStatusEQ(types.CheckoutStatusPending),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			SetSpanSuccess(span)
			return nil, nil
		}
		SetSpanError(span, err)
		return nil, fmt.Errorf("failed to get pending checkout by entity: %w", err)
	}
	SetSpanSuccess(span)
	return domainCheckout.FromEnt(entity), nil
}

func (r *checkoutRepository) ListPendingExpired(ctx context.Context, cutoff time.Time) ([]*domainCheckout.Checkout, error) {
	client := r.client.Reader(ctx)

	span := StartRepositorySpan(ctx, "checkout", "list_pending_expired", nil)
	defer FinishSpan(span)

	entities, err := client.Checkout.Query().
		Where(
			entcheckout.CheckoutStatusEQ(types.CheckoutStatusPending),
			entcheckout.ExpiresAtLT(cutoff),
		).
		All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, fmt.Errorf("failed to list pending expired checkouts: %w", err)
	}
	SetSpanSuccess(span)
	return domainCheckout.FromEntList(entities), nil
}
