# Addon Price Sync Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend plan price sync to support add-on prices — when an add-on's prices change, active subscriptions holding that add-on get their line items created/terminated automatically.

**Architecture:** New `addonpricesync` domain + repository (mirrors `planpricesync` with addon_associations join instead of plan_id filter). Service method `SyncAddonPrices` added to `addonService`. `PriceSyncWorkflow` generalized to route on `EntityType` (PLAN vs ADDON). Two new API endpoints under `/addons/:id/sync/subscriptions`.

**Tech Stack:** Go 1.23, Ent ORM, PostgreSQL (raw SQL CTEs), Temporal, Redis, Gin

**Parallelism note:** Tasks 1 and 4 have no dependencies on each other and can run concurrently.

---

## File Map

| File | Action | Responsibility |
|------|--------|---------------|
| `internal/domain/addonpricesync/repository.go` | Create | Domain interface + delta types |
| `internal/repository/ent/addon_price_sync.go` | Create | SQL CTE implementation |
| `internal/api/dto/addon_price_sync.go` | Create | `SyncAddonPricesResponse` DTO |
| `internal/cache/cache.go` | Modify | Add `PrefixAddonPriceSyncLock` constant |
| `internal/service/factory.go` | Modify | Add `AddonPriceSyncRepo` to `ServiceParams` + `NewServiceParams` |
| `internal/service/addon.go` | Modify | Add `SyncAddonPrices` method + `createAddonLineItem` helper |
| `internal/temporal/models/plan.go` | Modify | Generalize `PriceSyncWorkflowInput` with `EntityType`/`EntityID` |
| `internal/temporal/activities/addon/addon_activities.go` | Create | `AddonActivities` + `SyncAddonPrices` activity |
| `internal/temporal/workflows/price_sync_workflow.go` | Modify | Route on `EntityType` |
| `internal/temporal/service/service.go` | Modify | `buildPriceSyncInput` addon support |
| `internal/temporal/registration.go` | Modify | Register `AddonActivities` on price queue |
| `internal/api/v1/addon.go` | Modify | Add `SyncAddonPrices` + `SyncAddonPricesV2` handlers |
| `internal/api/router.go` | Modify | Add 2 new routes |
| `cmd/server/main.go` | Modify | Wire `NewAddonPriceSyncRepository` via fx |

---

## Task 1: Domain Package + DTO + Cache Constant

**No dependencies — can run in parallel with Task 4.**

**Files:**
- Create: `internal/domain/addonpricesync/repository.go`
- Create: `internal/api/dto/addon_price_sync.go`
- Modify: `internal/cache/cache.go`

- [ ] **Step 1: Create domain package**

Create `internal/domain/addonpricesync/repository.go`:

```go
package addonpricesync

import (
	"context"
	"time"
)

// AddonLineItemTerminationDelta is an addon-sync delta row for setting a line item's end_date.
type AddonLineItemTerminationDelta struct {
	LineItemID     string
	SubscriptionID string
	PriceID        string
	TargetEndDate  time.Time
}

// AddonLineItemCreationDelta is an addon-sync delta row for creating a new line item.
type AddonLineItemCreationDelta struct {
	SubscriptionID string
	PriceID        string // addon price ID (entity_type=ADDON)
	CustomerID     string
}

type ListAddonLineItemsToTerminateParams struct {
	AddonID string
	Limit   int
}

type ListAddonLineItemsToCreateParams struct {
	AddonID    string
	Limit      int
	AfterSubID string // Optional cursor: subscription_id from last row
}

type TerminateExpiredAddonPricesLineItemsParams struct {
	AddonID string
	Limit   int
}

// Repository defines the interface for addon price sync delta queries.
//
// Scoped to two canonical DB-driven queries:
// 1) addon-derived line items whose end_date must be set to price.end_date
// 2) missing (subscription_id, price_id) pairs where an addon-derived line item must be created
type Repository interface {
	// TerminateExpiredAddonPricesLineItems sets end_date on addon-derived line items whose price has expired.
	// If limit <= 0, an implementation-defined default is used.
	TerminateExpiredAddonPricesLineItems(
		ctx context.Context,
		p TerminateExpiredAddonPricesLineItemsParams,
	) (numTerminated int, err error)

	// ListAddonLineItemsToTerminate returns addon-derived line items whose end_date must be set.
	// If limit <= 0, an implementation-defined default is used.
	ListAddonLineItemsToTerminate(
		ctx context.Context,
		p ListAddonLineItemsToTerminateParams,
	) (items []AddonLineItemTerminationDelta, err error)

	// ListAddonLineItemsToCreate returns missing (subscription_id, price_id) pairs for an addon.
	// price_id is the addon price ID (prices.entity_type=ADDON).
	// If limit <= 0, an implementation-defined default is used.
	ListAddonLineItemsToCreate(
		ctx context.Context,
		p ListAddonLineItemsToCreateParams,
	) (items []AddonLineItemCreationDelta, err error)

	// GetLastSubscriptionIDInBatch returns the last subscription ID from the batch.
	// Returns nil when cursor can't advance.
	// Returns pointer to subscription ID when cursor can advance.
	GetLastSubscriptionIDInBatch(
		ctx context.Context,
		p ListAddonLineItemsToCreateParams,
	) (lastSubID *string, err error)
}
```

- [ ] **Step 2: Create DTO**

Create `internal/api/dto/addon_price_sync.go`:

```go
package dto

// SyncAddonPricesResponse is the response for the addon price sync endpoints.
type SyncAddonPricesResponse struct {
	AddonID string                  `json:"addon_id"`
	Message string                  `json:"message"`
	Summary SyncAddonPricesSummary  `json:"summary"`
}

// SyncAddonPricesSummary contains the metrics from an addon price sync run.
type SyncAddonPricesSummary struct {
	LineItemsFoundForCreation int `json:"line_items_found_for_creation"`
	LineItemsCreated          int `json:"line_items_created"`
	LineItemsTerminated       int `json:"line_items_terminated"`
}
```

- [ ] **Step 3: Add cache constant**

In `internal/cache/cache.go`, find the block with `PrefixPriceSyncLock` and add the addon constant directly below it:

```go
// PrefixAddonPriceSyncLock is the Redis key prefix for addon-level price sync lock (used with addonID).
PrefixAddonPriceSyncLock = "price_sync:addon:"
```

The existing line reads:
```go
PrefixPriceSyncLock = "price_sync:plan:"
```
Add the new constant on the next line inside the same `const` block.

- [ ] **Step 4: Verify it compiles**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice && go build ./internal/domain/addonpricesync/... ./internal/api/dto/... ./internal/cache/...
```

Expected: no output (success).

- [ ] **Step 5: Commit**

```bash
git add internal/domain/addonpricesync/repository.go internal/api/dto/addon_price_sync.go internal/cache/cache.go
git commit -m "feat(addon-price-sync): add domain package, DTO, and cache constant"
```

---

## Task 2: Repository Layer

**Depends on Task 1 (domain package).**

**Files:**
- Create: `internal/repository/ent/addon_price_sync.go`

- [ ] **Step 1: Create the repository file**

Create `internal/repository/ent/addon_price_sync.go`:

```go
package ent

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/domain/addonpricesync"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type addonPriceSyncRepository struct {
	client postgres.IClient
	log    *logger.Logger
}

func NewAddonPriceSyncRepository(client postgres.IClient, log *logger.Logger) addonpricesync.Repository {
	return &addonPriceSyncRepository{
		client: client,
		log:    log,
	}
}

