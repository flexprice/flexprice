package service

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// nanoUSDMultiplier scales a per-unit cost to the nano-USD unit that
// HuggingFace expects. Kept as a package-level var to avoid re-parsing on
// every request.
var nanoUSDMultiplier = decimal.NewFromInt(1_000_000_000)

// DebugEvent powers GET /events/:id (event debugger UI).
// Reads meter_usage instead of the removed feature_usage table.
func (s *meterUsageService) DebugEvent(ctx context.Context, eventID string) (*dto.GetEventByIDResponse, error) {
	event, err := s.EventRepo.GetEventByID(ctx, eventID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get event from events table").
			Mark(ierr.ErrDatabase)
	}

	tenantID := types.GetTenantID(ctx)
	envID := types.GetEnvironmentID(ctx)

	response := &dto.GetEventByIDResponse{
		Event: &dto.Event{
			ID:                 event.ID,
			EventName:          event.EventName,
			ExternalCustomerID: event.ExternalCustomerID,
			CustomerID:         event.CustomerID,
			Timestamp:          event.Timestamp,
			Properties:         event.Properties,
			Source:             event.Source,
			EnvironmentID:      event.EnvironmentID,
		},
	}

	meterUsage, err := s.MeterUsageRepo.GetByEventID(ctx, tenantID, envID, eventID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get event from meter_usage table").
			Mark(ierr.ErrDatabase)
	}

	if meterUsage != nil {
		processed, err := s.fanOutMeterUsageToLineItems(ctx, meterUsage)
		if err == nil && len(processed) > 0 {
			response.Status = types.EventProcessingStatusTypeProcessed
			response.ProcessedEvents = processed
			return response, nil
		}
		// Fall through to tracker if fan-out failed or produced nothing —
		// the event reached meter_usage but no active line item claimed it.
	}

	response.Status = types.EventProcessingStatusTypeProcessing
	tracker := s.runDebugTracker(ctx, event, meterUsage)
	response.DebugTracker = tracker
	if tracker.FailurePoint != nil {
		response.Status = types.EventProcessingStatusTypeFailed
	}
	return response, nil
}

