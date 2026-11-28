package service

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

const eventLookupMaxVersions = 50

// DebugEvent powers GET /events/:id (event debugger UI).
// Reads meter_usage instead of the removed feature_usage table.
func (s *meterUsageService) DebugEvent(ctx context.Context, externalCustomerID, eventID string, window events.TimeRange) (*dto.GetEventByIDResponse, error) {
	rawEvents, err := s.EventRepo.ListEventsByID(ctx, externalCustomerID, eventID, window, eventLookupMaxVersions)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get event from events table").
			Mark(ierr.ErrDatabase)
	}
	if len(rawEvents) == 0 {
		return nil, ierr.NewError("event not found").
			WithHint("Event not found in events table for this time range; pass start_time and end_time to search a different window").
			WithReportableDetails(map[string]interface{}{
				"event_id":             eventID,
				"external_customer_id": externalCustomerID,
				"start_time":           window.Start,
				"end_time":             window.End,
			}).
			Mark(ierr.ErrNotFound)
	}

	event := rawEvents[0]
	tenantID := types.GetTenantID(ctx)
	envID := types.GetEnvironmentID(ctx)

	response := &dto.GetEventByIDResponse{
		Event: eventToDTO(event),
	}

	meterUsages, err := s.MeterUsageRepo.GetEventsByEventIDAndExternalCustomerID(ctx, tenantID, envID, externalCustomerID, eventID, window)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get event from meter_usage table").
			Mark(ierr.ErrDatabase)
	}

	response.Events = eventsToDTOs(rawEvents)

	if len(meterUsages) > 0 {
		processed, err := s.fanOutMeterUsageToLineItems(ctx, meterUsages)
		if err == nil && len(processed) > 0 {
			response.Status = types.EventProcessingStatusTypeProcessed
			response.ProcessedEvents = processed
			return response, nil
		}
		// Fall through to tracker if fan-out failed or produced nothing —
		// the event reached meter_usage but no active line item claimed it.
	}

	response.Status = types.EventProcessingStatusTypeProcessing
	tracker := s.runDebugTracker(ctx, event, meterUsages, window)
	response.DebugTracker = tracker
	if tracker.FailurePoint != nil {
		response.Status = types.EventProcessingStatusTypeFailed
	}
	return response, nil
}

func eventToDTO(event *events.Event) *dto.Event {
	return &dto.Event{
		ID:                 event.ID,
		EventName:          event.EventName,
		ExternalCustomerID: event.ExternalCustomerID,
		CustomerID:         event.CustomerID,
		Timestamp:          event.Timestamp,
		IngestedAt:         event.IngestedAt,
		Properties:         event.Properties,
		Source:             event.Source,
		EnvironmentID:      event.EnvironmentID,
	}
}

func eventsToDTOs(rawEvents []*events.Event) []*dto.Event {
	out := make([]*dto.Event, 0, len(rawEvents))
	for _, ev := range rawEvents {
		out = append(out, eventToDTO(ev))
	}
	return out
}

// fanOutMeterUsageToLineItems turns an event's meter_usage rows (one per matched meter) into
// per-line-item FeatureUsageInfo entries, using the active line items on each row's meter.
func (s *meterUsageService) fanOutMeterUsageToLineItems(ctx context.Context, rows []*events.MeterUsage) ([]*dto.FeatureUsageInfo, error) {
	rowByMeterID := make(map[string]*events.MeterUsage, len(rows))
	for _, row := range rows {
		rowByMeterID[row.MeterID] = row
	}

	cust, err := s.CustomerRepo.GetByLookupKey(ctx, rows[0].ExternalCustomerID)
	if err != nil || cust == nil {
		return nil, err
	}

	subFilter := types.NewSubscriptionFilter()
	subFilter.CustomerID = cust.ID
	subFilter.WithLineItems = true
	subFilter.SubscriptionStatus = []types.SubscriptionStatus{
		types.SubscriptionStatusActive,
		types.SubscriptionStatusTrialing,
	}
	subService := NewSubscriptionService(s.ServiceParams)
	subs, err := subService.ListSubscriptions(ctx, subFilter)
	if err != nil {
		return nil, err
	}

	subIDs := make([]string, 0, len(subs.Items))
	for _, sub := range subs.Items {
		subIDs = append(subIDs, sub.ID)
	}
	if len(subIDs) == 0 {
		return nil, nil
	}

	liFilter := types.NewNoLimitSubscriptionLineItemFilter()
	liFilter.SubscriptionIDs = subIDs
	liFilter.MeterIDs = lo.Keys(rowByMeterID)
	liFilter.ActiveFilter = false
	lineItems, err := s.SubscriptionLineItemRepo.List(ctx, liFilter)
	if err != nil {
		return nil, err
	}

	// Resolve meter → feature (best-effort; feature_id is optional in the response).
	meterToFeatureID := map[string]string{}
	features, ferr := s.FeatureRepo.List(ctx, types.NewNoLimitFeatureFilter())
	if ferr == nil {
		for _, f := range features {
			if f.MeterID != "" {
				meterToFeatureID[f.MeterID] = f.ID
			}
		}
	}

	out := make([]*dto.FeatureUsageInfo, 0, len(lineItems))
	for _, li := range lineItems {
		mu, ok := rowByMeterID[li.MeterID]
		if !ok || !li.IsUsage() || !li.IsActive(mu.Timestamp) {
			continue
		}
		out = append(out, &dto.FeatureUsageInfo{
			CustomerID:     cust.ID,
			SubscriptionID: li.SubscriptionID,
			SubLineItemID:  li.ID,
			PriceID:        li.PriceID,
			MeterID:        li.MeterID,
			FeatureID:      meterToFeatureID[li.MeterID],
			QtyTotal:       mu.QtyTotal.String(),
			ProcessedAt:    mu.IngestedAt,
		})
	}
	return out, nil
}

