package service

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/types"
)

type MeterService interface {
	CreateMeter(ctx context.Context, req *dto.CreateMeterRequest) (*meter.Meter, error)
	GetMeter(ctx context.Context, id string) (*meter.Meter, error)
	GetAllMeters(ctx context.Context) ([]*meter.Meter, error)
	DisableMeter(ctx context.Context, id string) error
}

type meterService struct {
	meterRepo meter.Repository
}

func NewMeterService(meterRepo meter.Repository) MeterService {
	return &meterService{meterRepo: meterRepo}
}

func (s *meterService) CreateMeter(ctx context.Context, req *dto.CreateMeterRequest) (*meter.Meter, error) {
	if req == nil {
		return nil, fmt.Errorf("meter cannot be nil")
	}

	if req.EventName == "" {
		return nil, fmt.Errorf("event_name is required")
	}

	meter := req.ToMeter(types.GetTenantID(ctx), types.GetUserID(ctx))

	if err := meter.Validate(); err != nil {
		return nil, fmt.Errorf("validate meter: %w", err)
	}

	if err := s.meterRepo.CreateMeter(ctx, meter); err != nil {
		return nil, fmt.Errorf("create meter: %w", err)
	}

	return meter, nil
}

func (s *meterService) GetMeter(ctx context.Context, id string) (*meter.Meter, error) {
	if id == "" {
		return nil, fmt.Errorf("id is required")
	}
	return s.meterRepo.GetMeter(ctx, id)
}

func (s *meterService) GetAllMeters(ctx context.Context) ([]*meter.Meter, error) {
	return s.meterRepo.GetAllMeters(ctx)
}

func (s *meterService) DisableMeter(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("id is required")
	}
	return s.meterRepo.DisableMeter(ctx, id)
}
