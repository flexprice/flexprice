# Subscription Schedules & Phases

## 1. Problem Statement
Flexprice currently supports a single‐phase subscription model.  Customers often need future-dated changes (e.g. promotional credits for the first month, different commitment/overage rules after year-1, etc.).  Today these changes require manual intervention and are error-prone.

We will introduce **Subscription Schedules** – a first-class object that represents a timeline of **Phases**.  Each phase can customise a limited set of attributes:

* Credit Grants (add / remove / update)
* Commitment Amount
* Overage Factor
* Line-items (price IDs & quantities)

For v1 we will **persist & expose** this structure through APIs / UI but **NOT automatically apply** phase transitions.  A future worker/cron will activate phases and update the underlying subscription.

## 2. Goals & Non-Goals
### Goals
1. Persist schedules & phases in a normalised schema.
2. Allow schedules to be created alongside a new subscription (embedded in `CreateSubscription` request).
3. Expose CRUD + list APIs for schedules.
4. Display schedules/phases on Admin UI.
5. Keep current billing/usage flows untouched until a phase is activated.
6. Provide a roadmap for automated activation (out-of-scope for v1 implementation code).
7. **Maintain complete backward compatibility with existing subscriptions.**
8. **Ensure schedules are purely opt-in with zero migration required for existing data.**

### Non-Goals (v1)
* Automatic phase switching.
* Support for all Stripe phase features (e.g. trial settings, collection method overrides, proration behaviour, etc.).
* **Migration of existing subscriptions to use schedules.**
* **Making schedules a mandatory part of subscriptions.**

## 3. Glossary
| Term | Definition |
|------|------------|
| **Schedule** | Container mapping a subscription to an ordered list of phases. |
| **Phase** | A time-boxed configuration overriding parts of the parent subscription. |
| **End Behaviour** | What to do when the final phase ends: `RELEASE` (keep sub active) or `CANCEL`. |

## 4. Data Model (Postgres)
### 4.1 `subscription_schedules`
| Column | Type | Null | Default | Notes |
|--------|------|------|---------|-------|
| id | `text` PK (`sched_…`) |  |  | Generated UUID w/ prefix. |
| subscription_id | `text` FK → `subscriptions.id` |  |  | One-to-one.  Add `UNIQUE`. |
| status | `enum('ACTIVE','RELEASED','CANCELED')` |  | `'ACTIVE'` | Mirrors Stripe. |
| current_phase_index | `int` | ✔ | 0 | Index of the phase currently applied (managed by worker). |
| end_behavior | `enum('RELEASE','CANCEL')` |  | `'RELEASE'` | Action after last phase. |
| start_date | `timestamptz` |  |  | Convenience copy of first phase start. |
| metadata | `jsonb` | ✔ | `{}` | Free-form. |
| environment_id | `text` |  |  | Multi-tenant isolation. |
| tenant_id | `text` |  |  | |
| created_at / updated_at | `timestamptz` |  | `now()` | |

**Indexes**
* `idx_subscription_schedules_subscription_id` (unique)
* `idx_subscription_schedules_status`

### 4.2 `subscription_schedule_phases`
| Column | Type | Null | Notes |
|--------|------|------|-------|
| id | `text` PK (`phase_…`) |
| schedule_id | `text` FK → `subscription_schedules.id` |
| phase_index | `int` |  | Starts at 0. |
| start_date | `timestamptz` |  | |
| end_date | `timestamptz` | ✔ | `NULL` for indefinite last phase. |
| commitment_amount | `numeric(18,6)` | ✔ | 0 |
| overage_factor | `numeric(9,4)` | ✔ | 1.0000 |
| line_items | `jsonb` | ✔ | Serialized slice of `{price_id, quantity}` (until full normalisation needed). |
| credit_grants | `jsonb` | ✔ | Array of credit-grant payloads (same shape as existing DTO). |
| metadata | `jsonb` | ✔ | |
| environment_id | `text` |  | |
| tenant_id | `text` |  | |
| created_at / updated_at | `timestamptz` |

**Indexes**
* `idx_schedule_phases_schedule_id_phase_index` (unique)
* `idx_schedule_phases_start_date`

## 5. ENT Schema
* New ent.Schema `SubscriptionSchedule` & `SubscriptionSchedulePhase`.
* Edges: Schedule ⟷ Subscription (one-to-one), Schedule ⟶ Phase (one-to-many ordered).
* Generated models must implement `FromEnt` methods analogously to others.

