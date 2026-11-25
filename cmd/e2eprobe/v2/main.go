package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/bootstrap"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/inbox"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/incident"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/ledger"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/scenario"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/scenarios"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/suite"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/ui"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/world"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"go.opentelemetry.io/otel/trace"
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

// describeSuite renders the suite first, so an operator sees what a suite
// would run even when validation then rejects it.
func describeSuite(path string, cat *scenario.Catalog, out, errOut io.Writer) error {
	s, err := suite.Load(path)
	if err != nil {
		return err
	}
	renderErr := suite.Describe(out, s, cat)
	if err := suite.Validate(s, cat); err != nil {
		fmt.Fprintf(errOut, "suite has problems:\n%v\n", err)
		return err
	}
	return renderErr
}

// cliOptions is what the command line selects; each command parses only its own flags.
type cliOptions struct {
	suitePath string
	only      []string
	scenario  string
	persona   string
	asJSON    bool
}

// parseCommandLine reads "command flags"; flags with no command mean run.
func parseCommandLine(usage io.Writer, args []string) (string, cliOptions, error) {
	command := "run"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(usage)
	var opts cliOptions
	fs.StringVar(&opts.suitePath, "suite", os.Getenv("E2EPROBE_SUITE_FILE"), "suite file; empty uses the embedded default")
	var only string
	switch command {
	case "run":
		fs.StringVar(&only, "only", "", "comma-separated scenario names to run; other bindings are left out and their tracker state is dropped")
	case "once":
		fs.StringVar(&opts.scenario, "scenario", "", "scenario to run")
		fs.StringVar(&opts.persona, "persona", "", "persona to run it on; optional when the suite binds the scenario to one persona")
		fs.BoolVar(&opts.asJSON, "json", false, "print the result as JSON")
	case "validate", "describe":
	default:
		return command, opts, fmt.Errorf("unknown command %q (run | once | validate | describe)", command)
	}
	if err := fs.Parse(args); err != nil {
		return command, opts, err
	}
	if fs.NArg() != 0 {
		return command, opts, fmt.Errorf("unexpected argument %q: the command goes before its flags", fs.Arg(0))
	}
	opts.only = splitList(only)
	return command, opts, nil
}

func main() {
	command, opts, err := parseCommandLine(os.Stderr, os.Args[1:])
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(2)
	}
	cat := newCatalog()
	switch command {
	case "validate":
		err = validateSuite(opts.suitePath, cat, os.Stdout)
	case "describe":
		err = describeSuite(opts.suitePath, cat, os.Stdout, os.Stderr)
	case "run":
		err = run(cat, opts.suitePath, opts.only)
	case "once":
		err = once(cat, opts.suitePath, opts.scenario, opts.persona, opts.asJSON, os.Stdout, os.Stderr)
	}
	if err != nil {
		if !errors.Is(err, errScenarioFailed) {
			fmt.Fprintf(os.Stderr, "%v\n", err)
		}
		os.Exit(1)
	}
}

// errScenarioFailed is once's exit status when the run itself reported a failure.
var errScenarioFailed = errors.New("scenario failed")

// runtime is a configured probe: everything run and once share.
type runtime struct {
	suite    *suite.Suite
	bindings []suite.Binding
	world    *world.World
	inbox    *inbox.Inbox
	runner   *scenario.Runner
}

type runtimeOptions struct {
	only          []string     // scenario names to keep; empty keeps every binding
	slack         bool         // deliver incidents to Slack when it is configured
	otelIncidents bool         // mirror incidents as spans
	stateFile     string       // incident snapshot path; empty keeps state in memory
	tracer        trace.Tracer // nil runs without tracing
}

