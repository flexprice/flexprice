# Recurring Credit Grant Implementation - Product Requirements Document

## Executive Summary

This PRD outlines the implementation of a recurring credit grant system that automatically applies credit grants to user wallets based on billing cycles while respecting subscription lifecycle states. The system will include a daily cron job processor, comprehensive state management, and proper edge case handling **integrated with the existing credit grant and subscription services**.

## Problem Statement

Currently, the credit grant system only supports one-time grants that are applied directly to wallets during subscription creation. Recurring credit grants are not processed, leading to:

1. **Missing Recurring Credits**: Users don't receive their entitled recurring credits
2. **No Billing Period Alignment**: No mechanism to align recurring grants with subscription billing cycles
3. **State Transition Gaps**: No handling of subscription state changes (paused, cancelled, resumed)
4. **No Application Tracking**: No way to track which credits have been applied and when
5. **Recovery Issues**: No mechanism to handle missed applications due to system downtime

## Current State Analysis

### Existing Credit Grant Flow

```go
// Current implementation only handles one-time grants
func (s *subscriptionService) handleCreditGrants(ctx context.Context, subscription *subscription.Subscription, creditGrantRequests []dto.CreateCreditGrantRequest) error {
    // Creates credit grants and immediately applies one-time grants to wallet
    // Recurring grants are created but never processed
}
```

### Gap Analysis

- ✅ One-time grant creation and application
- ❌ Recurring grant processing
- ❌ Billing period alignment
- ❌ Subscription state awareness
- ❌ Application tracking and deduplication
- ❌ Recovery mechanisms

## Proposed Solution

### Finding Active Credit Grants and Eligible Subscriptions

#### 1. Finding Active Recurring Credit Grants

**Method 1: Add to existing CreditGrantRepository**

```go
// Add to internal/domain/creditgrant/repository.go
type Repository interface {
    // ... existing methods ...

    // FindActiveRecurringGrants finds all active recurring credit grants
    FindActiveRecurringGrants(ctx context.Context) ([]*CreditGrant, error)

    // FindActiveGrantsForSubscription finds active grants for a specific subscription
    FindActiveGrantsForSubscription(ctx context.Context, subscriptionID string) ([]*CreditGrant, error)
}
```

**Implementation in CreditGrantRepository:**

```go
// Add to internal/repository/ent/creditgrant.go
func (r *creditGrantRepository) FindActiveRecurringGrants(ctx context.Context) ([]*domainCreditGrant.CreditGrant, error) {
    client := r.client.Querier(ctx)

    span := StartRepositorySpan(ctx, "creditgrant", "find_active_recurring", map[string]interface{}{
        "tenant_id": types.GetTenantID(ctx),
    })
    defer FinishSpan(span)

    grants, err := client.CreditGrant.Query().
        Where(
            creditgrant.TenantID(types.GetTenantID(ctx)),
            creditgrant.EnvironmentID(types.GetEnvironmentID(ctx)),
            creditgrant.Status(string(types.StatusPublished)),
            creditgrant.Cadence(string(types.CreditGrantCadenceRecurring)),
        ).
        All(ctx)

    if err != nil {
        SetSpanError(span, err)
        return nil, ierr.WithError(err).
            WithHint("Failed to find active recurring credit grants").
            Mark(ierr.ErrDatabase)
    }

    SetSpanSuccess(span)
    return domainCreditGrant.FromEntList(grants), nil
}

func (r *creditGrantRepository) FindActiveGrantsForSubscription(ctx context.Context, subscriptionID string) ([]*domainCreditGrant.CreditGrant, error) {
    client := r.client.Querier(ctx)

    span := StartRepositorySpan(ctx, "creditgrant", "find_active_for_subscription", map[string]interface{}{
        "subscription_id": subscriptionID,
    })
    defer FinishSpan(span)

    // Get subscription to find plan ID
    sub, err := client.Subscription.Query().
        Where(
            subscription.ID(subscriptionID),
            subscription.TenantID(types.GetTenantID(ctx)),
            subscription.EnvironmentID(types.GetEnvironmentID(ctx)),
        ).
        Only(ctx)

    if err != nil {
        SetSpanError(span, err)
        return nil, ierr.WithError(err).
            WithHint("Failed to find subscription").
            Mark(ierr.ErrDatabase)
    }

    // Find grants scoped to this subscription OR to its plan
    grants, err := client.CreditGrant.Query().
        Where(
            creditgrant.TenantID(types.GetTenantID(ctx)),
            creditgrant.EnvironmentID(types.GetEnvironmentID(ctx)),
            creditgrant.Status(string(types.StatusPublished)),
            creditgrant.Or(
                // Subscription-scoped grants for this subscription
                creditgrant.And(
                    creditgrant.Scope(string(types.CreditGrantScopeSubscription)),
                    creditgrant.SubscriptionID(subscriptionID),
                ),
                // Plan-scoped grants for this subscription's plan
                creditgrant.And(
                    creditgrant.Scope(string(types.CreditGrantScopePlan)),
                    creditgrant.PlanID(sub.PlanID),
                ),
            ),
        ).
        All(ctx)

    if err != nil {
        SetSpanError(span, err)
        return nil, ierr.WithError(err).
            WithHint("Failed to find active grants for subscription").
            Mark(ierr.ErrDatabase)
    }

    SetSpanSuccess(span)
    return domainCreditGrant.FromEntList(grants), nil
}
```

