package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/bootstrap"
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
	for _, w := range cfg.Warnings {
		lg.Warn(context.Background(), "e2eprobe config warning", "warning", w)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	runID := fmt.Sprintf("e2eprobe-%d", time.Now().Unix())
	if err := bootstrapIfNeeded(ctx, cfg, lg, runID); err != nil {
		lg.Error(ctx, "bootstrap", "error", err)
		os.Exit(1)
	}
	lg.Info(ctx, "e2eprobe: run not wired yet", "run_id", runID, "host", cfg.APIHost)
}
