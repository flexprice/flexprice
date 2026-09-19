package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/revenuefact"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
)

const (
	exportDefaultPageSize = 1000
	exportMaxPageSize     = 5000
)

// ExportFacts pages the calling tenant's facts recomputed after `since`,
// ordered by (computed_at, id) so callers resume from the last row they saw.
// Denied unless the tenant opted in via revenue_analytics_config.
func (s *revenueService) ExportFacts(ctx context.Context, since time.Time, afterID string, limit int) ([]*revenuefact.RevenueFact, error) {
	setting, err := s.SettingsRepo.GetByKey(ctx, types.SettingKeyRevenueAnalyticsConfig)
	if err != nil {
		if ierr.IsNotFound(err) {
			return nil, exportNotEnabledError()
		}
		return nil, err
	}
	cfg, err := utils.ToStruct[types.RevenueAnalyticsConfig](setting.Value)
	if err != nil || !cfg.Enabled {
		return nil, exportNotEnabledError()
	}

	if limit <= 0 {
		limit = exportDefaultPageSize
	}
	if limit > exportMaxPageSize {
		limit = exportMaxPageSize
	}
	return s.RevenueFactRepo.ListForExport(ctx, since, afterID, limit)
}

func exportNotEnabledError() error {
	return ierr.NewError("revenue analytics is not enabled for this tenant").
		WithHint("Enable the revenue_analytics_config setting to export revenue facts").
		Mark(ierr.ErrPermissionDenied)
}