## 6. API Design
### 6.1 DTO Additions
```go
// dto/create_subscription_request.go (existing)
type CreateSubscriptionRequest struct {
  // ...existing fields
  Phases []SubscriptionSchedulePhaseInput `json:"phases" validate:"dive"` // optional
}

type SubscriptionSchedulePhaseInput struct {
  BillingCycle      types.BillingCycle        `json:"billing_cycle"`
  StartDate         time.Time                 `json:"start_date"`
  EndDate           *time.Time                `json:"end_date,omitempty"`
  LineItems         []SubscriptionLineItemIn  `json:"line_items"`
  CreditGrants      []CreateCreditGrantRequest `json:"credit_grants"`
  CommitmentAmount  decimal.Decimal           `json:"commitment_amount"`
  OverageFactor     decimal.Decimal           `json:"overage_factor"`
}

// dto/subscription_response.go (existing)
type SubscriptionResponse struct {
  // ... existing fields
  Schedule *SubscriptionScheduleResponse `json:"schedule,omitempty"` // Only populated if subscription has a schedule
}

type SubscriptionScheduleResponse struct {
  ID               string                          `json:"id"`
  Status           types.ScheduleStatus            `json:"status"`
  CurrentPhaseIndex int                            `json:"current_phase_index"`
  EndBehavior      types.ScheduleEndBehavior       `json:"end_behavior"`
  Phases           []*SubscriptionSchedulePhase    `json:"phases"`
}

type SubscriptionSchedulePhase struct {
  ID               string                    `json:"id"`
  PhaseIndex       int                       `json:"phase_index"`
  StartDate        time.Time                 `json:"start_date"`
  EndDate          *time.Time                `json:"end_date,omitempty"`
  CommitmentAmount decimal.Decimal           `json:"commitment_amount"`
  OverageFactor    decimal.Decimal           `json:"overage_factor"`
  CreditGrants     []CreditGrantResponse     `json:"credit_grants,omitempty"`
  LineItems        []SubscriptionLineItemDTO `json:"line_items,omitempty"`
}
```
Validation:
* `phases` must be ordered, contiguous (phase[i].end == phase[i+1].start).
* `start_date` ≥ subscription start.
* etc.

### 6.2 Endpoints
| Method | Path | Purpose |
|--------|------|---------|
| POST | `/v1/subscriptions/:id/schedules` | Create a schedule for existing sub (future). |
| GET | `/v1/subscriptions/:id/schedules` | Fetch schedule + phases. |
| PATCH | `/v1/subscription_schedules/:id` | Update status/end_behavior. |

### 6.3 Responses
Reuse Stripe style: embed `phases` array, include `current_phase`.  See sample payload later.

### 6.4 GET Subscription API Integration
The existing GET subscription endpoint will be enhanced to optionally include schedule information:

1. **No Change Required for Existing Clients**:
   * The `schedule` field is optional and omitted when not present
   * Existing clients continue to work without changes

2. **Expansion Parameter**:
   * Add support via `expand=schedule` query parameter
   * When specified, includes schedule and phases data

3. **Response Format**:
   ```json
   {
     "id": "sub_012345",
     "customer_id": "cust_67890",
     // ... all existing subscription fields
     "schedule": {
       "id": "sched_abcdef",
       "status": "ACTIVE",
       "current_phase_index": 0,
       "phases": [
         // ... phases content
       ]
     }
   }
   ```

4. **Performance Considerations**:
   * Lazy loading of schedule data unless explicitly requested
   * Use join hints to optimize DB queries when expansion requested

## 7. Backward Compatibility Strategy

### 7.1 Subscription Creation
* **Opt-in Creation**: Schedules are **only** created when `phases` array is explicitly provided in the request
* **Traditional Flow**: If no `phases` provided, follow existing subscription creation logic without schedule
* **Zero Changes**: Existing code paths for subscriptions without schedules remain unchanged

### 7.2 Schedule Creation Logic
```go
// Pseudocode showing how backwards compatibility is maintained
func (s *subscriptionService) CreateSubscription(ctx context.Context, req dto.CreateSubscriptionRequest) (*dto.SubscriptionResponse, error) {
  // Create subscription (existing logic)
  sub := createSubscriptionFromRequest(req)
  
  // Only create schedule if phases explicitly requested
  if len(req.Phases) > 0 {
    schedule := createScheduleFromPhases(sub, req.Phases)
    storeSchedule(schedule)
    // Include schedule in response
    return &dto.SubscriptionResponse{
      Subscription: sub,
      Schedule: schedule,
    }
  }
  
  // Default path - no schedule created or returned
  return &dto.SubscriptionResponse{
    Subscription: sub,
  }
}
```