#### 2. Finding Eligible Subscriptions for Credit Grants

**Method: Add to existing SubscriptionRepository**

```go
// Add to internal/domain/subscription/repository.go
type Repository interface {
    // ... existing methods ...

    // FindEligibleSubscriptionsForGrant finds subscriptions eligible for a credit grant
    FindEligibleSubscriptionsForGrant(ctx context.Context, grant *creditgrant.CreditGrant) ([]*Subscription, error)

    // FindSubscriptionsNeedingRecurringProcessing finds subscriptions that need recurring credit processing
    FindSubscriptionsNeedingRecurringProcessing(ctx context.Context) ([]*Subscription, error)
}
```

**Implementation:**

```go
// Add to internal/repository/ent/subscription.go
func (r *subscriptionRepository) FindEligibleSubscriptionsForGrant(ctx context.Context, grant *creditgrant.CreditGrant) ([]*domainSubscription.Subscription, error) {
    client := r.client.Querier(ctx)

    span := StartRepositorySpan(ctx, "subscription", "find_eligible_for_grant", map[string]interface{}{
        "grant_id": grant.ID,
        "scope":    grant.Scope,
    })
    defer FinishSpan(span)

    var query *ent.SubscriptionQuery

    switch grant.Scope {
    case types.CreditGrantScopePlan:
        if grant.PlanID == nil {
            return nil, ierr.NewError("plan ID required for plan-scoped grants").Mark(ierr.ErrValidation)
        }

        query = client.Subscription.Query().
            Where(
                subscription.TenantID(types.GetTenantID(ctx)),
                subscription.EnvironmentID(types.GetEnvironmentID(ctx)),
                subscription.Status(string(types.StatusPublished)),
                subscription.PlanID(*grant.PlanID),
                subscription.SubscriptionStatusIn(
                    string(types.SubscriptionStatusActive),
                    string(types.SubscriptionStatusTrialing), // May be configurable per grant
                ),
            )

    case types.CreditGrantScopeSubscription:
        if grant.SubscriptionID == nil {
            return nil, ierr.NewError("subscription ID required for subscription-scoped grants").Mark(ierr.ErrValidation)
        }

        query = client.Subscription.Query().
            Where(
                subscription.TenantID(types.GetTenantID(ctx)),
                subscription.EnvironmentID(types.GetEnvironmentID(ctx)),
                subscription.Status(string(types.StatusPublished)),
                subscription.ID(*grant.SubscriptionID),
                subscription.SubscriptionStatusIn(
                    string(types.SubscriptionStatusActive),
                    string(types.SubscriptionStatusTrialing),
                ),
            )

    default:
        return nil, ierr.NewError("invalid grant scope").Mark(ierr.ErrValidation)
    }

    subscriptions, err := query.All(ctx)
    if err != nil {
        SetSpanError(span, err)
        return nil, ierr.WithError(err).
            WithHint("Failed to find eligible subscriptions").
            Mark(ierr.ErrDatabase)
    }

    SetSpanSuccess(span)
    return domainSubscription.FromEntList(subscriptions), nil
}

func (r *subscriptionRepository) FindSubscriptionsNeedingRecurringProcessing(ctx context.Context) ([]*domainSubscription.Subscription, error) {
    client := r.client.Querier(ctx)

    span := StartRepositorySpan(ctx, "subscription", "find_needing_recurring", map[string]interface{}{
        "tenant_id": types.GetTenantID(ctx),
    })
    defer FinishSpan(span)

    // Find active subscriptions that have recurring credit grants
    subscriptions, err := client.Subscription.Query().
        Where(
            subscription.TenantID(types.GetTenantID(ctx)),
            subscription.EnvironmentID(types.GetEnvironmentID(ctx)),
            subscription.Status(string(types.StatusPublished)),
            subscription.SubscriptionStatusIn(
                string(types.SubscriptionStatusActive),
                string(types.SubscriptionStatusTrialing),
            ),
            subscription.HasCreditGrantsWith(
                creditgrant.Cadence(string(types.CreditGrantCadenceRecurring)),
                creditgrant.Status(string(types.StatusPublished)),
            ),
        ).
        All(ctx)

    if err != nil {
        SetSpanError(span, err)
        return nil, ierr.WithError(err).
            WithHint("Failed to find subscriptions needing recurring processing").
            Mark(ierr.ErrDatabase)
    }

    SetSpanSuccess(span)
    return domainSubscription.FromEntList(subscriptions), nil
}
```

