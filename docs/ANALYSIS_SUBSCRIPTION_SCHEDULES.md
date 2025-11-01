# Subscription Schedules & Phases - Complete Analysis

## Overview

**Subscription Schedules** are a first-class feature that allows subscriptions to have multiple **Phases** over time, each with different configurations. This enables future-dated changes to subscriptions without manual intervention.

---

## Purpose & Business Value

### Problem Solved

- **Future-dated changes**: Set up promotional pricing, different commitment amounts, or credit grants that activate at specific future dates
- **Multi-phase pricing**: Handle scenarios like:
  - First month promotional credits
  - Year-1 vs Year-2 pricing rules
  - Graduated commitment amounts
  - Different overage factors over time

### Key Characteristics

1. **Opt-in**: Schedules are optional - existing subscriptions work without them
2. **Backward Compatible**: Zero migration required, no breaking changes
3. **Persistence Only (v1)**: Phases are stored but **not automatically activated** (manual activation or future worker)

---

## Core Concepts

### 1. **SubscriptionSchedule** (Container)

A schedule is a **one-to-one** relationship with a subscription that contains an ordered list of phases.

**Key Properties:**

- `ID`: Unique identifier (prefix: `sched_`)
- `SubscriptionID`: Links to exactly one subscription (unique constraint)
- `ScheduleStatus`: `ACTIVE`, `RELEASED`, or `CANCELED`
- `CurrentPhaseIndex`: Points to the currently active phase (0-based)
- `EndBehavior`: What happens when final phase ends (`RELEASE` or `CANCEL`)
- `StartDate`: Convenience copy of first phase's start date
- `Phases[]`: Ordered list of phase configurations

**Schema Structure:**

```
subscription_schedules
├── id (PK)
├── subscription_id (FK, UNIQUE) → subscriptions.id
├── schedule_status (enum: ACTIVE|RELEASED|CANCELED)
├── current_phase_index (int, default: 0)
├── end_behavior (enum: RELEASE|CANCEL, default: RELEASE)
├── start_date (timestamptz)
└── metadata (jsonb)
```

### 2. **SchedulePhase** (Time-boxed Configuration)

Each phase represents a period of time with specific subscription settings that override the base subscription.

**Key Properties:**

- `ID`: Unique identifier (prefix: `phase_`)
- `ScheduleID`: Links to parent schedule
- `PhaseIndex`: Order within schedule (0, 1, 2, ...)
- `StartDate`: When this phase begins
- `EndDate`: When this phase ends (NULL for indefinite last phase)
- `CommitmentAmount`: Override for subscription commitment
- `OverageFactor`: Override for overage pricing multiplier
- `LineItems[]`: Price IDs and quantities for this phase
- `CreditGrants[]`: Credits to grant during this phase
- `Metadata`: Free-form JSON

**Schema Structure:**

```
subscription_schedule_phases
├── id (PK)
├── schedule_id (FK) → subscription_schedules.id
├── phase_index (int, indexed with schedule_id)
├── start_date (timestamptz, indexed)
├── end_date (timestamptz, nullable)
├── commitment_amount (numeric, nullable)
├── overage_factor (numeric, nullable)
├── line_items (jsonb) → Array of {price_id, quantity, display_name, metadata}
├── credit_grants (jsonb) → Array of credit grant definitions
└── metadata (jsonb)
```

**Phase Continuity Rules:**

- Phases must be **contiguous** - phase[i].end_date == phase[i+1].start_date
- All phases except the last must have an `end_date`
- Last phase can have `end_date = NULL` (indefinite)

---

## Data Flow & Architecture

### Layer 1: DTO Layer (`internal/api/dto/subscription_schedule.go`)

#### Request DTOs

1. **`SubscriptionSchedulePhaseInput`** (lines 15-24)

   - Input for creating/updating phases
   - Contains: billing cycle, dates, line items, credit grants, commitment, overage, metadata
   - Has `Validate()` method for phase-level validation

2. **`CreateSubscriptionScheduleRequest`** (lines 60-64)

   - Creates a new schedule with multiple phases
   - Requires: `subscription_id`, `phases[]`, optional `end_behavior`
   - Validates phase continuity (adjacent phases must connect)

3. **`AddSchedulePhaseRequest`** (lines 27-29)

   - Adds a single phase to existing schedule
   - Wraps a `SubscriptionSchedulePhaseInput`

