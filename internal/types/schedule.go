package types

import (
	"fmt"
	"strings"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
)

// ScheduleID is the Temporal server schedule ID for a recurring workflow.
type ScheduleID string

const (
	ScheduleIDCreditGrantProcessing        ScheduleID = "credit-grants-processing"
	ScheduleIDSubscriptionAutoCancellation ScheduleID = "subscription-auto-cancellation"
	ScheduleIDWalletCreditExpiry           ScheduleID = "wallet-credit-expiry"
	ScheduleIDSubscriptionBillingPeriods   ScheduleID = "subscription-billing-periods"
	ScheduleIDSubscriptionRenewalAlerts    ScheduleID = "subscription-renewal-due-alerts"
	ScheduleIDSubscriptionTrialEndDue      ScheduleID = "subscription-trial-end-due"
)

// String returns the raw schedule id.
func (id ScheduleID) String() string { return string(id) }

// AllTemporalServerScheduleIDs returns every managed Temporal server schedule id
// (keep aligned with AllTemporalScheduleConfigs in internal/temporal/service/schedules.go).
func AllTemporalServerScheduleIDs() []ScheduleID {
	return []ScheduleID{
		ScheduleIDCreditGrantProcessing,
		ScheduleIDSubscriptionAutoCancellation,
		ScheduleIDWalletCreditExpiry,
		ScheduleIDSubscriptionBillingPeriods,
		ScheduleIDSubscriptionRenewalAlerts,
		ScheduleIDSubscriptionTrialEndDue,
	}
}

// Validate returns nil if this id is a known managed Temporal server schedule.
func (id ScheduleID) Validate() error {
	if id == "" {
		return ierr.NewError("schedule_id is required").
			WithHint("Must be a known server schedule id").
			Mark(ierr.ErrValidation)
	}
	allowed := AllTemporalServerScheduleIDs()
	if lo.Contains(allowed, id) {
		return nil
	}
	return ierr.NewError("invalid schedule_id").
		WithHint(fmt.Sprintf("Must be one of: %s", strings.Join(lo.Map(allowed, func(s ScheduleID, _ int) string { return string(s) }), ", "))).
		Mark(ierr.ErrValidation)
}

// ScheduleConfig is everything needed to create or update one Temporal server schedule.
type ScheduleConfig struct {
	ID        ScheduleID
	Interval  time.Duration
	Workflow  interface{}
	Input     interface{}
	TaskQueue TemporalTaskQueue
}