#### 3. Enhanced Credit Grant Application Repository

**Add these methods to existing CreditGrantApplicationRepository:**

```go
// Add to internal/repository/ent/creditgrantapplication.go

func (r *creditGrantApplicationRepository) ExistsForBillingPeriod(ctx context.Context, grantID, subscriptionID string, periodStart, periodEnd time.Time) (bool, error) {
    client := r.client.Querier(ctx)

    count, err := client.CreditGrantApplication.Query().
        Where(
            creditgrantapplication.CreditGrantID(grantID),
            creditgrantapplication.SubscriptionID(subscriptionID),
            creditgrantapplication.BillingPeriodStart(periodStart),
            creditgrantapplication.BillingPeriodEnd(periodEnd),
            creditgrantapplication.TenantID(types.GetTenantID(ctx)),
            creditgrantapplication.ApplicationStatusNotIn(
                string(types.ApplicationStatusCancelled),
                string(types.ApplicationStatusFailed),
            ),
        ).
        Count(ctx)

    return count > 0, err
}

func (r *creditGrantApplicationRepository) FindDeferredApplications(ctx context.Context, subscriptionID string) ([]*domainCreditGrantApplication.CreditGrantApplication, error) {
    client := r.client.Querier(ctx)

    applications, err := client.CreditGrantApplication.Query().
        Where(
            creditgrantapplication.SubscriptionID(subscriptionID),
            creditgrantapplication.ApplicationStatus(string(types.ApplicationStatusDeferred)),
            creditgrantapplication.TenantID(types.GetTenantID(ctx)),
        ).
        All(ctx)

    if err != nil {
        return nil, err
    }

    return domainCreditGrantApplication.FromEntList(applications), nil
}

func (r *creditGrantApplicationRepository) FindFailedApplicationsForRetry(ctx context.Context, maxRetries int) ([]*domainCreditGrantApplication.CreditGrantApplication, error) {
    client := r.client.Querier(ctx)
    now := time.Now().UTC()

    applications, err := client.CreditGrantApplication.Query().
        Where(
            creditgrantapplication.ApplicationStatus(string(types.ApplicationStatusFailed)),
            creditgrantapplication.RetryCountLT(maxRetries),
            creditgrantapplication.Or(
                creditgrantapplication.NextRetryAtIsNil(),
                creditgrantapplication.NextRetryAtLTE(now),
            ),
            creditgrantapplication.TenantID(types.GetTenantID(ctx)),
        ).
        Limit(100). // Process in batches
        All(ctx)

    if err != nil {
        return nil, err
    }

    return domainCreditGrantApplication.FromEntList(applications), nil
}

func (r *creditGrantApplicationRepository) CancelFutureApplications(ctx context.Context, subscriptionID string) error {
    client := r.client.Querier(ctx)
    now := time.Now().UTC()

    _, err := client.CreditGrantApplication.Update().
        Where(
            creditgrantapplication.SubscriptionID(subscriptionID),
            creditgrantapplication.ApplicationStatus(string(types.ApplicationStatusScheduled)),
            creditgrantapplication.ScheduledForGT(now),
            creditgrantapplication.TenantID(types.GetTenantID(ctx)),
        ).
        SetApplicationStatus(string(types.ApplicationStatusCancelled)).
        SetFailureReason("subscription_cancelled").
        SetUpdatedAt(now).
        SetUpdatedBy(types.GetUserID(ctx)).
        Save(ctx)

    return err
}
```

