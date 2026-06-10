//go:build synthetic_integration

package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/synthetic"
	checks_pkg "github.com/flexprice/flexprice/internal/synthetic/checks"
	"github.com/flexprice/flexprice/internal/types"
)

// Runs against local `make dev-setup`. Requires:
//
//	SYNTHETIC_API_HOST  (e.g. http://localhost:8080/v1)
//	SYNTHETIC_API_KEY   (a key in the local synthetic tenant)
//	go test -tags synthetic_integration ./cmd/synthetic/
func TestSynthetic_LocalSmoke(t *testing.T) {
	if os.Getenv("SYNTHETIC_API_HOST") == "" {
		t.Skip("SYNTHETIC_API_HOST not set; skipping integration smoke")
	}
	cfg, err := synthetic.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	lg, err := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: types.LogLevelInfo}})
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}
	client := synthetic.NewSDKClient(cfg.APIHost, cfg.APIKey)
	reg := synthetic.NewRegistry()
	rep := synthetic.NewLogReporter(lg)
	runner := synthetic.NewRunner(rep, lg, "integration-smoke")

	seed := checks_pkg.NewSeedEnsure(client, reg, "integration-smoke")
	runner.Add(seed, synthetic.NewOneShotScheduler(seed))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	runner.Start(ctx)
}
