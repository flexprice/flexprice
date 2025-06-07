# Credit Grant Billing Lifecycle Management - Product Requirements Document

## Problem Statement

The current credit grant system needs comprehensive lifecycle management to handle credit grants appropriately across all subscription states and billing scenarios. Current gaps include:

1. **Incomplete Billing Lifecycle Integration**: Credit grants are not properly applied or managed during subscription state transitions (paused, cancelled, expired, trial, etc.)
2. **Unclear Timing Logic**: No clear specification for when recurring credits should be applied relative to billing periods
3. **State Transition Handling**: No defined behavior for credit grants when subscriptions change states
4. **Billing Period Alignment**: Recurring credit grants are not aligned with subscription billing cycles
5. **Credit Grant Recovery**: No mechanism to handle missed credit applications due to system downtime or subscription state changes

Without proper lifecycle management, customers may:

- Receive inappropriate credits during paused subscriptions
- Miss credits they're entitled to during state transitions
- Experience inconsistent credit application timing
- Face billing disputes due to unclear credit grant behavior

## Current Architecture

### Existing Credit Grant Structure

```go
type CreateCreditGrantRequest struct {
    Name           string                   `json:"name" binding:"required"`
    Scope          types.CreditGrantScope   `json:"scope" binding:"required"`
    PlanID         *string                  `json:"plan_id,omitempty"`
    SubscriptionID *string                  `json:"subscription_id,omitempty"`
    Amount         decimal.Decimal          `json:"amount" binding:"required"`
    Currency       string                   `json:"currency" binding:"required"`
    Cadence        types.CreditGrantCadence `json:"cadence" binding:"required"`
    Period         *types.CreditGrantPeriod `json:"period,omitempty"`
    PeriodCount    *int                     `json:"period_count,omitempty"`
    ExpireInDays   *int                     `json:"expire_in_days,omitempty"`
    Priority       *int                     `json:"priority,omitempty"`
    Metadata       types.Metadata           `json:"metadata,omitempty"`
}
```

### Current Limitations

1. No billing period alignment logic
2. No subscription state awareness
3. No credit grant application tracking
4. No retry or recovery mechanisms
5. No support for prorated credit adjustments

## Proposed Solution

### Core Enhancement Areas

1. **Billing Period Alignment**: Align recurring credit grants with subscription billing cycles
2. **State-Aware Processing**: Handle credit grants appropriately for each subscription state
3. **Application Tracking**: Track when and how credit grants are applied
4. **Recovery Mechanisms**: Handle missed applications and state transition scenarios
5. **Prorated Adjustments**: Support partial credit grants for mid-cycle changes

### Enhanced Database Schema

#### 1. Credit Grant Application Tracking

```sql
CREATE TABLE credit_grant_applications (
    id VARCHAR(50) PRIMARY KEY,
    credit_grant_id VARCHAR(50) NOT NULL REFERENCES credit_grants(id),
    subscription_id VARCHAR(50) NOT NULL REFERENCES subscriptions(id),

    -- Application timing
    scheduled_at TIMESTAMP WITH TIME ZONE NOT NULL,
    applied_at TIMESTAMP WITH TIME ZONE,

    -- Billing period context
    billing_period_start TIMESTAMP WITH TIME ZONE NOT NULL,
    billing_period_end TIMESTAMP WITH TIME ZONE NOT NULL,

    -- Application details
    status VARCHAR(50) NOT NULL DEFAULT 'scheduled', -- scheduled, applied, failed, skipped, cancelled
    amount_applied DECIMAL(19, 4) NOT NULL,
    currency VARCHAR(3) NOT NULL,

    -- Context and reason
    application_reason VARCHAR(100) NOT NULL, -- billing_cycle, subscription_created, manual_trigger, recovery
    subscription_status_at_application VARCHAR(50),

    -- Prorating information
    is_prorated BOOLEAN DEFAULT FALSE,
    proration_factor DECIMAL(5, 4), -- e.g., 0.5000 for half period
    full_period_amount DECIMAL(19, 4),

    -- Retry and failure handling
    retry_count INTEGER DEFAULT 0,
    failure_reason TEXT,
    next_retry_at TIMESTAMP WITH TIME ZONE,

    -- Metadata
    metadata JSONB,

    -- Standard fields
    tenant_id VARCHAR(50) NOT NULL,
    environment_id VARCHAR(50),
    created_by VARCHAR(255) NOT NULL,
    updated_by VARCHAR(255) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_credit_grant_applications_credit_grant_id ON credit_grant_applications(credit_grant_id);
CREATE INDEX idx_credit_grant_applications_subscription_id ON credit_grant_applications(subscription_id);
CREATE INDEX idx_credit_grant_applications_scheduled_at ON credit_grant_applications(scheduled_at);
CREATE INDEX idx_credit_grant_applications_status ON credit_grant_applications(status);
CREATE INDEX idx_credit_grant_applications_tenant_id ON credit_grant_applications(tenant_id);
```

