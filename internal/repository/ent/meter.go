package ent

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/meter"
	domainMeter "github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type meterRepository struct {
	client postgres.IClient
	log    *logger.Logger
}

func NewMeterRepository(client postgres.IClient, log *logger.Logger) domainMeter.Repository {
	return &meterRepository{
		client: client,
		log:    log,
	}
}

func (r *meterRepository) CreateMeter(ctx context.Context, m *domainMeter.Meter) error {
	client := r.client.Querier(ctx)

	meter, err := client.Meter.Create().
		SetID(m.ID).
		SetTenantID(m.TenantID).
		SetEventName(m.EventName).
		SetName(m.Name).
		SetAggregation(m.ToEntAggregation()).
		SetFilters(m.ToEntFilters()).
		SetResetUsage(string(m.ResetUsage)).
		SetStatus(string(m.Status)).
		SetCreatedAt(m.CreatedAt).
		SetUpdatedAt(m.UpdatedAt).
		SetCreatedBy(m.CreatedBy).
		SetUpdatedBy(m.UpdatedBy).
		Save(ctx)

	if err != nil {
		return fmt.Errorf("failed to create meter: %w", err)
	}

	*m = *domainMeter.FromEnt(meter)
	return nil
}

func (r *meterRepository) GetMeter(ctx context.Context, id string) (*domainMeter.Meter, error) {
	client := r.client.Querier(ctx)

	m, err := client.Meter.Query().
		Where(
			meter.ID(id),
			meter.TenantID(types.GetTenantID(ctx)),
		).
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("meter not found")
		}
		return nil, fmt.Errorf("failed to get meter: %w", err)
	}

	return domainMeter.FromEnt(m), nil
}

func (r *meterRepository) GetAllMeters(ctx context.Context) ([]*domainMeter.Meter, error) {
	client := r.client.Querier(ctx)

	meters, err := client.Meter.Query().
		Where(meter.TenantID(types.GetTenantID(ctx))).
		Order(ent.Desc(meter.FieldCreatedAt)).
		All(ctx)

	if err != nil {
		return nil, fmt.Errorf("failed to list meters: %w", err)
	}

	return domainMeter.FromEntList(meters), nil
}

func (r *meterRepository) DisableMeter(ctx context.Context, id string) error {
	client := r.client.Querier(ctx)

	_, err := client.Meter.Update().
		Where(
			meter.ID(id),
			meter.TenantID(types.GetTenantID(ctx)),
		).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return fmt.Errorf("meter not found")
		}
		return fmt.Errorf("failed to disable meter: %w", err)
	}

	return nil
}

func (r *meterRepository) UpdateMeter(ctx context.Context, id string, filters []domainMeter.Filter) error {
	client := r.client.Querier(ctx)

	r.log.Debug("updating meter",
		"meter_id", id,
		"tenant_id", types.GetTenantID(ctx),
	)

	m := &domainMeter.Meter{Filters: filters}
	_, err := client.Meter.Update().
		Where(
			meter.ID(id),
			meter.TenantID(types.GetTenantID(ctx)),
		).
		SetFilters(m.ToEntFilters()).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return fmt.Errorf("meter not found")
		}
		return fmt.Errorf("failed to update meter: %w", err)
	}

	return nil
}