// TerminateExpiredAddonPricesLineItems terminates addon-derived line items whose price has ended.
func (r *addonPriceSyncRepository) TerminateExpiredAddonPricesLineItems(
	ctx context.Context,
	p addonpricesync.TerminateExpiredAddonPricesLineItemsParams,
) (numTerminated int, err error) {
	addonID := p.AddonID
	limit := p.Limit

	if addonID == "" {
		return 0, ierr.NewError("addon_id is required").
			WithReportableDetails(map[string]any{"addon_id": addonID}).
			Mark(ierr.ErrValidation)
	}
	if limit <= 0 {
		limit = DEFAULT_LIMIT
	}

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	userID := types.GetUserID(ctx)

	span := StartRepositorySpan(ctx, "addon_price_sync", "terminate_expired_addon_prices_line_items", map[string]interface{}{
		"addon_id": addonID,
		"limit":    limit,
	})
	defer FinishSpan(span)

	query := fmt.Sprintf(`
		WITH
			subs AS (
				SELECT
					aa.entity_id AS id
				FROM
					addon_associations aa
				WHERE
					aa.tenant_id = $1
					AND aa.environment_id = $2
					AND aa.status = '%s'
					AND aa.addon_id = $3
					AND aa.entity_type = 'SUBSCRIPTION'
					AND aa.addon_status = 'active'
			),
			ended_addon_prices AS (
				SELECT
					id,
					end_date
				FROM
					prices
				WHERE
					tenant_id = $1
					AND environment_id = $2
					AND status = '%s'
					AND entity_type = '%s'
					AND entity_id = $3
					AND end_date IS NOT NULL
					AND type <> '%s'
			),
			targets AS (
				SELECT
					li.id AS line_item_id,
					GREATEST(COALESCE(li.start_date, p.end_date), p.end_date) AS target_end_date
				FROM
					subscription_line_items li
					JOIN subs s ON s.id = li.subscription_id
					JOIN ended_addon_prices p ON p.id = li.price_id
				WHERE
					li.tenant_id = $1
					AND li.environment_id = $2
					AND li.status = '%s'
					AND li.entity_type = '%s'
					AND li.end_date IS NULL
				ORDER BY li.id
				LIMIT $4
			)
		UPDATE
			subscription_line_items li
		SET
			end_date = t.target_end_date,
			updated_at = NOW(),
			updated_by = $5
		FROM
			targets t
		WHERE
			li.id = t.line_item_id
	`,
		string(types.StatusPublished),
		string(types.StatusPublished),
		string(types.PRICE_ENTITY_TYPE_ADDON),
		string(types.PRICE_TYPE_FIXED),
		string(types.StatusPublished),
		string(types.SubscriptionLineItemEntityTypeAddon),
	)

	result, qerr := r.client.Writer(ctx).ExecContext(
		ctx,
		query,
		tenantID,
		environmentID,
		addonID,
		limit,
		userID,
	)
	if qerr != nil {
		r.log.Errorw("failed to execute termination query for addon line items",
			"addon_id", addonID, "limit", limit, "error", qerr)
		SetSpanError(span, qerr)
		return 0, ierr.WithError(qerr).
			WithHint("Failed to terminate addon line items").
			WithReportableDetails(map[string]any{"addon_id": addonID, "limit": limit}).
			Mark(ierr.ErrDatabase)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		r.log.Errorw("failed to get rows affected for terminated addon line items",
			"addon_id", addonID, "limit", limit, "error", err)
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithReportableDetails(map[string]any{"addon_id": addonID, "limit": limit}).
			Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return int(rowsAffected), nil
}

// ListAddonLineItemsToTerminate returns addon-derived line items whose end_date must be set.
func (r *addonPriceSyncRepository) ListAddonLineItemsToTerminate(
	ctx context.Context,
	p addonpricesync.ListAddonLineItemsToTerminateParams,
) (items []addonpricesync.AddonLineItemTerminationDelta, err error) {
	addonID := p.AddonID
	limit := p.Limit

	if addonID == "" {
		return nil, ierr.NewError("addon_id is required").
			WithReportableDetails(map[string]any{"addon_id": addonID}).
			Mark(ierr.ErrValidation)
	}
	if limit <= 0 {
		limit = DEFAULT_LIMIT
	}

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	span := StartRepositorySpan(ctx, "addon_price_sync", "list_addon_line_items_to_terminate", map[string]interface{}{
		"addon_id": addonID,
		"limit":    limit,
	})
	defer FinishSpan(span)

	query := fmt.Sprintf(`
		WITH
			subs AS (
				SELECT
					aa.entity_id AS id
				FROM
					addon_associations aa
				WHERE
					aa.tenant_id = $1
					AND aa.environment_id = $2
					AND aa.status = '%s'
					AND aa.addon_id = $3
					AND aa.entity_type = 'SUBSCRIPTION'
					AND aa.addon_status = 'active'
			),
			ended_addon_prices AS (
				SELECT
					id,
					end_date
				FROM
					prices
				WHERE
					tenant_id = $1
					AND environment_id = $2
					AND status = '%s'
					AND entity_type = '%s'
					AND entity_id = $3
					AND end_date IS NOT NULL
					AND type <> '%s'
			)
		SELECT
			li.id AS line_item_id,
			li.subscription_id AS subscription_id,
			li.price_id AS price_id,
			GREATEST(COALESCE(li.start_date, p.end_date), p.end_date) AS target_end_date
		FROM
			subscription_line_items li
			JOIN subs s ON s.id = li.subscription_id
			JOIN ended_addon_prices p ON p.id = li.price_id
		WHERE
			li.tenant_id = $1
			AND li.environment_id = $2
			AND li.status = '%s'
			AND li.entity_type = '%s'
			AND li.end_date IS NULL
		ORDER BY li.start_date, li.id
		LIMIT $4
	`,
		string(types.StatusPublished),
		string(types.StatusPublished),
		string(types.PRICE_ENTITY_TYPE_ADDON),
		string(types.PRICE_TYPE_FIXED),
		string(types.StatusPublished),
		string(types.SubscriptionLineItemEntityTypeAddon),
	)

	rows, qerr := r.client.Reader(ctx).QueryContext(ctx, query, tenantID, environmentID, addonID, limit)
	if qerr != nil {
		r.log.Errorw("failed to query addon line items to terminate",
			"addon_id", addonID, "limit", limit, "error", qerr)
		SetSpanError(span, qerr)
		return nil, ierr.WithError(qerr).
			WithHint("Failed to list addon line items to terminate").
			WithReportableDetails(map[string]any{"addon_id": addonID, "limit": limit}).
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	for rows.Next() {
		var delta addonpricesync.AddonLineItemTerminationDelta
		if scanErr := rows.Scan(&delta.LineItemID, &delta.SubscriptionID, &delta.PriceID, &delta.TargetEndDate); scanErr != nil {
			r.log.Errorw("failed to scan addon termination delta row",
				"addon_id", addonID, "error", scanErr)
			SetSpanError(span, scanErr)
			return nil, ierr.WithError(scanErr).
				WithHint("Failed to scan addon termination delta row").
				Mark(ierr.ErrDatabase)
		}
		items = append(items, delta)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		SetSpanError(span, rowsErr)
		return nil, ierr.WithError(rowsErr).Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return items, nil
}

// ListAddonLineItemsToCreate returns missing (subscription_id, price_id) pairs for an addon.
func (r *addonPriceSyncRepository) ListAddonLineItemsToCreate(
	ctx context.Context,
	p addonpricesync.ListAddonLineItemsToCreateParams,
) (items []addonpricesync.AddonLineItemCreationDelta, err error) {
	addonID := p.AddonID
	limit := p.Limit

	if addonID == "" {
		return nil, ierr.NewError("addon_id is required").
			WithReportableDetails(map[string]any{"addon_id": addonID}).
			Mark(ierr.ErrValidation)
	}
	if limit <= 0 {
		limit = DEFAULT_LIMIT
	}

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cursorSubID := p.AfterSubID
	hasCursor := cursorSubID != ""

	span := StartRepositorySpan(ctx, "addon_price_sync", "list_addon_line_items_to_create", map[string]interface{}{
		"addon_id":      addonID,
		"limit":         limit,
		"has_cursor":    hasCursor,
		"cursor_sub_id": cursorSubID,
	})
	defer FinishSpan(span)

	cursorCondition := "AND (p.last_sub_id = '' OR s.entity_id >= p.last_sub_id) "

	query := fmt.Sprintf(`
		WITH
			params AS (
				SELECT $5::text AS last_sub_id
			),
			subs_batch AS (
				SELECT
					aa.entity_id AS id,
					sub.customer_id,
					sub.tenant_id,
					sub.environment_id,
					sub.currency,
					sub.billing_period,
					sub.billing_period_count,
					sub.start_date
				FROM
					addon_associations aa
					JOIN subscriptions sub ON sub.id = aa.entity_id,
					params p
				WHERE
					aa.tenant_id = $1
					AND aa.environment_id = $2
					AND aa.status = '%s'
					AND aa.addon_id = $3
					AND aa.entity_type = 'SUBSCRIPTION'
					AND aa.addon_status = 'active'
					AND sub.status = '%s'
					AND sub.subscription_status IN ('%s', '%s')
					%s
				ORDER BY aa.entity_id
				LIMIT $4
			),
			addon_prices AS (
				SELECT
					p.id,
					p.tenant_id,
					p.environment_id,
					p.currency,
					p.billing_period,
					p.billing_period_count,
					p.parent_price_id,
					p.end_date
				FROM
					prices p
				WHERE
					p.tenant_id = $1
					AND p.environment_id = $2
					AND p.status = '%s'
					AND p.entity_type = '%s'
					AND p.entity_id = $3
					AND p.type <> '%s'
			)
		SELECT
			s.id AS subscription_id,
			p.id AS missing_price_id,
			s.customer_id AS customer_id
		FROM
			subs_batch s
			JOIN addon_prices p ON lower(p.currency) = lower(s.currency)
				AND p.billing_period = s.billing_period
				AND p.billing_period_count = s.billing_period_count
		WHERE
			(p.end_date IS NULL OR s.start_date <= p.end_date)
			AND NOT EXISTS (
				SELECT 1
				FROM prices sp
				WHERE
					sp.tenant_id = s.tenant_id
					AND sp.environment_id = s.environment_id
					AND sp.status = '%s'
					AND sp.entity_type = '%s'
					AND sp.entity_id = s.id
					AND (
						sp.parent_price_id = p.id
						OR (p.parent_price_id IS NOT NULL AND sp.parent_price_id = p.parent_price_id)
					)
			)
			AND NOT EXISTS (
				SELECT 1
				FROM subscription_line_items li
				WHERE
					li.tenant_id = s.tenant_id
					AND li.environment_id = s.environment_id
					AND li.status = '%s'
					AND li.subscription_id = s.id
					AND li.price_id = p.id
					AND li.entity_type = '%s'
			)
		`,
		string(types.StatusPublished),
		string(types.StatusPublished),
		string(types.SubscriptionStatusActive),
		string(types.SubscriptionStatusTrialing),
		cursorCondition,
		string(types.StatusPublished),
		string(types.PRICE_ENTITY_TYPE_ADDON),
		string(types.PRICE_TYPE_FIXED),
		string(types.StatusPublished),
		string(types.PRICE_ENTITY_TYPE_SUBSCRIPTION),
		string(types.StatusPublished),
		string(types.SubscriptionLineItemEntityTypeAddon),
	)

	cursorParam := ""
	if hasCursor {
		cursorParam = cursorSubID
	}
	args := []interface{}{tenantID, environmentID, addonID, limit, cursorParam}

	rows, qerr := r.client.Reader(ctx).QueryContext(ctx, query, args...)
	if qerr != nil {
		r.log.Errorw("failed to query addon line items to create",
			"addon_id", addonID, "limit", limit, "error", qerr)
		SetSpanError(span, qerr)
		return nil, ierr.WithError(qerr).
			WithHint("Failed to list addon line items to create").
			WithReportableDetails(map[string]any{"addon_id": addonID, "limit": limit}).
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	for rows.Next() {
		var subID, priceID, customerID string
		if scanErr := rows.Scan(&subID, &priceID, &customerID); scanErr != nil {
			r.log.Errorw("failed to scan addon creation delta row",
				"addon_id", addonID, "error", scanErr)
			SetSpanError(span, scanErr)
			return nil, ierr.WithError(scanErr).
				WithHint("Failed to scan addon creation delta row").
				Mark(ierr.ErrDatabase)
		}
		items = append(items, addonpricesync.AddonLineItemCreationDelta{
			SubscriptionID: subID,
			PriceID:        priceID,
			CustomerID:     customerID,
		})
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		SetSpanError(span, rowsErr)
		return nil, ierr.WithError(rowsErr).Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return items, nil
}

// GetLastSubscriptionIDInBatch returns the last subscription ID from the batch for cursor advancement.
func (r *addonPriceSyncRepository) GetLastSubscriptionIDInBatch(
	ctx context.Context,
	p addonpricesync.ListAddonLineItemsToCreateParams,
) (lastSubID *string, err error) {
	addonID := p.AddonID
	limit := p.Limit

	if addonID == "" {
		return nil, ierr.NewError("addon_id is required").
			WithReportableDetails(map[string]any{"addon_id": addonID}).
			Mark(ierr.ErrValidation)
	}
	if limit <= 0 {
		limit = DEFAULT_LIMIT
	}

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cursorSubID := p.AfterSubID
	hasCursor := cursorSubID != ""

	cursorCondition := "AND (p.last_sub_id = '' OR aa.entity_id >= p.last_sub_id) "

	query := fmt.Sprintf(`
		WITH
			params AS (
				SELECT $5::text AS last_sub_id
			),
			subs_batch AS (
				SELECT
					aa.entity_id AS id
				FROM
					addon_associations aa,
					params p
				WHERE
					aa.tenant_id = $1
					AND aa.environment_id = $2
					AND aa.status = '%s'
					AND aa.addon_id = $3
					AND aa.entity_type = 'SUBSCRIPTION'
					AND aa.addon_status = 'active'
					%s
				ORDER BY aa.entity_id
				LIMIT $4
			)
		SELECT
			COALESCE(MAX(s.id), '') AS last_sub_id
		FROM
			subs_batch s
		`,
		string(types.StatusPublished),
		cursorCondition,
	)

	cursorParam := ""
	if hasCursor {
		cursorParam = cursorSubID
	}
	args := []interface{}{tenantID, environmentID, addonID, limit, cursorParam}

	rows, qerr := r.client.Reader(ctx).QueryContext(ctx, query, args...)
	if qerr != nil {
		r.log.Errorw("failed to query last subscription ID in addon batch",
			"addon_id", addonID, "limit", limit, "error", qerr)
		return nil, ierr.WithError(qerr).
			WithHint("Failed to get last subscription ID in addon batch").
			WithReportableDetails(map[string]any{"addon_id": addonID, "limit": limit}).
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	var batchLastSubID string
	if rows.Next() {
		if scanErr := rows.Scan(&batchLastSubID); scanErr != nil {
			r.log.Errorw("failed to scan last addon subscription ID",
				"addon_id", addonID, "error", scanErr)
			return nil, ierr.WithError(scanErr).
				WithHint("Failed to scan last addon subscription ID").
				Mark(ierr.ErrDatabase)
		}
	} else {
		batchLastSubID = ""
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, ierr.WithError(rowsErr).Mark(ierr.ErrDatabase)
	}

	if batchLastSubID == "" || batchLastSubID == cursorSubID {
		return nil, nil
	}
	return &batchLastSubID, nil
}
```

- [ ] **Step 2: Verify it compiles**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice && go build ./internal/repository/ent/...
```

Expected: no output (success).

- [ ] **Step 3: Commit**

```bash
git add internal/repository/ent/addon_price_sync.go
git commit -m "feat(addon-price-sync): add addon price sync SQL repository"
```

---

## Task 3: ServiceParams + Service Layer

**Depends on Tasks 1 and 2.**

**Files:**
- Modify: `internal/service/factory.go`
- Modify: `internal/service/addon.go`

- [ ] **Step 1: Add `AddonPriceSyncRepo` to `ServiceParams`**

In `internal/service/factory.go`, add the import and field:

At the top imports, add:
```go
"github.com/flexprice/flexprice/internal/domain/addonpricesync"
```

In the `ServiceParams` struct, add after `PlanPriceSyncRepo`:
```go
AddonPriceSyncRepo           addonpricesync.Repository
```

In `NewServiceParams` function signature, add as a new parameter after `planPriceSyncRepo planpricesync.Repository`:
```go
addonPriceSyncRepo addonpricesync.Repository,
```

In the `return ServiceParams{...}` block, add after `PlanPriceSyncRepo: planPriceSyncRepo,`:
```go
AddonPriceSyncRepo: addonPriceSyncRepo,
```

- [ ] **Step 2: Add `SyncAddonPrices` to `AddonService` interface and implementation**

In `internal/service/addon.go`:

**2a. Add to the `AddonService` interface** (after `GetActiveAddonAssociation`):
```go
// SyncAddonPrices syncs addon prices to subscription line items for all active subscriptions
// that have this addon attached. Idempotent: safe to re-run.
SyncAddonPrices(ctx context.Context, addonID string) (*dto.SyncAddonPricesResponse, error)
```

**2b. Add import** at the top (add to existing import block):
```go
"time"

"github.com/flexprice/flexprice/internal/domain/addonpricesync"
domainPrice "github.com/flexprice/flexprice/internal/domain/price"
"github.com/flexprice/flexprice/internal/domain/subscription"
eventsWorkflowModels "github.com/flexprice/flexprice/internal/temporal/models/events"
temporalService "github.com/flexprice/flexprice/internal/temporal/service"
"github.com/samber/lo"
"github.com/shopspring/decimal"
```

**2c. Add `createAddonLineItem` helper function** at the bottom of the file:
```go
// createAddonLineItem builds a SubscriptionLineItem for an addon-derived price.
func createAddonLineItem(
	ctx context.Context,
	sub *subscription.Subscription,
	price *domainPrice.Price,
) *subscription.SubscriptionLineItem {
	metadata := make(map[string]string)
	for k, v := range price.Metadata {
		metadata[k] = v
	}
	metadata["added_by"] = "addon_sync_api"
	metadata["sync_version"] = "1.0"

	req := dto.CreateSubscriptionLineItemRequest{
		PriceID:     price.ID,
		Quantity:    decimal.Zero,
		Metadata:    metadata,
		DisplayName: price.DisplayName,
		StartDate:   price.StartDate,
		EndDate:     price.EndDate,
	}

	lineItemParams := dto.LineItemParams{
		Subscription: &dto.SubscriptionResponse{Subscription: sub},
		Price:        &dto.PriceResponse{Price: price},
		EntityType:   types.SubscriptionLineItemEntityTypeAddon,
	}

	return req.ToSubscriptionLineItem(ctx, lineItemParams)
}
```

**2d. Add `SyncAddonPrices` method** on `addonService`:
```go
// SyncAddonPrices syncs addon prices to subscription line items.
func (s *addonService) SyncAddonPrices(ctx context.Context, addonID string) (*dto.SyncAddonPricesResponse, error) {
	syncStartTime := time.Now()

	lineItemsFoundForCreation := 0
	lineItemsCreated := 0
	lineItemsTerminated := 0

	if _, err := s.AddonRepo.GetByID(ctx, addonID); err != nil {
		s.Logger.ErrorwCtx(ctx, "failed to get addon for price synchronization", "addon_id", addonID, "error", err)
		return nil, err
	}

	// Termination loop
	terminationStartTime := time.Now()
	terminationParams := addonpricesync.TerminateExpiredAddonPricesLineItemsParams{
		AddonID: addonID,
		Limit:   1000,
	}
	for {
		numTerminated, err := s.AddonPriceSyncRepo.TerminateExpiredAddonPricesLineItems(ctx, terminationParams)
		if err != nil {
			s.Logger.ErrorwCtx(ctx, "failed to terminate expired addon price line items", "addon_id", addonID, "error", err)
			return nil, err
		}
		lineItemsTerminated += numTerminated
		if numTerminated == 0 || numTerminated < terminationParams.Limit {
			break
		}
	}
	terminationDuration := time.Since(terminationStartTime)

	// Cursor creation loop
	creationStartTime := time.Now()
	cursorSubID := ""
	for {
		queryParams := addonpricesync.ListAddonLineItemsToCreateParams{
			AddonID:    addonID,
			Limit:      1000,
			AfterSubID: cursorSubID,
		}

		missingPairs, err := s.AddonPriceSyncRepo.ListAddonLineItemsToCreate(ctx, queryParams)
		if err != nil {
			s.Logger.ErrorwCtx(ctx, "failed to list addon line items to create", "addon_id", addonID, "error", err)
			return nil, err
		}

		nextSubID, err := s.AddonPriceSyncRepo.GetLastSubscriptionIDInBatch(ctx, queryParams)
		if err != nil {
			s.Logger.ErrorwCtx(ctx, "failed to get last subscription ID in addon batch", "addon_id", addonID, "error", err)
			return nil, err
		}

		if len(missingPairs) == 0 && nextSubID == nil {
			break
		}

		if len(missingPairs) == 0 {
			cursorSubID = *nextSubID
			continue
		}

		lineItemsFoundForCreation += len(missingPairs)

		priceIDs := lo.Uniq(lo.Map(missingPairs, func(pair addonpricesync.AddonLineItemCreationDelta, _ int) string {
			return pair.PriceID
		}))
		subscriptionIDs := lo.Uniq(lo.Map(missingPairs, func(pair addonpricesync.AddonLineItemCreationDelta, _ int) string {
			return pair.SubscriptionID
		}))

		priceFilter := types.NewNoLimitPriceFilter().
			WithPriceIDs(priceIDs).
			WithEntityType(types.PRICE_ENTITY_TYPE_ADDON).
			WithAllowExpiredPrices(true)

		prices, err := s.PriceRepo.List(ctx, priceFilter)
		if err != nil {
			s.Logger.ErrorwCtx(ctx, "failed to fetch addon prices for line item creation", "addon_id", addonID, "error", err)
			return nil, err
		}
		priceMap := lo.KeyBy(prices, func(p *domainPrice.Price) string { return p.ID })

		subFilter := types.NewNoLimitSubscriptionFilter()
		subFilter.SubscriptionIDs = subscriptionIDs
		subs, err := s.SubRepo.List(ctx, subFilter)
		if err != nil {
			s.Logger.ErrorwCtx(ctx, "failed to fetch subscriptions for addon line item creation", "addon_id", addonID, "error", err)
			return nil, err
		}
		subMap := lo.KeyBy(subs, func(s *subscription.Subscription) string { return s.ID })

		var lineItemsToCreate []*subscription.SubscriptionLineItem
		for _, pair := range missingPairs {
			price, priceFound := priceMap[pair.PriceID]
			sub, subFound := subMap[pair.SubscriptionID]
			if !priceFound || !subFound {
				return nil, ierr.NewError("price or subscription not found to create addon line item").
					WithHint("Price or subscription not found to create addon line item").
					WithReportableDetails(map[string]interface{}{
						"price_id":        pair.PriceID,
						"subscription_id": pair.SubscriptionID,
					}).
					Mark(ierr.ErrDatabase)
			}
			lineItemsToCreate = append(lineItemsToCreate, createAddonLineItem(ctx, sub, price))
		}

		if len(lineItemsToCreate) > 0 {
			const bulkInsertBatchSize = 2000
			totalCreated := 0
			for i := 0; i < len(lineItemsToCreate); i += bulkInsertBatchSize {
				end := i + bulkInsertBatchSize
				if end > len(lineItemsToCreate) {
					end = len(lineItemsToCreate)
				}
				batch := lineItemsToCreate[i:end]
				if err = s.SubscriptionLineItemRepo.CreateBulk(ctx, batch); err != nil {
					s.Logger.ErrorwCtx(ctx, "failed to create addon line items in bulk batch",
						"addon_id", addonID, "error", err,
						"batch_start", i, "batch_end", end)
					return nil, err
				}
				totalCreated += len(batch)
			}
			lineItemsCreated += totalCreated

			// Fire reprocess events workflow non-blocking for usage-based prices
			if temporalSvc := temporalService.GetGlobalTemporalService(); temporalSvc != nil {
				pairs := make([]eventsWorkflowModels.MissingPair, len(missingPairs))
				for j, p := range missingPairs {
					pairs[j] = eventsWorkflowModels.MissingPair{
						SubscriptionID: p.SubscriptionID,
						PriceID:        p.PriceID,
						CustomerID:     p.CustomerID,
					}
				}
				workflowInput := eventsWorkflowModels.ReprocessEventsForPlanWorkflowInput{
					MissingPairs:  pairs,
					TenantID:      types.GetTenantID(ctx),
					EnvironmentID: types.GetEnvironmentID(ctx),
					UserID:        types.GetUserID(ctx),
				}
				workflowRun, err := temporalSvc.ExecuteWorkflow(ctx, types.TemporalReprocessEventsForPlanWorkflow, workflowInput)
				if err != nil {
					s.Logger.WarnwCtx(ctx, "failed to start reprocess events workflow for addon",
						"addon_id", addonID, "missing_pairs_count", len(missingPairs), "error", err)
				} else {
					s.Logger.DebugwCtx(ctx, "reprocess events workflow started for addon",
						"addon_id", addonID, "workflow_id", workflowRun.GetID())
				}
			}
		}

		if nextSubID != nil {
			cursorSubID = *nextSubID
		}
	}
	creationDuration := time.Since(creationStartTime)

	totalDuration := time.Since(syncStartTime)
	s.Logger.InfowCtx(ctx, "completed addon price synchronization",
		"addon_id", addonID,
		"line_items_found_for_creation", lineItemsFoundForCreation,
		"line_items_created", lineItemsCreated,
		"line_items_terminated", lineItemsTerminated,
		"total_duration_ms", totalDuration.Milliseconds(),
		"termination_duration_ms", terminationDuration.Milliseconds(),
		"creation_duration_ms", creationDuration.Milliseconds())

	return &dto.SyncAddonPricesResponse{
		AddonID: addonID,
		Message: "Addon prices synchronized to subscription line items successfully",
		Summary: dto.SyncAddonPricesSummary{
			LineItemsFoundForCreation: lineItemsFoundForCreation,
			LineItemsCreated:          lineItemsCreated,
			LineItemsTerminated:       lineItemsTerminated,
		},
	}, nil
}
```

- [ ] **Step 3: Verify it compiles**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice && go build ./internal/service/...
```

Expected: no output (success). Fix any import errors (remove unused imports, add missing ones).

- [ ] **Step 4: Commit**

```bash
git add internal/service/factory.go internal/service/addon.go
git commit -m "feat(addon-price-sync): add SyncAddonPrices service method and ServiceParams wiring"
```

---

## Task 4: Temporal Models Generalization

**No dependencies on Tasks 1-3 — can run in parallel with Task 1.**

**Files:**
- Modify: `internal/temporal/models/plan.go`

- [ ] **Step 1: Generalize `PriceSyncWorkflowInput`**

Replace the existing `PriceSyncWorkflowInput` struct and its `Validate` method in `internal/temporal/models/plan.go` with:

```go
// PriceSyncWorkflowInput represents input for the price sync workflow.
// Supports both plan and addon price sync via EntityType + EntityID.
// PlanID is kept for backward compatibility — if EntityID is empty and PlanID is set,
// EntityID is backfilled from PlanID and EntityType is set to PRICE_ENTITY_TYPE_PLAN.
type PriceSyncWorkflowInput struct {
	EntityType    types.PriceEntityType `json:"entity_type"`    // PRICE_ENTITY_TYPE_PLAN or PRICE_ENTITY_TYPE_ADDON
	EntityID      string                `json:"entity_id"`       // plan ID or addon ID
	PlanID        string                `json:"plan_id"`         // deprecated: use EntityID+EntityType
	TenantID      string                `json:"tenant_id"`
	EnvironmentID string                `json:"environment_id"`
	UserID        string                `json:"user_id"`
}

func (p *PriceSyncWorkflowInput) Validate() error {
	// Backward compat: backfill EntityID/EntityType from PlanID
	if p.EntityID == "" && p.PlanID != "" {
		p.EntityID = p.PlanID
		p.EntityType = types.PRICE_ENTITY_TYPE_PLAN
	}

	if p.EntityID == "" {
		return ierr.NewError("entity ID is required").
			WithHint("Provide entity_id (plan ID or addon ID)").
			Mark(ierr.ErrValidation)
	}

	if p.EntityType != types.PRICE_ENTITY_TYPE_PLAN && p.EntityType != types.PRICE_ENTITY_TYPE_ADDON {
		return ierr.NewError("invalid entity type for price sync").
			WithHintf("entity_type must be %s or %s", types.PRICE_ENTITY_TYPE_PLAN, types.PRICE_ENTITY_TYPE_ADDON).
			Mark(ierr.ErrValidation)
	}

	if p.TenantID == "" || p.EnvironmentID == "" || p.UserID == "" {
		return ierr.NewError("tenant ID, environment ID and user ID are required").
			WithHint("Tenant ID, environment ID and user ID are required").
			Mark(ierr.ErrValidation)
	}

	return nil
}
```

Add the `types` import if not already present:
```go
"github.com/flexprice/flexprice/internal/types"
```

- [ ] **Step 2: Verify it compiles**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice && go build ./internal/temporal/models/...
```

Expected: no output (success).

- [ ] **Step 3: Commit**

```bash
git add internal/temporal/models/plan.go
git commit -m "feat(addon-price-sync): generalize PriceSyncWorkflowInput with EntityType/EntityID"
```

---

## Task 5: AddonActivities + Workflow Generalization + Temporal Service

**Depends on Tasks 3 and 4.**

**Files:**
- Create: `internal/temporal/activities/addon/addon_activities.go`
- Modify: `internal/temporal/workflows/price_sync_workflow.go`
- Modify: `internal/temporal/service/service.go`

- [ ] **Step 1: Create `AddonActivities`**

Create `internal/temporal/activities/addon/addon_activities.go`:

```go
package activities

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/cache"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
)

const ActivityPrefix = "AddonActivities"

// AddonActivities contains all addon-related Temporal activities.
type AddonActivities struct {
	addonService service.AddonService
}

// NewAddonActivities creates a new AddonActivities instance.
func NewAddonActivities(addonService service.AddonService) *AddonActivities {
	return &AddonActivities{addonService: addonService}
}

// SyncAddonPricesInput is the input for the SyncAddonPrices activity.
type SyncAddonPricesInput struct {
	AddonID       string                `json:"addon_id"`
	EntityType    types.PriceEntityType `json:"entity_type"` // always PRICE_ENTITY_TYPE_ADDON
	TenantID      string                `json:"tenant_id"`
	EnvironmentID string                `json:"environment_id"`
	UserID        string                `json:"user_id"`
}

// ActivitySyncAddonPrices is the registered Temporal activity name.
const ActivitySyncAddonPrices = "SyncAddonPrices"

// SyncAddonPrices syncs addon prices to subscription line items.
// Registered as "SyncAddonPrices" in Temporal.
func (a *AddonActivities) SyncAddonPrices(ctx context.Context, input SyncAddonPricesInput) (*dto.SyncAddonPricesResponse, error) {
	if input.AddonID == "" {
		return nil, ierr.NewError("addon ID is required").
			WithHint("Addon ID is required").
			Mark(ierr.ErrValidation)
	}
	if input.TenantID == "" || input.EnvironmentID == "" {
		return nil, ierr.NewError("tenant ID and environment ID are required").
			WithHint("Tenant ID and environment ID are required").
			Mark(ierr.ErrValidation)
	}

	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)
	ctx = types.SetUserID(ctx, input.UserID)

	lockKey := cache.PrefixAddonPriceSyncLock + input.AddonID
	log := logger.GetLogger()
	defer func() {
		redisCache := cache.GetRedisCache()
		if redisCache == nil {
			log.Warnw("addon_price_sync_lock_release_skipped",
				"addon_id", input.AddonID, "lock_key", lockKey, "reason", "redis_cache_nil")
			return
		}
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		redisCache.Delete(releaseCtx, lockKey)
		log.Infow("addon_price_sync_lock_released", "addon_id", input.AddonID, "lock_key", lockKey)
	}()

	return a.addonService.SyncAddonPrices(ctx, input.AddonID)
}
```

- [ ] **Step 2: Generalize `PriceSyncWorkflow`**

Replace the contents of `internal/temporal/workflows/price_sync_workflow.go` with:

```go
// internal/temporal/workflows/price_sync.go
package workflows

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	addonActivities "github.com/flexprice/flexprice/internal/temporal/activities/addon"
	planActivities "github.com/flexprice/flexprice/internal/temporal/activities/plan"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	// WorkflowPriceSync is the Temporal workflow name.
	WorkflowPriceSync = "PriceSyncWorkflow"
	// ActivitySyncPlanPrices is the registered plan sync activity name.
	ActivitySyncPlanPrices = "SyncPlanPrices"
)

func PriceSyncWorkflow(ctx workflow.Context, in models.PriceSyncWorkflowInput) (*dto.SyncPlanPricesResponse, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: time.Hour * 1,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute * 5,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	switch in.EntityType {
	case types.PRICE_ENTITY_TYPE_PLAN:
		activityInput := planActivities.SyncPlanPricesInput{
			PlanID:        in.EntityID,
			TenantID:      in.TenantID,
			EnvironmentID: in.EnvironmentID,
			UserID:        in.UserID,
		}
		var out dto.SyncPlanPricesResponse
		if err := workflow.ExecuteActivity(ctx, ActivitySyncPlanPrices, activityInput).Get(ctx, &out); err != nil {
			return nil, err
		}
		return &out, nil

	case types.PRICE_ENTITY_TYPE_ADDON:
		activityInput := addonActivities.SyncAddonPricesInput{
			AddonID:       in.EntityID,
			EntityType:    in.EntityType,
			TenantID:      in.TenantID,
			EnvironmentID: in.EnvironmentID,
			UserID:        in.UserID,
		}
		var addonOut dto.SyncAddonPricesResponse
		if err := workflow.ExecuteActivity(ctx, addonActivities.ActivitySyncAddonPrices, activityInput).Get(ctx, &addonOut); err != nil {
			return nil, err
		}
		// Map addon response onto SyncPlanPricesResponse for uniform workflow return type
		return &dto.SyncPlanPricesResponse{
			PlanID:  addonOut.AddonID,
			Message: addonOut.Message,
			Summary: dto.SyncPlanPricesSummary{
				LineItemsFoundForCreation: addonOut.Summary.LineItemsFoundForCreation,
				LineItemsCreated:          addonOut.Summary.LineItemsCreated,
				LineItemsTerminated:       addonOut.Summary.LineItemsTerminated,
			},
		}, nil

	default:
		return nil, ierr.NewError("unsupported entity type for price sync").
			WithHintf("entity_type must be %s or %s, got %s",
				types.PRICE_ENTITY_TYPE_PLAN, types.PRICE_ENTITY_TYPE_ADDON, in.EntityType).
			Mark(ierr.ErrValidation)
	}
}
```

- [ ] **Step 3: Update `buildPriceSyncInput` in temporal service**

In `internal/temporal/service/service.go`, find the `buildPriceSyncInput` function (around line 656) and replace it with:

```go
// buildPriceSyncInput builds input for the price sync workflow (plan or addon).
func (s *temporalService) buildPriceSyncInput(_ context.Context, tenantID, environmentID, userID string, params interface{}) (interface{}, error) {
	// If already correct type, just ensure context fields are set
	if input, ok := params.(models.PriceSyncWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		input.UserID = userID
		return input, nil
	}

	// Handle string input — treat as plan ID for backward compatibility
	entityID, ok := params.(string)
	if !ok || entityID == "" {
		return nil, errors.NewError("entity ID is required").
			WithHint("Provide plan ID or addon ID as string, or a PriceSyncWorkflowInput").
			Mark(errors.ErrValidation)
	}

	// Default to plan for backward compat when a bare string is passed
	return models.PriceSyncWorkflowInput{
		EntityType:    types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:      entityID,
		PlanID:        entityID, // keep PlanID populated for backward compat
		TenantID:      tenantID,
		EnvironmentID: environmentID,
		UserID:        userID,
	}, nil
}
```

Make sure `types` is imported in that file — add if not present:
```go
"github.com/flexprice/flexprice/internal/types"
```

- [ ] **Step 4: Verify it compiles**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice && go build ./internal/temporal/...
```

Expected: no output (success).

- [ ] **Step 5: Commit**

```bash
git add internal/temporal/activities/addon/addon_activities.go \
        internal/temporal/workflows/price_sync_workflow.go \
        internal/temporal/service/service.go
git commit -m "feat(addon-price-sync): add AddonActivities, generalize PriceSyncWorkflow, update temporal service"
```

---

## Task 6: Registration + API Handlers + Router + DI Wiring

**Depends on Tasks 3, 4, and 5.**

**Files:**
- Modify: `internal/temporal/registration.go`
- Modify: `internal/api/v1/addon.go`
- Modify: `internal/api/router.go`
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Register `AddonActivities` in registration.go**

In `internal/temporal/registration.go`:

**1a. Add import** at the top:
```go
addonActivities "github.com/flexprice/flexprice/internal/temporal/activities/addon"
```

**1b. Create `addonActivities` instance** after `planActivities := planActivities.NewPlanActivities(planService)` (around line 51):
```go
addonService := service.NewAddonService(params)
addonActs := addonActivities.NewAddonActivities(addonService)
```

**1c. Pass `addonActs` to `buildWorkerConfig`** — add to the call at line ~208:
```go
config := buildWorkerConfig(taskQueue, workflowTrackingActivities, planActivities, addonActs, /* ... rest of existing args ... */)
```

**1d. Add `addonActs` parameter to `buildWorkerConfig` signature**:
```go
func buildWorkerConfig(
    taskQueue types.TemporalTaskQueue,
    workflowTrackingActivities *workflowActivities.WorkflowTrackingActivities,
    planActivities *planActivities.PlanActivities,
    addonActs *addonActivities.AddonActivities,  // NEW
    // ... rest unchanged
) WorkerConfig {
```

**1e. Register `SyncAddonPrices` on the price task queue** — in the `case types.TemporalTaskQueuePrice:` block, add after `planActivities.SyncPlanPrices`:
```go
addonActs.SyncAddonPrices,
```

- [ ] **Step 2: Add handlers to `internal/api/v1/addon.go`**

**2a. Add `temporalService` to `AddonHandler`**:

Replace the `AddonHandler` struct and constructor:
```go
type AddonHandler struct {
	service            service.AddonService
	entitlementService service.EntitlementService
	temporalService    temporalservice.TemporalService
	log                *logger.Logger
}

func NewAddonHandler(
	service service.AddonService,
	entitlementService service.EntitlementService,
	temporalService temporalservice.TemporalService,
	log *logger.Logger,
) *AddonHandler {
	return &AddonHandler{
		service:            service,
		entitlementService: entitlementService,
		temporalService:    temporalService,
		log:                log,
	}
}
```

**2b. Add imports** to `internal/api/v1/addon.go`:
```go
"github.com/flexprice/flexprice/internal/cache"
"github.com/flexprice/flexprice/internal/temporal/models"
temporalservice "github.com/flexprice/flexprice/internal/temporal/service"
```

**2c. Add lock key helper** at the end of the file:
```go
func addonPriceSyncLockKey(addonID string) string {
	return cache.PrefixAddonPriceSyncLock + addonID
}
```

**2d. Add `SyncAddonPrices` handler** (v1 — Temporal async):
```go
// @Summary Sync addon prices to subscriptions (async)
// @ID syncAddonPrices
// @Description Starts a Temporal workflow to sync addon prices to subscription line items for all active subscriptions with this addon.
// @Tags Addons
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Addon ID"
// @Success 200 {object} temporalModels.TemporalWorkflowResult
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 409 {object} ierr.ErrorResponse "Sync already in progress"
// @Failure 500 {object} ierr.ErrorResponse
// @Router /addons/{id}/sync/subscriptions [post]
func (h *AddonHandler) SyncAddonPrices(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("addon ID is required").
			WithHint("Addon ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	// Verify addon exists
	if _, err := h.service.GetAddon(c.Request.Context(), id); err != nil {
		c.Error(err)
		return
	}

	// Acquire addon-level lock
	redisCache := cache.GetRedisCache()
	if redisCache == nil {
		c.Error(ierr.NewError("price sync lock unavailable").
			WithHint("Redis cache is not available. Try again later.").
			Mark(ierr.ErrServiceUnavailable))
		return
	}
	lockKey := addonPriceSyncLockKey(id)
	acquired, err := redisCache.TrySetNX(c.Request.Context(), lockKey, "1", cache.ExpiryPriceSyncLock)
	if err != nil {
		h.log.Errorw("addon_price_sync_lock_acquire_failed", "addon_id", id, "lock_key", lockKey, "error", err)
		c.Error(ierr.NewError("failed to acquire addon price sync lock").
			WithHint("Try again later.").
			Mark(ierr.ErrInternal))
		return
	}
	if !acquired {
		h.log.Infow("addon_price_sync_lock_rejected", "addon_id", id, "lock_key", lockKey, "reason", "already_held")
		c.Error(ierr.NewError("price sync already in progress for this addon").
			WithHint("Try again later or wait up to 2 hours for the current sync to complete.").
			Mark(ierr.ErrAlreadyExists))
		return
	}
	h.log.Infow("addon_price_sync_lock_acquired", "addon_id", id, "lock_key", lockKey)

	workflowInput := models.PriceSyncWorkflowInput{
		EntityType: types.PRICE_ENTITY_TYPE_ADDON,
		EntityID:   id,
	}
	workflowRun, err := h.temporalService.ExecuteWorkflow(c.Request.Context(), types.TemporalPriceSyncWorkflow, workflowInput)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":     "addon price sync workflow started successfully",
		"workflow_id": workflowRun.GetID(),
		"run_id":      workflowRun.GetRunID(),
	})
}
```

**2e. Add `SyncAddonPricesV2` handler** (v2 — direct sync):
```go
// @Summary Sync addon prices to subscriptions (synchronous)
// @ID syncAddonPricesV2
// @Description Synchronously syncs addon prices to subscription line items. Blocks until complete.
// @Tags Addons
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Addon ID"
// @Success 200 {object} dto.SyncAddonPricesResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 409 {object} ierr.ErrorResponse "Sync already in progress"
// @Failure 500 {object} ierr.ErrorResponse
// @Router /addons/{id}/sync/subscriptions/v2 [post]
func (h *AddonHandler) SyncAddonPricesV2(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("addon ID is required").
			WithHint("Addon ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	redisCache := cache.GetRedisCache()
	if redisCache == nil {
		c.Error(ierr.NewError("price sync lock unavailable").
			WithHint("Redis cache is not available. Try again later.").
			Mark(ierr.ErrServiceUnavailable))
		return
	}
	lockKey := addonPriceSyncLockKey(id)
	acquired, err := redisCache.TrySetNX(c.Request.Context(), lockKey, "1", cache.ExpiryPriceSyncLock)
	if err != nil {
		h.log.Errorw("addon_price_sync_lock_acquire_failed", "addon_id", id, "lock_key", lockKey, "error", err)
		c.Error(ierr.NewError("failed to acquire addon price sync lock").
			WithHint("Try again later.").
			Mark(ierr.ErrInternal))
		return
	}
	if !acquired {
		h.log.Infow("addon_price_sync_lock_rejected", "addon_id", id, "lock_key", lockKey, "reason", "already_held")
		c.Error(ierr.NewError("price sync already in progress for this addon").
			WithHint("Try again later or wait up to 2 hours for the current sync to complete.").
			Mark(ierr.ErrAlreadyExists))
		return
	}
	h.log.Infow("addon_price_sync_lock_acquired", "addon_id", id, "lock_key", lockKey)
	defer redisCache.Delete(c.Request.Context(), lockKey)

	resp, err := h.service.SyncAddonPrices(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
```

- [ ] **Step 3: Add routes to `internal/api/router.go`**

Find the addon route group (around line 244):
```go
addon := v1Private.Group("/addons")
{
    // existing routes...
    addon.DELETE("/:id", handlers.Addon.DeleteAddon)
```

Add after the last existing addon route:
```go
addon.POST("/:id/sync/subscriptions", handlers.Addon.SyncAddonPrices)
addon.POST("/:id/sync/subscriptions/v2", handlers.Addon.SyncAddonPricesV2)
```

- [ ] **Step 4: Update `NewAddonHandler` call in router/DI**

Find where `NewAddonHandler` is called (likely in `router.go` or a handlers init file) and add `temporalService` as the third argument. Search for the call:

```bash
grep -rn "NewAddonHandler" /Users/omkar/Developer/source-code/flexprice/flexprice/
```

Update the call to pass `temporalService` as the third parameter (before `log`).

- [ ] **Step 5: Add `NewAddonPriceSyncRepository` to DI in `cmd/server/main.go`**

Find the line `repository.NewPlanPriceSyncRepository,` in `cmd/server/main.go` and add directly after it:
```go
repository.NewAddonPriceSyncRepository,
```

Also update `NewServiceParams` call to pass `addonPriceSyncRepo` — find the call to `service.NewServiceParams(...)` and add the new repo as the last parameter before `workflowExecutionRepo`:

The function signature now has `addonPriceSyncRepo addonpricesync.Repository` as a new parameter. The FX DI will inject it automatically since `NewAddonPriceSyncRepository` is registered.

- [ ] **Step 6: Verify it compiles**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice && make build
```

Expected: build succeeds with no errors.

- [ ] **Step 7: Commit**

```bash
git add internal/temporal/registration.go \
        internal/api/v1/addon.go \
        internal/api/router.go \
        cmd/server/main.go
git commit -m "feat(addon-price-sync): wire registration, API handlers, routes, and DI"
```

---

## Task 7: Build Verification

**Depends on Task 6.**

- [ ] **Step 1: Full build**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice && make build
```

Expected: exits 0, no errors.

- [ ] **Step 2: Vet**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice && go vet ./...
```

Expected: no output (no issues).

- [ ] **Step 3: Commit if any fixes were made**

```bash
git add -A
git commit -m "fix(addon-price-sync): address build and vet issues"
```

---

## Task 8: Tests

**Depends on Tasks 1–6.**

**Files:**
- Modify or create: `internal/service/addon_test.go`

- [ ] **Step 1: Verify existing addon tests still pass**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice && go test -v -race ./internal/service/ -run TestAddon -count=1
```

Expected: PASS (or SKIP if they use `t.Skip()`).

- [ ] **Step 2: Add `TestSyncAddonPrices_NoSubscriptions`**

Add to `internal/service/addon_test.go` (or create the file if it doesn't exist for sync tests):

```go
func (s *AddonServiceTestSuite) TestSyncAddonPrices_NoSubscriptions() {
    s.T().Skip("requires DB fixture setup — enable when integration test infra is wired for addon_associations")
    // Setup: create an addon with 1 active price, no addon_associations
    // Call: resp, err := s.addonService.SyncAddonPrices(ctx, addonID)
    // Assert: err == nil
    // Assert: resp.Summary.LineItemsCreated == 0
    // Assert: resp.Summary.LineItemsTerminated == 0
}
```

- [ ] **Step 3: Add `TestSyncAddonPrices_BasicCreation`**

```go
func (s *AddonServiceTestSuite) TestSyncAddonPrices_BasicCreation() {
    s.T().Skip("requires DB fixture setup — enable when integration test infra is wired for addon_associations")
    // Setup:
    //   - addon with 1 active price (entity_type=ADDON, entity_id=addonID)
    //   - 1 subscription with addon_association (entity_type=SUBSCRIPTION, addon_id=addonID, addon_status=active)
    //   - subscription has matching currency/billing_period/billing_period_count
    //   - no existing line item for this price on the subscription
    // Call: resp, err := s.addonService.SyncAddonPrices(ctx, addonID)
    // Assert: err == nil
    // Assert: resp.Summary.LineItemsCreated == 1
    // Assert: resp.Summary.LineItemsTerminated == 0
    // Assert: line item exists in DB with entity_type="addon", price_id=priceID, subscription_id=subID
}
```

- [ ] **Step 4: Add `TestSyncAddonPrices_Idempotency`**

```go
func (s *AddonServiceTestSuite) TestSyncAddonPrices_Idempotency() {
    s.T().Skip("requires DB fixture setup")
    // Setup: same as BasicCreation, but run SyncAddonPrices twice
    // First call: resp1.Summary.LineItemsCreated == 1
    // Second call: resp2.Summary.LineItemsCreated == 0 (already exists, no duplicate)
}
```

- [ ] **Step 5: Add `TestSyncAddonPrices_Termination`**

```go
func (s *AddonServiceTestSuite) TestSyncAddonPrices_Termination() {
    s.T().Skip("requires DB fixture setup")
    // Setup:
    //   - addon price with end_date = yesterday (expired)
    //   - 1 subscription with addon_association
    //   - existing line item with end_date=NULL for this price
    // Call: resp, err := s.addonService.SyncAddonPrices(ctx, addonID)
    // Assert: err == nil
    // Assert: resp.Summary.LineItemsTerminated == 1
    // Assert: line item end_date is now set to price.end_date
}
```

- [ ] **Step 6: Add `TestSyncAddonPrices_InactiveAssociation`**

```go
func (s *AddonServiceTestSuite) TestSyncAddonPrices_InactiveAssociation() {
    s.T().Skip("requires DB fixture setup")
    // Setup:
    //   - addon with 1 active price
    //   - 1 subscription with addon_association where addon_status = "cancelled"
    // Call: resp, err := s.addonService.SyncAddonPrices(ctx, addonID)
    // Assert: err == nil
    // Assert: resp.Summary.LineItemsCreated == 0 (inactive association excluded)
}
```

- [ ] **Step 7: Run all new tests**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice && go test -v -race ./internal/service/ -run TestSyncAddonPrices -count=1
```

Expected: all SKIP (skipped until infra wired), no FAIL.

- [ ] **Step 8: Run full service test suite to check for regressions**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice && go test -race ./internal/service/... -count=1 -timeout 120s
```

Expected: all existing tests PASS or SKIP. No new failures.

- [ ] **Step 9: Commit**

```bash
git add internal/service/addon_test.go
git commit -m "test(addon-price-sync): add placeholder test cases for SyncAddonPrices"
```

---

## Parallelism Summary

```
[parallel] Task 1 (domain + DTO + cache)   Task 4 (temporal models)
                    ↓                              ↓
               Task 2 (repo)                       |
                    ↓                              |
               Task 3 (service) ←─────────────────┘
                    ↓
               Task 5 (activities + workflow + temporal svc)
                    ↓
               Task 6 (registration + API + router + DI)
                    ↓
               Task 7 (build verification)
                    ↓
               Task 8 (tests)
```
