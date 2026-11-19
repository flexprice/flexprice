package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/bootstrap"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/inbox"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/incident"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/ledger"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/scenario"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/scenarios"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/suite"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/world"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

// buildLoggingConfig wires OTLP log export from the standard OTEL env vars so
// the probe's logs land in the same pipeline as the app's. Traces are wired
// separately in internal/ee/e2eprobe/otel.go.
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

// parseFirstOTLPHeader extracts the first "name=value" pair from the OTLP
// headers env var; the logger supports a single auth header.
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

// mintFunc provisions a fresh credential. Injected so tests need no network.
type mintFunc func() (*bootstrap.Credentials, error)

// resolveAPIKey returns the API key the probe should run with. A stored key
// always wins: env vars resolve at container start, so a restart inside the
// same pod would otherwise mint a new key on every crash loop.
func resolveAPIKey(ctx context.Context, cfg *e2eprobe.Config, store bootstrap.SecretStore, inCluster bool, mint mintFunc) (string, error) {
	if inCluster && store != nil {
		stored, _, err := store.Read(ctx, cfg.PodNamespace, cfg.BootstrapSecretName, cfg.BootstrapSecretKey)
		if err != nil {
			return "", fmt.Errorf("bootstrap step=read_secret: %w", err)
		}
		if stored != "" {
			return stored, nil
		}
	}

	creds, err := mint()
	if err != nil {
		return "", err
	}

	if inCluster && store != nil {
		// Fatal on purpose: a probe that cannot persist its key would
		// re-bootstrap on every restart, minting keys nothing cleans up.
		if err := store.Write(ctx, cfg.PodNamespace, cfg.BootstrapSecretName, cfg.BootstrapSecretKey, creds); err != nil {
			return "", fmt.Errorf("bootstrap step=persist: %w", err)
		}
	}

	cfg.TenantID = creds.TenantID()
	cfg.EnvironmentID = creds.EnvironmentID()
	return creds.APIKey(), nil
}

// bootstrapIfNeeded mints and persists an API key when none is configured.
func bootstrapIfNeeded(ctx context.Context, cfg *e2eprobe.Config, lg *logger.Logger, runID string) error {
	if !cfg.NeedsBootstrap() {
		return nil
	}
	store, inCluster, err := bootstrap.NewK8sStore()
	if err != nil {
		return fmt.Errorf("bootstrap: kubernetes client unavailable: %w", err)
	}
	key, err := resolveAPIKey(ctx, cfg, store, inCluster, func() (*bootstrap.Credentials, error) {
		return bootstrap.Run(ctx, http.DefaultClient, cfg.APIHost, cfg.Email, cfg.Password, "e2eprobe-"+runID)
	})
	if err != nil {
		return fmt.Errorf("bootstrap failed: %w", err)
	}
	cfg.APIKey = key
	if !inCluster {
		// No Secret exists outside a cluster, so hand the operator the
		// credential they would otherwise have no way to keep.
		lg.Info(ctx, "bootstrap complete (not in-cluster): store this key as E2EPROBE_API_KEY to skip bootstrap next run",
			"api_key", cfg.APIKey, "tenant_id", cfg.TenantID, "environment_id", cfg.EnvironmentID)
		return nil
	}
	lg.Info(ctx, "bootstrap complete: credential written to secret",
		"secret", cfg.BootstrapSecretName, "tenant_id", cfg.TenantID, "environment_id", cfg.EnvironmentID)
	return nil
}

// newCatalog is every scenario this binary ships.
func newCatalog() *scenario.Catalog {
	cat := scenario.NewCatalog()
	scenarios.Register(cat)
	return cat
}

// loadSuite reads and validates the suite the probe will run.
func loadSuite(path string, cat *scenario.Catalog) (*suite.Suite, error) {
	s, err := suite.Load(path)
	if err != nil {
		return nil, err
	}
	if err := suite.Validate(s, cat); err != nil {
		return nil, err
	}
	return s, nil
}

func validateSuite(path string, cat *scenario.Catalog, out io.Writer) error {
	s, err := loadSuite(path, cat)
	if err != nil {
		return err
	}
	bindings, err := s.Bindings(cat)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "suite ok: %d personas, %d bindings, %d events/day\n", len(s.Personas), len(bindings), s.EventsPerDay(cat))
	return nil
}

func describeSuite(path string, cat *scenario.Catalog, out io.Writer) error {
	s, err := loadSuite(path, cat)
	if err != nil {
		return err
	}
	return suite.Describe(out, s, cat)
}

