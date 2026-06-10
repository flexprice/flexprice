package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/synthetic"
	checks_pkg "github.com/flexprice/flexprice/internal/synthetic/checks"
	"github.com/flexprice/flexprice/internal/types"
)

func main() {
	cfg, err := synthetic.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}
	if !cfg.Enabled {
		fmt.Println("SYNTHETIC_ENABLED=false; nothing to do")
		return
	}

	lg, err := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: types.LogLevelInfo}})
	if err != nil {
		fmt.Fprintf(os.Stderr, "logger: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	runID := fmt.Sprintf("syn-%d", time.Now().Unix())

	tp, shutdownTracer, err := synthetic.NewTracerProvider(ctx, cfg.OTEL, "synthetic")
	if err != nil {
		lg.Errorw("tracer init failed; continuing without OTEL", "error", err)
	}
	defer func() {
		shutCtx, cancelShut := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelShut()
		if shutdownTracer != nil {
			_ = shutdownTracer(shutCtx)
		}
	}()

	reporters := []synthetic.Reporter{synthetic.NewLogReporter(lg)}
	if cfg.Slack.WebhookURL != "" {
		reporters = append(reporters, synthetic.NewSlackReporter(cfg.Slack.WebhookURL, cfg.Slack.Channel, nil))
	}
	if tp != nil {
		reporters = append(reporters, synthetic.NewOTELReporter(tp.Tracer("synthetic")))
	}
	reporter := synthetic.NewCompositeReporter(reporters...)

	runner := synthetic.NewRunner(reporter, lg, runID)

	client := synthetic.NewSDKClient(cfg.APIHost, cfg.APIKey)
	reg := synthetic.NewRegistry()

	addCheck := func(check synthetic.Check, sched synthetic.Scheduler, key string) {
		if cfg.Checks[key].Enabled {
			runner.Add(check, sched)
		}
	}

	seed := checks_pkg.NewSeedEnsure(client, reg, runID)
	addCheck(seed, synthetic.NewOneShotScheduler(seed), "SEED_ENSURE")
	addCheck(seed, synthetic.NewTickerScheduler(seed, cfg.Checks["SEED_ENSURE"].Interval), "SEED_ENSURE")

	lg.Infow("synthetic probe starting", "run_id", runID, "host", cfg.APIHost, "checks", len(cfg.Checks))
	runner.Start(ctx)
	lg.Infow("synthetic probe shutdown")
}
