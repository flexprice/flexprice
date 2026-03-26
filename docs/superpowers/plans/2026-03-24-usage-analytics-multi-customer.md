# Usage Analytics Multi-Customer ID Support — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `ExternalCustomerIDs []string` to `GetUsageAnalyticsRequest` so callers get a single merged/aggregated analytics response across multiple customers; simplify `IncludeChildren` to use the same loop instead of a separate code path.

**Architecture:** Three changes to three files. A new package-level helper `resolveEffectiveExternalIDs` deduplicates the union of the two ID fields; `GetDetailedUsageAnalytics` (V2 primary) is refactored to loop over all resolved IDs using `mergeAnalyticsData` (already exists); V1 gets a one-line normalisation. `buildAggregatedAnalyticsWithChildren` is deleted.

**Tech Stack:** Go 1.23, Gin (`ShouldBindJSON`), `github.com/samber/lo` (dedup), `github.com/stretchr/testify/assert` (tests).

**Spec:** `docs/superpowers/specs/2026-03-24-usage-analytics-multi-customer-design.md`

---

## File Map

| File | Action | What changes |
|------|--------|-------------|
| `internal/api/dto/events.go` | Modify `:321-336` | Add `ExternalCustomerIDs`, drop `binding:"required"`, update `IncludeChildren` comment |
| `internal/service/feature_usage_tracking.go` | Modify `:1763-1793` | Replace `GetDetailedUsageAnalytics` body |
| `internal/service/feature_usage_tracking.go` | Modify `:1876-1888` | Replace `validateAnalyticsRequest` body |
| `internal/service/feature_usage_tracking.go` | Modify `:1913-1956` | Delete `buildAggregatedAnalyticsWithChildren` |
| `internal/service/feature_usage_tracking.go` | Modify `:3715-3731` | Replace `fetchCustomers` body |
| `internal/service/feature_usage_tracking.go` | Add after `:1896` | New `resolveEffectiveExternalIDs` function |
| `internal/service/event_post_processing.go` | Modify `:955-961` | Add one-line normalisation before existing validation |
| `internal/service/feature_usage_tracking_analytics_test.go` | Create | Unit tests for `resolveEffectiveExternalIDs` and `validateAnalyticsRequest` |

---

## Task 1 — DTO: add `ExternalCustomerIDs`, drop `binding:"required"`

**Files:**
- Modify: `internal/api/dto/events.go:321-336`

- [ ] **Open `internal/api/dto/events.go` and find `GetUsageAnalyticsRequest` (line 321).**

  Replace:
  ```go
  type GetUsageAnalyticsRequest struct {
  	ExternalCustomerID string           `json:"external_customer_id" binding:"required"`
  	FeatureIDs         []string         `json:"feature_ids,omitempty"`
  	Sources            []string         `json:"sources,omitempty"`
  	StartTime          time.Time        `json:"start_time,omitempty"`
  	EndTime            time.Time        `json:"end_time,omitempty"`
  	GroupBy            []string         `json:"group_by,omitempty"` // allowed values: "source", "feature_id", "properties.<field_name>"
  	WindowSize         types.WindowSize `json:"window_size,omitempty"`
  	Expand             []string         `json:"expand,omitempty"` // allowed values: "price", "meter", "feature", "subscription_line_item","plan","addon"
  	// Property filters to filter the events by the keys in `properties` field of the event
  	PropertyFilters map[string][]string `json:"property_filters,omitempty"`
  	// IncludeChildren when true aggregates child customers' usage into the parent's total and adds
  	// the parent itself as a breakdown item alongside children so that sum(breakdowns) == total.
  	// Default: false.
  	IncludeChildren bool `json:"include_children,omitempty"`
  }
  ```

  With:
  ```go
  type GetUsageAnalyticsRequest struct {
  	// ExternalCustomerID is the single external customer ID.
  	// Optional when ExternalCustomerIDs is provided; required otherwise.
  	ExternalCustomerID string `json:"external_customer_id"`
  	// ExternalCustomerIDs is a list of external customer IDs whose usage will be merged
  	// into a single aggregated response. Unioned with ExternalCustomerID if both are set;
  	// duplicates are dropped. At least one of ExternalCustomerID or ExternalCustomerIDs
  	// must be provided.
  	ExternalCustomerIDs []string         `json:"external_customer_ids,omitempty"`
  	FeatureIDs          []string         `json:"feature_ids,omitempty"`
  	Sources             []string         `json:"sources,omitempty"`
  	StartTime           time.Time        `json:"start_time,omitempty"`
  	EndTime             time.Time        `json:"end_time,omitempty"`
  	GroupBy             []string         `json:"group_by,omitempty"` // allowed values: "source", "feature_id", "properties.<field_name>"
  	WindowSize          types.WindowSize `json:"window_size,omitempty"`
  	Expand              []string         `json:"expand,omitempty"` // allowed values: "price", "meter", "feature", "subscription_line_item","plan","addon"
  	// Property filters to filter the events by the keys in `properties` field of the event
  	PropertyFilters map[string][]string `json:"property_filters,omitempty"`
  	// IncludeChildren when true folds child customers' usage into the single aggregated total.
  	// Default: false.
  	IncludeChildren bool `json:"include_children,omitempty"`
  }
  ```