func newRuntime(cat *scenario.Catalog, cfg *e2eprobe.Config, s *suite.Suite, client e2eprobe.Client, lg *logger.Logger, runID string, opts runtimeOptions) (*runtime, error) {
	bindings, err := s.Bindings(cat)
	if err != nil {
		return nil, err
	}
	if len(opts.only) > 0 {
		if bindings = filterBindings(bindings, opts.only); len(bindings) == 0 {
			return nil, fmt.Errorf("--only %s matches no binding in the suite; see describe", strings.Join(opts.only, ","))
		}
	}
	notifiers := []incident.Notifier{incident.LogNotifier(lg)}
	if opts.slack && (cfg.Slack.BotToken != "" || cfg.Slack.WebhookURL != "") {
		notifiers = append(notifiers, incident.SlackNotifier(cfg.Slack.WebhookURL, cfg.Slack.BotToken, cfg.Slack.Channel, nil, lg))
	}
	if opts.otelIncidents && opts.tracer != nil {
		notifiers = append(notifiers, incident.OTELNotifier(opts.tracer))
	}
	notifier := incident.Multi(notifiers...)
	tracker, err := incident.NewTracker(func(k incident.Key) incident.Policy {
		a := s.AlertPolicy(suiteEntryFor(s, k.Scenario))
		return incident.Policy{AfterFailures: a.AfterFailures, AfterDuration: a.AfterDuration.Duration, RenotifyEvery: a.RenotifyEvery.Duration, SkipAfter: a.SkipAfter.Duration}
	}, incident.DefaultRollup(), notifier, incident.FileStore(opts.stateFile), time.Now)
	if err != nil {
		return nil, err
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
		Tracer:    opts.tracer,
		RunID:     runID,
		Tick:      s.Defaults.Tick.Duration,
		Now:       time.Now,
		StartedAt: time.Now(),
	}
	runner, err := scenario.NewRunner(cat, deps, bindings, tracker, notifier, scenario.Options{
		Concurrency: s.Defaults.Concurrency,
		Jitter:      time.Minute,
		Heartbeat:   time.Hour,
		DigestEvery: s.Defaults.Digest.Every.Duration,
	})
	if err != nil {
		return nil, err
	}
	return &runtime{suite: s, bindings: bindings, world: wld, inbox: box, runner: runner}, nil
}

func newLogger(cfg *e2eprobe.Config) (*logger.Logger, error) {
	lg, err := logger.NewLogger(&config.Configuration{Logging: buildLoggingConfig(cfg)})
	if err != nil {
		return nil, fmt.Errorf("logger: %w", err)
	}
	for _, w := range cfg.Warnings {
		lg.Warn(context.Background(), "e2eprobe config warning", "warning", w)
	}
	return lg, nil
}

func newClient(ctx context.Context, cfg *e2eprobe.Config, lg *logger.Logger) e2eprobe.Client {
	var client e2eprobe.Client = e2eprobe.NewSDKClient(cfg.APIHost, cfg.APIKey)
	if cfg.DryRun {
		client = e2eprobe.NewDryRunClient(client, lg)
		lg.Info(ctx, "dry-run mode: mutating calls are logged, not sent")
	}
	return client
}

func orDefault(path, fallback string) string {
	if path != "" {
		return path
	}
	return fallback
}