#### 2. Enhanced Credit Grant Schema

```sql
-- Add additional fields to existing credit_grants table
ALTER TABLE credit_grants ADD COLUMN billing_anchor VARCHAR(50) DEFAULT 'period_start'; -- period_start, period_end, subscription_created
ALTER TABLE credit_grants ADD COLUMN proration_mode VARCHAR(50) DEFAULT 'none'; -- none, daily, full_period_only
ALTER TABLE credit_grants ADD COLUMN state_handling JSONB DEFAULT '{}'; -- JSON configuration for state-specific behavior
ALTER TABLE credit_grants ADD COLUMN last_application_at TIMESTAMP WITH TIME ZONE;
ALTER TABLE credit_grants ADD COLUMN next_application_at TIMESTAMP WITH TIME ZONE;
ALTER TABLE credit_grants ADD COLUMN application_status VARCHAR(50) DEFAULT 'active'; -- active, paused, cancelled
```

### Enhanced Golang Structures

#### 1. Credit Grant Application Entity

```go
type CreditGrantApplication struct {
    ID           string    `db:"id" json:"id"`
    CreditGrantID string   `db:"credit_grant_id" json:"credit_grant_id"`
    SubscriptionID string  `db:"subscription_id" json:"subscription_id"`

    // Timing
    ScheduledAt  time.Time  `db:"scheduled_at" json:"scheduled_at"`
    AppliedAt    *time.Time `db:"applied_at" json:"applied_at,omitempty"`

    // Billing period context
    BillingPeriodStart time.Time `db:"billing_period_start" json:"billing_period_start"`
    BillingPeriodEnd   time.Time `db:"billing_period_end" json:"billing_period_end"`

    // Application details
    Status        ApplicationStatus `db:"status" json:"status"`
    AmountApplied decimal.Decimal   `db:"amount_applied" json:"amount_applied"`
    Currency      string           `db:"currency" json:"currency"`

    // Context
    ApplicationReason           string  `db:"application_reason" json:"application_reason"`
    SubscriptionStatusAtApplication string `db:"subscription_status_at_application" json:"subscription_status_at_application"`

    // Prorating
    IsProrated         bool             `db:"is_prorated" json:"is_prorated"`
    ProrationFactor    *decimal.Decimal `db:"proration_factor" json:"proration_factor,omitempty"`
    FullPeriodAmount   *decimal.Decimal `db:"full_period_amount" json:"full_period_amount,omitempty"`

    // Retry handling
    RetryCount     int        `db:"retry_count" json:"retry_count"`
    FailureReason  *string    `db:"failure_reason" json:"failure_reason,omitempty"`
    NextRetryAt    *time.Time `db:"next_retry_at" json:"next_retry_at,omitempty"`

    Metadata   types.Metadata `db:"metadata" json:"metadata,omitempty"`
    BaseModel                 // Standard fields
}

type ApplicationStatus string

const (
    ApplicationStatusScheduled ApplicationStatus = "scheduled"
    ApplicationStatusApplied   ApplicationStatus = "applied"
    ApplicationStatusFailed    ApplicationStatus = "failed"
    ApplicationStatusSkipped   ApplicationStatus = "skipped"
    ApplicationStatusCancelled ApplicationStatus = "cancelled"
)
```

#### 2. Enhanced Credit Grant Configuration

