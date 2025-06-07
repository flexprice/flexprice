# Flexible Credit Grant Expiry System

## Overview

This document outlines the design for a flexible and scalable credit grant expiry system that can handle various expiry scenarios based on different anchoring points such as subscription billing periods, time-based durations, and custom business logic.

## Problem Statement

The current credit grant system only supports simple day-based expiry (`expire_in_days`), which is insufficient for complex business requirements. Customers need flexible expiry configurations that can:

1. Expire credits at the end of subscription billing periods
2. Expire credits after specific durations from various anchor points (billing period start, trial end, etc.)
3. Handle recurring credit grants with period-specific expiry rules
4. Support efficient batch processing via cron jobs
5. Provide clear audit trails for credit expiry events

## Goals

### Primary Goals

- Design a flexible expiry configuration system that can handle multiple expiry scenarios
- Implement efficient cron-based processing for credit expiry
- Maintain backward compatibility with existing `expire_in_days` field
- Provide clear visibility into credit expiry timelines
- Support both one-time and recurring credit grants

### Secondary Goals

- Enable proactive notifications before credit expiry
- Support grace periods and expiry extensions
- Implement expiry pause/resume for subscription pauses
- Provide analytics on credit utilization vs expiry patterns

## Non-Goals

- Real-time credit expiry processing (we'll use cron-based batch processing)
- Complex business rule engines (we'll provide flexible primitives)
- Credit transfer between customers (separate feature)

## Success Metrics

- 100% backward compatibility with existing credit grants
- Sub-second p99 response time for credit balance queries
- 99.9% accuracy in expiry calculations
- Zero missed expiry processing windows

## Design Overview

### Core Concepts

#### 1. Expiry Configuration Structure

```go
type CreditGrantExpiryConfig struct {
    // Type of expiry configuration
    Type CreditGrantExpiryType `json:"type"`

    // Anchor point for expiry calculation
    Anchor CreditGrantExpiryAnchor `json:"anchor,omitempty"`

    // Duration-based configuration
    Duration *CreditGrantExpiryDuration `json:"duration,omitempty"`

    // Period-based configuration (for billing period alignment)
    PeriodAlignment *CreditGrantExpiryPeriodAlignment `json:"period_alignment,omitempty"`

    // Grace period before actual expiry
    GracePeriod *time.Duration `json:"grace_period,omitempty"`

    // Whether to pause expiry during subscription pause
    PauseWithSubscription bool `json:"pause_with_subscription"`

    // Fixed date for expiry (used with FIXED_DATE type)
    FixedDate *time.Time `json:"fixed_date,omitempty"`
}

type CreditGrantExpiryType string

const (
    // Never expire
    CreditGrantExpiryTypeNever CreditGrantExpiryType = "NEVER"

    // Expire after a fixed duration from anchor
    CreditGrantExpiryTypeDuration CreditGrantExpiryType = "DURATION"

    // Expire at the end of a billing period
    CreditGrantExpiryTypePeriodEnd CreditGrantExpiryType = "PERIOD_END"

    // Expire at the start of a billing period
    CreditGrantExpiryTypePeriodStart CreditGrantExpiryType = "PERIOD_START"

    // Expire on a specific date
    CreditGrantExpiryTypeFixedDate CreditGrantExpiryType = "FIXED_DATE"

    // Custom expiry logic (for future extensibility)
    CreditGrantExpiryTypeCustom CreditGrantExpiryType = "CUSTOM"
)

type CreditGrantExpiryAnchor string

const (
    // Anchor to when the credit grant was created
    CreditGrantExpiryAnchorGrantCreated CreditGrantExpiryAnchor = "GRANT_CREATED"

    // Anchor to when the credit grant becomes active
    CreditGrantExpiryAnchorGrantActive CreditGrantExpiryAnchor = "GRANT_ACTIVE"

    // Anchor to subscription start date
    CreditGrantExpiryAnchorSubscriptionStart CreditGrantExpiryAnchor = "SUBSCRIPTION_START"

    // Anchor to current billing period start
    CreditGrantExpiryAnchorBillingPeriodStart CreditGrantExpiryAnchor = "BILLING_PERIOD_START"

    // Anchor to current billing period end
    CreditGrantExpiryAnchorBillingPeriodEnd CreditGrantExpiryAnchor = "BILLING_PERIOD_END"

    // Anchor to trial period start
    CreditGrantExpiryAnchorTrialStart CreditGrantExpiryAnchor = "TRIAL_START"

    // Anchor to trial period end
    CreditGrantExpiryAnchorTrialEnd CreditGrantExpiryAnchor = "TRIAL_END"

    // Anchor to first usage of the credit
    CreditGrantExpiryAnchorFirstUsage CreditGrantExpiryAnchor = "FIRST_USAGE"
)

type CreditGrantExpiryDuration struct {
    // Duration amount
    Amount int `json:"amount"`

    // Duration unit
    Unit CreditGrantExpiryDurationUnit `json:"unit"`
}

type CreditGrantExpiryDurationUnit string

const (
    CreditGrantExpiryDurationUnitDays    CreditGrantExpiryDurationUnit = "DAYS"
    CreditGrantExpiryDurationUnitWeeks   CreditGrantExpiryDurationUnit = "WEEKS"
    CreditGrantExpiryDurationUnitMonths  CreditGrantExpiryDurationUnit = "MONTHS"
    CreditGrantExpiryDurationUnitYears   CreditGrantExpiryDurationUnit = "YEARS"
)

type CreditGrantExpiryPeriodAlignment struct {
    // Which billing period to align with
    PeriodOffset int `json:"period_offset"` // 0 = current, 1 = next, -1 = previous

    // Whether to align with subscription billing cycle or grant-specific cycle
    UseSubscriptionCycle bool `json:"use_subscription_cycle"`

    // Custom cycle definition (only used if UseSubscriptionCycle is false)
    CustomCycle *CreditGrantCustomCycle `json:"custom_cycle,omitempty"`
}

type CreditGrantCustomCycle struct {
    Period      CreditGrantPeriod `json:"period"`
    PeriodCount int               `json:"period_count"`
    Anchor      time.Time         `json:"anchor"`
}
```

#### 2. Expiry Tracking

```go
type CreditGrantExpiry struct {
    ID              string                    `json:"id"`
    CreditGrantID   string                    `json:"credit_grant_id"`

    // Computed expiry date
    ExpiryDate      time.Time                 `json:"expiry_date"`

    // When this expiry calculation was made
    CalculatedAt    time.Time                 `json:"calculated_at"`

    // Status of the expiry
    Status          CreditGrantExpiryStatus   `json:"status"`

    // Amount to expire
    AmountToExpire  decimal.Decimal           `json:"amount_to_expire"`

    // Remaining amount after expiry
    RemainingAmount decimal.Decimal           `json:"remaining_amount"`

    // Anchor point used for calculation
    AnchorDate      time.Time                 `json:"anchor_date"`

    // Grace period end date (if applicable)
    GracePeriodEnd  *time.Time                `json:"grace_period_end,omitempty"`

    // Processing metadata
    ProcessedAt     *time.Time                `json:"processed_at,omitempty"`
    ProcessedBy     *string                   `json:"processed_by,omitempty"`

    // Audit trail
    Events          []CreditGrantExpiryEvent  `json:"events,omitempty"`

    types.BaseModel
}

type CreditGrantExpiryStatus string

const (
    CreditGrantExpiryStatusPending    CreditGrantExpiryStatus = "PENDING"
    CreditGrantExpiryStatusActive     CreditGrantExpiryStatus = "ACTIVE"
    CreditGrantExpiryStatusExpired    CreditGrantExpiryStatus = "EXPIRED"
    CreditGrantExpiryStatusCancelled  CreditGrantExpiryStatus = "CANCELLED"
    CreditGrantExpiryStatusPaused     CreditGrantExpiryStatus = "PAUSED"
)

type CreditGrantExpiryEvent struct {
    ID           string                      `json:"id"`
    Type         CreditGrantExpiryEventType  `json:"type"`
    Timestamp    time.Time                   `json:"timestamp"`
    Data         map[string]interface{}      `json:"data,omitempty"`
    Description  string                      `json:"description"`
}

type CreditGrantExpiryEventType string

const (
    CreditGrantExpiryEventTypeCalculated  CreditGrantExpiryEventType = "CALCULATED"
    CreditGrantExpiryEventTypeUpdated     CreditGrantExpiryEventType = "UPDATED"
    CreditGrantExpiryEventTypeExpired     CreditGrantExpiryEventType = "EXPIRED"
    CreditGrantExpiryEventTypePaused      CreditGrantExpiryEventType = "PAUSED"
    CreditGrantExpiryEventTypeResumed     CreditGrantExpiryEventType = "RESUMED"
    CreditGrantExpiryEventTypeCancelled   CreditGrantExpiryEventType = "CANCELLED"
)
```

## Detailed Design

### Expiry Configuration Examples

#### 1. Simple Duration-Based Expiry

```json
{
  "type": "DURATION",
  "anchor": "GRANT_CREATED",
  "duration": {
    "amount": 30,
    "unit": "DAYS"
  }
}
```

#### 2. Billing Period End Expiry

```json
{
  "type": "PERIOD_END",
  "period_alignment": {
    "period_offset": 0,
    "use_subscription_cycle": true
  },
  "pause_with_subscription": true
}
```

#### 3. Trial End + Grace Period

```json
{
  "type": "DURATION",
  "anchor": "TRIAL_END",
  "duration": {
    "amount": 5,
    "unit": "DAYS"
  },
  "grace_period": "24h"
}
```

#### 4. Next Billing Period Start

```json
{
  "type": "PERIOD_START",
  "period_alignment": {
    "period_offset": 1,
    "use_subscription_cycle": true
  }
}
```

### Expiry Calculation Logic

#### Core Calculator Interface

```go
type ExpiryCalculator interface {
    // Calculate the expiry date for a credit grant
    CalculateExpiry(ctx context.Context, grant *CreditGrant, subscription *Subscription) (*time.Time, error)

    // Recalculate expiry when subscription changes
    RecalculateOnSubscriptionChange(ctx context.Context, grant *CreditGrant, subscription *Subscription, changeType SubscriptionChangeType) (*time.Time, error)

    // Check if recalculation is needed
    NeedsRecalculation(changeType SubscriptionChangeType) bool
}

type SubscriptionChangeType string

const (
    SubscriptionChangePeriodTransition SubscriptionChangeType = "PERIOD_TRANSITION"
    SubscriptionChangePause            SubscriptionChangeType = "PAUSE"
    SubscriptionChangeResume           SubscriptionChangeType = "RESUME"
    SubscriptionChangeCancellation     SubscriptionChangeType = "CANCELLATION"
    SubscriptionChangeTrialEnd         SubscriptionChangeType = "TRIAL_END"
)
```

#### Calculation Strategies

```go
// Duration-based calculator
type DurationExpiryCalculator struct {
    timeService TimeService
}

func (c *DurationExpiryCalculator) CalculateExpiry(ctx context.Context, grant *CreditGrant, subscription *Subscription) (*time.Time, error) {
    anchorDate, err := c.getAnchorDate(grant.ExpiryConfig.Anchor, grant, subscription)
    if err != nil {
        return nil, err
    }

    duration := c.parseDuration(grant.ExpiryConfig.Duration)
    expiryDate := anchorDate.Add(duration)

    if grant.ExpiryConfig.GracePeriod != nil {
        expiryDate = expiryDate.Add(*grant.ExpiryConfig.GracePeriod)
    }

    return &expiryDate, nil
}

// Period-based calculator
type PeriodExpiryCalculator struct {
    billingService BillingService
}

func (c *PeriodExpiryCalculator) CalculateExpiry(ctx context.Context, grant *CreditGrant, subscription *Subscription) (*time.Time, error) {
    alignment := grant.ExpiryConfig.PeriodAlignment

    var targetPeriod BillingPeriod
    if alignment.UseSubscriptionCycle {
        targetPeriod = c.getSubscriptionPeriod(subscription, alignment.PeriodOffset)
    } else {
        targetPeriod = c.getCustomPeriod(alignment.CustomCycle, alignment.PeriodOffset)
    }

    switch grant.ExpiryConfig.Type {
    case CreditGrantExpiryTypePeriodEnd:
        return &targetPeriod.End, nil
    case CreditGrantExpiryTypePeriodStart:
        return &targetPeriod.Start, nil
    }

    return nil, errors.New("invalid period expiry type")
}
```

### Cron-Based Processing System

#### Main Processor

```go
type CreditGrantExpiryProcessor struct {
    repo            CreditGrantRepository
    expiryRepo      CreditGrantExpiryRepository
    subscRepo       SubscriptionRepository
    calculator      ExpiryCalculatorRegistry
    eventPublisher  EventPublisher
    logger          Logger
    metrics         Metrics
}

// Main processing job that runs every 15 minutes
func (p *CreditGrantExpiryProcessor) ProcessExpiryBatch(ctx context.Context) error {
    batchSize := 1000
    now := time.Now().UTC()

    // Process expiries that are due
    if err := p.processExpiredCredits(ctx, now, batchSize); err != nil {
        return err
    }

    // Calculate new expiry dates for credits that need it
    if err := p.calculatePendingExpiries(ctx, now, batchSize); err != nil {
        return err
    }

    // Recalculate expiries for subscription changes
    if err := p.recalculateChangedSubscriptions(ctx, now, batchSize); err != nil {
        return err
    }

    return nil
}

func (p *CreditGrantExpiryProcessor) processExpiredCredits(ctx context.Context, now time.Time, batchSize int) error {
    offset := 0
    for {
        expiries, err := p.expiryRepo.ListExpiredCredits(ctx, now, batchSize, offset)
        if err != nil {
            return err
        }

        if len(expiries) == 0 {
            break
        }

        for _, expiry := range expiries {
            if err := p.processExpiry(ctx, expiry); err != nil {
                p.logger.Errorw("failed to process expiry", "expiry_id", expiry.ID, "error", err)
                p.metrics.IncrementCounter("credit_expiry_processing_errors")
                continue
            }
        }

        offset += len(expiries)
    }

    return nil
}

func (p *CreditGrantExpiryProcessor) processExpiry(ctx context.Context, expiry *CreditGrantExpiry) error {
    // Begin transaction
    tx, err := p.repo.BeginTx(ctx)
    if err != nil {
        return err
    }
    defer tx.Rollback()

    // Lock credit grant for update
    grant, err := tx.GetCreditGrantForUpdate(ctx, expiry.CreditGrantID)
    if err != nil {
        return err
    }

    // Calculate amount to expire
    amountToExpire := decimal.Min(grant.RemainingAmount, expiry.AmountToExpire)

    // Update grant balance
    grant.RemainingAmount = grant.RemainingAmount.Sub(amountToExpire)
    if grant.RemainingAmount.IsZero() {
        grant.Status = types.StatusExpired
    }

    // Update expiry record
    expiry.Status = CreditGrantExpiryStatusExpired
    expiry.ProcessedAt = lo.ToPtr(time.Now().UTC())
    expiry.ProcessedBy = lo.ToPtr("system")

    // Save updates
    if err := tx.UpdateCreditGrant(ctx, grant); err != nil {
        return err
    }

    if err := tx.UpdateCreditGrantExpiry(ctx, expiry); err != nil {
        return err
    }

    // Publish event
    event := &CreditGrantExpiredEvent{
        CreditGrantID:   grant.ID,
        CustomerID:      grant.CustomerID,
        AmountExpired:   amountToExpire,
        RemainingAmount: grant.RemainingAmount,
        ExpiryDate:      expiry.ExpiryDate,
    }

    if err := p.eventPublisher.PublishCreditGrantExpired(ctx, event); err != nil {
        p.logger.Warnw("failed to publish expiry event", "error", err)
    }

    return tx.Commit()
}
```

#### Efficient Querying Strategy

```sql
-- Index strategy for efficient expiry processing
CREATE INDEX CONCURRENTLY idx_credit_grant_expiry_processing
ON credit_grant_expiry (status, expiry_date, tenant_id)
WHERE status IN ('PENDING', 'ACTIVE');

-- Query for expired credits
SELECT cge.*, cg.remaining_amount
FROM credit_grant_expiry cge
JOIN credit_grant cg ON cge.credit_grant_id = cg.id
WHERE cge.status = 'ACTIVE'
  AND cge.expiry_date <= $1
  AND cg.status = 'PUBLISHED'
  AND cg.remaining_amount > 0
ORDER BY cge.expiry_date ASC
LIMIT $2 OFFSET $3;
```

### Integration Points

#### 1. Credit Grant Creation

```go
func (s *creditGrantService) CreateCreditGrant(ctx context.Context, req *CreateCreditGrantRequest) (*CreditGrant, error) {
    // Create the grant
    grant := req.ToCreditGrant(ctx)

    // Calculate initial expiry if configured
    if grant.ExpiryConfig != nil {
        expiry, err := s.calculateInitialExpiry(ctx, grant)
        if err != nil {
            return nil, err
        }

        if expiry != nil {
            grant.ExpiryTracker = expiry
        }
    }

    // Save grant and expiry
    return s.repo.CreateWithExpiry(ctx, grant)
}
```

#### 2. Subscription Changes

```go
func (s *subscriptionService) UpdateBillingPeriodsWithExpiryRecalculation(ctx context.Context) error {
    // Existing billing period update logic...

    // Trigger expiry recalculation for affected credit grants
    affectedGrants, err := s.creditGrantRepo.ListBySubscriptionID(ctx, subscription.ID)
    if err != nil {
        return err
    }

    for _, grant := range affectedGrants {
        if grant.ExpiryConfig != nil && s.needsRecalculation(grant.ExpiryConfig, SubscriptionChangePeriodTransition) {
            if err := s.expiryService.RecalculateExpiry(ctx, grant, subscription); err != nil {
                s.logger.Warnw("failed to recalculate expiry", "grant_id", grant.ID, "error", err)
            }
        }
    }

    return nil
}
```

### Database Schema Changes

#### Credit Grant Table Updates

```sql
-- Add new expiry configuration columns
ALTER TABLE credit_grant
ADD COLUMN expiry_config JSONB,
ADD COLUMN expiry_tracker_id VARCHAR(50);

-- Create index for expiry config queries
CREATE INDEX idx_credit_grant_expiry_config
ON credit_grant USING gin(expiry_config)
WHERE expiry_config IS NOT NULL;
```

#### New Expiry Tracking Table

```sql
CREATE TABLE credit_grant_expiry (
    id VARCHAR(50) PRIMARY KEY,
    tenant_id VARCHAR(50) NOT NULL,
    credit_grant_id VARCHAR(50) NOT NULL REFERENCES credit_grant(id),
    expiry_date TIMESTAMP WITH TIME ZONE NOT NULL,
    calculated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'PENDING',
    amount_to_expire DECIMAL(20,6) NOT NULL,
    remaining_amount DECIMAL(20,6) NOT NULL,
    anchor_date TIMESTAMP WITH TIME ZONE NOT NULL,
    grace_period_end TIMESTAMP WITH TIME ZONE,
    processed_at TIMESTAMP WITH TIME ZONE,
    processed_by VARCHAR(50),
    events JSONB,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_by VARCHAR(50),
    updated_by VARCHAR(50)
);

CREATE INDEX idx_credit_grant_expiry_status_date
ON credit_grant_expiry (status, expiry_date, tenant_id);

CREATE INDEX idx_credit_grant_expiry_grant_id
ON credit_grant_expiry (credit_grant_id);
```

### Backward Compatibility

The existing `expire_in_days` field will be automatically converted to the new flexible format:

```go
func MigrateExistingExpiryConfig(grant *CreditGrant) {
    if grant.ExpireInDays != nil && grant.ExpiryConfig == nil {
        grant.ExpiryConfig = &CreditGrantExpiryConfig{
            Type:   CreditGrantExpiryTypeDuration,
            Anchor: CreditGrantExpiryAnchorGrantCreated,
            Duration: &CreditGrantExpiryDuration{
                Amount: *grant.ExpireInDays,
                Unit:   CreditGrantExpiryDurationUnitDays,
            },
        }
    }
}
```

## Implementation Plan

### Phase 1: Foundation (Week 1-2)

- [ ] Define new type structures and enums
- [ ] Create database migrations
- [ ] Implement basic expiry calculation logic
- [ ] Add expiry config to credit grant creation API

### Phase 2: Core Processing (Week 3-4)

- [ ] Implement cron-based expiry processor
- [ ] Add expiry tracking repository layer
- [ ] Implement event publishing for expiry events
- [ ] Add monitoring and alerting

### Phase 3: Integration (Week 5-6)

- [ ] Integrate with subscription lifecycle events
- [ ] Add expiry recalculation on subscription changes
- [ ] Implement subscription pause handling
- [ ] Add API endpoints for expiry management

### Phase 4: Migration & Testing (Week 7-8)

- [ ] Migrate existing credit grants
- [ ] Comprehensive testing including edge cases
- [ ] Performance testing with large datasets
- [ ] Documentation and API updates

## Monitoring & Alerting

### Key Metrics

- Credit expiry processing rate and latency
- Failed expiry calculations
- Subscription change impact on expiry recalculations
- Credit utilization vs expiry rates

### Alerts

- Expiry processing failures above threshold
- Large batch processing delays
- Unexpected expiry calculation errors
- Missing expiry configurations for active grants

## Risk Assessment

### High Risk

- **Data Migration**: Complex migration of existing grants could lead to incorrect expiry dates
- **Performance**: Large-scale batch processing might impact database performance

### Medium Risk

- **Calculation Accuracy**: Complex period calculations might have edge cases
- **Event Ordering**: Subscription changes and expiry calculations need proper ordering

### Low Risk

- **API Backward Compatibility**: Well-designed migration should maintain compatibility
- **Monitoring Gaps**: Comprehensive metrics should catch most issues

## Appendix

### Example Use Cases

#### 1. Monthly Subscription with Period-End Credit Expiry

A customer gets 100 credits at the start of each billing period that expire at the end of that period.

#### 2. Trial Credits with Grace Period

A customer gets 50 credits during trial that expire 5 days after trial ends, with a 24-hour grace period.

#### 3. Annual Commitment Credits

A customer gets 1000 credits that expire at their annual subscription renewal date.

#### 4. Usage-Based Expiry

A customer gets credits that expire 30 days after first usage, encouraging active engagement.
