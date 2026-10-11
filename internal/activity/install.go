package activity

import (
	"context"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"go.uber.org/fx"
)

// Install adds the capture hook to the writer client and runs the publisher, unless the kill switch is off.
func Install(lc fx.Lifecycle, cfg *config.Configuration, clients *postgres.EntClients, reg *Registry, pub *Publisher, log *logger.Logger) {
	if !cfg.Activity.Enabled {
		return
	}
	clients.Writer.Use(Hook(reg, pub, HookOptions{
		Strict:     cfg.Deployment.Mode == types.ModeLocal,
		EntryPoint: string(cfg.Deployment.Mode),
	}))
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			pub.Start()
			log.Info(context.Background(), "activity log capture enabled")
			return nil
		},
		OnStop: func(ctx context.Context) error { return pub.Stop(ctx) },
	})
}