- [ ] **Verify it compiles.**
  ```bash
  cd /Users/omkar/Developer/source-code/flexprice/flexprice && go build ./internal/api/dto/...
  ```
  Expected: no output (success).

- [ ] **Commit.**
  ```bash
  git add internal/api/dto/events.go
  git commit -m "feat(dto): add ExternalCustomerIDs to GetUsageAnalyticsRequest"
  ```

---

## Task 2 — Helper: `resolveEffectiveExternalIDs` + updated `validateAnalyticsRequest`

**Files:**
- Modify: `internal/service/feature_usage_tracking.go` (after line 1896, and lines 1876-1888)
- Create: `internal/service/feature_usage_tracking_analytics_test.go`

- [ ] **Write the failing tests first.**

  Create `internal/service/feature_usage_tracking_analytics_test.go`:

  ```go
  package service

  import (
  	"testing"

  	"github.com/flexprice/flexprice/internal/api/dto"
  	ierr "github.com/flexprice/flexprice/internal/errors"
  	"github.com/stretchr/testify/assert"
  	"github.com/stretchr/testify/require"
  )

  // --- resolveEffectiveExternalIDs ---

  func TestResolveEffectiveExternalIDs(t *testing.T) {
  	tests := []struct {
  		name     string
  		req      *dto.GetUsageAnalyticsRequest
  		expected []string
  	}{
  		{
  			name:     "only ExternalCustomerID",
  			req:      &dto.GetUsageAnalyticsRequest{ExternalCustomerID: "cust_a"},
  			expected: []string{"cust_a"},
  		},
  		{
  			name:     "only ExternalCustomerIDs",
  			req:      &dto.GetUsageAnalyticsRequest{ExternalCustomerIDs: []string{"cust_b", "cust_c"}},
  			expected: []string{"cust_b", "cust_c"},
  		},
  		{
  			name: "both fields, no overlap",
  			req: &dto.GetUsageAnalyticsRequest{
  				ExternalCustomerID:  "cust_a",
  				ExternalCustomerIDs: []string{"cust_b", "cust_c"},
  			},
  			expected: []string{"cust_a", "cust_b", "cust_c"},
  		},
  		{
  			name: "both fields, with duplicate — each unique ID appears once",
  			req: &dto.GetUsageAnalyticsRequest{
  				ExternalCustomerID:  "cust_a",
  				ExternalCustomerIDs: []string{"cust_a", "cust_b"},
  			},
  			expected: []string{"cust_a", "cust_b"},
  		},
  		{
  			name:     "both empty returns nil",
  			req:      &dto.GetUsageAnalyticsRequest{},
  			expected: nil,
  		},
  		{
  			name:     "empty string in ExternalCustomerID is ignored",
  			req:      &dto.GetUsageAnalyticsRequest{ExternalCustomerID: "", ExternalCustomerIDs: []string{"cust_x"}},
  			expected: []string{"cust_x"},
  		},
  		{
  			name:     "duplicates within ExternalCustomerIDs are deduplicated",
  			req:      &dto.GetUsageAnalyticsRequest{ExternalCustomerIDs: []string{"cust_a", "cust_a", "cust_b"}},
  			expected: []string{"cust_a", "cust_b"},
  		},
  	}

  	for _, tc := range tests {
  		t.Run(tc.name, func(t *testing.T) {
  			got := resolveEffectiveExternalIDs(tc.req)
  			assert.Equal(t, tc.expected, got)
  		})
  	}
  }

  // --- validateAnalyticsRequest ---

  func TestValidateAnalyticsRequest(t *testing.T) {
  	svc := &featureUsageTrackingService{}

  	t.Run("both fields empty returns validation error", func(t *testing.T) {
  		err := svc.validateAnalyticsRequest(&dto.GetUsageAnalyticsRequest{})
  		require.Error(t, err)
  		assert.True(t, ierr.IsValidation(err))
  	})

  	t.Run("only ExternalCustomerID set is valid", func(t *testing.T) {
  		err := svc.validateAnalyticsRequest(&dto.GetUsageAnalyticsRequest{ExternalCustomerID: "cust_a"})
  		assert.NoError(t, err)
  	})

  	t.Run("only ExternalCustomerIDs set is valid", func(t *testing.T) {
  		err := svc.validateAnalyticsRequest(&dto.GetUsageAnalyticsRequest{ExternalCustomerIDs: []string{"cust_b"}})
  		assert.NoError(t, err)
  	})

  	t.Run("both fields set is valid", func(t *testing.T) {
  		err := svc.validateAnalyticsRequest(&dto.GetUsageAnalyticsRequest{
  			ExternalCustomerID:  "cust_a",
  			ExternalCustomerIDs: []string{"cust_b"},
  		})
  		assert.NoError(t, err)
  	})

  	t.Run("ExternalCustomerIDs with only empty strings is a validation error", func(t *testing.T) {
  		err := svc.validateAnalyticsRequest(&dto.GetUsageAnalyticsRequest{
  			ExternalCustomerIDs: []string{"", ""},
  		})
  		require.Error(t, err)
  		assert.True(t, ierr.IsValidation(err))
  	})
  }

  // Note: scenarios requiring real DB or mocked repos (currency mismatch across customers,
  // all-customers-fail → empty response, IncludeChildren expansion) are covered by
  // integration tests in the standard test suite (testutil.SetupTestDB). They are not
  // duplicated here as unit tests because fetchAnalyticsData, fetchCustomer, and
  // fetchChildCustomers all require repo implementations.
  ```