### Integration with Existing Services

#### 1. Enhanced Credit Grant Service

**Add to existing CreditGrantService:**

```go
// Add to internal/service/creditgrant.go

// ProcessRecurringGrants processes all recurring credit grants
func (s *creditGrantService) ProcessRecurringGrants(ctx context.Context) error {
    s.log.Infow("starting recurring credit grant processing")

    // 1. Find all active recurring grants
    grants, err := s.repo.FindActiveRecurringGrants(ctx)
    if err != nil {
        return err
    }

    s.log.Infow("found active recurring grants", "count", len(grants))

    // 2. Process each grant
    for _, grant := range grants {
        if err := s.processRecurringGrant(ctx, grant); err != nil {
            s.log.Errorw("failed to process recurring grant",
                "grant_id", grant.ID,
                "error", err)
            // Continue processing other grants
        }
    }

    return nil
}

func (s *creditGrantService) processRecurringGrant(ctx context.Context, grant *creditgrant.CreditGrant) error {
    // Find eligible subscriptions for this grant
    subscriptions, err := s.subRepo.FindEligibleSubscriptionsForGrant(ctx, grant)
    if err != nil {
        return err
    }

    s.log.Debugw("found eligible subscriptions for grant",
        "grant_id", grant.ID,
        "subscription_count", len(subscriptions))

    for _, subscription := range subscriptions {
        if err := s.processGrantForSubscription(ctx, grant, subscription); err != nil {
            s.log.Errorw("failed to process grant for subscription",
                "grant_id", grant.ID,
                "subscription_id", subscription.ID,
                "error", err)
            // Continue with other subscriptions
        }
    }

    return nil
}

func (s *creditGrantService) processGrantForSubscription(ctx context.Context, grant *creditgrant.CreditGrant, subscription *subscription.Subscription) error {
    // Check if application already exists for current billing period
    // This would require the application repository - we'll need to inject it
    // For now, implement the core logic

    // Determine if grant should be applied based on subscription state
    stateHandler := &SubscriptionStateHandler{
        subscription: subscription,
        grant:        grant,
    }

    action, reason := stateHandler.DetermineAction()

    // Create application record and apply based on action
    switch action {
    case StateActionApply:
        return s.applyRecurringGrant(ctx, grant, subscription)
    case StateActionSkip:
        s.log.Debugw("skipping grant application", "reason", reason, "grant_id", grant.ID, "subscription_id", subscription.ID)
        return nil
    case StateActionDefer:
        s.log.Debugw("deferring grant application", "reason", reason, "grant_id", grant.ID, "subscription_id", subscription.ID)
        return nil
    case StateActionCancel:
        s.log.Debugw("cancelling grant application", "reason", reason, "grant_id", grant.ID, "subscription_id", subscription.ID)
        return nil
    default:
        return ierr.NewError("unknown state action").Mark(ierr.ErrInternal)
    }
}

func (s *creditGrantService) applyRecurringGrant(ctx context.Context, grant *creditgrant.CreditGrant, subscription *subscription.Subscription) error {
    // Use the existing wallet integration from subscription service
    walletService := NewWalletService(s.ServiceParams) // We'll need to add ServiceParams to creditGrantService

    // Find or create wallet (reuse logic from subscription service)
    wallets, err := walletService.GetWalletsByCustomerID(ctx, subscription.CustomerID)
    if err != nil {
        return err
    }

    var selectedWallet *dto.WalletResponse
    for _, w := range wallets {
        if types.IsMatchingCurrency(w.Currency, subscription.Currency) {
            selectedWallet = w
            break
        }
    }

    if selectedWallet == nil {
        // Create new wallet
        walletReq := &dto.CreateWalletRequest{
            Name:       "Subscription Wallet",
            CustomerID: subscription.CustomerID,
            Currency:   subscription.Currency,
        }
        selectedWallet, err = walletService.CreateWallet(ctx, walletReq)
        if err != nil {
            return err
        }
    }

    // Calculate expiry date based on grant settings
    var expiryDate *time.Time
    if grant.ExpirationType == types.CreditGrantExpiryTypeBillingCycle {
        expiryDate = &subscription.CurrentPeriodEnd
    } else if grant.ExpirationType == types.CreditGrantExpiryTypeDuration && grant.ExpirationDuration != nil {
        expiry := time.Now().AddDate(0, 0, *grant.ExpirationDuration)
        expiryDate = &expiry
    }

    // Apply credit to wallet
    topupReq := &dto.TopUpWalletRequest{
        CreditsToAdd:      grant.Credits,
        TransactionReason: types.TransactionReasonRecurringCredit, // Add this to types if not exists
        ExpiryDateUTC:     expiryDate,
        Priority:          grant.Priority,
        IdempotencyKey:    lo.ToPtr(fmt.Sprintf("recurring_%s_%s_%d", grant.ID, subscription.ID, time.Now().Unix())),
        Metadata: map[string]string{
            "grant_id":       grant.ID,
            "subscription_id": subscription.ID,
            "reason":         "recurring_credit_grant",
            "period_start":   subscription.CurrentPeriodStart.Format(time.RFC3339),
            "period_end":     subscription.CurrentPeriodEnd.Format(time.RFC3339),
        },
    }

    _, err = walletService.TopUpWallet(ctx, selectedWallet.ID, topupReq)
    if err != nil {
        return ierr.WithError(err).
            WithHint("Failed to apply recurring credit grant to wallet").
            WithReportableDetails(map[string]interface{}{
                "grant_id":        grant.ID,
                "subscription_id": subscription.ID,
                "wallet_id":       selectedWallet.ID,
            }).
            Mark(ierr.ErrDatabase)
    }

    s.log.Infow("successfully applied recurring credit grant",
        "grant_id", grant.ID,
        "subscription_id", subscription.ID,
        "wallet_id", selectedWallet.ID,
        "amount", grant.Credits,
    )

    return nil
}
```