```go
type CreditGrantBillingConfig struct {
    BillingAnchor  BillingAnchor  `json:"billing_anchor"`
    ProrationMode  ProrationMode  `json:"proration_mode"`
    StateHandling  StateHandling  `json:"state_handling"`
}

type BillingAnchor string

const (
    BillingAnchorPeriodStart       BillingAnchor = "period_start"
    BillingAnchorPeriodEnd         BillingAnchor = "period_end"
    BillingAnchorSubscriptionCreated BillingAnchor = "subscription_created"
)

type ProrationMode string

const (
    ProrationModeNone           ProrationMode = "none"
    ProrationModeDaily          ProrationMode = "daily"
    ProrationModeFullPeriodOnly ProrationMode = "full_period_only"
)

type StateHandling struct {
    Trialing          StateAction `json:"trialing"`
    Active            StateAction `json:"active"`
    PastDue           StateAction `json:"past_due"`
    Cancelled         StateAction `json:"cancelled"`
    Unpaid            StateAction `json:"unpaid"`
    Incomplete        StateAction `json:"incomplete"`
    IncompleteExpired StateAction `json:"incomplete_expired"`
    Paused            StateAction `json:"paused"`
}

type StateAction string

const (
    StateActionApply     StateAction = "apply"      // Apply credit grant
    StateActionSkip      StateAction = "skip"       // Skip this cycle
    StateActionPause     StateAction = "pause"      // Pause until state changes
    StateActionCancel    StateAction = "cancel"     // Cancel future applications
    StateActionDefer     StateAction = "defer"      // Defer until next valid state
)
```

### Subscription State Handling Logic

#### State-Specific Behaviors

| Subscription State   | One-Time Grant Behavior | Recurring Grant Behavior  | Rationale                     |
| -------------------- | ----------------------- | ------------------------- | ----------------------------- |
| `active`             | ✅ Apply immediately    | ✅ Apply on billing cycle | Normal operation              |
| `trialing`           | ✅ Apply (configurable) | ✅ Apply (configurable)   | Support usage during trial    |
| `past_due`           | ⏸️ Defer until active   | ⏸️ Pause until active     | Avoid rewarding non-payment   |
| `unpaid`             | ⏸️ Defer until active   | ⏸️ Pause until active     | Same as past_due              |
| `cancelled`          | ❌ Cancel               | ❌ Cancel all future      | No benefit for cancelled subs |
| `incomplete`         | ⏸️ Defer until active   | ⏸️ Defer until active     | Wait for payment setup        |
| `incomplete_expired` | ❌ Cancel               | ❌ Cancel                 | Treat as dead subscription    |
| `paused`             | ⏸️ Defer until resumed  | ⏸️ Pause until resumed    | No billing during pause       |

### Billing Period Alignment

#### 1. Timing Calculation Logic

```go
type BillingAlignmentCalculator struct {
    subscription *subscription.Subscription
    grant        *creditgrant.CreditGrant
}

func (calc *BillingAlignmentCalculator) CalculateNextApplication() (*time.Time, error) {
    switch calc.grant.BillingAnchor {
    case BillingAnchorPeriodStart:
        return calc.subscription.CurrentPeriodStart, nil

    case BillingAnchorPeriodEnd:
        return calc.subscription.CurrentPeriodEnd, nil

    case BillingAnchorSubscriptionCreated:
        return calc.calculateFromSubscriptionCreated()

    default:
        return nil, errors.New("invalid billing anchor")
    }
}

func (calc *BillingAlignmentCalculator) calculateFromSubscriptionCreated() (*time.Time, error) {
    // Calculate based on subscription created date plus period intervals
    createdAt := calc.subscription.CreatedAt

    switch calc.grant.Period {
    case types.CreditGrantPeriodMonthly:
        return calc.addMonthsFromDate(createdAt, calc.getCurrentCycleNumber())
    case types.CreditGrantPeriodYearly:
        return calc.addYearsFromDate(createdAt, calc.getCurrentCycleNumber())
    default:
        return nil, errors.New("unsupported period for subscription anchor")
    }
}
```

#### 2. Proration Logic

