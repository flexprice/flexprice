//go:build e2eprobe_integration

package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/suite"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/world"
)

// Provisions the default suite against a live API. Requires:
//
//	E2EPROBE_API_HOST  (e.g. http://localhost:8080/v1)
//	E2EPROBE_API_KEY   (a key in the probe tenant)
//	go test -tags e2eprobe_integration ./cmd/e2eprobe/
func TestE2EProbe_LocalSmoke(t *testing.T) {
	if os.Getenv("E2EPROBE_API_HOST") == "" || os.Getenv("E2EPROBE_API_KEY") == "" {
		t.Skip("E2EPROBE_API_HOST and E2EPROBE_API_KEY must both be set; skipping integration smoke")
	}
	cfg, err := e2eprobe.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cat := newCatalog()
	s, err := loadSuite(cfg.SuiteFile, cat)
	if err != nil {
		t.Fatalf("suite: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	w := world.New(e2eprobe.NewSDKClient(cfg.APIHost, cfg.APIKey), s, "integration-smoke", nil)
	if err := w.Provision(ctx); err != nil {
		t.Fatalf("provision: %v", err)
	}
	for _, p := range w.Personas() {
		t.Logf("persona %s ready=%v gaps=%v", p.Name(), p.Ready(), p.Gaps())
	}
	_ = suite.Default()
}
