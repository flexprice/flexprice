package profiling

import (
	"context"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/grafana/pyroscope-go"
	"go.uber.org/fx"
)

type Service struct {
	cfg      *config.Configuration
	logger   *logger.Logger
	profiler *pyroscope.Profiler
}

func Module() fx.Option {
	return fx.Module("profiling",
		fx.Provide(NewPyroscopeClient),
		fx.Invoke(RegisterHooks),
	)
}

func NewPyroscopeClient(cfg *config.Configuration, logger *logger.Logger) *Service {
	return &Service{
		cfg:      cfg,
		logger:   logger,
		profiler: nil,
	}
}

func RegisterHooks(lc fx.Lifecycle, svc *Service) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			if !svc.cfg.Pyroscope.Enabled {
				svc.logger.Info("Pyroscope is disabled")
				return nil
			}

			profiler, err := pyroscope.Start(
				pyroscope.Config{
					ApplicationName: svc.cfg.Pyroscope.AppName,
					ServerAddress:   svc.cfg.Pyroscope.ServerAddress,
					Logger:          svc.logger, // TODO(akshat): confirm if this would work
					ProfileTypes: []pyroscope.ProfileType{
						// these profile types are enabled by default:
						pyroscope.ProfileCPU,
						pyroscope.ProfileAllocObjects,
						pyroscope.ProfileAllocSpace,
						pyroscope.ProfileInuseObjects,
						pyroscope.ProfileInuseSpace,

						// these profile types are optional:
						pyroscope.ProfileGoroutines,
						pyroscope.ProfileMutexCount,
						pyroscope.ProfileMutexDuration,
						pyroscope.ProfileBlockCount,
						pyroscope.ProfileBlockDuration,
					},
				},
			)
			if err != nil {
				return err
			}
			svc.profiler = profiler
			return nil
		},
		OnStop: func(ctx context.Context) error {
			if svc.cfg.Pyroscope.Enabled {
				svc.logger.Info("Stopping Pyroscope")
				if err := svc.profiler.Stop(); err != nil {
					return err
				}
			}
			return nil
		},
	})
}