```go
type ProrationCalculator struct {
    grant        *creditgrant.CreditGrant
    subscription *subscription.Subscription
    applicationDate time.Time
}

func (calc *ProrationCalculator) CalculateProrationFactor() (*decimal.Decimal, error) {
    if calc.grant.ProrationMode == ProrationModeNone {
        return nil, nil // No proration
    }

    if calc.grant.ProrationMode == ProrationModeFullPeriodOnly {
        // Only apply if at the start of a billing period
        if calc.isAtPeriodStart() {
            return nil, nil // Full amount
        }
        return decimal.Zero.Ptr(), nil // Skip application
    }

    if calc.grant.ProrationMode == ProrationModeDaily {
        return calc.calculateDailyProration()
    }

    return nil, errors.New("invalid proration mode")
}

func (calc *ProrationCalculator) calculateDailyProration() (*decimal.Decimal, error) {
    periodStart := calc.subscription.CurrentPeriodStart
    periodEnd := calc.subscription.CurrentPeriodEnd

    totalDays := periodEnd.Sub(periodStart).Hours() / 24
    remainingDays := periodEnd.Sub(calc.applicationDate).Hours() / 24

    if remainingDays <= 0 {
        return decimal.Zero.Ptr(), nil
    }

    factor := decimal.NewFromFloat(remainingDays / totalDays)
    return &factor, nil
}
```

### Credit Grant Scheduler Service

#### 1. Scheduling Logic

```go
type CreditGrantScheduler struct {
    repo             *CreditGrantRepository
    subscriptionRepo *SubscriptionRepository
    applicationRepo  *CreditGrantApplicationRepository
    walletService    *WalletService
}

func (s *CreditGrantScheduler) ScheduleRecurringGrants(ctx context.Context) error {
    // Get all active recurring grants
    grants, err := s.repo.FindActiveRecurringGrants(ctx)
    if err != nil {
        return err
    }

    for _, grant := range grants {
        if err := s.scheduleGrantApplications(ctx, grant); err != nil {
            // Log error but continue with other grants
            log.WithError(err).
                WithField("grant_id", grant.ID).
                Error("Failed to schedule grant applications")
        }
    }

    return nil
}

func (s *CreditGrantScheduler) scheduleGrantApplications(ctx context.Context, grant *creditgrant.CreditGrant) error {
    subscriptions, err := s.getEligibleSubscriptions(ctx, grant)
    if err != nil {
        return err
    }

    for _, sub := range subscriptions {
        calculator := &BillingAlignmentCalculator{
            subscription: sub,
            grant:        grant,
        }

        nextApplication, err := calculator.CalculateNextApplication()
        if err != nil {
            continue
        }

        // Check if application already scheduled
        exists, err := s.applicationRepo.ExistsForPeriod(ctx, grant.ID, sub.ID, *nextApplication)
        if err != nil || exists {
            continue
        }

        application := &CreditGrantApplication{
            ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CREDIT_GRANT_APPLICATION),
            CreditGrantID:      grant.ID,
            SubscriptionID:     sub.ID,
            ScheduledAt:        *nextApplication,
            BillingPeriodStart: sub.CurrentPeriodStart,
            BillingPeriodEnd:   sub.CurrentPeriodEnd,
            Status:             ApplicationStatusScheduled,
            AmountApplied:      grant.Amount,
            Currency:           grant.Currency,
            ApplicationReason:  "billing_cycle",
            BaseModel:          types.GetDefaultBaseModel(ctx),
        }

        if err := s.applicationRepo.Create(ctx, application); err != nil {
            log.WithError(err).
                WithField("grant_id", grant.ID).
                WithField("subscription_id", sub.ID).
                Error("Failed to create scheduled application")
        }
    }

    return nil
}
```

#### 2. Application Processor

