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
	"github.com/flexprice/flexprice/internal/e2eprobe"
	checks_pkg "github.com/flexprice/flexprice/internal/e2eprobe/checks"
	"github.com/flexprice/flexprice/internal/types"
)

func main() {
	cfg, err := e2eprobe.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}
	if !cfg.Enabled {
		fmt.Println("E2EPROBE_ENABLED=false; nothing to do")
		return
	}

	lg, err := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: types.LogLevelInfo}})
	if err != nil {
		fmt.Fprintf(os.Stderr, "logger: %v\n", err)
		os.Exit(1)
	}
	// Drain any config warnings (malformed env vars that fell back to defaults)
	// into the structured logger now that it exists.
	for _, w := range cfg.Warnings {
		lg.Warn(context.Background(), "e2eprobe config warning", "warning", w)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	runID := fmt.Sprintf("e2eprobe-%d", time.Now().Unix())

	tp, shutdownTracer, err := e2eprobe.NewTracerProvider(ctx, cfg.OTEL, "e2eprobe")
	if err != nil {
		lg.Error(ctx, "tracer init failed; continuing without OTEL", "error", err)
	}
	defer func() {
		shutCtx, cancelShut := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelShut()
		if shutdownTracer != nil {
			_ = shutdownTracer(shutCtx)
		}
	}()

	reporters := []e2eprobe.Reporter{e2eprobe.NewLogReporter(lg)}
	if cfg.Slack.WebhookURL != "" {
		reporters = append(reporters, e2eprobe.NewSlackReporter(cfg.Slack.WebhookURL, cfg.Slack.Channel, nil, lg))
	}
	if tp != nil {
		reporters = append(reporters, e2eprobe.NewOTELReporter(tp.Tracer("e2eprobe")))
	}
	reporter := e2eprobe.NewCompositeReporter(reporters...)

	globalAttrs := map[string]string{}
	if cfg.TenantID != "" {
		globalAttrs["tenant_id"] = cfg.TenantID
	}
	if cfg.EnvironmentID != "" {
		globalAttrs["environment_id"] = cfg.EnvironmentID
	}
	runner := e2eprobe.NewRunner(reporter, lg, runID, globalAttrs)
	runner.SetHeartbeatInterval(cfg.HeartbeatInterval)

	var client e2eprobe.Client = e2eprobe.NewSDKClient(cfg.APIHost, cfg.APIKey)
	if cfg.DryRun {
		client = e2eprobe.NewDryRunClient(client, lg)
		lg.Info(ctx, "dry-run mode enabled: mutating SDK calls will be logged as no-ops")
	}
	reg := e2eprobe.NewRegistry()

	addCheck := func(check e2eprobe.Check, sched e2eprobe.Scheduler, key string) {
		if cfg.Checks[key].Enabled {
			runner.Add(check, sched)
		}
	}

	seed := checks_pkg.NewSeedEnsure(client, reg, runID, lg)
	addCheck(seed, e2eprobe.NewTickerScheduler(seed, cfg.Checks["SEED_ENSURE"].Interval), "SEED_ENSURE")

	var ingest *checks_pkg.EventIngestDriver
	if cfg.Checks["EVENT_INGEST_DRIVER"].Enabled {
		ingest = checks_pkg.NewEventIngestDriver(client, reg, cfg.EventIngestSeed, runID)
		runner.Add(ingest, e2eprobe.NewRateScheduler(ingest, cfg.EventIngestRate))
	}
	defer func() {
		if ingest != nil {
			_ = ingest.Close()
		}
	}()

	if cfg.Checks["ANALYTICS_PROBE"].Enabled {
		ap := checks_pkg.NewAnalyticsProbe(client, reg, runID)
		runner.Add(ap, e2eprobe.NewTickerScheduler(ap, cfg.Checks["ANALYTICS_PROBE"].Interval))
	}

	if cfg.Checks["WALLET_BALANCE_PROBE"].Enabled {
		wp := checks_pkg.NewWalletBalanceProbe(client, reg, runID)
		runner.Add(wp, e2eprobe.NewTickerScheduler(wp, cfg.Checks["WALLET_BALANCE_PROBE"].Interval))
	}

	if cfg.Checks["WALLET_DEBIT_VERIFICATION"].Enabled {
		wd := checks_pkg.NewWalletDebitVerification(client, reg, runID, checks_pkg.WalletDebitOpts{})
		runner.Add(wd, e2eprobe.NewTickerScheduler(wd, cfg.Checks["WALLET_DEBIT_VERIFICATION"].Interval))
	}

	if cfg.Checks["CYCLE_INVOICE_PROBE"].Enabled {
		ci := checks_pkg.NewCycleInvoiceProbe(client, reg, runID)
		runner.Add(ci, e2eprobe.NewTickerScheduler(ci, cfg.Checks["CYCLE_INVOICE_PROBE"].Interval))
	}

	if cfg.Checks["ENTITLEMENT_AND_USAGE_PROBE"].Enabled {
		eu := checks_pkg.NewEntitlementAndUsageProbe(client, reg, runID)
		runner.Add(eu, e2eprobe.NewTickerScheduler(eu, cfg.Checks["ENTITLEMENT_AND_USAGE_PROBE"].Interval))
	}

	if cfg.Checks["NEW_CUSTOMER_LIFECYCLE"].Enabled {
		nl := checks_pkg.NewNewCustomerLifecycle(client, reg, runID, checks_pkg.NewCustomerLifecycleOpts{})
		runner.Add(nl, e2eprobe.NewTickerScheduler(nl, cfg.Checks["NEW_CUSTOMER_LIFECYCLE"].Interval))
	}

	if cfg.Checks["CANCEL_CUSTOMER_FLOW"].Enabled {
		cc := checks_pkg.NewCancelCustomerFlow(client, reg, runID, checks_pkg.InvoicePoll{})
		runner.Add(cc, e2eprobe.NewTickerScheduler(cc, cfg.Checks["CANCEL_CUSTOMER_FLOW"].Interval))
	}

	if cfg.Checks["SUBSCRIPTION_MODIFICATION_FLOW"].Enabled {
		smf := checks_pkg.NewSubscriptionModificationFlow(client, reg, runID)
		runner.Add(smf, e2eprobe.NewTickerScheduler(smf, cfg.Checks["SUBSCRIPTION_MODIFICATION_FLOW"].Interval))
	}

	if cfg.Checks["LOW_WALLET_ALERT_LISTENER"].Enabled {
		wl := e2eprobe.NewHTTPWebhookListener(cfg.ListenerPort)
		lwl := checks_pkg.NewLowWalletAlertListener(runID)
		runner.Add(lwl, e2eprobe.NewListenerScheduler(lwl, wl))
	}

	if cfg.Checks["JANITOR"].Enabled {
		jn := checks_pkg.NewJanitor(client, reg, 4*time.Hour, runID)
		runner.Add(jn, e2eprobe.NewTickerScheduler(jn, cfg.Checks["JANITOR"].Interval))
	}

	lg.Info(ctx, "e2eprobe probe starting", "run_id", runID, "host", cfg.APIHost, "checks", len(cfg.Checks))
	runner.Start(ctx)
	lg.Info(ctx, "e2eprobe probe shutdown")
}
