# Product Requirements Document: Subscription Renewal Due Webhook

## Overview

This document outlines the implementation of a webhook notification system that triggers 24 hours before a subscription's end time. The system will automatically check for subscriptions due for renewal and send webhook notifications to relevant endpoints.

## Technical Requirements

### Implementation Details

1. **Cron Layer (`cron/subscription`)**
   ```go
   // ProcessSubscriptionsDueForRenewal handles the hourly check for subscriptions due for renewal
   func (c *Cron) ProcessSubscriptionsDueForRenewal(ctx context.Context) error {
       // 1. Get subscriptions from repository
       // 2. Process subscriptions via service layer
       // 3. Handle errors and logging
   }
   ```

2. **Repository Layer (`repository/ent/subscription`)**
   ```go
   // List/ListAll to fetch active subscriptions due for renewal
   func (r *SubscriptionRepository) ListActiveSubscriptionsDueForRenewal(ctx context.Context) ([]*ent.Subscription, error) {
       // Query active subscriptions where:
       // - status == ACTIVE
       // - end_time ~ time.Now().UTC() == 24hrs
   }
   ```

3. **Service Layer (`service/subscription`)**
   ```go
   // ProcessSubscriptionRenewalDueAlert handles webhook publishing for due renewals
   func (s *Service) ProcessSubscriptionRenewalDueAlert(ctx context.Context, subscriptions []*ent.Subscription) error {
       // For each subscription:
       // 1. Prepare webhook payload
       // 2. Publish renewal due webhook
   }
   ```

4. **Process Flow**
   1. Daily cron trigger
   2. Repository layer fetches all ACTIVE subscriptions with end time ~24hrs from now
   3. Service layer processes each subscription:
      - Prepares and publishes webhook- subscription.renewal.due

### Webhook Specifications

1. **Timing**
   - Trigger: 24 hours before subscription end time
   - Timezone: All times in UTC
   - Precision: Second-level accuracy

2. **Payload Structure**
```json
{
    "event_type": "subscription.renewal_due",
    "timestamp": "2024-03-20T10:00:00Z",
    "subscription": {
        "id": "sub_123",
        "customer_id": "cust_456",
        "current_period_end": "2024-03-21T10:00:00Z",
        "renewal_amount": "100.00",
        "currency": "USD",
        "status": "active",
        "product": {
            "id": "prod_789",
            "name": "Enterprise Plan"
        }
    },
    "metadata": {
        "webhook_id": "whk_123",
        "attempt_number": 1
    }
}
```

### System Requirements

1. **Event Processing**
   - Queue-based architecture for reliable delivery
   - Idempotency handling
   - Dead letter queue for failed deliveries

2. **Retry Logic**
   - Maximum 3 retry attempts
   - Exponential backoff: 5min, 15min, 45min
   - Configurable retry intervals

3. **Monitoring**
   - Webhook delivery success rate
   - Delivery latency metrics
   - Retry attempt tracking
   - Error rate monitoring

## Success Metrics

1. **Technical Metrics**
   - 99.9% webhook delivery success rate
   - < 100ms average delivery latency
   - < 0.1% error rate

2. **Business Metrics**
   - Increased renewal rate
   - Reduced unexpected subscription lapses
   - Improved customer satisfaction