```go
type CreditGrantProcessor struct {
    applicationRepo *CreditGrantApplicationRepository
    grantRepo       *CreditGrantRepository
    subscriptionRepo *SubscriptionRepository
    walletService   *WalletService
}

func (p *CreditGrantProcessor) ProcessScheduledApplications(ctx context.Context) error {
    // Get applications due for processing
    applications, err := p.applicationRepo.FindDueApplications(ctx, time.Now())
    if err != nil {
        return err
    }

    for _, app := range applications {
        if err := p.processApplication(ctx, app); err != nil {
            p.handleApplicationFailure(ctx, app, err)
        }
    }

    return nil
}

func (p *CreditGrantProcessor) processApplication(ctx context.Context, app *CreditGrantApplication) error {
    // Get current subscription state
    subscription, err := p.subscriptionRepo.FindByID(ctx, app.SubscriptionID)
    if err != nil {
        return err
    }

    // Get grant configuration
    grant, err := p.grantRepo.FindByID(ctx, app.CreditGrantID)
    if err != nil {
        return err
    }

    // Check if subscription state allows application
    stateHandler := &SubscriptionStateHandler{
        subscription: subscription,
        grant:        grant,
        application:  app,
    }

    action, err := stateHandler.DetermineAction()
    if err != nil {
        return err
    }

    switch action {
    case StateActionApply:
        return p.applyCreditGrant(ctx, app, subscription, grant)
    case StateActionSkip:
        return p.skipApplication(ctx, app, "subscription_state_skip")
    case StateActionPause:
        return p.pauseApplication(ctx, app)
    case StateActionCancel:
        return p.cancelApplication(ctx, app)
    case StateActionDefer:
        return p.deferApplication(ctx, app)
    }

    return nil
}

func (p *CreditGrantProcessor) applyCreditGrant(ctx context.Context, app *CreditGrantApplication, sub *subscription.Subscription, grant *creditgrant.CreditGrant) error {
    // Calculate final amount (with proration if needed)
    calculator := &ProrationCalculator{
        grant:           grant,
        subscription:    sub,
        applicationDate: app.ScheduledAt,
    }

    prorationFactor, err := calculator.CalculateProrationFactor()
    if err != nil {
        return err
    }

    finalAmount := app.AmountApplied
    if prorationFactor != nil {
        if prorationFactor.IsZero() {
            return p.skipApplication(ctx, app, "proration_skip")
        }
        finalAmount = finalAmount.Mul(*prorationFactor)
        app.IsProrated = true
        app.ProrationFactor = prorationFactor
        app.FullPeriodAmount = &app.AmountApplied
    }

    // Apply credit to wallet
    creditRequest := &wallet.AddCreditRequest{
        SubscriptionID: sub.ID,
        Amount:         finalAmount,
        Currency:       app.Currency,
        Source:         "credit_grant",
        SourceID:       app.ID,
        ExpireInDays:   grant.ExpireInDays,
        Priority:       grant.Priority,
        Metadata: map[string]string{
            "grant_id":       grant.ID,
            "application_id": app.ID,
            "billing_period": fmt.Sprintf("%s_%s", app.BillingPeriodStart.Format("2006-01-02"), app.BillingPeriodEnd.Format("2006-01-02")),
        },
    }

    if err := p.walletService.AddCredit(ctx, creditRequest); err != nil {
        return err
    }

    // Update application status
    now := time.Now()
    app.Status = ApplicationStatusApplied
    app.AppliedAt = &now
    app.AmountApplied = finalAmount
    app.SubscriptionStatusAtApplication = string(sub.Status)

    return p.applicationRepo.Update(ctx, app)
}
```

### Subscription State Transition Handlers

#### 1. State Change Event Handler

```go
type SubscriptionStateChangeHandler struct {
    grantRepo       *CreditGrantRepository
    applicationRepo *CreditGrantApplicationRepository
    processor       *CreditGrantProcessor
}

func (h *SubscriptionStateChangeHandler) HandleStateChange(ctx context.Context, event *subscription.StateChangeEvent) error {
    switch {
    case event.From.IsInactive() && event.To.IsActive():
        return h.handleActivation(ctx, event.SubscriptionID)

    case event.From.IsActive() && event.To.IsInactive():
        return h.handleDeactivation(ctx, event.SubscriptionID)

    case event.To == subscription.StatusCancelled:
        return h.handleCancellation(ctx, event.SubscriptionID)

    case event.To == subscription.StatusPaused:
        return h.handlePause(ctx, event.SubscriptionID)

    case event.From == subscription.StatusPaused && event.To.IsActive():
        return h.handleResume(ctx, event.SubscriptionID)
    }

    return nil
}

func (h *SubscriptionStateChangeHandler) handleActivation(ctx context.Context, subscriptionID string) error {
    // Process deferred applications
    deferredApps, err := h.applicationRepo.FindDeferredApplications(ctx, subscriptionID)
    if err != nil {
        return err
    }

    for _, app := range deferredApps {
        if err := h.processor.processApplication(ctx, app); err != nil {
            log.WithError(err).
                WithField("application_id", app.ID).
                Error("Failed to process deferred application")
        }
    }

    return nil
}

func (h *SubscriptionStateChangeHandler) handleCancellation(ctx context.Context, subscriptionID string) error {
    // Cancel all future scheduled applications
    return h.applicationRepo.CancelFutureApplications(ctx, subscriptionID)
}

func (h *SubscriptionStateChangeHandler) handlePause(ctx context.Context, subscriptionID string) error {
    // Pause scheduled applications during pause period
    return h.applicationRepo.PauseScheduledApplications(ctx, subscriptionID)
}

func (h *SubscriptionStateChangeHandler) handleResume(ctx context.Context, subscriptionID string) error {
    // Resume paused applications and catch up if needed

    // 1. Resume paused applications
    if err := h.applicationRepo.ResumeScheduledApplications(ctx, subscriptionID); err != nil {
        return err
    }

    // 2. Check if we missed any applications during pause
    subscription, err := h.subscriptionRepo.FindByID(ctx, subscriptionID)
    if err != nil {
        return err
    }

    // Get active grants for this subscription
    grants, err := h.grantRepo.FindActiveGrantsForSubscription(ctx, subscriptionID)
    if err != nil {
        return err
    }

    // Schedule any missed recurring applications
    for _, grant := range grants {
        if grant.Cadence == types.CreditGrantCadenceRecurring {
            scheduler := &CreditGrantScheduler{
                repo:             h.grantRepo,
                subscriptionRepo: h.subscriptionRepo,
                applicationRepo:  h.applicationRepo,
            }

            if err := scheduler.scheduleGrantApplications(ctx, grant); err != nil {
                log.WithError(err).
                    WithField("grant_id", grant.ID).
                    Error("Failed to schedule missed applications")
            }
        }
    }

    return nil
}
```