// fanOutMeterUsageToLineItems turns one meter_usage row into per-line-item
// FeatureUsageInfo entries by resolving the customer's active subscription
// line items that reference this meter at the event's timestamp. Mirrors the
// per-line-item shape that feature_usage used to persist.
func (s *meterUsageService) fanOutMeterUsageToLineItems(ctx context.Context, mu *events.MeterUsage) ([]*dto.FeatureUsageInfo, error) {
	cust, err := s.CustomerRepo.GetByLookupKey(ctx, mu.ExternalCustomerID)
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
	liFilter.MeterIDs = []string{mu.MeterID}
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
		if !li.IsUsage() {
			continue
		}
		if !li.IsActive(mu.Timestamp) {
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
// attribution and stops at the first failure. When meterUsage is non-nil it is
// reused for Step 5 to save an extra ClickHouse round-trip.
func (s *meterUsageService) runDebugTracker(ctx context.Context, event *events.Event, meterUsage *events.MeterUsage) *dto.DebugTracker {
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
	mu := meterUsage
	if mu == nil {
		mu, err = s.MeterUsageRepo.GetByEventID(ctx, tenantID, envID, event.ID)
		if err != nil {
			status, code := ierr.ResolveError(err)
			errResp := &ierr.ErrorResponse{Code: code, Message: err.Error(), HTTPStatusCode: status}
			tracker.AttributedToCustomer.Status = types.DebugTrackerStatusError
			tracker.AttributedToCustomer.Error = errResp
			tracker.FailurePoint = &types.FailurePoint{FailurePointType: types.FailurePointTypeAttributedToCustomer, Error: errResp}
			return tracker
		}
	}
	if mu == nil {
		// Event hasn't landed in meter_usage yet — pipeline still in flight, not a hard failure.
		tracker.AttributedToCustomer.Status = types.DebugTrackerStatusProcessing
		return tracker
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

// GetHuggingFaceBillingData resolves per-event cost in nano-USD for the
// requested event IDs. Powers /events/huggingface-billing under the
// meter-usage pipeline. Docs: https://docs.flexprice.io/api-reference/events/get-hugging-face-inference-data
func (s *meterUsageService) GetHuggingFaceBillingData(ctx context.Context, req *dto.GetHuggingFaceBillingDataRequest) (*dto.GetHuggingFaceBillingDataResponse, error) {
	empty := &dto.GetHuggingFaceBillingDataResponse{Data: make([]dto.EventCostInfo, 0)}
	if len(req.EventIDs) == 0 {
		return empty, nil
	}
	tenantID := types.GetTenantID(ctx)
	envID := types.GetEnvironmentID(ctx)

	priceService := NewPriceService(s.ServiceParams)
	// customer cache: external_customer_id -> customer.ID (empty string = miss)
	custCache := map[string]string{}
	// line-item cache: (customer.ID, meter.ID) -> resolved price (nil = no match)
	// ponytail: naive per-request cache, N events tenants rarely repeat; drop if usage tightens
	type liKey struct{ CustID, MeterID string }
	priceCache := map[liKey]interface{}{} // *price.Price or nil

	out := make([]dto.EventCostInfo, 0, len(req.EventIDs))
	for _, eventID := range req.EventIDs {
		mu, err := s.MeterUsageRepo.GetByEventID(ctx, tenantID, envID, eventID)
		if err != nil {
			s.Logger.Info(ctx, "hf-billing: failed to load meter_usage", "event_id", eventID, "error", err)
			out = append(out, dto.EventCostInfo{EventID: eventID, CostInNanoUSD: decimal.Zero})
			continue
		}
		if mu == nil {
			out = append(out, dto.EventCostInfo{EventID: eventID, CostInNanoUSD: decimal.Zero})
			continue
		}

		custID, cached := custCache[mu.ExternalCustomerID]
		if !cached {
			cust, err := s.CustomerRepo.GetByLookupKey(ctx, mu.ExternalCustomerID)
			if err == nil && cust != nil {
				custID = cust.ID
			}
			custCache[mu.ExternalCustomerID] = custID
		}
		if custID == "" {
			out = append(out, dto.EventCostInfo{EventID: eventID, CostInNanoUSD: decimal.Zero})
			continue
		}

		key := liKey{CustID: custID, MeterID: mu.MeterID}
		resolvedPrice, seen := priceCache[key]
		if !seen {
			resolvedPrice = s.resolveActivePriceForCustomerMeter(ctx, custID, mu.MeterID, mu.Timestamp)
			priceCache[key] = resolvedPrice
		}
		p := resolvedPrice
		if p == nil {
			out = append(out, dto.EventCostInfo{EventID: eventID, CostInNanoUSD: decimal.Zero})
			continue
		}

		cost := priceService.CalculateCost(ctx, priceFromCache(p), mu.QtyTotal)
		out = append(out, dto.EventCostInfo{
			EventID:       eventID,
			CostInNanoUSD: cost.Mul(nanoUSDMultiplier),
		})
	}
	return &dto.GetHuggingFaceBillingDataResponse{Data: out}, nil
}

// resolveActivePriceForCustomerMeter finds the price on the customer's active
// subscription line item that references the given meter at eventTime. Returns
// nil when no active line item matches. Wrapped as interface{} in the cache so
// misses can be memoised alongside hits.
func (s *meterUsageService) resolveActivePriceForCustomerMeter(ctx context.Context, customerID, meterID string, eventTime time.Time) interface{} {
	subFilter := types.NewSubscriptionFilter()
	subFilter.CustomerID = customerID
	subFilter.WithLineItems = true
	subFilter.SubscriptionStatus = []types.SubscriptionStatus{
		types.SubscriptionStatusActive,
		types.SubscriptionStatusTrialing,
	}
	subs, err := NewSubscriptionService(s.ServiceParams).ListSubscriptions(ctx, subFilter)
	if err != nil || len(subs.Items) == 0 {
		return nil
	}
	subIDs := make([]string, len(subs.Items))
	for i, sub := range subs.Items {
		subIDs[i] = sub.ID
	}
	liFilter := types.NewNoLimitSubscriptionLineItemFilter()
	liFilter.SubscriptionIDs = subIDs
	liFilter.MeterIDs = []string{meterID}
	liFilter.ActiveFilter = false
	lineItems, err := s.SubscriptionLineItemRepo.List(ctx, liFilter)
	if err != nil {
		return nil
	}
	var priceID string
	for _, li := range lineItems {
		if !li.IsUsage() || !li.IsActive(eventTime) {
			continue
		}
		priceID = li.PriceID
		break
	}
	if priceID == "" {
		return nil
	}
	p, err := s.PriceRepo.Get(ctx, priceID)
	if err != nil {
		return nil
	}
	return p
}

// priceFromCache unpacks the interface{} used to memoise nil resolutions.
func priceFromCache(v interface{}) *price.Price {
	if v == nil {
		return nil
	}
	if p, ok := v.(*price.Price); ok {
		return p
	}
	return nil
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