func main() {
	command, args := "run", os.Args[1:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet(command, flag.ExitOnError)
	suitePath := fs.String("suite", os.Getenv("E2EPROBE_SUITE_FILE"), "suite file; empty uses the embedded default")
	_ = fs.Parse(args)

	cat := newCatalog()
	switch command {
	case "validate":
		if err := validateSuite(*suitePath, cat, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
	case "describe":
		if err := describeSuite(*suitePath, cat, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
	case "run":
		if err := run(cat, *suitePath); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q (run | validate | describe)\n", command)
		os.Exit(2)
	}
}

func run(cat *scenario.Catalog, suitePath string) error {
	cfg, err := e2eprobe.LoadConfig()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if !cfg.Enabled {
		fmt.Println("E2EPROBE_ENABLED=false; nothing to do")
		return nil
	}
	if suitePath == "" {
		suitePath = cfg.SuiteFile
	}
	s, err := loadSuite(suitePath, cat)
	if err != nil {
		return err
	}
	bindings, err := s.Bindings(cat)
	if err != nil {
		return err
	}

	lg, err := logger.NewLogger(&config.Configuration{Logging: buildLoggingConfig(cfg)})
	if err != nil {
		return fmt.Errorf("logger: %w", err)
	}
	for _, w := range cfg.Warnings {
		lg.Warn(context.Background(), "e2eprobe config warning", "warning", w)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	runID := fmt.Sprintf("e2eprobe-%d", time.Now().Unix())
	if err := bootstrapIfNeeded(ctx, cfg, lg, runID); err != nil {
		return err
	}

	tp, shutdownTracer, err := e2eprobe.NewTracerProvider(ctx, cfg.OTEL, "e2eprobe")
	if err != nil {
		lg.Error(ctx, "tracer init failed; continuing without OTEL", "error", err)
	}
	defer func() {
		if shutdownTracer != nil {
			shutCtx, cancelShut := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancelShut()
			_ = shutdownTracer(shutCtx)
		}
	}()

	var client e2eprobe.Client = e2eprobe.NewSDKClient(cfg.APIHost, cfg.APIKey)
	if cfg.DryRun {
		client = e2eprobe.NewDryRunClient(client, lg)
		lg.Info(ctx, "dry-run mode: mutating calls are logged, not sent")
	}

	notifiers := []incident.Notifier{incident.LogNotifier(lg)}
	if cfg.Slack.BotToken != "" || cfg.Slack.WebhookURL != "" {
		notifiers = append(notifiers, incident.SlackNotifier(cfg.Slack.WebhookURL, cfg.Slack.BotToken, cfg.Slack.Channel, nil, lg))
	}
	if tp != nil && cfg.OTEL.Enabled {
		notifiers = append(notifiers, incident.OTELNotifier(tp.Tracer("e2eprobe")))
	}
	notifier := incident.Multi(notifiers...)
	tracker, err := incident.NewTracker(func(k incident.Key) incident.Policy {
		a := s.AlertPolicy(suiteEntryFor(s, k.Scenario))
		return incident.Policy{AfterFailures: a.AfterFailures, AfterDuration: a.AfterDuration.Duration, RenotifyEvery: a.RenotifyEvery.Duration, SkipAfter: a.SkipAfter.Duration}
	}, incident.DefaultRollup(), notifier, incident.FileStore(cfg.StateFile), time.Now)
	if err != nil {
		return err
	}

	box := inbox.New(inbox.Options{Token: cfg.WebhookToken})
	wld := world.New(client, s, runID, lg)
	deps := scenario.Deps{
		Client:    client,
		World:     wld,
		Ledger:    ledger.NewLedger(48 * time.Hour),
		Inbox:     box,
		Suite:     s,
		Logger:    lg,
		RunID:     runID,
		Tick:      s.Defaults.Tick.Duration,
		Now:       time.Now,
		StartedAt: time.Now(),
	}
	if tp != nil {
		deps.Tracer = tp.Tracer("e2eprobe")
	}
	runner, err := scenario.NewRunner(cat, deps, bindings, tracker, notifier, scenario.Options{
		Concurrency: s.Defaults.Concurrency,
		Jitter:      time.Minute,
		Heartbeat:   time.Hour,
		DigestEvery: s.Defaults.Digest.Every.Duration,
	})
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.Handle("/webhook", box.Handler())
	mux.Handle("/healthz", box.Handler())
	mux.Handle("/inbox/", box.Handler())
	mux.Handle("/status", runner.Handler())
	port, err := inbox.NewServer(cfg.ListenerPort, mux).Start(ctx)
	if err != nil {
		return fmt.Errorf("listener: %w", err)
	}

	if err := wld.Provision(ctx); err != nil {
		// Scenarios skip while their persona is missing; the provision scenario retries.
		lg.Error(ctx, "initial provisioning incomplete", "error", err)
	}
	lg.Info(ctx, "e2eprobe starting",
		"run_id", runID, "host", cfg.APIHost, "suite", suitePath, "bindings", len(bindings),
		"events_per_day", s.EventsPerDay(cat), "tick", s.Defaults.Tick.String(), "listener_port", port)
	runner.Start(ctx)
	lg.Info(ctx, "e2eprobe shutdown", "run_id", runID)
	return nil
}

// suiteEntryFor maps a catalog name back to the suite entry that enabled it,
// so family scenarios share their entry's alert override.
func suiteEntryFor(s *suite.Suite, scenarioName string) string {
	for _, sc := range s.Scenarios {
		if sc.Name == scenarioName {
			return sc.Name
		}
		for _, f := range sc.Families {
			if sc.Name+"-"+f == scenarioName {
				return sc.Name
			}
		}
	}
	return scenarioName
}