### API Enhancements

#### 1. Credit Grant Application Endpoints

```go
// GET /v1/credit-grants/{grantId}/applications
func (h *CreditGrantHandler) ListApplications(c *gin.Context) {
    grantID := c.Param("grantId")

    req := &ListApplicationsRequest{}
    if err := c.ShouldBindQuery(req); err != nil {
        c.JSON(400, gin.H{"error": err.Error()})
        return
    }

    applications, err := h.applicationRepo.FindByGrantID(c.Request.Context(), grantID, req.ToFilter())
    if err != nil {
        c.JSON(500, gin.H{"error": err.Error()})
        return
    }

    c.JSON(200, gin.H{"applications": applications})
}

// POST /v1/credit-grants/{grantId}/applications/{applicationId}/retry
func (h *CreditGrantHandler) RetryApplication(c *gin.Context) {
    applicationID := c.Param("applicationId")

    application, err := h.applicationRepo.FindByID(c.Request.Context(), applicationID)
    if err != nil {
        c.JSON(404, gin.H{"error": "Application not found"})
        return
    }

    if application.Status != ApplicationStatusFailed {
        c.JSON(400, gin.H{"error": "Can only retry failed applications"})
        return
    }

    if err := h.processor.processApplication(c.Request.Context(), application); err != nil {
        c.JSON(500, gin.H{"error": err.Error()})
        return
    }

    c.JSON(200, gin.H{"message": "Application retry initiated"})
}

// GET /v1/subscriptions/{subscriptionId}/credit-grant-applications
func (h *SubscriptionHandler) ListCreditGrantApplications(c *gin.Context) {
    subscriptionID := c.Param("subscriptionId")

    applications, err := h.applicationRepo.FindBySubscriptionID(c.Request.Context(), subscriptionID)
    if err != nil {
        c.JSON(500, gin.H{"error": err.Error()})
        return
    }

    c.JSON(200, gin.H{"applications": applications})
}
```

#### 2. Enhanced Request/Response Types

```go
type ListApplicationsRequest struct {
    Status    *ApplicationStatus `form:"status"`
    StartDate *time.Time         `form:"start_date"`
    EndDate   *time.Time         `form:"end_date"`
    Limit     int                `form:"limit,default=50"`
    Offset    int                `form:"offset,default=0"`
}

type CreditGrantApplicationResponse struct {
    ID           string    `json:"id"`
    CreditGrantID string   `json:"credit_grant_id"`
    SubscriptionID string  `json:"subscription_id"`

    ScheduledAt  time.Time  `json:"scheduled_at"`
    AppliedAt    *time.Time `json:"applied_at,omitempty"`

    BillingPeriod BillingPeriodInfo `json:"billing_period"`

    Status        ApplicationStatus `json:"status"`
    AmountApplied decimal.Decimal   `json:"amount_applied"`
    Currency      string           `json:"currency"`

    ApplicationContext ApplicationContext `json:"application_context"`
    ProrationInfo      *ProrationInfo     `json:"proration_info,omitempty"`
    RetryInfo          *RetryInfo         `json:"retry_info,omitempty"`

    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
}

type BillingPeriodInfo struct {
    Start time.Time `json:"start"`
    End   time.Time `json:"end"`
}

type ApplicationContext struct {
    Reason                         string `json:"reason"`
    SubscriptionStatusAtApplication string `json:"subscription_status_at_application"`
}

type ProrationInfo struct {
    IsProrated       bool            `json:"is_prorated"`
    ProrationFactor  decimal.Decimal `json:"proration_factor"`
    FullPeriodAmount decimal.Decimal `json:"full_period_amount"`
}

type RetryInfo struct {
    RetryCount    int        `json:"retry_count"`
    FailureReason *string    `json:"failure_reason,omitempty"`
    NextRetryAt   *time.Time `json:"next_retry_at,omitempty"`
}
```

