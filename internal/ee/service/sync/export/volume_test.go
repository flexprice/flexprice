package export

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/logger" // pragma: allowlist secret
	"github.com/flexprice/flexprice/internal/types"  // pragma: allowlist secret
)

func TestEnforceExportVolume_AllowsAtLimit(t *testing.T) {
	err := enforceExportVolume(
		context.Background(),
		logger.NewNoopLogger(),
		types.ScheduledTaskEntityTypeEvents,
		"tenant-1",
		time.Now().Add(-time.Hour),
		time.Now(),
		maxExportRows,
	)
	if err != nil {
		t.Fatalf("expected allow at limit %d, got %v", maxExportRows, err)
	}
}

func TestEnforceExportVolume_RejectsOverLimit(t *testing.T) {
	err := enforceExportVolume(
		context.Background(),
		logger.NewNoopLogger(),
		types.ScheduledTaskEntityTypeEvents,
		"tenant-1",
		time.Now().Add(-time.Hour),
		time.Now(),
		maxExportRows+1,
	)
	if err == nil {
		t.Fatal("expected volume limit error")
	}
}
