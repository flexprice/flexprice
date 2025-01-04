package meter

import "context"

type Repository interface {
	CreateMeter(ctx context.Context, meter *Meter) error
	GetMeter(ctx context.Context, id string) (*Meter, error)
	GetAllMeters(ctx context.Context) ([]*Meter, error)
	DisableMeter(ctx context.Context, id string) error
	UpdateMeter(ctx context.Context, id string, filters []Filter) error
}