## Implementation Phases

### Phase 1: Foundation (Week 1-2)

- [ ] Create credit grant application entity and repository
- [ ] Implement billing alignment calculator
- [ ] Create basic scheduler service
- [ ] Add subscription state handlers

### Phase 2: Core Processing (Week 3-4)

- [ ] Implement application processor with state logic
- [ ] Add proration calculation logic
- [ ] Create retry and failure handling mechanisms
- [ ] Implement state transition handlers

### Phase 3: API and Integration (Week 5)

- [ ] Build enhanced API endpoints
- [ ] Add monitoring and alerting
- [ ] Implement recovery mechanisms
- [ ] Create admin tools for manual intervention

### Phase 4: Testing and Optimization (Week 6)

- [ ] Comprehensive testing of all state transitions
- [ ] Load testing of scheduler and processor
- [ ] Performance optimization
- [ ] Documentation and runbooks

## Operational Considerations

### 1. Monitoring and Alerting

```go
// Key metrics to track
type CreditGrantMetrics struct {
    ApplicationsScheduled    prometheus.Counter
    ApplicationsProcessed    prometheus.Counter
    ApplicationsFailed       prometheus.Counter
    ApplicationsSkipped      prometheus.Counter
    ProcessingLatency        prometheus.Histogram
    ProrationCalculationTime prometheus.Histogram
}

// Alerts to configure
// - Failed application rate > 5%
// - Processing latency > 30 seconds
// - No applications processed in last hour
// - High retry queue depth
```

### 2. Recovery Mechanisms

- **Missed Applications**: Daily job to detect and schedule missed applications
- **Failed Applications**: Exponential backoff retry with max 5 attempts
- **State Inconsistencies**: Reconciliation job to fix application/subscription mismatches
- **Emergency Override**: Admin tools to manually trigger/cancel applications

### 3. Performance Optimization

- **Batch Processing**: Process applications in batches to reduce database load
- **Database Indexing**: Proper indexes on scheduling and lookup queries
- **Caching**: Cache frequently accessed grant configurations
- **Async Processing**: Use background jobs for non-critical operations

## Testing Strategy

### 1. Unit Tests

- Billing alignment calculations
- Proration logic
- State transition handlers
- Application processors

### 2. Integration Tests

- End-to-end credit grant application flow
- Subscription state change scenarios
- Recovery and retry mechanisms
- API endpoint functionality

### 3. Load Tests

- Scheduler performance with 10k+ subscriptions
- Processor throughput under load
- Database performance with large application history

### 4. Edge Case Tests

- Rapid subscription state changes
- Timezone edge cases in billing periods
- Concurrent application processing
- System downtime recovery scenarios

## Security Considerations

1. **Authorization**: Ensure only authorized users can trigger manual applications
2. **Audit Trail**: Complete audit log of all credit grant applications
3. **Data Privacy**: Respect customer data retention policies for application history
4. **Rate Limiting**: Prevent abuse of manual retry endpoints
5. **Validation**: Strict validation of all inputs to prevent injection attacks

## Documentation Requirements

1. **API Documentation**: Complete OpenAPI specification
2. **Operational Runbooks**: Step-by-step troubleshooting guides
3. **Developer Guide**: Integration patterns and best practices
4. **Admin Guide**: Manual intervention procedures
5. **Architecture Decision Records**: Document key design decisions

This comprehensive PRD ensures robust credit grant lifecycle management across all subscription states while maintaining billing accuracy and customer satisfaction.