func run(cat *scenario.Catalog, suitePath string, only []string) error {
	cfg, err := e2eprobe.LoadConfig()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if !cfg.Enabled {
		fmt.Println("E2EPROBE_ENABLED=false; nothing to do")
		return nil
	}
	suitePath = orDefault(suitePath, cfg.SuiteFile)
	s, err := loadSuite(suitePath, cat)
	if err != nil {
		return err
	}
	lg, err := newLogger(cfg)
	if err != nil {
		return err
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
	var tracer trace.Tracer
	if tp != nil {
		tracer = tp.Tracer("e2eprobe")
	}
	rt, err := newRuntime(cat, cfg, s, newClient(ctx, cfg, lg), lg, runID, runtimeOptions{
		only: only, slack: true, otelIncidents: cfg.OTEL.Enabled, stateFile: cfg.StateFile, tracer: tracer,
	})
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.Handle("/webhook", rt.inbox.Handler())
	mux.Handle("/healthz", rt.inbox.Handler())
	mux.Handle("/inbox/", rt.inbox.Handler())
	mux.Handle("/status", rt.runner.Handler())
	mux.Handle("/history", rt.runner.Handler())
	mux.Handle("GET /{$}", ui.Handler())
	mux.Handle("GET /ui", ui.Handler())
	port, err := inbox.NewServer(cfg.ListenerPort, mux).Start(ctx)
	if err != nil {
		return fmt.Errorf("listener: %w", err)
	}

	if err := rt.world.Provision(ctx); err != nil {
		// Scenarios skip while their persona is missing; the provision scenario retries.
		lg.Error(ctx, "initial provisioning incomplete", "error", err)
	}
	lg.Info(ctx, "e2eprobe starting",
		"run_id", runID, "host", cfg.APIHost, "suite", suitePath, "bindings", len(rt.bindings), "only", strings.Join(only, ","),
		"events_per_day", s.EventsPerDay(cat), "tick", s.Defaults.Tick.String(), "listener_port", port)
	rt.runner.Start(ctx)
	lg.Info(ctx, "e2eprobe shutdown", "run_id", runID)
	return nil
}

// once provisions the suite and runs one binding, for developing a scenario
// or confirming a region by hand: Slack stays quiet, incident state stays in
// memory and the exit status says whether the run passed.
func once(cat *scenario.Catalog, suitePath, scenarioName, personaName string, asJSON bool, out, errOut io.Writer) error {
	if scenarioName == "" {
		return errors.New("once: --scenario is required (see describe for the bindings)")
	}
	cfg, err := e2eprobe.LoadConfig()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if cfg.APIKey == "" {
		return errors.New("once needs E2EPROBE_API_KEY; the signup bootstrap applies to run only")
	}
	s, err := loadSuite(orDefault(suitePath, cfg.SuiteFile), cat)
	if err != nil {
		return err
	}
	lg, err := newLogger(cfg)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	runID := fmt.Sprintf("e2eprobe-once-%d", time.Now().Unix())
	rt, err := newRuntime(cat, cfg, s, newClient(ctx, cfg, lg), lg, runID, runtimeOptions{})
	if err != nil {
		return err
	}
	return runOnce(ctx, rt, scenarioName, personaName, asJSON, out, errOut)
}

// runOnce provisions, runs one binding and prints its result to out; notes
// that are not the result go to errOut so JSON output stays one document.
func runOnce(ctx context.Context, rt *runtime, scenarioName, personaName string, asJSON bool, out, errOut io.Writer) error {
	b, err := resolveBinding(rt.bindings, scenarioName, personaName)
	if err != nil {
		return err
	}
	if err := rt.world.Provision(ctx); err != nil {
		fmt.Fprintf(errOut, "provisioning incomplete: %v\n", err)
	}
	res := rt.runner.RunOnce(ctx, b)
	if asJSON {
		if err := json.NewEncoder(out).Encode(res); err != nil {
			return err
		}
	} else {
		printResult(out, b, res)
	}
	if res.Failed() {
		return errScenarioFailed
	}
	return nil
}

func printResult(out io.Writer, b suite.Binding, r e2eprobe.Result) {
	fmt.Fprintf(out, "%s: %s", incident.Key{Scenario: b.Scenario, Persona: b.Persona}, strings.ToUpper(string(r.Status)))
	if r.Step != "" {
		fmt.Fprintf(out, " at %s", r.Step)
	}
	if r.Reason != "" {
		fmt.Fprintf(out, " (%s)", r.Reason)
	}
	fmt.Fprintln(out)
	for _, m := range r.Mismatches {
		fmt.Fprintf(out, "  - %s\n", m)
	}
	if r.Err != nil {
		fmt.Fprintf(out, "  error: %v\n", r.Err)
	}
	keys := make([]string, 0, len(r.Attrs))
	for k := range r.Attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(out, "  %s=%s\n", k, r.Attrs[k])
	}
}

// resolveBinding picks the one binding a scenario and persona name; a persona
// is only needed when the suite binds the scenario to more than one.
func resolveBinding(bindings []suite.Binding, scenarioName, personaName string) (suite.Binding, error) {
	var matches []suite.Binding
	for _, b := range bindings {
		if b.Scenario == scenarioName && (personaName == "" || b.Persona == personaName) {
			matches = append(matches, b)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		if personaName != "" {
			return suite.Binding{}, fmt.Errorf("the suite does not bind %q to persona %q; see describe", scenarioName, personaName)
		}
		return suite.Binding{}, fmt.Errorf("the suite has no binding for %q; see describe", scenarioName)
	}
	personas := make([]string, 0, len(matches))
	for _, b := range matches {
		personas = append(personas, b.Persona)
	}
	return suite.Binding{}, fmt.Errorf("%q binds to %d personas (%s); pick one with --persona", scenarioName, len(matches), strings.Join(personas, ", "))
}

func filterBindings(bindings []suite.Binding, only []string) []suite.Binding {
	keep := map[string]bool{}
	for _, name := range only {
		keep[name] = true
	}
	var out []suite.Binding
	for _, b := range bindings {
		if keep[b.Scenario] {
			out = append(out, b)
		}
	}
	return out
}

func splitList(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
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
