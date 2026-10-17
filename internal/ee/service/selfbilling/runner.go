package selfbilling

import (
	"context"
	"errors"
	"time"
)

// Runner is what a scheduler calls once an hour.
type Runner interface {
	RunHourly(ctx context.Context) error
}

var _ Runner = (*selfBillingService)(nil)

// RunHourly pushes usage then syncs statuses; the sync runs even if the push fails and both errors are joined.
func (s *selfBillingService) RunHourly(ctx context.Context) error {
	if !s.Enabled() {
		return nil
	}
	pushErr := s.pushUsage(ctx, time.Now().UTC())
	if pushErr != nil {
		s.Logger.Error(ctx, "self-billing usage push failed", "error", pushErr)
	}
	return errors.Join(pushErr, s.syncStatuses(ctx))
}
