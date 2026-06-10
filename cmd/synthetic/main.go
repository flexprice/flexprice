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

	var ingest *checks_pkg.EventIngestDriver
	if cfg.Checks["EVENT_INGEST_DRIVER"].Enabled {
		ingest = checks_pkg.NewEventIngestDriver(client, reg, cfg.EventIngestSeed, runID)
		runner.Add(ingest, synthetic.NewRateScheduler(ingest, cfg.EventIngestRate))
	}
	defer func() {
		if ingest != nil {
			_ = ingest.Close()
		}
	}()

	if cfg.Checks["ANALYTICS_PROBE"].Enabled {
		ap := checks_pkg.NewAnalyticsProbe(client, reg, runID)
		runner.Add(ap, synthetic.NewTickerScheduler(ap, cfg.Checks["ANALYTICS_PROBE"].Interval))
	}

	if cfg.Checks["WALLET_BALANCE_PROBE"].Enabled {
		wp := checks_pkg.NewWalletBalanceProbe(client, reg, runID)
		runner.Add(wp, synthetic.NewTickerScheduler(wp, cfg.Checks["WALLET_BALANCE_PROBE"].Interval))
	}

	if cfg.Checks["WALLET_DEBIT_VERIFICATION"].Enabled {
		wd := checks_pkg.NewWalletDebitVerification(client, reg, runID, checks_pkg.WalletDebitOpts{})
		runner.Add(wd, synthetic.NewTickerScheduler(wd, cfg.Checks["WALLET_DEBIT_VERIFICATION"].Interval))
	}

	if cfg.Checks["CYCLE_INVOICE_PROBE"].Enabled {
		ci := checks_pkg.NewCycleInvoiceProbe(client, reg, runID)
		runner.Add(ci, synthetic.NewTickerScheduler(ci, cfg.Checks["CYCLE_INVOICE_PROBE"].Interval))
	}

	if cfg.Checks["ENTITLEMENT_AND_USAGE_PROBE"].Enabled {
		eu := checks_pkg.NewEntitlementAndUsageProbe(client, reg, runID)
		runner.Add(eu, synthetic.NewTickerScheduler(eu, cfg.Checks["ENTITLEMENT_AND_USAGE_PROBE"].Interval))
	}

	lg.Infow("synthetic probe starting", "run_id", runID, "host", cfg.APIHost, "checks", len(cfg.Checks))
	runner.Start(ctx)
	lg.Infow("synthetic probe shutdown")
}
