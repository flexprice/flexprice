package export

import (
	"context"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors" // pragma: allowlist secret
	"github.com/flexprice/flexprice/internal/logger"      // pragma: allowlist secret
	"github.com/flexprice/flexprice/internal/types"       // pragma: allowlist secret
)

// maxExportRows caps events exports. Workers buffer the full CSV in memory.
const maxExportRows = 500_000

func enforceExportVolume(
	ctx context.Context,
	log *logger.Logger,
	entityType types.ScheduledTaskEntityType,
	tenantID string,
	startTime, endTime time.Time,
	rowCount int,
) error {
	allowed := rowCount <= maxExportRows
	log.Info(ctx, "export volume check",
		"tenant_id", tenantID,
		"entity_type", entityType,
		"start_time", startTime,
		"end_time", endTime,
		"row_count", rowCount,
		"limit", maxExportRows,
		"allowed", allowed,
	)
	if allowed {
		return nil
	}

	return ierr.NewError("export exceeds volume limit").
		WithHintf("This export would return about %d rows (limit %d). Narrow the date range and try again, or use a scheduled hourly/daily export.", rowCount, maxExportRows).
		WithReportableDetails(map[string]interface{}{
			"row_count":   rowCount,
			"limit":       maxExportRows,
			"entity_type": entityType,
		}).
		Mark(ierr.ErrValidation)
}