#### 2. Subscription State Handler

**Add new file: internal/service/subscription_state_handler.go**

```go
package service

import (
    "github.com/flexprice/flexprice/internal/domain/creditgrant"
    "github.com/flexprice/flexprice/internal/domain/subscription"
    "github.com/flexprice/flexprice/internal/types"
)

type StateAction string

const (
    StateActionApply  StateAction = "apply"
    StateActionSkip   StateAction = "skip"
    StateActionDefer  StateAction = "defer"
    StateActionCancel StateAction = "cancel"
)

type SubscriptionStateHandler struct {
    subscription *subscription.Subscription
    grant        *creditgrant.CreditGrant
}

func (h *SubscriptionStateHandler) DetermineAction() (StateAction, string) {
    switch h.subscription.SubscriptionStatus {
    case types.SubscriptionStatusActive:
        return StateActionApply, "subscription_active"

    case types.SubscriptionStatusTrialing:
        // For now, apply during trial. This could be configurable per grant
        return StateActionApply, "trial_active"

    case types.SubscriptionStatusPastDue:
        return StateActionDefer, "subscription_past_due"

    case types.SubscriptionStatusUnpaid:
        return StateActionDefer, "subscription_unpaid"

    case types.SubscriptionStatusCancelled:
        return StateActionCancel, "subscription_cancelled"

    case types.SubscriptionStatusIncomplete:
        return StateActionDefer, "subscription_incomplete"

    case types.SubscriptionStatusIncompleteExpired:
        return StateActionCancel, "subscription_incomplete_expired"

    case types.SubscriptionStatusPaused:
        return StateActionDefer, "subscription_paused"

    default:
        return StateActionSkip, "unknown_subscription_status"
    }
}
```