### 7.3 Data Access and Repository Layer
* **Null Check**: All code handling schedules must check for existence
* **Repository Methods**: Add optional methods like `GetWithSchedule()` alongside existing methods
* **Transaction Boundaries**: Schedule operations use separate transactions from core subscription CRUD

### 7.4 Feature Flag Protection
* All schedule-related code paths protected by feature flag
* Enable `SUBSCRIPTION_SCHEDULES_ENABLED=true` for environments using schedules
* Default `false` to enforce backward compatibility by default

## 8. Service Layer Changes
1. **SubscriptionService.CreateSubscription**
   * If `req.Phases` provided:
     * Persist Schedule + Phases in same transaction as subscription.
     * Validate first phase dates align with subscription start.
   * Else: **NO schedule is created** - maintain backward compatibility.

2. **SubscriptionService.GetSubscription**
   * Enhance to optionally fetch & include schedule data based on expand parameter.
   * Default behavior (no expand) returns subscription without schedule.

3. **Repositories**
   * `SubscriptionScheduleRepo` with CRUD & helper `GetBySubscriptionID`.
   * Strict null handling to keep backward compatibility.

4. **BillingService**
   * No runtime change for v1.  In future, `CalculateCharges` will read current phase overrides already copied onto `subscription` when phase activates.

## 9. Phase Activation Engine (Future-work)
* Cron job every minute scans `subscription_schedules` where `status='ACTIVE'` and `current_phase_index.nextPhase.start_date <= now`.
* Uses `FOR UPDATE SKIP LOCKED` to avoid race.
* Transition algorithm:
  1. Fetch schedule + phase.
  2. Begin tx.
  3. Update Subscription fields (commitment_amount, overage_factor).
  4. Re-sync line items (soft delete, add new).
  5. Grant credits via existing `CreditGrantService`.
  6. Increment `current_phase_index`; if done – apply `end_behavior`.
  7. Commit.
* Publish webhook `subscription.phase.activated`.

## 10. Effects on Existing Logic
| Area | Impact |
|------|--------|
| Usage Calc (`SubscriptionService.GetUsageBySubscription`) | None – uses subscription fields. |
| Billing Period Update Job | None. |
| Pause/Resume | Schedules remain untouched (phases dates are absolute). |
| Subscription Creation | None for traditional flow (no phases). Only augments when phases provided. |
| Subscription Retrieval | None by default. Only includes schedule data when `expand=schedule`. |

## 11. Migrations & Schema Changes
1. Alembic/Goose migration adding two tables & enums.
2. **No back-fill** - existing subscriptions remain without schedules.
3. All foreign key constraints use `ON DELETE CASCADE` to ensure clean deletion.

## 12. Roll-out Plan
1. Merge schema & repository code behind feature flag `SUBSCRIPTION_SCHEDULES_ENABLED`.
2. Deploy – migration adds tables (no runtime path yet).
3. UI/API changes behind same flag.
4. Test creation of new subscriptions with and without schedules.
5. Verify existing subscription workflows unaffected.

## 13. UI Requirements
* Subscription Detail page – new **Schedule** section with timeline view.
* Phase card shows start/end, commitment, overage, credit grants, line-items.
* Allow creation & simple edit (delete & recreate) until first phase starts.
* UI hides schedule section entirely for subscriptions without schedules.

## 14. Monitoring & Observability
* Emit metrics: `schedule_created_total`, `phase_activated_total`.
* Logs tagged with `schedule_id`, `phase_index`.
* Sentry breadcrumbs around phase engine.
* **Add metric for schedule percentage**: Track what % of new subs have schedules.

## 15. Testing Strategy
* Unit: validation helpers, repository round-trip, service create.
* Integration: create subscription w/ and w/o phases, assert correct DB state.
* **Backward compatibility**: Ensure old subscription API calls work identically.

## 16. Sample JSON
```json
{
  "id": "sched_01XXX",
  "subscription_id": "sub_01YYY",
  "status": "ACTIVE",
  "current_phase_index": 0,
  "phases": [
    {
      "id": "phase_01AAA",
      "phase_index": 0,
      "start_date": "2025-05-20T08:30:20Z",
      "end_date": "2025-05-29T18:30:00Z",
      "commitment_amount": "0",
      "overage_factor": "1",
      "credit_grants": [ {"amount": 23, "currency": "USD", "name": "Free Credits"} ],
      "line_items": []
    },
    {
      "id": "phase_01AAB",
      "phase_index": 1,
      "start_date": "2025-05-29T18:30:00Z",
      "end_date": null,
      "commitment_amount": "0",
      "overage_factor": "1",
      "credit_grants": [],
      "line_items": []
    }
  ]
}
```

---
**Author:** Backend Platform Team

**Last Updated:** {{DATE}} 