4. **`UpdateSubscriptionScheduleRequest`** (lines 67-70)
   - Updates schedule metadata: `status`, `end_behavior`

#### Response DTOs

1. **`SubscriptionScheduleResponse`** (lines 32-42)

   - Full schedule with embedded phases
   - Includes: ID, status, current phase index, end behavior, start date, phases array

2. **`SubscriptionSchedulePhaseResponse`** (lines 45-57)
   - Individual phase response
   - Includes all phase properties plus timestamps

#### Conversion Functions

- `SubscriptionScheduleResponseFromDomain()`: Domain → DTO
- `SubscriptionSchedulePhaseResponseFromDomain()`: Phase domain → DTO

---

### Layer 2: Domain Layer (`internal/domain/subscription/schedule.go`)

#### Domain Models

1. **`SubscriptionSchedule`** (lines 13-24)

   - Business logic entity
   - Methods:
     - `GetCurrentPhase()`: Returns phase at `CurrentPhaseIndex`
     - `GetNextPhase()`: Returns next phase (if exists)
     - `IsActive()`: Checks if status is `ACTIVE`
     - `HasFuturePhases()`: Checks if more phases exist

2. **`SchedulePhase`** (lines 27-40)
   - Phase business entity
   - Contains overrides: commitment, overage, line items, credit grants

#### Conversion from Database

- `GetSubscriptionScheduleFromEnt()`: Ent → Domain
- `GetSchedulePhasesFromEnt()`: Ent phases → Domain phases

---

### Layer 3: Service Layer (`internal/service/subscription.go`)

#### Key Service Methods

1. **`CreateSubscriptionSchedule()`** (line ~2717)

   - Creates a schedule for an existing subscription
   - Validates subscription exists and doesn't already have a schedule
   - Creates schedule + phases in transaction
   - Uses `createScheduleFromPhases()` helper

2. **`createScheduleFromPhases()`** (line ~2907)

   - **Reusable helper** that:
     - Creates schedule entity (ID, subscription ID, status, dates)
     - Converts phase inputs to domain phases
     - Transforms line items and credit grants
     - Persists via `SubscriptionScheduleRepo.CreateWithPhases()`

3. **`AddSchedulePhase()`** (line ~2989)

   - Adds a phase to existing schedule
   - Validates phase continuity
   - Updates schedule

4. **`AddSubscriptionPhase()`** (line ~3155)
   - Convenience method - adds phase using subscription ID
   - Auto-creates schedule if none exists (with initial phase from subscription start)

---

## Validation Flow

### Phase-Level Validation (`SubscriptionSchedulePhaseInput.Validate()`)

1. ✅ `start_date` required (not zero)
2. ✅ `commitment_amount` ≥ 0
3. ✅ `overage_factor` ≥ 1.0
4. ✅ All credit grants validate (recursive)

### Schedule-Level Validation (`CreateSubscriptionScheduleRequest.Validate()`)

1. ✅ At least one phase required
2. ✅ Each phase validates individually
3. ✅ **Phase Continuity Check**:
   - For phases after the first (i > 0):
     - Previous phase must have `end_date`
     - Previous phase's `end_date` must equal current phase's `start_date`
     - Ensures no gaps or overlaps

---

## Creation Flow Examples

### Example 1: Create Subscription WITH Schedule

```json
POST /v1/subscriptions
{
  "customer_id": "cust_123",
  "plan_id": "plan_456",
  "phases": [
    {
      "start_date": "2025-01-01T00:00:00Z",
      "end_date": "2025-02-01T00:00:00Z",
      "commitment_amount": "0",
      "overage_factor": "1.0",
      "credit_grants": [{
        "name": "Welcome Credits",
        "credits": "100",
        "cadence": "MONTHLY"
      }]
    },
    {
      "start_date": "2025-02-01T00:00:00Z",
      "end_date": null,
      "commitment_amount": "500",
      "overage_factor": "1.5"
    }
  ]
}
```

**Flow:**

1. Create subscription normally
2. If `phases` provided → call `createScheduleFromPhases()`
3. Create schedule with status `ACTIVE`, `current_phase_index = 0`
4. Create all phases with sequential `phase_index` (0, 1, 2...)
5. Persist in transaction
6. Return subscription + schedule in response

### Example 2: Add Phase to Existing Schedule

```json
POST /v1/subscriptions/{id}/phases
{
  "phase": {
    "start_date": "2025-12-01T00:00:00Z",
    "end_date": null,
    "commitment_amount": "1000"
  }
}
```

