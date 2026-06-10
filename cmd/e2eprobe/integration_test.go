//go:build e2eprobe_integration

package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/e2eprobe"
	checks_pkg "github.com/flexprice/flexprice/internal/e2eprobe/checks"
	"github.com/flexprice/flexprice/internal/types"
)

// Runs against local `make dev-setup`. Requires:
//
//	E2EPROBE_API_HOST  (e.g. http://localhost:8080/v1)
//	E2EPROBE_API_KEY   (a key in the local e2eprobe tenant)
//	go test -tags e2eprobe_integration ./cmd/e2eprobe/
func TestE2EProbe_LocalSmoke(t *testing.T) {
	if os.Getenv("E2EPROBE_API_HOST") == "" || os.Getenv("E2EPROBE_API_KEY") == "" {
		t.Skip("E2EPROBE_API_HOST and E2EPROBE_API_KEY must both be set; skipping integration smoke")
	}
	cfg, err := e2eprobe.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	lg, err := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: types.LogLevelInfo}})
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}
	client := e2eprobe.NewSDKClient(cfg.APIHost, cfg.APIKey)
	reg := e2eprobe.NewRegistry()
	rep := e2eprobe.NewLogReporter(lg)
	runner := e2eprobe.NewRunner(rep, lg, "integration-smoke")

	seed := checks_pkg.NewSeedEnsure(client, reg, "integration-smoke")
	runner.Add(seed, e2eprobe.NewOneShotScheduler(seed))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	runner.Start(ctx)
}
