package ent

import (
	"context"

	"github.com/flexprice/flexprice/ent"
	entpaymentmethod "github.com/flexprice/flexprice/ent/paymentmethod"
	domain "github.com/flexprice/flexprice/internal/domain/paymentmethod"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type paymentMethodRepository struct {
	client postgres.IClient
	log    *logger.Logger
}

func NewPaymentMethodRepository(client postgres.IClient, log *logger.Logger) domain.Repository {
	return &paymentMethodRepository{client: client, log: log}
}

func (r *paymentMethodRepository) Create(ctx context.Context, pm *domain.PaymentMethod) error {
	client := r.client.Writer(ctx)

	if pm.EnvironmentID == "" {
		pm.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	_, err := client.PaymentMethod.Create().
		SetID(pm.ID).
		SetTenantID(pm.TenantID).
		SetEnvironmentID(pm.EnvironmentID).
		SetCustomerID(pm.CustomerID).
		SetType(string(pm.Type)).
		SetGateway(string(pm.Gateway)).
		SetGatewayMethodID(pm.GatewayMethodID).
		SetPaymentMethodStatus(string(pm.PaymentMethodStatus)).
		SetIsDefault(pm.IsDefault).
		SetStatus(string(pm.Status)).
		SetMethodDetails(pm.MethodDetails).
		SetNillableCreatedBy(func() *string {
			if pm.CreatedBy == "" {
				return nil
			}
			return &pm.CreatedBy
		}()).
		Save(ctx)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to create payment method").
			Mark(ierr.ErrDatabase)
	}
	return nil
}

func (r *paymentMethodRepository) GetByID(ctx context.Context, id string) (*domain.PaymentMethod, error) {
	client := r.client.Reader(ctx)
	e, err := client.PaymentMethod.Query().
		Where(entpaymentmethod.ID(id)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.NewError("payment method not found").Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).Mark(ierr.ErrDatabase)
	}
	return domain.FromEnt(e), nil
}

func (r *paymentMethodRepository) List(ctx context.Context, filter *types.PaymentMethodFilter) ([]*domain.PaymentMethod, error) {
	client := r.client.Reader(ctx)

	query := client.PaymentMethod.Query().
		Where(entpaymentmethod.TenantID(types.GetTenantID(ctx)))

	if envID := types.GetEnvironmentID(ctx); envID != "" {
		query = query.Where(entpaymentmethod.EnvironmentID(envID))
	}

	if filter.CustomerID != "" {
		query = query.Where(entpaymentmethod.CustomerID(filter.CustomerID))
	}

	if filter.Status != nil {
		query = query.Where(entpaymentmethod.PaymentMethodStatus(string(*filter.Status)))
	}

	if filter.Gateway != nil {
		query = query.Where(entpaymentmethod.Gateway(*filter.Gateway))
	}

	// default: default methods first, then by creation time ascending
	query = query.Order(
		ent.Desc(entpaymentmethod.FieldIsDefault),
		ent.Asc(entpaymentmethod.FieldCreatedAt),
	)

	list, err := query.All(ctx)
	if err != nil {
		return nil, ierr.WithError(err).Mark(ierr.ErrDatabase)
	}
	return domain.FromEntList(list), nil
}

func (r *paymentMethodRepository) Update(ctx context.Context, pm *domain.PaymentMethod) error {
	client := r.client.Writer(ctx)
	q := client.PaymentMethod.UpdateOneID(pm.ID).
		SetPaymentMethodStatus(string(pm.PaymentMethodStatus)).
		SetIsDefault(pm.IsDefault).
		SetMethodDetails(pm.MethodDetails)
	if pm.UpdatedBy != "" {
		q = q.SetUpdatedBy(pm.UpdatedBy)
	}
	if _, err := q.Save(ctx); err != nil {
		return ierr.WithError(err).Mark(ierr.ErrDatabase)
	}
	return nil
}