**Flow:**

1. Get existing schedule
2. Validate new phase starts when last phase ends (or create initial phase)
3. Create new phase with `phase_index = len(existing_phases)`
4. Update schedule metadata if needed
5. Persist phase

---

## Stripe's Subscription Schedule Behavior

### How Stripe Handles Phase Transitions

**Key Behavior: Stripe does NOT create a new subscription for each phase.**

Instead, Stripe:

1. **Updates the existing subscription** - When a new phase activates, Stripe modifies the existing subscription's attributes (prices, discounts, billing cycle, etc.) to match the phase configuration.

2. **Maintains subscription identity** - The subscription ID remains constant throughout all phase transitions. The subscription entity is never replaced or recreated.

3. **Attribute inheritance** - Attributes set in a phase override the subscription's current settings. If a phase doesn't specify an attribute, the subscription retains the previous value.

4. **Proration handling** - During phase transitions, Stripe can handle billing adjustments:

   - `create_prorations`: Generate proration adjustments for billing changes
   - `none`: No prorations created
   - `always_invoice`: Generate prorations and immediately finalize an invoice

5. **End behavior** - After the final phase:
   - `release` (default): Subscription continues with settings from the last phase, detached from the schedule
   - `cancel`: Subscription is automatically canceled

### Example Flow (Stripe Model)

```
Subscription: sub_123 (created Jan 1)
├── Phase 0 (Jan 1 - Feb 1): $50/month
├── Phase 1 (Feb 1 - Mar 1): $75/month  ← Updates sub_123's price
└── Phase 2 (Mar 1+): $100/month         ← Updates sub_123's price

Result: Same subscription (sub_123), different prices over time
```

### Implications for Flexprice

Your current implementation follows the **same pattern** - phases are designed to **update** the existing subscription, not create new ones. The `createScheduleFromPhases()` method creates a schedule that will modify subscription attributes when phases activate.

**This differs from your `SubscriptionChange` implementation**, which creates a new subscription (archives old, creates new). Subscription schedules are intended for **seamless updates** to the same subscription over time.

---

## Current State (v1 Implementation)

### ✅ Implemented

- Schema & database persistence
- CRUD APIs for schedules and phases
- Validation and continuity checks
- Domain models with helper methods
- DTO conversions
- Backward compatibility (opt-in)

### ❌ Not Implemented (v1)

- **Automatic phase activation** - phases are stored but not applied
- **Phase transition worker** - no cron job to activate phases
- **Real-time application** - phase settings don't affect billing until manually applied

### 🔮 Future Implementation (Phase Activation Engine)

As per PRD (section 9) and following Stripe's model:

```go
// Pseudocode for future worker (Stripe-style updates)
func (w *ScheduleWorker) ActivatePendingPhases() {
  // 1. Find schedules with phases ready to activate
  schedules := w.repo.FindSchedulesWithPendingPhases(now)

  for schedule := range schedules {
    nextPhase := schedule.GetNextPhase()
    if nextPhase.StartDate <= now {
      // 2. Get the existing subscription (NOT create new)
      subscription := w.subRepo.Get(schedule.SubscriptionID)

      // 3. Update existing subscription with phase overrides
      // This follows Stripe's model - UPDATE, don't replace
      if nextPhase.CommitmentAmount != nil {
        subscription.CommitmentAmount = nextPhase.CommitmentAmount
      }
      if nextPhase.OverageFactor != nil {
        subscription.OverageFactor = nextPhase.OverageFactor
      }

      // Update line items (soft delete old, add new)
      w.updateSubscriptionLineItems(subscription, nextPhase.LineItems)

      // Grant credits for this phase
      w.applyPhaseCreditGrants(subscription, nextPhase.CreditGrants)

      // 4. Handle proration if needed (like Stripe's proration_behavior)
      if nextPhase.ProrationBehavior == ProrationBehaviorCreateProrations {
        w.createProrationInvoice(subscription, schedule.GetCurrentPhase(), nextPhase)
      }

      // 5. Persist updated subscription (same ID, updated attributes)
      w.subRepo.Update(subscription)

      // 6. Increment phase index on schedule
      schedule.CurrentPhaseIndex++
      w.scheduleRepo.Update(schedule)

      // 7. If last phase ended, apply end_behavior
      if schedule.IsLastPhase() {
        if schedule.EndBehavior == EndBehaviorCancel {
          subscription.Cancel()
          w.subRepo.Update(subscription)
        }
        // If RELEASE, subscription continues with last phase's settings
      }

      // 8. Publish webhook: subscription.phase.activated
      w.publishPhaseActivatedWebhook(subscription.ID, schedule.ID, nextPhase)
    }
  }
}
```

