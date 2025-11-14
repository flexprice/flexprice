# Feature Wallet Balance Alert System - Complete Developer Guide

## System Overview
Feature Wallet Balance Alert is a real-time monitoring system that tracks wallet balances for features and triggers automated alerts when configured thresholds are breached. The system supports three alert levels: Critical, Warning, and Info.

## How It Works

### Alert States
- **OK**: Balance is healthy, no action needed
- **Info**: Balance crossed info threshold, informational only
- **Warning**: Balance crossed warning threshold, attention needed
- **In Alarm**: Balance crossed critical threshold, immediate action required

### Threshold Configuration
Three independent threshold types that can be configured separately:
1. **Critical** (highest priority): Immediate action required
2. **Warning** (medium priority): Attention needed  
3. **Info** (lowest priority): Informational tracking

Each threshold has:
- **threshold**: The numeric value to monitor
- **condition**: "above" or "below" - defines which direction triggers alert

### Threshold Ordering
For **"below"** condition: critical < warning < info
For **"above"** condition: critical > warning > info

Example "below" configuration:
```json
{
  "critical": 100.00,   // Balance below 100 triggers critical
  "warning": 500.00,    // Balance below 500 triggers warning  
  "info": 1000.00       // Balance below 1000 triggers info
}
```

### Alert Settings Structure
```json
{
  "critical": {
    "threshold": 100.00,
    "condition": "below"
  },
  "warning": {
    "threshold": 500.00,
    "condition": "below"
  },
  "info": {
    "threshold": 1000.00,
    "condition": "below"
  },
  "alert_enabled": true
}
```

