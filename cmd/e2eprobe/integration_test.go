//go:build e2eprobe_integration

package main

import (
	"os"
	"testing"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
)

// Runs against a live API. Requires E2EPROBE_API_HOST and E2EPROBE_API_KEY:
//
//	go test -tags e2eprobe_integration ./cmd/e2eprobe/
func TestE2EProbe_LocalSmoke(t *testing.T) {
	if os.Getenv("E2EPROBE_API_HOST") == "" || os.Getenv("E2EPROBE_API_KEY") == "" {
		t.Skip("E2EPROBE_API_HOST and E2EPROBE_API_KEY must both be set; skipping integration smoke")
	}
	if _, err := e2eprobe.LoadConfig(); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
}