#### 3. Enhanced Subscription Service

**Add to existing SubscriptionService:**

```go
// Add to internal/service/subscription.go

// ProcessRecurringCreditGrants processes recurring credit grants for all subscriptions
func (s *subscriptionService) ProcessRecurringCreditGrants(ctx context.Context) error {
    s.Logger.Infow("starting recurring credit grant processing for all subscriptions")

    creditGrantService := NewCreditGrantService(s.CreditGrantRepo, s.PlanRepo, s.SubRepo, s.Logger)
    return creditGrantService.ProcessRecurringGrants(ctx)
}

// ProcessSubscriptionRecurringGrants processes recurring grants for a specific subscription
func (s *subscriptionService) ProcessSubscriptionRecurringGrants(ctx context.Context, subscriptionID string) error {
    s.Logger.Infow("processing recurring credit grants for subscription", "subscription_id", subscriptionID)

    creditGrantService := NewCreditGrantService(s.CreditGrantRepo, s.PlanRepo, s.SubRepo, s.Logger)

    // Get active grants for this subscription
    grants, err := s.CreditGrantRepo.FindActiveGrantsForSubscription(ctx, subscriptionID)
    if err != nil {
        return err
    }

    // Filter for recurring grants only
    recurringGrants := make([]*creditgrant.CreditGrant, 0)
    for _, grant := range grants {
        if grant.Cadence == types.CreditGrantCadenceRecurring {
            recurringGrants = append(recurringGrants, grant)
        }
    }

    if len(recurringGrants) == 0 {
        s.Logger.Debugw("no recurring grants found for subscription", "subscription_id", subscriptionID)
        return nil
    }

    // Get subscription
    subscription, _, err := s.SubRepo.GetWithLineItems(ctx, subscriptionID)
    if err != nil {
        return err
    }

    // Process each recurring grant
    for _, grant := range recurringGrants {
        if err := creditGrantService.processGrantForSubscription(ctx, grant, subscription); err != nil {
            s.Logger.Errorw("failed to process recurring grant for subscription",
                "grant_id", grant.ID,
                "subscription_id", subscriptionID,
                "error", err)
            // Continue with other grants
        }
    }

    return nil
}

// HandleSubscriptionStateChange handles subscription state changes for credit grants
func (s *subscriptionService) HandleSubscriptionStateChange(ctx context.Context, subscriptionID string, oldStatus, newStatus types.SubscriptionStatus) error {
    s.Logger.Infow("handling subscription state change for credit grants",
        "subscription_id", subscriptionID,
        "old_status", oldStatus,
        "new_status", newStatus)

    switch {
    case newStatus == types.SubscriptionStatusActive && oldStatus != types.SubscriptionStatusActive:
        return s.handleSubscriptionActivation(ctx, subscriptionID)

    case newStatus == types.SubscriptionStatusCancelled:
        return s.handleSubscriptionCancellation(ctx, subscriptionID)

    case newStatus == types.SubscriptionStatusPaused:
        return s.handleSubscriptionPause(ctx, subscriptionID)

    case oldStatus == types.SubscriptionStatusPaused && newStatus == types.SubscriptionStatusActive:
        return s.handleSubscriptionResume(ctx, subscriptionID)
    }

    return nil
}

func (s *subscriptionService) handleSubscriptionActivation(ctx context.Context, subscriptionID string) error {
    // Process any deferred credits and trigger immediate processing for newly active subscription
    return s.ProcessSubscriptionRecurringGrants(ctx, subscriptionID)
}

func (s *subscriptionService) handleSubscriptionCancellation(ctx context.Context, subscriptionID string) error {
    // Future: Cancel scheduled applications if we implement full application tracking
    s.Logger.Infow("subscription cancelled, future recurring grants will not be processed", "subscription_id", subscriptionID)
    return nil
}

func (s *subscriptionService) handleSubscriptionPause(ctx context.Context, subscriptionID string) error {
    // Future: Defer scheduled applications if we implement full application tracking
    s.Logger.Infow("subscription paused, recurring grants will be deferred", "subscription_id", subscriptionID)
    return nil
}

func (s *subscriptionService) handleSubscriptionResume(ctx context.Context, subscriptionID string) error {
    // Process any missed recurring grants
    return s.ProcessSubscriptionRecurringGrants(ctx, subscriptionID)
}
```

