package cron

import (
	"context"

	"github.com/flexprice/flexprice/internal/ee/service/selfbilling"
	"github.com/flexprice/flexprice/internal/logger"
)

// SelfBillingHourlyActivities wraps the hourly self-billing run.
type SelfBillingHourlyActivities struct {
	selfBilling selfbilling.Service
	logger      *logger.Logger
}

func NewSelfBillingHourlyActivities(
	selfBilling selfbilling.Service,
	log *logger.Logger,
) *SelfBillingHourlyActivities {
	return &SelfBillingHourlyActivities{
		selfBilling: selfBilling,
		logger:      log,
	}
}

// SelfBillingHourlyActivity pushes closed hours' usage to Flexprice Cloud and syncs tenant statuses.
func (a *SelfBillingHourlyActivities) SelfBillingHourlyActivity(ctx context.Context) error {
	if err := a.selfBilling.RunHourly(ctx); err != nil {
		a.logger.Error(ctx, "self-billing hourly run failed", "error", err)
		return err
	}
	return nil
}
