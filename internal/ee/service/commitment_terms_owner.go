package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"time"

	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	core "github.com/flexprice/flexprice/internal/ee/service/billingterms"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type commitmentTermsOwner struct {
	commitments   commitment.Repository
	subscriptions subscription.Repository
}

// NewCommitmentTermsOwner provides commitment economics and subscription alignment.
func NewCommitmentTermsOwner(commitments commitment.Repository, subscriptions subscription.Repository) domain.OwnerAdapter {
	return &commitmentTermsOwner{commitments: commitments, subscriptions: subscriptions}
}

func (a *commitmentTermsOwner) Type() types.BillingTermOwnerType {
	return types.BillingTermOwnerCommitment
}

func (a *commitmentTermsOwner) Snapshot(ctx context.Context, owner domain.OwnerRef) (*domain.OwnerSnapshot, error) {
	if ctx == nil {
		return nil, ierr.Validation("billing terms require tenant and environment context")
	}
	contextValue := reflect.ValueOf(ctx)
	if contextValue.Kind() == reflect.Pointer && contextValue.IsNil() {
		return nil, ierr.Validation("billing context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if types.GetTenantID(ctx) == "" || types.GetEnvironmentID(ctx) == "" {
		return nil, ierr.Validation("billing terms require tenant and environment context")
	}
	if owner.Type != a.Type() || owner.ID == "" {
		return nil, ierr.Validation("commitment collection requires a commitment owner ID")
	}
	if a == nil || a.commitments == nil || a.subscriptions == nil {
		return nil, ierr.NewError("commitment collection repositories are unavailable").Mark(ierr.ErrInternal)
	}
	ctx = types.WithForceWriter(ctx)
	contract, err := a.commitments.Get(ctx, owner.ID)
	if err != nil {
		return nil, err
	}
	if contract == nil || contract.ID != owner.ID || !commitmentTermsScope(ctx, contract.TenantID, contract.EnvironmentID, contract.Status) {
		return nil, ierr.NewError("commitment not found").Mark(ierr.ErrNotFound)
	}
	if !contract.Binding || contract.CollectionMethod == nil || *contract.CollectionMethod != types.CommitmentCollectionBillingTerms {
		return nil, ierr.Validation("commitment does not use billing terms collection")
	}
	if err := contract.CommitmentStatus.Validate(); err != nil {
		return nil, err
	}
	settlement, err := readCommitmentSettlement(contract)
	if err != nil {
		return nil, err
	}
	if !validCommitmentTermsTotal(contract.TotalAmount, contract.Currency) {
		return nil, ierr.Validation("billing terms require a positive currency-rounded total_amount within storage precision")
	}
	if !commitmentTermsDate(contract.StartDate) || !commitmentTermsDate(contract.EndDate) || !contract.StartDate.Before(contract.EndDate) || !commitmentTermsTimezone(contract.Timezone) {
		return nil, ierr.Validation("commitment collection requires a finite ordered term and IANA timezone")
	}
	subscription, err := a.subscription(ctx, contract.SubscriptionID)
	if err != nil {
		return nil, err
	}
	if contract.CustomerID == "" || contract.CustomerID != subscription.CustomerID || subscription.GetInvoicingCustomerID() == "" || !strings.EqualFold(contract.Currency, subscription.Currency) {
		return nil, ierr.Validation("commitment collection subscription attribution is inconsistent")
	}
	if !commitmentTermsDate(subscription.StartDate) || !commitmentTermsDate(subscription.BillingAnchor) || !commitmentTermsTimezone(subscription.Timezone) || contract.StartDate.Before(subscription.StartDate) || (subscription.EndDate != nil && (!commitmentTermsDate(*subscription.EndDate) || contract.EndDate.After(*subscription.EndDate) && settlement == nil)) {
		return nil, ierr.Validation("commitment collection term must fit the subscription calendar")
	}
	if err := subscription.SubscriptionStatus.Validate(); err != nil {
		return nil, err
	}
	if err := subscription.PauseStatus.Validate(); err != nil {
		return nil, err
	}
	if subscription.BillingCadence != types.BILLING_CADENCE_RECURRING || !commitmentTermsFrequency(subscription.BillingPeriod, subscription.BillingPeriodCount) {
		return nil, ierr.Validation("commitment collection requires a supported recurring subscription cadence")
	}
	for _, date := range []*time.Time{subscription.EndDate, subscription.TrialStart, subscription.TrialEnd, subscription.CancelAt, subscription.CancelledAt} {
		if date != nil && !commitmentTermsDate(*date) {
			return nil, ierr.Validation("subscription collection lifecycle dates must be finite")
		}
	}
	pause, err := a.pauseState(ctx, subscription)
	if err != nil {
		return nil, err
	}
	delivery := types.BillingTermSubscriptionInvoice
	if contract.InstalmentInvoicing != nil {
		delivery = types.BillingTermDelivery(*contract.InstalmentInvoicing)
	}
	if settlement != nil {
		delivery = settlement.Delivery
	}
	if err := delivery.Validate(); err != nil {
		return nil, err
	}
	destination := subscription
	scopes, err := commitmentEligibilityScopes(ctx, a, subscription)
	if err != nil {
		return nil, err
	}
	var destinationPause map[string]any
	if subscription.SubscriptionType == types.SubscriptionTypeGroupedInvoicing {
		destination, err = a.subscription(ctx, *subscription.ParentSubscriptionID)
		if err != nil {
			return nil, err
		}
		if !strings.EqualFold(destination.Currency, contract.Currency) || destination.GetInvoicingCustomerID() == "" {
			return nil, ierr.Validation("grouped collection destination has inconsistent currency or recipient")
		}
		if destination.SubscriptionStatus.Validate() != nil || destination.PauseStatus.Validate() != nil {
			return nil, ierr.Validation("grouped collection destination has invalid lifecycle state")
		}
		destinationPause, err = a.pauseState(ctx, destination)
		if err != nil {
			return nil, err
		}
	}
	calendar, err := core.NewCalendar(subscription.BillingAnchor, subscription.Timezone)
	if err != nil {
		return nil, err
	}
	calendar = &commitmentTermsCalendar{
		calendar: calendar,
		anchor:   subscription.BillingAnchor,
		timezone: subscription.Timezone,
		period:   subscription.BillingPeriod,
		count:    subscription.BillingPeriodCount,
		start:    contract.StartDate,
		end:      contract.EndDate,
		ended:    contract.CommitmentStatus != types.CommitmentStatusActive,
	}
	alignment, err := commitmentTermsKey(map[string]any{
		"policy":   "subscription_grid_v1",
		"anchor":   subscription.BillingAnchor.UTC(),
		"period":   subscription.BillingPeriod,
		"count":    subscription.BillingPeriodCount,
		"timezone": subscription.Timezone,
		"cadence":  subscription.BillingCadence,
	})
	if err != nil {
		return nil, err
	}
	lifecycle, err := commitmentTermsKey(map[string]any{
		"settlement":             settlement,
		"destination":            commitmentDestinationState(destination, destinationPause),
		"commitment_status":      contract.CommitmentStatus,
		"terminated_at":          commitmentTermsUTC(contract.TerminatedAt),
		"alignment":              alignment,
		"subscription_status":    subscription.SubscriptionStatus,
		"pause_status":           subscription.PauseStatus,
		"active_pause_id":        subscription.ActivePauseID,
		"pause":                  pause,
		"trial_start":            commitmentTermsUTC(subscription.TrialStart),
		"trial_end":              commitmentTermsUTC(subscription.TrialEnd),
		"cancel_at":              commitmentTermsUTC(subscription.CancelAt),
		"cancelled_at":           commitmentTermsUTC(subscription.CancelledAt),
		"cancel_at_period_end":   subscription.CancelAtPeriodEnd,
		"subscription_start":     subscription.StartDate.UTC(),
		"subscription_end":       commitmentTermsUTC(subscription.EndDate),
		"subscription_type":      subscription.SubscriptionType,
		"parent_subscription_id": subscription.ParentSubscriptionID,
	})
	if err != nil {
		return nil, err
	}
	reason := commitmentCollectionDeferred(ctx, subscription, destination, delivery, settlement != nil)
	terminal := contract.CommitmentStatus == types.CommitmentStatusTerminated || contract.TerminatedAt != nil
	canEdit := !terminal || settlement != nil
	if terminal && settlement == nil {
		reason = "settlement_required"
	}
	if (subscription.SubscriptionStatus == types.SubscriptionStatusCancelled || subscription.CancelledAt != nil) && settlement == nil {
		canEdit = false
		reason = "settlement_required"
	}
	target := *contract.TotalAmount
	if settlement != nil {
		target = settlement.Target
	}
	description := contract.InvoiceLabel
	if description == "" {
		description = contract.Name
	}
	return &domain.OwnerSnapshot{
		Owner: owner, TenantID: contract.TenantID, EnvironmentID: contract.EnvironmentID,
		OriginalTotal: *contract.TotalAmount, CollectionTarget: target,
		StartDate: contract.StartDate.UTC(), EndDate: contract.EndDate.UTC(), Timezone: contract.Timezone, Currency: strings.ToLower(contract.Currency),
		CustomerID: destination.GetInvoicingCustomerID(), SubscriptionID: lo.ToPtr(subscription.ID), Delivery: delivery,
		EligibilityScopes: scopes,
		CanEdit:           canEdit, CanCollect: reason == "", DeferredReason: reason, Description: description, DatedOnceOnly: contract.CommitmentStatus != types.CommitmentStatusActive,
		Calendar: calendar, CalendarAnchor: subscription.BillingAnchor.UTC(), AlignmentKey: alignment, LifecycleKey: lifecycle,
		DefaultInterval: subscription.BillingPeriod, DefaultIntervalCount: subscription.BillingPeriodCount,
	}, nil
}

func commitmentDestinationState(sub *subscription.Subscription, pause map[string]any) map[string]any {
	return map[string]any{"id": sub.ID, "status": sub.SubscriptionStatus, "pause_status": sub.PauseStatus, "pause": pause,
		"trial_start": commitmentTermsUTC(sub.TrialStart), "trial_end": commitmentTermsUTC(sub.TrialEnd),
		"cancel_at": commitmentTermsUTC(sub.CancelAt), "cancelled_at": commitmentTermsUTC(sub.CancelledAt), "cancel_at_period_end": sub.CancelAtPeriodEnd,
		"start": sub.StartDate.UTC(), "end": commitmentTermsUTC(sub.EndDate), "recipient": sub.GetInvoicingCustomerID(), "currency": sub.Currency,
		"type": sub.SubscriptionType, "parent": sub.ParentSubscriptionID}
}

func commitmentCollectionDeferred(ctx context.Context, sub, destination *subscription.Subscription, delivery types.BillingTermDelivery, settled bool) string {
	for _, participant := range []*subscription.Subscription{sub, destination} {
		if participant.SubscriptionStatus == types.SubscriptionStatusPaused || participant.PauseStatus == types.PauseStatusActive {
			return "subscription_paused"
		}
	}
	for _, participant := range []*subscription.Subscription{sub, destination} {
		if participant.SubscriptionStatus == types.SubscriptionStatusTrialing {
			return "subscription_trialing"
		}
	}
	for _, participant := range []*subscription.Subscription{sub, destination} {
		if participant.SubscriptionStatus == types.SubscriptionStatusCancelled || participant.CancelledAt != nil {
			if delivery == types.BillingTermSeparateInvoice && settled {
				continue
			}
			return "subscription_destination_unavailable"
		}
		if participant.SubscriptionStatus == types.SubscriptionStatusDraft || participant.SubscriptionStatus == types.SubscriptionStatusIncomplete {
			if participant.SubscriptionStatus == types.SubscriptionStatusIncomplete && destination.SubscriptionStatus == types.SubscriptionStatusIncomplete && pendingCollectionAllowed(ctx, destination, delivery) {
				continue
			}
			if participant.SubscriptionStatus == types.SubscriptionStatusDraft && participant.ID == destination.ID && pendingCollectionAllowed(ctx, destination, delivery) {
				continue
			}
			return "subscription_pending"
		}
	}
	return ""
}

func (a *commitmentTermsOwner) subscription(ctx context.Context, id string) (*subscription.Subscription, error) {
	if id == "" {
		return nil, ierr.Validation("commitment collection requires a subscription")
	}
	// List bypasses the subscription cache without acquiring a subscription row lock after an owner lock.
	rows, err := a.subscriptions.List(ctx, &types.SubscriptionFilter{
		QueryFilter:        &types.QueryFilter{Limit: lo.ToPtr(2), Status: lo.ToPtr(types.StatusPublished)},
		SubscriptionIDs:    []string{id},
		SubscriptionStatus: types.AllSubscriptionStatuses(),
	})
	if err != nil {
		return nil, err
	}
	if len(rows) != 1 || rows[0] == nil || rows[0].ID != id || !commitmentTermsScope(ctx, rows[0].TenantID, rows[0].EnvironmentID, rows[0].Status) {
		return nil, ierr.NewError("subscription not found").Mark(ierr.ErrNotFound)
	}
	return rows[0], nil
}

func (a *commitmentTermsOwner) pauseState(ctx context.Context, sub *subscription.Subscription) (map[string]any, error) {
	if sub.ActivePauseID == nil {
		return nil, nil
	}
	pause, err := a.subscriptions.GetPause(ctx, *sub.ActivePauseID)
	if err != nil {
		return nil, err
	}
	if pause == nil || pause.ID != *sub.ActivePauseID || pause.SubscriptionID != sub.ID || !commitmentTermsScope(ctx, pause.TenantID, pause.EnvironmentID, pause.Status) || !commitmentTermsDate(pause.PauseStart) {
		return nil, ierr.Validation("subscription collection pause state is inconsistent")
	}
	if err := pause.PauseStatus.Validate(); err != nil {
		return nil, err
	}
	for _, date := range []*time.Time{pause.PauseEnd, pause.ResumedAt} {
		if date != nil && !commitmentTermsDate(*date) {
			return nil, ierr.Validation("subscription collection pause dates must be finite")
		}
	}
	return map[string]any{
		"status":      pause.PauseStatus,
		"mode":        pause.PauseMode,
		"resume_mode": pause.ResumeMode,
		"start":       pause.PauseStart.UTC(),
		"end":         commitmentTermsUTC(pause.PauseEnd),
		"resumed_at":  commitmentTermsUTC(pause.ResumedAt),
	}, nil
}

type commitmentTermsCalendar struct {
	calendar domain.Calendar
	anchor   time.Time
	timezone string
	period   types.BillingPeriod
	count    int
	start    time.Time
	end      time.Time
	ended    bool
}

func (c *commitmentTermsCalendar) Next(start, end time.Time, interval types.BillingPeriod, count int) (time.Time, error) {
	if c == nil || c.calendar == nil || c.ended {
		return time.Time{}, ierr.Validation("ended commitment corrections require dated once rules")
	}
	baseFamily, baseUnits := commitmentTermsUnits(c.period)
	family, units := commitmentTermsUnits(interval)
	if !commitmentTermsFrequency(c.period, c.count) || !commitmentTermsFrequency(interval, count) || family != baseFamily || units*int64(count)%(baseUnits*int64(c.count)) != 0 {
		return time.Time{}, ierr.Validation("recurring commitment cadence must be a whole multiple of the subscription cadence")
	}
	if !commitmentTermsDate(start) || !commitmentTermsDate(end) || !start.Before(end) || start.Before(c.start) || end.After(c.end) {
		return time.Time{}, ierr.Validation("recurring commitment range must fit its owner term")
	}
	grid, err := types.NewBillingPeriodGrid(c.anchor, c.period, c.count, c.timezone)
	if err != nil {
		return time.Time{}, err
	}
	startAligned := start.Equal(c.start) || types.FullBillingPeriod(start, grid).Start.Equal(start)
	endAligned := end.Equal(c.end) || types.FullBillingPeriod(end, grid).Start.Equal(end)
	if !startAligned || !endAligned {
		return time.Time{}, ierr.Validation("interior recurring boundaries must align with the subscription calendar")
	}
	return c.calendar.Next(start, end, interval, count)
}

func commitmentTermsUnits(period types.BillingPeriod) (string, int64) {
	switch period {
	case types.BILLING_PERIOD_DAILY:
		return "days", 1
	case types.BILLING_PERIOD_WEEKLY:
		return "days", 7
	case types.BILLING_PERIOD_MONTHLY:
		return "months", 1
	case types.BILLING_PERIOD_QUARTER:
		return "months", 3
	case types.BILLING_PERIOD_HALF_YEAR:
		return "months", 6
	case types.BILLING_PERIOD_ANNUAL:
		return "months", 12
	default:
		return "", 0
	}
}

func commitmentTermsFrequency(period types.BillingPeriod, count int) bool {
	_, units := commitmentTermsUnits(period)
	return units > 0 && count > 0 && count <= math.MaxInt32
}

func commitmentTermsScope(ctx context.Context, tenantID, environmentID string, status types.Status) bool {
	return tenantID == types.GetTenantID(ctx) && environmentID == types.GetEnvironmentID(ctx) && status == types.StatusPublished
}

func commitmentTermsDate(date time.Time) bool {
	return !date.IsZero() && date.UTC().Year() >= 1 && date.UTC().Year() <= 9999
}

func commitmentTermsTimezone(timezone string) bool {
	_, err := time.LoadLocation(timezone)
	return timezone != "" && timezone != "Local" && err == nil
}

func validCommitmentTermsTotal(total *decimal.Decimal, currency string) bool {
	if total == nil || !total.IsPositive() || types.ValidateCurrencyCode(strings.ToLower(currency)) != nil {
		return false
	}
	precision := types.GetCurrencyPrecision(strings.ToLower(currency))
	magnitude := int64(len(total.Coefficient().String())) + int64(total.Exponent())
	return precision <= 8 && magnitude <= 12 && magnitude >= -int64(precision) && total.Equal(total.Round(precision))
}

func commitmentTermsUTC(date *time.Time) *time.Time {
	if date == nil {
		return nil
	}
	return lo.ToPtr(date.UTC())
}

func commitmentTermsKey(state map[string]any) (string, error) {
	encoded, err := json.Marshal(state)
	if err != nil {
		return "", ierr.WithError(err).WithHint("Invalid commitment collection state").Mark(ierr.ErrInternal)
	}
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:]), nil
}