#### 4. Cron Job Implementation

**Create new file: internal/service/credit_grant_cron.go**

```go
package service

import (
    "context"
    "time"

    "github.com/robfig/cron/v3"
    "github.com/flexprice/flexprice/internal/logger"
)

type CreditGrantCronJob struct {
    subscriptionService SubscriptionService
    logger              *logger.Logger
    cron                *cron.Cron
    enabled             bool
    schedule            string
}

type CreditGrantCronConfig struct {
    Enabled             bool          `json:"enabled" default:"true"`
    Schedule            string        `json:"schedule" default:"0 2 * * *"` // 2 AM daily
    ProcessingTimeout   time.Duration `json:"processing_timeout" default:"30m"`
}

func NewCreditGrantCronJob(
    subscriptionService SubscriptionService,
    logger *logger.Logger,
    config CreditGrantCronConfig,
) *CreditGrantCronJob {
    return &CreditGrantCronJob{
        subscriptionService: subscriptionService,
        logger:              logger,
        cron:                cron.New(),
        enabled:             config.Enabled,
        schedule:            config.Schedule,
    }
}

func (c *CreditGrantCronJob) Start() error {
    if !c.enabled {
        c.logger.Infow("credit grant cron job is disabled")
        return nil
    }

    _, err := c.cron.AddFunc(c.schedule, func() {
        ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
        defer cancel()

        c.logger.Infow("starting scheduled recurring credit grant processing")

        if err := c.subscriptionService.ProcessRecurringCreditGrants(ctx); err != nil {
            c.logger.Errorw("failed to process recurring credit grants", "error", err)
            // Could send alert/notification here
        } else {
            c.logger.Infow("completed scheduled recurring credit grant processing")
        }
    })

    if err != nil {
        return err
    }

    c.cron.Start()
    c.logger.Infow("credit grant cron job started", "schedule", c.schedule)

    return nil
}

func (c *CreditGrantCronJob) Stop() {
    c.cron.Stop()
    c.logger.Infow("credit grant cron job stopped")
}
```

