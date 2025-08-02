package pyroscope

import (
	"context"
	"strings"

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

// Module provides fx options for Pyroscope
func Module() fx.Option {
	return fx.Options(
		fx.Provide(NewPyroscopeService),
		fx.Invoke(RegisterHooks),
	)
}

// RegisterHooks registers lifecycle hooks for Pyroscope
func RegisterHooks(lc fx.Lifecycle, svc *Service) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			if !svc.cfg.Pyroscope.Enabled {
				svc.logger.Info("Pyroscope profiling is disabled")
				return nil
			}

			profileTypes := svc.getProfileTypes()

			// Log configuration details for debugging
			svc.logger.Infow("Starting Pyroscope with configuration",
				"server_address", svc.cfg.Pyroscope.ServerAddress,
				"application_name", svc.cfg.Pyroscope.ApplicationName,
				"has_basic_auth", svc.cfg.Pyroscope.BasicAuthUser != "",
				"sample_rate", svc.cfg.Pyroscope.SampleRate,
				"profile_types", profileTypes,
			)

			pyroscopeConfig := pyroscope.Config{
				ApplicationName: svc.cfg.Pyroscope.ApplicationName,
				ServerAddress:   svc.cfg.Pyroscope.ServerAddress,
				ProfileTypes:    profileTypes,
				SampleRate:      svc.cfg.Pyroscope.SampleRate,
				DisableGCRuns:   svc.cfg.Pyroscope.DisableGCRuns,
				Logger:          svc, // Use our custom logger
			}

			// Add authentication if provided
			if svc.cfg.Pyroscope.BasicAuthUser != "" {
				pyroscopeConfig.BasicAuthUser = svc.cfg.Pyroscope.BasicAuthUser
				pyroscopeConfig.BasicAuthPassword = svc.cfg.Pyroscope.BasicAuthPass
				svc.logger.Infow("Using basic authentication for Pyroscope")
			}

			profiler, err := pyroscope.Start(pyroscopeConfig)
			if err != nil {
				svc.logger.Errorw("Failed to initialize Pyroscope", "error", err)
				return err
			}
			svc.logger.Infow("Pyroscope profiling initialized successfully",
				"application_name", svc.cfg.Pyroscope.ApplicationName,
				"server_address", svc.cfg.Pyroscope.ServerAddress,
				"profile_types", profileTypes,
				"sample_rate", svc.cfg.Pyroscope.SampleRate,
			)

			// Store profiler reference for potential future use
			svc.profiler = profiler

			return nil
		},
		OnStop: func(ctx context.Context) error {
			if svc.cfg.Pyroscope.Enabled {
				svc.logger.Info("Stopping Pyroscope profiling")
				// Pyroscope doesn't provide an explicit stop method
				// It will automatically stop when the application exits
			}
			return nil
		},
	})
}

// Implement pyroscope.Logger interface for better debugging
func (s *Service) Debugf(format string, args ...interface{}) {
	return // force disable debug logging for now
	if s.cfg.Logging.Level == "debug" {
		s.logger.Debugf("[Pyroscope] "+format, args...)
	}
}

func (s *Service) Infof(format string, args ...interface{}) {
	s.logger.Infof("[Pyroscope] "+format, args...)
}

func (s *Service) Errorf(format string, args ...interface{}) {
	s.logger.Errorf("[Pyroscope] "+format, args...)
}

// NewPyroscopeService creates a new Pyroscope service
func NewPyroscopeService(cfg *config.Configuration, logger *logger.Logger) *Service {
	return &Service{
		cfg:    cfg,
		logger: logger,
	}
}

// IsEnabled returns whether Pyroscope profiling is enabled
func (s *Service) IsEnabled() bool {
	return s.cfg.Pyroscope.Enabled
}

// getProfileTypes converts string profile types to pyroscope.ProfileType
func (s *Service) getProfileTypes() []pyroscope.ProfileType {
	if len(s.cfg.Pyroscope.ProfileTypes) == 0 {
		// Default profile types
		return []pyroscope.ProfileType{
			pyroscope.ProfileCPU,
			pyroscope.ProfileInuseObjects,
			pyroscope.ProfileAllocObjects,
			pyroscope.ProfileInuseSpace,
			pyroscope.ProfileAllocSpace,
			pyroscope.ProfileGoroutines,
		}
	}

	var types []pyroscope.ProfileType
	for _, profileType := range s.cfg.Pyroscope.ProfileTypes {
		switch strings.ToLower(profileType) {
		case "cpu":
			types = append(types, pyroscope.ProfileCPU)
		case "inuse_objects":
			types = append(types, pyroscope.ProfileInuseObjects)
		case "alloc_objects":
			types = append(types, pyroscope.ProfileAllocObjects)
		case "inuse_space":
			types = append(types, pyroscope.ProfileInuseSpace)
		case "alloc_space":
			types = append(types, pyroscope.ProfileAllocSpace)
		case "goroutines":
			types = append(types, pyroscope.ProfileGoroutines)
		case "mutex_count":
			types = append(types, pyroscope.ProfileMutexCount)
		case "mutex_duration":
			types = append(types, pyroscope.ProfileMutexDuration)
		case "block_count":
			types = append(types, pyroscope.ProfileBlockCount)
		case "block_duration":
			types = append(types, pyroscope.ProfileBlockDuration)
		default:
			s.logger.Warnw("Unknown profile type", "type", profileType)
		}
	}

	return types
}

// TagWrapper adds profiling labels to a function execution
func (s *Service) TagWrapper(ctx context.Context, labels map[string]string, fn func(context.Context)) {
	if !s.IsEnabled() {
		fn(ctx)
		return
	}

	// Convert map to pyroscope.Labels format
	var labelPairs []string
	for key, value := range labels {
		labelPairs = append(labelPairs, key, value)
	}

	pyroscope.TagWrapper(ctx, pyroscope.Labels(labelPairs...), fn)
}