### Validation Rules
1. When alert_enabled=true, at least one threshold (critical, warning, or info) must be provided
2. Warning requires Critical (can't have warning without critical)
3. Info is independent (can exist standalone without critical or warning)
4. Threshold ordering must be correct for the selected condition
5. All thresholds for a feature must use the same condition (all "above" or all "below")

## Real-World Use Cases

### Use Case 1: Credit Monitoring for SaaS API Usage
**Problem**: Track remaining API credits to prevent service interruption
**Configuration**:
- Critical: 100 credits (below) - Immediate alert, service may stop
- Warning: 500 credits (below) - Low balance alert
- Info: 1000 credits (below) - Usage tracking milestone

**Scenario**: Customer has 10,000 credits. When credits drop to:
- 1000: Info alert triggered (informational)
- 500: Warning alert triggered (needs attention)
- 100: Critical alert triggered (urgent action needed)

**Why this works**: Graduated alerts prevent sudden service interruption

### Use Case 2: Budget Management for Enterprise Customers
**Problem**: Monitor spending against allocated budget
**Configuration**:
- Info: $500 spent (above) - Usage milestone
- Warning: $800 spent (above) - Approaching budget limit
- Critical: $1000 spent (above) - Budget exceeded

**Scenario**: Budget of $1000 allocated. Spending tracking:
- $500: Info alert (spending milestone reached)
- $800: Warning alert (approaching limit)
- $1000: Critical alert (budget exceeded, spending freeze)

**Why this works**: Prevents budget overruns with graduated warnings

### Use Case 3: Freemium Usage Limits
**Problem**: Track free tier usage against limits
**Configuration**:
- Info: 500 API calls (above) - Usage milestone
- Warning: 800 API calls (above) - Approaching free limit
- Critical: 1000 API calls (above) - Free tier limit reached

**Scenario**: Free tier allows 1000 API calls per month:
- 500 calls: Info alert (usage milestone)
- 800 calls: Warning alert (approaching limit)
- 1000 calls: Critical alert (limit reached, upgrade needed)

**Why this works**: Graduated warnings encourage upgrades before hitting limit

### Use Case 4: Subscription Health Monitoring
**Problem**: Monitor wallet balance to prevent service suspension
**Configuration**:
- Critical: $0 balance (below) - Service will suspend
- Warning: $50 balance (below) - Low balance, payment needed
- Info: $200 balance (below) - Balance tracking

**Scenario**: Subscription wallet with auto-recharge:
- $200: Info alert (healthy balance)
- $50: Warning alert (low balance, recharge soon)
- $0: Critical alert (service suspended, immediate action)

**Why this works**: Prevents service interruption with advance warnings

### Use Case 5: Cloud Resource Consumption
**Problem**: Monitor compute usage against allocated resources
**Configuration**:
- Info: 50% usage (above) - Resource usage milestone
- Warning: 75% usage (above) - High resource utilization
- Critical: 90% usage (above) - Resource limit reached

**Scenario**: Allocated 100GB storage:
- 50GB: Info alert (usage milestone)
- 75GB: Warning alert (high usage)
- 90GB: Critical alert (resource limit reached)

**Why this works**: Prevents resource exhaustion with graduated alerts

## API Usage

### Creating Feature with Alerts
```bash
POST /api/v1/features
Content-Type: application/json

{
  "name": "API Credits",
  "type": "metered",
  "alert_settings": {
    "critical": {
      "threshold": "100.00",
      "condition": "below"
    },
    "warning": {
      "threshold": "500.00", 
      "condition": "below"
    },
    "info": {
      "threshold": "1000.00",
      "condition": "below"
    },
    "alert_enabled": true
  }
}
```

### Updating Alert Settings (Partial Update)
```bash
PATCH /api/v1/features/feat_123
Content-Type: application/json

{
  "alert_settings": {
    "info": {
      "threshold": "1500.00",
      "condition": "below"
    }
  }
}
```

This keeps existing critical and warning thresholds unchanged, only updates info threshold.

### Info-Only Alert (Standalone)
```bash
POST /api/v1/features
Content-Type: application/json

{
  "name": "Usage Tracker",
  "type": "metered",
  "alert_settings": {
    "info": {
      "threshold": "1000.00",
      "condition": "below"
    },
    "alert_enabled": true
  }
}
```

### Critical + Info (No Warning)
```bash
POST /api/v1/features
Content-Type: application/json

{
  "name": "Critical Monitor",
  "type": "metered", 
  "alert_settings": {
    "critical": {
      "threshold": "100.00",
      "condition": "below"
    },
    "info": {
      "threshold": "1000.00",
      "condition": "below"
    },
    "alert_enabled": true
  }
}
```

## How State Determination Works

### State Evaluation Logic
1. Check if alert_enabled=true (required)
2. Find the primary condition (critical > warning > info priority)
3. Evaluate balance against each threshold in order:
   - If balance breaches critical threshold → return In Alarm
   - If balance breaches warning threshold → return Warning
   - If balance breaches info threshold → return Info
   - If balance is above all thresholds → return OK

### State Transition Examples
**Balance starting at 1500 (above all thresholds)**:
- Balance drops to 1000 → Transition: OK → Info
- Balance drops to 500 → Transition: Info → Warning
- Balance drops to 100 → Transition: Warning → In Alarm
- Balance returns to 2000 → Transition: In Alarm → OK

**Balance starting at 2000 (above all thresholds)**:
- Balance drops to 50 → Transition: OK → In Alarm (skips info and warning)
- Balance recovers to 1500 → Transition: In Alarm → Info

## State Transition Rules
- OK → Info: Balance crosses info threshold
- OK → Warning: Balance crosses warning threshold (skips info)
- OK → In Alarm: Balance crosses critical threshold (skips info/warning)
- Info → Warning: Balance crosses warning threshold
- Info → In Alarm: Balance crosses critical threshold
- Warning → In Alarm: Balance crosses critical threshold  
- Any state → OK: Balance returns above all thresholds

**Important**: State transitions never go backwards (Info → OK is not allowed, only when balance crosses the threshold again)

## Webhook Integration

### Webhook Event
**Event Type**: `feature.wallet_balance.alert`

### Webhook Payload
```json
{
  "event_type": "feature.wallet_balance.alert",
  "feature_id": "feat_01ABC123",
  "wallet_id": "wallet_01DEF456",
  "alert_state": "warning",
  "current_balance": "450.50",
  "threshold_breached": {
    "type": "warning",
    "threshold": "500.00",
    "condition": "below"
  },
  "timestamp": "2025-10-23T10:30:00Z",
  "tenant_id": "tenant_123"
}
```

### Webhook Trigger Conditions
- ALL alert states trigger webhooks (info, warning, critical)
- Webhook sent immediately on state transition
- Retry logic for failed deliveries
- Rate limiting to prevent spam

## Monitoring Implementation

### Real-Time Monitoring
- Triggered by wallet transactions (credit/debit operations)
- Immediate state evaluation on balance changes
- Webhook notifications sent instantly
- State persisted to database

### Cron-Based Monitoring  
- Periodic balance checks every 5 minutes
- Ongoing balance alert monitoring
- Handles edge cases from missed real-time events
- Ensures no alert is ever missed

### Alert Logging
- All state changes logged to AlertLogs table
- Historical tracking of threshold breaches
- Audit trail for compliance
- Debugging and troubleshooting data

## Error Handling and Validation

### Validation Errors
- **Invalid threshold ordering**: Critical/Warning/Info out of order
- **Missing required thresholds**: Warning without Critical
- **Invalid condition**: Must be "above" or "below"
- **Alert enabled without thresholds**: Must provide at least one threshold when alert_enabled=true

### Error Messages
1. "warning threshold must be greater than critical threshold" (for below condition)
2. "critical threshold is required when warning threshold is provided"
3. "at least one threshold (critical, warning, or info) is required when alert_enabled is true"
4. "invalid critical threshold condition" (must be above or below)

### Runtime Errors
- Balance calculation failures
- Webhook delivery failures  
- State transition conflicts
- Database connection issues

### Recovery Mechanisms
- Retry failed webhook deliveries (3 attempts)
- Fallback to email notifications for critical alerts
- Graceful degradation on errors
- Alert system health monitoring

## Best Practices

### Threshold Configuration
1. **Critical Threshold**: Set at service interruption point
2. **Warning Threshold**: Set at 2-5x critical threshold
3. **Info Threshold**: Set at 5-10x critical threshold
4. **Condition Selection**: "below" for credit depletion, "above" for usage limits

### Alert Management
1. **Enable Alerts**: Only when monitoring is needed
2. **Threshold Ordering**: Always maintain proper ordering
3. **Testing**: Test alert thresholds in staging environment first
4. **Documentation**: Document why each threshold value was chosen

### Performance Optimization
1. **Batch Processing**: Group balance checks for efficiency
2. **Caching**: Cache alert states to reduce computation
3. **Rate Limiting**: Implement webhook rate limiting (10/sec max)
4. **Monitoring**: Monitor alert system performance metrics

### Security
1. **Access Control**: Feature-level alert configuration permissions
2. **Data Protection**: Encrypt sensitive balance data
3. **Audit Logging**: Track all configuration changes
4. **Compliance**: GDPR/SOX compliance for financial alerts

## Integration Points

### Wallet Service
- Balance updates trigger alert checks
- Transaction processing includes alert evaluation
- Credit/debit operations update alert states
- Wallet termination resets alert states

### Billing Service
- Usage calculations affect balance alerts
- Invoice generation triggers balance updates
- Payment processing updates alert states
- Subscription renewal resets thresholds

### Notification Service
- Webhook delivery for real-time alerts
- Email notifications for critical alerts
- SMS notifications for emergency alerts
- Slack/PagerDuty integration for ops teams

## Troubleshooting Guide

### Common Issues

**Issue 1: Alerts Not Triggering**
- Check alert_enabled status (must be true)
- Verify threshold configuration is correct
- Check balance calculation logic
- Review state transition logs

**Issue 2: Wrong Threshold Order**
- Verify condition type (above vs below)
- Check threshold values for correct ordering
- Review validation error messages
- Test with sample balance values

**Issue 3: Webhook Failures**
- Check webhook endpoint configuration
- Verify webhook URL is accessible
- Review webhook delivery logs
- Test webhook endpoint manually

**Issue 4: State Inconsistencies**
- Review state transition logic
- Check for race conditions in concurrent updates
- Verify alert log entries match current state
- Review balance calculation accuracy

### Debug Tools
- Alert state history queries in database
- Webhook delivery logs
- Balance calculation traces
- Threshold validation checks
- State transition logs

### Support Escalation Process
1. Check alert configuration
2. Verify balance calculations
3. Review webhook delivery logs
4. Test threshold configurations
5. Check for system errors
6. Escalate to engineering team with full logs

## Development Guidelines

### Adding New Alert Types
1. Add state constant in types
2. Update state determination logic
3. Add validation rules
4. Update webhook mapping
5. Add test cases
6. Update documentation

### Testing Alert Functionality
1. Test each threshold independently
2. Test state transitions
3. Test partial updates
4. Test error scenarios
5. Test webhook delivery
6. Test performance with high volume

### Code Review Checklist
- [ ] Threshold ordering validation correct
- [ ] State transitions are correct
- [ ] Webhook payload is complete
- [ ] Error handling is robust
- [ ] Tests cover all scenarios
- [ ] Documentation is updated

## Future Enhancements

### Planned Features
- Multi-currency alert support
- Custom alert conditions beyond above/below
- Alert escalation chains (time-based)
- Machine learning-based threshold suggestions
- Alert dashboard UI
- Historical alert analytics

### Integration Opportunities
- Slack notifications
- PagerDuty integration
- Custom webhook formats
- Mobile app push notifications
- Voice call alerts for critical states

## Quick Reference

### Alert State Priority
1. In Alarm (Critical) - Highest
2. Warning - Medium  
3. Info - Low
4. OK - No alert

### Threshold Ordering Rules
**Below condition**: critical < warning < info (ascending values)
**Above condition**: critical > warning > info (descending values)

### Minimum Required Fields
When alert_enabled=true: Must provide at least one threshold (critical, warning, or info)
When warning is provided: Must provide critical as well
When info is provided: Can exist standalone

### API Response Example
```json
{
  "id": "feat_01ABC123",
  "name": "API Credits",
  "alert_settings": {
    "critical": {
      "threshold": "100.00",
      "condition": "below"
    },
    "warning": {
      "threshold": "500.00",
      "condition": "below"
    },
    "info": {
      "threshold": "1000.00",
      "condition": "below"
    },
    "alert_enabled": true
  }
}
```

## Conclusion

The Feature Wallet Balance Alert system is a powerful, flexible monitoring solution. Key takeaways:
- Supports three independent alert levels
- Flexible configuration (standalone info, critical+info, full stack)
- Real-time monitoring with webhook notifications
- Comprehensive error handling and validation
- Production-ready with extensive logging
- Scales to handle high-volume alert scenarios

Developers should understand: threshold ordering rules, state determination logic, webhook integration, and error scenarios to implement alerts effectively.