**Key Difference from Subscription Changes:**

- **Subscription Change**: Archives old subscription → Creates new subscription (new ID)
- **Phase Activation**: Updates existing subscription → Same subscription ID, modified attributes

---

## Relationships & Constraints

### One-to-One: Subscription ↔ Schedule

- **Constraint**: One subscription can have at most one schedule
- **Enforced**: `UNIQUE` index on `subscription_id`
- **Nullability**: Subscription can exist without schedule (backward compatibility)

### One-to-Many: Schedule → Phases

- **Ordering**: Phases ordered by `phase_index` (0, 1, 2, ...)
- **Contiguity**: Phases must have contiguous date ranges
- **Cascade**: Deleting schedule deletes all phases

### Phase Override Model

Phases **override** subscription settings:

- If phase has `commitment_amount` → use phase value
- If phase has `overage_factor` → use phase value
- If phase has `line_items` → replace subscription line items
- If phase has `credit_grants` → apply phase grants (in addition/subtraction TBD)

---

## Key Design Decisions

### 1. **Backward Compatibility First**

- Schedules are **never created** unless explicitly requested
- Existing subscriptions work identically
- No migration or backfill required

### 2. **JSONB for Complex Data**

- `line_items` and `credit_grants` stored as JSONB
- Allows flexibility without normalization overhead
- Can normalize later if query patterns require it

### 3. **Phase Index for Ordering**

- Uses `phase_index` integer (0, 1, 2...) instead of sorting by date
- Explicit ordering prevents date-based bugs
- `current_phase_index` points directly to active phase

### 4. **End Behavior Pattern**

- `RELEASE`: After last phase, subscription continues with base config
- `CANCEL`: After last phase, subscription is canceled
- Mimics Stripe's subscription schedule model

---

## Usage Patterns

### Pattern 1: Promotional Pricing

```json
Phases: [
  { start: "2025-01-01", end: "2025-02-01",
    commitment_amount: "0", credit_grants: [...] },
  { start: "2025-02-01", end: null,
    commitment_amount: "500" }
]
```

### Pattern 2: Graduated Commitments

```json
Phases: [
  { start: "2025-01-01", end: "2025-07-01", commitment_amount: "500" },
  { start: "2025-07-01", end: "2026-01-01", commitment_amount: "750" },
  { start: "2026-01-01", end: null, commitment_amount: "1000" }
]
```

### Pattern 3: Changing Overage Rules

```json
Phases: [
  { start: "2025-01-01", end: "2026-01-01",
    commitment_amount: "500", overage_factor: "1.0" },
  { start: "2026-01-01", end: null,
    commitment_amount: "500", overage_factor: "1.5" }
]
```

---

## API Surface

### Endpoints

| Method | Path                              | Purpose                                      |
| ------ | --------------------------------- | -------------------------------------------- |
| POST   | `/v1/subscriptions`               | Create subscription (optionally with phases) |
| POST   | `/v1/subscription_schedules`      | Create schedule for existing subscription    |
| GET    | `/v1/subscriptions/{id}/schedule` | Get schedule with phases                     |
| POST   | `/v1/subscriptions/{id}/phases`   | Add phase to subscription's schedule         |
| PATCH  | `/v1/subscription_schedules/{id}` | Update schedule (status, end_behavior)       |

### Query Parameters

- `expand=schedule`: Include schedule data in subscription GET response

---

## Testing Considerations

1. **Phase Continuity**: Test gaps and overlaps are rejected
2. **Validation**: All phase fields validate correctly
3. **Backward Compatibility**: Subscriptions without schedules work normally
4. **Edge Cases**:
   - Single phase schedules
   - Phases with NULL end_date
   - Adding phases to schedules with existing phases
   - Schedule status transitions

---

## Summary

**Subscription Schedules** provide a powerful way to model multi-phase subscriptions with different pricing, commitments, and credit grant configurations over time. The v1 implementation focuses on **persistence and API exposure**, with automatic phase activation planned for future iterations.

The design prioritizes **backward compatibility** and **opt-in behavior**, ensuring existing subscriptions continue to work without changes while new subscriptions can leverage the phase-based model when needed.