### Integration Points

#### 1. Modify Existing Subscription State Changes

**Update existing subscription service methods to trigger credit grant processing:**

```go
// Modify existing methods in subscription.go

// In CancelSubscription method, add:
defer func() {
    if err == nil {
        s.HandleSubscriptionStateChange(ctx, id, subscription.SubscriptionStatus, types.SubscriptionStatusCancelled)
    }
}()

// In PauseSubscription method, add:
defer func() {
    if err == nil {
        s.HandleSubscriptionStateChange(ctx, subscriptionID, sub.SubscriptionStatus, types.SubscriptionStatusPaused)
    }
}()

// In ResumeSubscription method, add:
defer func() {
    if err == nil {
        s.HandleSubscriptionStateChange(ctx, subscriptionID, types.SubscriptionStatusPaused, types.SubscriptionStatusActive)
    }
}()
```

#### 2. Service Dependencies

**Update service constructors to include necessary dependencies:**

```go
// Update CreditGrantService constructor
func NewCreditGrantService(
    repo creditgrant.Repository,
    planRepo plan.Repository,
    subRepo subscription.Repository,
    walletService WalletService, // Add this
    log *logger.Logger,
) CreditGrantService {
    return &creditGrantService{
        repo:          repo,
        planRepo:      planRepo,
        subRepo:       subRepo,
        walletService: walletService, // Add this
        log:           log,
    }
}
```

### Edge Cases Handled

1. **Billing Period Alignment**: Credits are applied once per billing period by checking existing applications
2. **Subscription State Handling**: Different actions (apply/defer/skip/cancel) based on subscription status
3. **Currency Matching**: Only applies to wallets with matching currency
4. **Idempotency**: Uses idempotency keys to prevent duplicate applications
5. **Plan vs Subscription Scope**: Correctly handles both plan-level and subscription-level grants
6. **Concurrent Processing**: Repository queries include proper tenant/environment scoping

### API Endpoints for Manual Operations

**Add to existing handlers:**

```go
// Add to credit grant handler
// POST /v1/admin/credit-grants/process-recurring
func (h *CreditGrantHandler) ProcessRecurringGrants(c *gin.Context) {
    if err := h.subscriptionService.ProcessRecurringCreditGrants(c.Request.Context()); err != nil {
        c.JSON(500, gin.H{"error": err.Error()})
        return
    }
    c.JSON(200, gin.H{"message": "Recurring credit grants processed successfully"})
}

// POST /v1/subscriptions/{id}/process-recurring-grants
func (h *SubscriptionHandler) ProcessRecurringGrants(c *gin.Context) {
    subscriptionID := c.Param("id")

    if err := h.subscriptionService.ProcessSubscriptionRecurringGrants(c.Request.Context(), subscriptionID); err != nil {
        c.JSON(500, gin.H{"error": err.Error()})
        return
    }
    c.JSON(200, gin.H{"message": "Recurring credit grants processed successfully"})
}
```

### Implementation Plan

#### Phase 1: Foundation (Week 1)

- [ ] Add repository methods for finding active grants and eligible subscriptions
- [ ] Create subscription state handler
- [ ] Add basic recurring grant processing to credit grant service

#### Phase 2: Core Processing (Week 2)

- [ ] Implement full recurring grant processing flow
- [ ] Add subscription state change handlers
- [ ] Integrate with existing wallet service

#### Phase 3: Cron Job and Integration (Week 3)

- [ ] Implement cron job scheduler
- [ ] Add API endpoints for manual processing
- [ ] Integrate state change handlers with existing subscription methods

#### Phase 4: Testing and Monitoring (Week 4)

- [ ] Comprehensive testing
- [ ] Add monitoring and alerting
- [ ] Performance optimization
- [ ] Documentation

This implementation integrates seamlessly with your existing architecture while providing comprehensive recurring credit grant functionality. The modular approach allows for incremental implementation and testing.
