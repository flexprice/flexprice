package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/e2eprobe"
	checks_pkg "github.com/flexprice/flexprice/internal/e2eprobe/checks"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

// buildLoggingConfig assembles the LoggingConfig used by logger.NewLogger.
// It wires OTLP log export from the standard OTEL env vars so that
// e2eprobe's structured logs land in the same SigNoz/Grafana pipeline as
// the app's — auth via OTEL_EXPORTER_OTLP_HEADERS (single "name=value" pair).
//
// Traces already flow through internal/e2eprobe/otel.go, which uses the
// SDK's implicit env var handling for endpoint/headers. This function
// only wires LOGS, which take an explicit config path.
func buildLoggingConfig(cfg *e2eprobe.Config) config.LoggingConfig {
	lc := config.LoggingConfig{
		Level: types.LogLevel(cfg.LogLevel),
	}
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if !cfg.OTEL.Enabled || endpoint == "" {
		return lc
	}
	lc.OtelEnabled = true
	lc.OtelEndpoint = endpoint
	protocol := os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL")
	if protocol == "" {
		protocol = "grpc"
	}
	lc.OtelProtocol = protocol
	if hdr, val, ok := parseFirstOTLPHeader(os.Getenv("OTEL_EXPORTER_OTLP_HEADERS")); ok {
		lc.OtelAuthHeader = hdr
		lc.OtelAuthValue = val
	}
	return lc
}

// parseFirstOTLPHeader extracts the first "name=value" pair from the
// standard OTLP headers env var (which allows comma-separated pairs).
// The logger's LoggingConfig only supports a single auth header, which is
// enough for common SigNoz / Grafana Cloud / Datadog access-token setups.
func parseFirstOTLPHeader(raw string) (string, string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", false
	}
	first, _, _ := strings.Cut(raw, ",")
	name, value, ok := strings.Cut(first, "=")
	if !ok {
		return "", "", false
	}
	name = strings.TrimSpace(name)
	value = strings.TrimSpace(value)
	if name == "" || value == "" {
		return "", "", false
	}
	return name, value, true
}

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

	lg, err := logger.NewLogger(&config.Configuration{Logging: buildLoggingConfig(cfg)})
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
		ap := checks_pkg.NewAnalyticsProbe(client, reg, runID, lg)
		runner.Add(ap, e2eprobe.NewTickerScheduler(ap, cfg.Checks["ANALYTICS_PROBE"].Interval))
	}

	if cfg.Checks["METER_AGGREGATION_PROBE"].Enabled {
		mapr := checks_pkg.NewMeterAggregationProbe(client, reg, runID, lg)
		runner.Add(mapr, e2eprobe.NewTickerScheduler(mapr, cfg.Checks["METER_AGGREGATION_PROBE"].Interval))
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

	if cfg.Checks["BUCKETED_METER_PROBE"].Enabled {
		bmp := checks_pkg.NewBucketedMeterProbe(client, reg, runID, lg)
		runner.Add(bmp, e2eprobe.NewTickerScheduler(bmp, cfg.Checks["BUCKETED_METER_PROBE"].Interval))
	}

	if cfg.Checks["COMMITMENT_TRUE_UP_PROBE"].Enabled {
		ctup := checks_pkg.NewCommitmentTrueUpProbe(client, reg, runID, lg)
		runner.Add(ctup, e2eprobe.NewTickerScheduler(ctup, cfg.Checks["COMMITMENT_TRUE_UP_PROBE"].Interval))
	}

	if cfg.Checks["ENTITLEMENT_ENFORCEMENT_PROBE"].Enabled {
		eep := checks_pkg.NewEntitlementEnforcementProbe(client, reg, runID, lg)
		runner.Add(eep, e2eprobe.NewTickerScheduler(eep, cfg.Checks["ENTITLEMENT_ENFORCEMENT_PROBE"].Interval))
	}

	if cfg.Checks["TAX_APPLICATION_PROBE"].Enabled {
		tap := checks_pkg.NewTaxApplicationProbe(client, reg, runID, lg)
		runner.Add(tap, e2eprobe.NewTickerScheduler(tap, cfg.Checks["TAX_APPLICATION_PROBE"].Interval))
	}

	if cfg.Checks["COUPON_APPLICATION_PROBE"].Enabled {
		cap_ := checks_pkg.NewCouponApplicationProbe(client, reg, runID, lg)
		runner.Add(cap_, e2eprobe.NewTickerScheduler(cap_, cfg.Checks["COUPON_APPLICATION_PROBE"].Interval))
	}

	if cfg.Checks["PERSISTENT_BILLING_INVARIANTS_PROBE"].Enabled {
		pbip := checks_pkg.NewPersistentBillingInvariantsProbe(client, reg, runID, lg)
		runner.Add(pbip, e2eprobe.NewTickerScheduler(pbip, cfg.Checks["PERSISTENT_BILLING_INVARIANTS_PROBE"].Interval))
	}

	if cfg.Checks["ENTITLEMENT_GRANT_ADDITIVE_PROBE"].Enabled {
		egap := checks_pkg.NewEntitlementGrantAdditiveProbe(client, reg, runID, lg)
		runner.Add(egap, e2eprobe.NewTickerScheduler(egap, cfg.Checks["ENTITLEMENT_GRANT_ADDITIVE_PROBE"].Interval))
	}

	// The listener is created regardless of the LOW_WALLET_ALERT_LISTENER flag
	// because LOW_BALANCE_ALERT_PROBE reads SeenThresholds from it to verify
	// webhook receipt. If neither the listener check nor the probe is enabled,
	// no HTTP server is bound.
	lwl := checks_pkg.NewLowWalletAlertListener(runID, lg)
	if cfg.Checks["LOW_WALLET_ALERT_LISTENER"].Enabled || cfg.Checks["LOW_BALANCE_ALERT_PROBE"].Enabled {
		wl := e2eprobe.NewHTTPWebhookListener(cfg.ListenerPort)
		runner.Add(lwl, e2eprobe.NewListenerScheduler(lwl, wl))
	}

	if cfg.Checks["LOW_BALANCE_ALERT_PROBE"].Enabled {
		lbap := checks_pkg.NewLowBalanceAlertProbe(client, reg, lwl, runID, lg, checks_pkg.LowBalanceAlertOpts{})
		runner.Add(lbap, e2eprobe.NewTickerScheduler(lbap, cfg.Checks["LOW_BALANCE_ALERT_PROBE"].Interval))
	}

	if cfg.Checks["JANITOR"].Enabled {
		jn := checks_pkg.NewJanitor(client, reg, cfg.JanitorMaxAge, runID)
		runner.Add(jn, e2eprobe.NewTickerScheduler(jn, cfg.Checks["JANITOR"].Interval))
	}

	lg.Info(ctx, "e2eprobe probe starting", "run_id", runID, "host", cfg.APIHost, "checks", len(cfg.Checks))
	runner.Start(ctx)
	lg.Info(ctx, "e2eprobe probe shutdown")
}
