## Implement subscription upgrade downgrade

### Workflow
1. User is allowed to change the plan of the subscription
2. Whenever user changes the plan in subscription
    - Archived the old Subscription
    - Calculate the prorated charges
    - Create a new subscription with proration mode active
    - Invoice will be generated 
    - Add the old subscription credit or charges to new subscription invoice


## Implementation Plan

1. API and Routes
    - Create 2 routes
        1. /Preview-Subscription-Change
        2. /Execute-Subscription-Change

2. Handler
    - SubscriptionChangeHandler

3. DTO:
    - SubscriptionChangeRequest
        - TargetPlanID
        - ProrationMode
        - CustomerTimezone
    
    This DTO will be used for both preview and excecuting the change

4. Service Layer
   - Subscription Change Service
   - Contains two main methods
     1. Preview Subscription Change
        - Show the effect of upgradation
     2. Execute Subscription Change
        - Execute the actual change
        - calculate the proration of the old subscription using ProrationService.CalculateProrationOnSubscription method
        this is already written method
        - Archive the old subscription
        - move it charges and credits to new susbcription Invoice.


   - Contains service specific methods
     - HandleSubscriptionUpgrade
     - HandleSubscriptionDowngrade
     - DetermineSubsriptionChangeAction(Is upgrade or downgrade)