- [ ] **Run tests to confirm they fail (functions don't exist yet).**
  ```bash
  cd /Users/omkar/Developer/source-code/flexprice/flexprice && go test ./internal/service/ -run "TestResolveEffectiveExternalIDs|TestValidateAnalyticsRequest" -v 2>&1 | head -30
  ```
  Expected: compile error — `resolveEffectiveExternalIDs undefined`.

- [ ] **Add `resolveEffectiveExternalIDs` to `feature_usage_tracking.go`.**

  Insert after `validateAnalyticsRequestV2` (after line 1896):

  ```go
  // resolveEffectiveExternalIDs returns a deduplicated, ordered union of
  // req.ExternalCustomerID and req.ExternalCustomerIDs. Empty strings are dropped.
  func resolveEffectiveExternalIDs(req *dto.GetUsageAnalyticsRequest) []string {
  	seen := make(map[string]bool)
  	var ids []string
  	all := append([]string{req.ExternalCustomerID}, req.ExternalCustomerIDs...)
  	for _, id := range all {
  		if id != "" && !seen[id] {
  			seen[id] = true
  			ids = append(ids, id)
  		}
  	}
  	return ids
  }
  ```

- [ ] **Replace `validateAnalyticsRequest` body (lines 1876-1888).**

  Replace:
  ```go
  // validateAnalyticsRequest validates the analytics request
  func (s *featureUsageTrackingService) validateAnalyticsRequest(req *dto.GetUsageAnalyticsRequest) error {
  	if req.ExternalCustomerID == "" {
  		return ierr.NewError("external_customer_id is required").
  			WithHint("External customer ID is required").
  			Mark(ierr.ErrValidation)
  	}

  	if req.WindowSize != "" {
  		return req.WindowSize.Validate()
  	}

  	return nil
  }
  ```

  With:
  ```go
  // validateAnalyticsRequest validates the analytics request.
  // At least one of ExternalCustomerID or ExternalCustomerIDs must be provided.
  func (s *featureUsageTrackingService) validateAnalyticsRequest(req *dto.GetUsageAnalyticsRequest) error {
  	if len(resolveEffectiveExternalIDs(req)) == 0 {
  		return ierr.NewError("external_customer_id or external_customer_ids is required").
  			WithHint("Provide at least one external customer ID").
  			Mark(ierr.ErrValidation)
  	}

  	if req.WindowSize != "" {
  		return req.WindowSize.Validate()
  	}

  	return nil
  }
  ```

- [ ] **Run the tests — they should pass now.**
  ```bash
  cd /Users/omkar/Developer/source-code/flexprice/flexprice && go test ./internal/service/ -run "TestResolveEffectiveExternalIDs|TestValidateAnalyticsRequest" -v
  ```
  Expected: all cases PASS.

- [ ] **Commit.**
  ```bash
  git add internal/service/feature_usage_tracking.go internal/service/feature_usage_tracking_analytics_test.go
  git commit -m "feat(analytics): add resolveEffectiveExternalIDs and update validateAnalyticsRequest"
  ```

---

## Task 3 — Refactor `GetDetailedUsageAnalytics`, delete `buildAggregatedAnalyticsWithChildren`

**Files:**
- Modify: `internal/service/feature_usage_tracking.go:1763-1793` (replace body)
- Modify: `internal/service/feature_usage_tracking.go:1913-1956` (delete function)

- [ ] **Replace `GetDetailedUsageAnalytics` body (lines 1763-1793).**

  Replace the entire function:
  ```go
  func (s *featureUsageTrackingService) GetDetailedUsageAnalytics(ctx context.Context, req *dto.GetUsageAnalyticsRequest) (*dto.GetUsageAnalyticsResponse, error) {
  	// 1. Validate request
  	if err := s.validateAnalyticsRequest(req); err != nil {
  		return nil, err
  	}

  	// 2. Fetch all required data in parallel
  	data, err := s.fetchAnalyticsData(ctx, req)
  	if err != nil {
  		return nil, err
  	}

  	// 3. Process and return response
  	response, err := s.buildAnalyticsResponse(ctx, data, req)
  	if err != nil {
  		return nil, err
  	}

  	// 4. Optionally aggregate children into the total
  	if req.IncludeChildren && data.Customer != nil {
  		children, err := s.fetchChildCustomers(ctx, data.Customer.ID)
  		if err != nil {
  			s.Logger.WarnwCtx(ctx, "failed to fetch child customers for include_children aggregation, returning parent-only",
  				"customer_id", data.Customer.ID, "error", err)
  		} else {
  			response = s.buildAggregatedAnalyticsWithChildren(ctx, data, children, req)
  		}
  	}

  	return response, nil
  }
  ```

  With:
  ```go
  func (s *featureUsageTrackingService) GetDetailedUsageAnalytics(ctx context.Context, req *dto.GetUsageAnalyticsRequest) (*dto.GetUsageAnalyticsResponse, error) {
  	// 1. Validate request
  	if err := s.validateAnalyticsRequest(req); err != nil {
  		return nil, err
  	}

  	// 2. Resolve the flat list of external IDs to fetch (deduped union of both fields)
  	effectiveIDs := resolveEffectiveExternalIDs(req)

  	// 3. If IncludeChildren, expand the list with each customer's hierarchy children.
  	//    Children are resolved here — before the fetch loop — so we never append to a
  	//    slice while ranging over it.
  	//    NOTE: fetchCustomer is called once per parent here to get the internal ID for
  	//    fetchChildCustomers. fetchAnalyticsData will call it again internally. This is an
  	//    accepted N+1 (one extra DB lookup per parent) kept for code clarity; the call is cheap.
  	if req.IncludeChildren {
  		var childExternalIDs []string
  		for _, id := range effectiveIDs {
  			cust, err := s.fetchCustomer(ctx, id)
  			if err != nil {
  				return nil, err
  			}
  			children, err := s.fetchChildCustomers(ctx, cust.ID)
  			if err != nil {
  				s.Logger.WarnwCtx(ctx, "failed to fetch child customers, skipping children for this parent",
  					"external_customer_id", id, "error", err)
  				continue
  			}
  			for _, child := range children {
  				childExternalIDs = append(childExternalIDs, child.ExternalID)
  			}
  		}
  		effectiveIDs = lo.Uniq(append(effectiveIDs, childExternalIDs...))
  	}

  	// 4. Fetch and merge analytics for all resolved customers.
  	//    mergeAnalyticsData merges subscription/line-item/feature/meter/price lookup maps.
  	//    The Analytics slice is accumulated separately (mergeAnalyticsData does not touch it).
  	//    currency is tracked separately (like GetDetailedUsageAnalyticsV2) because
  	//    mergeAnalyticsData does not update aggregatedData.Currency for subsequent customers.
  	var aggregatedData *AnalyticsData
  	var allAnalytics []*events.DetailedUsageAnalytic
  	var currency string

  	for _, id := range effectiveIDs {
  		customerReq := *req
  		customerReq.ExternalCustomerID = id
  		customerReq.ExternalCustomerIDs = nil
  		customerReq.IncludeChildren = false

  		data, err := s.fetchAnalyticsData(ctx, &customerReq)
  		if err != nil {
  			s.Logger.WarnwCtx(ctx, "failed to fetch analytics data for customer, skipping",
  				"external_customer_id", id, "error", err)
  			continue
  		}

  		allAnalytics = append(allAnalytics, data.Analytics...)

  		if aggregatedData == nil {
  			aggregatedData = data
  			currency = data.Currency
  		} else {
  			if data.Currency != "" && currency != "" && data.Currency != currency {
  				return nil, ierr.NewError("multiple currencies detected across customers").
  					WithHint("Analytics is only supported when all customers use the same currency").
  					WithReportableDetails(map[string]interface{}{
  						"expected_currency": currency,
  						"found_currency":    data.Currency,
  						"external_customer_id": id,
  					}).
  					Mark(ierr.ErrValidation)
  			}
  			s.mergeAnalyticsData(aggregatedData, data)
  		}
  	}

  	// 5. If all customers failed, return an empty response — no panic on nil aggregatedData.
  	if aggregatedData == nil {
  		return &dto.GetUsageAnalyticsResponse{
  			TotalCost: decimal.Zero,
  			Currency:  "",
  			Items:     []dto.UsageAnalyticItem{},
  		}, nil
  	}

  	// 6. Assign accumulated analytics and currency, then build the single aggregated response.
  	aggregatedData.Analytics = allAnalytics
  	aggregatedData.Currency = currency

  	return s.buildAnalyticsResponse(ctx, aggregatedData, req)
  }
  ```

  **Note:** `lo` is already imported in this file. `decimal` is already imported. `events` is already imported.

- [ ] **Delete `buildAggregatedAnalyticsWithChildren` (lines 1913-1956).**

  Remove the entire function:
  ```go
  // buildAggregatedAnalyticsWithChildren merges parent + all children analytics into a single
  // total response.
  func (s *featureUsageTrackingService) buildAggregatedAnalyticsWithChildren(
  	ctx context.Context,
  	parentData *AnalyticsData,
  	children []*customer.Customer,
  	req *dto.GetUsageAnalyticsRequest,
  ) *dto.GetUsageAnalyticsResponse {
      ... (entire function body)
  }
  ```

- [ ] **Verify it compiles.**
  ```bash
  cd /Users/omkar/Developer/source-code/flexprice/flexprice && go build ./internal/service/...
  ```
  Expected: no output.

- [ ] **Run the full service test suite to catch regressions.**
  ```bash
  cd /Users/omkar/Developer/source-code/flexprice/flexprice && go test ./internal/service/... -timeout 120s 2>&1 | tail -20
  ```
  Expected: all existing tests pass (no failures introduced).

- [ ] **Commit.**
  ```bash
  git add internal/service/feature_usage_tracking.go
  git commit -m "feat(analytics): refactor GetDetailedUsageAnalytics to support multi-customer aggregation"
  ```

---

## Task 4 — Update `fetchCustomers` (used by `GetDetailedUsageAnalyticsV2`)

**Files:**
- Modify: `internal/service/feature_usage_tracking.go:3715-3731`

- [ ] **Replace `fetchCustomers` body.**

  Replace:
  ```go
  func (s *featureUsageTrackingService) fetchCustomers(ctx context.Context, req *dto.GetUsageAnalyticsRequest) ([]*customer.Customer, error) {
  	if req.ExternalCustomerID != "" {
  		cust, err := s.fetchCustomer(ctx, req.ExternalCustomerID)
  		if err != nil {
  			return nil, err
  		}
  		return []*customer.Customer{cust}, nil
  	} else {
  		customers, err := s.CustomerRepo.List(ctx, types.NewNoLimitCustomerFilter())
  		if err != nil {
  			return nil, ierr.WithError(err).
  				WithHint("Failed to fetch customers").
  				Mark(ierr.ErrDatabase)
  		}
  		return customers, nil
  	}
  }
  ```

  With:
  ```go
  func (s *featureUsageTrackingService) fetchCustomers(ctx context.Context, req *dto.GetUsageAnalyticsRequest) ([]*customer.Customer, error) {
  	effectiveIDs := resolveEffectiveExternalIDs(req)
  	if len(effectiveIDs) > 0 {
  		// NOTE: fetchCustomer is called once per ID here. GetDetailedUsageAnalyticsV2 will
  		// call fetchAnalyticsData per customer which calls fetchCustomer again internally —
  		// an accepted N+1 (2N lookups total) kept for code clarity; the call is cheap.
  		customers := make([]*customer.Customer, 0, len(effectiveIDs))
  		for _, id := range effectiveIDs {
  			cust, err := s.fetchCustomer(ctx, id)
  			if err != nil {
  				return nil, err
  			}
  			customers = append(customers, cust)
  		}
  		return customers, nil
  	}
  	// No IDs specified — fetch all customers (existing behaviour for V2 aggregate-all path)
  	customers, err := s.CustomerRepo.List(ctx, types.NewNoLimitCustomerFilter())
  	if err != nil {
  		return nil, ierr.WithError(err).
  			WithHint("Failed to fetch customers").
  			Mark(ierr.ErrDatabase)
  	}
  	return customers, nil
  }
  ```

- [ ] **Verify it compiles.**
  ```bash
  cd /Users/omkar/Developer/source-code/flexprice/flexprice && go build ./internal/service/...
  ```
  Expected: no output.

- [ ] **Commit.**
  ```bash
  git add internal/service/feature_usage_tracking.go
  git commit -m "feat(analytics): update fetchCustomers to respect ExternalCustomerIDs via resolveEffectiveExternalIDs"
  ```

---

## Task 5 — V1 legacy normalisation

**Files:**
- Modify: `internal/service/event_post_processing.go:955-961`

- [ ] **Add one-line normalisation at the top of V1's `GetDetailedUsageAnalytics`.**

  The current opening of the function (lines 955-961):
  ```go
  func (s *eventPostProcessingService) GetDetailedUsageAnalytics(ctx context.Context, req *dto.GetUsageAnalyticsRequest) (*dto.GetUsageAnalyticsResponse, error) {
  	// Validate the request
  	if req.ExternalCustomerID == "" {
  		return nil, ierr.NewError("external_customer_id is required").
  			WithHint("External customer ID is required").
  			Mark(ierr.ErrValidation)
  	}
  ```

  Replace with:
  ```go
  func (s *eventPostProcessingService) GetDetailedUsageAnalytics(ctx context.Context, req *dto.GetUsageAnalyticsRequest) (*dto.GetUsageAnalyticsResponse, error) {
  	// V1 is single-customer only. If ExternalCustomerID is absent but ExternalCustomerIDs
  	// is provided, use the first entry so V1 callers aren't broken.
  	// Warn when multiple IDs are passed so operators can detect misuse.
  	if req.ExternalCustomerID == "" && len(req.ExternalCustomerIDs) > 0 {
  		if len(req.ExternalCustomerIDs) > 1 {
  			s.Logger.WarnwCtx(ctx, "V1 analytics does not support multiple customer IDs; using first entry only",
  				"external_customer_ids", req.ExternalCustomerIDs)
  		}
  		req.ExternalCustomerID = req.ExternalCustomerIDs[0]
  	}

  	// Validate the request
  	if req.ExternalCustomerID == "" {
  		return nil, ierr.NewError("external_customer_id is required").
  			WithHint("External customer ID is required").
  			Mark(ierr.ErrValidation)
  	}
  ```

- [ ] **Verify it compiles.**
  ```bash
  cd /Users/omkar/Developer/source-code/flexprice/flexprice && go build ./internal/service/...
  ```
  Expected: no output.

- [ ] **Commit.**
  ```bash
  git add internal/service/event_post_processing.go
  git commit -m "feat(analytics): add ExternalCustomerIDs fallback in V1 legacy path"
  ```

---

## Task 6 — Final verification

- [ ] **Run the full test suite.**
  ```bash
  cd /Users/omkar/Developer/source-code/flexprice/flexprice && go test ./... -timeout 180s 2>&1 | tail -30
  ```
  Expected: all tests pass, no regressions.

- [ ] **Run vet.**
  ```bash
  cd /Users/omkar/Developer/source-code/flexprice/flexprice && go vet ./...
  ```
  Expected: no output.

- [ ] **Regenerate Swagger docs** (new field must appear in the API spec).
  ```bash
  cd /Users/omkar/Developer/source-code/flexprice/flexprice && make swagger
  ```
  Expected: `docs/swagger/` updated with `external_customer_ids` field on `GetUsageAnalyticsRequest`.

- [ ] **Commit swagger update if changed.**
  ```bash
  git add docs/swagger/
  git diff --staged --quiet || git commit -m "docs(swagger): regenerate after adding ExternalCustomerIDs to usage analytics"
  ```