// runDebugTracker walks customer → meter → price → line-item → meter_usage
// attribution and stops at the first failure. Non-empty meterUsages are reused
// for Step 5 to save an extra ClickHouse round-trip.
func (s *meterUsageService) runDebugTracker(ctx context.Context, event *events.Event, meterUsages []*events.MeterUsage, window events.TimeRange) *dto.DebugTracker {
	tenantID := types.GetTenantID(ctx)
	envID := types.GetEnvironmentID(ctx)
	tracker := &dto.DebugTracker{
		CustomerLookup:             &dto.CustomerLookupResult{Status: types.DebugTrackerStatusUnprocessed},
		MeterMatching:              &dto.MeterMatchingResult{Status: types.DebugTrackerStatusUnprocessed},
		PriceLookup:                &dto.PriceLookupResult{Status: types.DebugTrackerStatusUnprocessed},
		SubscriptionLineItemLookup: &dto.SubscriptionLineItemLookupResult{Status: types.DebugTrackerStatusUnprocessed},
	}

	cust, err := s.CustomerRepo.GetByLookupKey(ctx, event.ExternalCustomerID)
	if err != nil {
		status, code := ierr.ResolveError(err)
		errResp := &ierr.ErrorResponse{Code: code, Message: err.Error(), HTTPStatusCode: status}
		tracker.CustomerLookup.Status = types.DebugTrackerStatusError
		tracker.CustomerLookup.Error = errResp
		tracker.FailurePoint = &types.FailurePoint{FailurePointType: types.FailurePointTypeCustomerLookup, Error: errResp}
		return tracker
	}
	if cust == nil {
		msg := fmt.Sprintf("Customer not found for external_customer_id: %s", event.ExternalCustomerID)
		errResp := &ierr.ErrorResponse{Code: ierr.ErrCodeNotFound, Message: msg, HTTPStatusCode: 404}
		tracker.CustomerLookup.Status = types.DebugTrackerStatusNotFound
		tracker.CustomerLookup.Error = errResp
		tracker.FailurePoint = &types.FailurePoint{FailurePointType: types.FailurePointTypeCustomerLookup, Error: errResp}
		return tracker
	}
	tracker.CustomerLookup.Status = types.DebugTrackerStatusFound
	tracker.CustomerLookup.Customer = cust

	meterFilter := types.NewNoLimitMeterFilter()
	meterFilter.EventName = event.EventName
	meters, err := s.MeterRepo.List(ctx, meterFilter)
	if err != nil {
		status, code := ierr.ResolveError(err)
		errResp := &ierr.ErrorResponse{Code: code, Message: err.Error(), HTTPStatusCode: status}
		tracker.MeterMatching.Status = types.DebugTrackerStatusError
		tracker.MeterMatching.Error = errResp
		tracker.FailurePoint = &types.FailurePoint{FailurePointType: types.FailurePointTypeMeterLookup, Error: errResp}
		return tracker
	}
	matchedMeters := make([]dto.MatchedMeter, 0, len(meters))
	for _, m := range meters {
		if debugMeterMatches(event, m.Filters) {
			matchedMeters = append(matchedMeters, dto.MatchedMeter{MeterID: m.ID, EventName: m.EventName, Meter: m})
		}
	}
	if len(matchedMeters) == 0 {
		errResp := &ierr.ErrorResponse{
			Code:           ierr.ErrCodeNotFound,
			Message:        fmt.Sprintf("No meters found matching event_name: %s", event.EventName),
			HTTPStatusCode: 404,
		}
		tracker.MeterMatching.Status = types.DebugTrackerStatusNotFound
		tracker.FailurePoint = &types.FailurePoint{FailurePointType: types.FailurePointTypeMeterLookup, Error: errResp}
		return tracker
	}
	tracker.MeterMatching.Status = types.DebugTrackerStatusFound
	tracker.MeterMatching.MatchedMeters = matchedMeters

	meterIDs := make([]string, len(matchedMeters))
	for i, m := range matchedMeters {
		meterIDs[i] = m.MeterID
	}
	priceFilter := types.NewNoLimitPriceFilter().WithStatus(types.StatusPublished)
	priceFilter.MeterIDs = meterIDs
	prices, err := s.PriceRepo.List(ctx, priceFilter)
	if err != nil {
		status, code := ierr.ResolveError(err)
		errResp := &ierr.ErrorResponse{Code: code, Message: err.Error(), HTTPStatusCode: status}
		tracker.PriceLookup.Status = types.DebugTrackerStatusError
		tracker.PriceLookup.Error = errResp
		tracker.FailurePoint = &types.FailurePoint{FailurePointType: types.FailurePointTypePriceLookup, Error: errResp}
		return tracker
	}
	matchedPrices := make([]dto.MatchedPrice, 0, len(prices))
	for _, p := range prices {
		if p.IsUsage() {
			matchedPrices = append(matchedPrices, dto.MatchedPrice{PriceID: p.ID, MeterID: p.MeterID, Status: string(p.Status), Price: p})
		}
	}
	if len(matchedPrices) == 0 {
		errResp := &ierr.ErrorResponse{Code: ierr.ErrCodeNotFound, Message: "No prices found for matched meters", HTTPStatusCode: 404}
		tracker.PriceLookup.Status = types.DebugTrackerStatusNotFound
		tracker.FailurePoint = &types.FailurePoint{FailurePointType: types.FailurePointTypePriceLookup, Error: errResp}
		return tracker
	}
	tracker.PriceLookup.Status = types.DebugTrackerStatusFound
	tracker.PriceLookup.MatchedPrices = matchedPrices

	subFilter := types.NewSubscriptionFilter()
	subFilter.CustomerID = cust.ID
	subFilter.WithLineItems = true
	subFilter.SubscriptionStatus = []types.SubscriptionStatus{
		types.SubscriptionStatusActive,
		types.SubscriptionStatusTrialing,
	}
	subs, err := NewSubscriptionService(s.ServiceParams).ListSubscriptions(ctx, subFilter)
	if err != nil {
		status, code := ierr.ResolveError(err)
		errResp := &ierr.ErrorResponse{Code: code, Message: err.Error(), HTTPStatusCode: status}
		tracker.SubscriptionLineItemLookup.Status = types.DebugTrackerStatusError
		tracker.SubscriptionLineItemLookup.Error = errResp
		tracker.FailurePoint = &types.FailurePoint{FailurePointType: types.FailurePointTypeSubscriptionLineItemLookup, Error: errResp}
		return tracker
	}
	subIDs := make([]string, len(subs.Items))
	for i, sub := range subs.Items {
		subIDs[i] = sub.ID
	}
	priceIDs := make([]string, len(matchedPrices))
	for i, p := range matchedPrices {
		priceIDs[i] = p.PriceID
	}
	liFilter := types.NewNoLimitSubscriptionLineItemFilter()
	liFilter.SubscriptionIDs = subIDs
	liFilter.PriceIDs = priceIDs
	liFilter.MeterIDs = meterIDs
	liFilter.ActiveFilter = false
	lineItems, err := s.SubscriptionLineItemRepo.List(ctx, liFilter)
	if err != nil {
		status, code := ierr.ResolveError(err)
		errResp := &ierr.ErrorResponse{Code: code, Message: err.Error(), HTTPStatusCode: status}
		tracker.SubscriptionLineItemLookup.Status = types.DebugTrackerStatusError
		tracker.SubscriptionLineItemLookup.Error = errResp
		tracker.FailurePoint = &types.FailurePoint{FailurePointType: types.FailurePointTypeSubscriptionLineItemLookup, Error: errResp}
		return tracker
	}
	matchedLI := make([]dto.MatchedSubscriptionLineItem, 0, len(lineItems))
	for _, item := range lineItems {
		if !item.IsUsage() {
			continue
		}
		withinRange := event.Timestamp.After(item.StartDate) && (item.EndDate.IsZero() || event.Timestamp.Before(item.EndDate))
		matchedLI = append(matchedLI, dto.MatchedSubscriptionLineItem{
			SubLineItemID:        item.ID,
			SubscriptionID:       item.SubscriptionID,
			PriceID:              item.PriceID,
			StartDate:            item.StartDate,
			EndDate:              item.EndDate,
			IsActiveForEvent:     item.IsActive(event.Timestamp),
			TimestampWithinRange: withinRange,
			SubscriptionLineItem: item,
		})
	}
	if len(matchedLI) == 0 {
		errResp := &ierr.ErrorResponse{Code: ierr.ErrCodeNotFound, Message: "No subscription line items found for matched prices", HTTPStatusCode: 404}
		tracker.SubscriptionLineItemLookup.Status = types.DebugTrackerStatusNotFound
		tracker.SubscriptionLineItemLookup.Error = errResp
		tracker.FailurePoint = &types.FailurePoint{FailurePointType: types.FailurePointTypeSubscriptionLineItemLookup, Error: errResp}
		return tracker
	}
	tracker.SubscriptionLineItemLookup.MatchedLineItems = matchedLI
	hasActive := false
	for _, li := range matchedLI {
		if li.TimestampWithinRange {
			hasActive = true
			break
		}
	}
	if !hasActive {
		errResp := &ierr.ErrorResponse{
			Code:           ierr.ErrCodeNotFound,
			Message:        fmt.Sprintf("Found %d subscription line item(s) but none are active for event timestamp %s", len(matchedLI), event.Timestamp.Format(time.RFC3339)),
			HTTPStatusCode: 404,
		}
		tracker.SubscriptionLineItemLookup.Status = types.DebugTrackerStatusNotFound
		tracker.SubscriptionLineItemLookup.Error = errResp
		tracker.FailurePoint = &types.FailurePoint{FailurePointType: types.FailurePointTypeSubscriptionLineItemLookup, Error: errResp}
		return tracker
	}
	tracker.SubscriptionLineItemLookup.Status = types.DebugTrackerStatusFound

	// Step 5: meter_usage attribution — always run under the meter-usage pipeline.
	tracker.AttributedToCustomer = &dto.AttributedToCustomerResult{Status: types.DebugTrackerStatusUnprocessed}
	if len(meterUsages) == 0 {
		meterUsages, err = s.MeterUsageRepo.GetEventsByEventIDAndExternalCustomerID(ctx, tenantID, envID, event.ExternalCustomerID, event.ID, window)
		if err != nil {
			status, code := ierr.ResolveError(err)
			errResp := &ierr.ErrorResponse{Code: code, Message: err.Error(), HTTPStatusCode: status}
			tracker.AttributedToCustomer.Status = types.DebugTrackerStatusError
			tracker.AttributedToCustomer.Error = errResp
			tracker.FailurePoint = &types.FailurePoint{FailurePointType: types.FailurePointTypeAttributedToCustomer, Error: errResp}
			return tracker
		}
	}
	if len(meterUsages) == 0 {
		// Event hasn't landed in meter_usage yet — pipeline still in flight, not a hard failure.
		tracker.AttributedToCustomer.Status = types.DebugTrackerStatusProcessing
		return tracker
	}
	// Show the row of a meter an active line item bills, not whichever meter sorts first.
	mu := meterUsages[0]
	for _, row := range meterUsages {
		if lo.ContainsBy(matchedLI, func(li dto.MatchedSubscriptionLineItem) bool {
			return li.TimestampWithinRange && li.SubscriptionLineItem != nil && li.SubscriptionLineItem.MeterID == row.MeterID
		}) {
			mu = row
			break
		}
	}
	tracker.AttributedToCustomer.Status = types.DebugTrackerStatusAttributed
	tracker.AttributedToCustomer.MeterUsage = &dto.MeterUsageAttribution{
		MeterID:            mu.MeterID,
		ExternalCustomerID: mu.ExternalCustomerID,
		QtyTotal:           mu.QtyTotal.String(),
	}
	tracker.FailurePoint = nil
	return tracker
}

// debugMeterMatches checks that every meter filter has a matching event property.
// Kept local to this file — same shape as meterUsageTrackingService.checkMeterFilters
// but decoupled so this file has no cross-service dependency.
func debugMeterMatches(event *events.Event, filters []meter.Filter) bool {
	for _, f := range filters {
		val, ok := event.Properties[f.Key]
		if !ok {
			return false
		}
		vs := fmt.Sprintf("%v", val)
		match := false
		for _, allowed := range f.Values {
			if allowed == vs {
				match = true
				break
			}
		}
		if !match {
			return false
		}
	}
	return true
}
