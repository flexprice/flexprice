# Service Architecture Improvement Plan

## 1. Current Challenges

The FlexPrice service layer has grown significantly, leading to several architectural challenges:

1. **Service-to-Service Dependencies**
   - Manual service instantiation within other services
   - Potential for cyclic dependencies
   - Difficulty tracking service dependencies

2. **Repository Access Patterns**
   - Multiple services accessing the same repositories
   - Inconsistent data access patterns
   - Potential duplication of business logic

3. **Large Service Functions**
   - Methods growing to 200+ lines
   - Complex business logic becoming difficult to maintain
   - Helper functions with limited reusability

4. **Inconsistent Responsibility Boundaries**
   - Mixing direct repository access with service calls
   - Unclear ownership of business logic
   - Difficulty extending functionality

## 2. Architectural Improvements

### 2.1 Enhanced Dependency Injection

#### Current Implementation

The current implementation uses Uber's fx framework for dependency injection, but services are still manually instantiated within other services:

```go
// Example from billing.go
subscriptionService := NewSubscriptionService(s.ServiceParams)
```

This approach:
- Creates new service instances on each method call
- Makes it difficult to track dependencies
- Can lead to cyclic dependencies

#### Proposed Improvement

Enhance the dependency injection to inject service dependencies directly:

```go
// Service definition
type BillingService interface {
    // Methods...
}

type billingService struct {
    ServiceParams
    subscriptionService SubscriptionService
    invoiceService      InvoiceService
    // Other service dependencies
}

// Constructor with explicit dependencies
func NewBillingService(
    params ServiceParams,
    subscriptionService SubscriptionService,
    invoiceService InvoiceService,
) BillingService {
    return &billingService{
        ServiceParams:       params,
        subscriptionService: subscriptionService,
        invoiceService:      invoiceService,
    }
}
```

This approach:
- Makes dependencies explicit and traceable
- Prevents cyclic dependencies through compile-time checks
- Allows for easier testing through dependency mocking
- Ensures consistent service instances throughout the application lifecycle

### 2.2 Service Composition Pattern

To address the challenge of large service methods and complex interdependencies, implement a service composition pattern:

#### Domain Service Organization

Reorganize services into three categories:

1. **Core Domain Services**
   - Focus on single entity operations (CRUD)
   - Implement business rules specific to a single domain entity
   - Example: `SubscriptionService`, `InvoiceService`

2. **Composite Services**
   - Orchestrate operations across multiple domain entities
   - Implement complex business processes
   - Example: `BillingService`, `PaymentProcessorService`

3. **Application Services**
   - Handle cross-cutting concerns
   - Implement system-wide operations
   - Example: `AuthService`, `TenantService`

#### Implementation Example

```go
// Domain service - focused on a single entity
type SubscriptionService interface {
    Create(ctx context.Context, sub *subscription.Subscription) error
    Get(ctx context.Context, id string) (*subscription.Subscription, error)
    // Other CRUD operations
}

// Composite service - orchestrates across domains
type BillingService interface {
    CalculateCharges(ctx context.Context, subscriptionID string, period BillingPeriod) (*BillingResult, error)
    GenerateInvoice(ctx context.Context, subscriptionID string, period BillingPeriod) (*invoice.Invoice, error)
}
```

### 2.3 Command/Query Responsibility Segregation (CQRS)

For complex business operations, implement a lightweight CQRS pattern to separate read and write operations:

#### When to Use Command Handlers

Command handlers are ideal for complex operations that **modify state** and involve multiple steps or business rules. They're particularly useful when:

1. **The operation is complex** - Involves multiple steps, validations, or business rules
2. **Multiple entities are affected** - Updates data across different domain entities
3. **The operation has side effects** - Triggers events, notifications, or other processes

#### Real-World Example: Invoice Generation

```go
// Command definition - represents the intent to perform an action
type GenerateInvoiceCommand struct {
    SubscriptionID string
    PeriodStart    time.Time
    PeriodEnd      time.Time
    IsPreview      bool
    CustomerID     string  // Optional, for validation
}

// Command handler - contains the logic to execute the command
type GenerateInvoiceHandler struct {
    subscriptionService core.SubscriptionService
    customerService     core.CustomerService
    invoiceService      core.InvoiceService
    billingCalculator   query.CalculateChargesHandler
    logger              *logger.Logger
}

// Constructor with dependencies
func NewGenerateInvoiceHandler(
    subscriptionService core.SubscriptionService,
    customerService core.CustomerService,
    invoiceService core.InvoiceService,
    billingCalculator query.CalculateChargesHandler,
    logger *logger.Logger,
) *GenerateInvoiceHandler {
    return &GenerateInvoiceHandler{
        subscriptionService: subscriptionService,
        customerService:     customerService,
        invoiceService:      invoiceService,
        billingCalculator:   billingCalculator,
        logger:              logger,
    }
}

// Handle method - executes the command
func (h *GenerateInvoiceHandler) Handle(ctx context.Context, cmd GenerateInvoiceCommand) (*invoice.Invoice, error) {
    // 1. Validate the command
    if cmd.SubscriptionID == "" {
        return nil, ierr.NewError("subscription ID is required").
            WithHint("Please provide a valid subscription ID").
            Mark(ierr.ErrValidation)
    }
    
    // 2. Get required data
    sub, err := h.subscriptionService.Get(ctx, cmd.SubscriptionID)
    if err != nil {
        return nil, err // Error is already properly formatted by the service
    }
    
    // 3. Additional validation
    if sub.Status != subscription.StatusActive {
        return nil, ierr.NewError("subscription is not active").
            WithHintf("Subscription %s has status %s", sub.ID, sub.Status).
            Mark(ierr.ErrInvalidOperation)
    }
    
    // 4. Calculate charges using a query handler
    calculationResult, err := h.billingCalculator.Handle(ctx, query.CalculateChargesQuery{
        SubscriptionID: cmd.SubscriptionID,
        PeriodStart:    cmd.PeriodStart,
        PeriodEnd:      cmd.PeriodEnd,
    })
    if err != nil {
        return nil, err
    }
    
    // 5. Create invoice request
    invoiceRequest := &dto.CreateInvoiceRequest{
        CustomerID:     sub.CustomerID,
        SubscriptionID: sub.ID,
        DueDate:        time.Now().Add(30 * 24 * time.Hour), // 30 days from now
        IssueDate:      time.Now(),
        Currency:       calculationResult.Currency,
        LineItems:      append(calculationResult.FixedCharges, calculationResult.UsageCharges...),
        Status:         cmd.IsPreview ? "draft" : "pending",
        // Other fields...
    }
    
    // 6. Create the invoice
    createdInvoice, err := h.invoiceService.Create(ctx, invoiceRequest)
    if err != nil {
        return nil, err
    }
    
    // 7. Log the operation
    h.logger.Infow("invoice generated", 
        "invoice_id", createdInvoice.ID,
        "subscription_id", sub.ID,
        "amount", calculationResult.TotalAmount,
        "is_preview", cmd.IsPreview)
    
    return createdInvoice, nil
}
```

#### When to Use Query Handlers

Query handlers are ideal for complex read operations that don't modify state. They're particularly useful when:

1. **The query is complex** - Involves multiple data sources or complex calculations
2. **The query has specific performance requirements** - Needs caching or optimization
3. **The query logic is reused in multiple places** - Centralizes common query logic

#### Real-World Example: Calculating Subscription Charges

```go
// Query definition - represents a request for information
type CalculateChargesQuery struct {
    SubscriptionID string
    PeriodStart    time.Time
    PeriodEnd      time.Time
}

// Query handler - contains the logic to execute the query
type CalculateChargesHandler struct {
    subscriptionService core.SubscriptionService
    priceService        core.PriceService
    usageService        core.UsageService
    strategyFactory     strategy.BillingStrategyFactory
    cache               cache.Cache
}

// Constructor with dependencies
func NewCalculateChargesHandler(
    subscriptionService core.SubscriptionService,
    priceService core.PriceService,
    usageService core.UsageService,
    strategyFactory strategy.BillingStrategyFactory,
    cache cache.Cache,
) *CalculateChargesHandler {
    return &CalculateChargesHandler{
        subscriptionService: subscriptionService,
        priceService:        priceService,
        usageService:        usageService,
        strategyFactory:     strategyFactory,
        cache:               cache,
    }
}

// Handle method - executes the query
func (h *CalculateChargesHandler) Handle(ctx context.Context, query CalculateChargesQuery) (*BillingCalculationResult, error) {
    // 1. Check cache for existing calculation
    cacheKey := fmt.Sprintf("billing:charges:%s:%s:%s", 
        query.SubscriptionID, 
        query.PeriodStart.Format(time.RFC3339), 
        query.PeriodEnd.Format(time.RFC3339))
    
    if cachedResult, found := h.cache.Get(cacheKey); found {
        return cachedResult.(*BillingCalculationResult), nil
    }
    
    // 2. Get subscription
    sub, err := h.subscriptionService.Get(ctx, query.SubscriptionID)
    if err != nil {
        return nil, err
    }
    
    // 3. Get usage data
    usage, err := h.usageService.GetBySubscription(ctx, query.SubscriptionID, query.PeriodStart, query.PeriodEnd)
    if err != nil {
        return nil, err
    }
    
    // 4. Get appropriate billing strategy based on subscription type
    billingStrategy := h.strategyFactory.GetStrategy(sub.Type)
    
    // 5. Calculate charges using the strategy
    result, err := billingStrategy.CalculateCharges(ctx, sub, usage, query.PeriodStart, query.PeriodEnd)
    if err != nil {
        return nil, err
    }
    
    // 6. Cache the result
    h.cache.Set(cacheKey, result, 15*time.Minute)
    
    return result, nil
}
```

#### Key Differences Between Command and Query Handlers

| Aspect | Command Handlers | Query Handlers |
|--------|-----------------|----------------|
| **Purpose** | Modify state | Read state |
| **Return Value** | Often minimal (success/failure) | Data-rich response |
| **Side Effects** | Expected (DB changes, events) | None (idempotent) |
| **Validation** | Strict business rule validation | Minimal validation |
| **Caching** | Rarely cached | Often cached |
| **Transactions** | Often require transactions | Rarely need transactions |

### 2.4 Strategy Pattern for Variant Behavior

For operations that have multiple implementation strategies (e.g., different billing calculations based on subscription type), use the strategy pattern:

```go
// Strategy interface
type BillingCalculationStrategy interface {
    CalculateCharges(ctx context.Context, sub *subscription.Subscription, usage *usage.Usage, periodStart, periodEnd time.Time) (*BillingCalculationResult, error)
}

// Concrete strategy implementation with dependencies
type UsageBasedBillingStrategy struct {
    priceService core.PriceService
    // Other dependencies as needed
}

// Constructor with dependencies
func NewUsageBasedBillingStrategy(priceService core.PriceService) *UsageBasedBillingStrategy {
    return &UsageBasedBillingStrategy{
        priceService: priceService,
    }
}

// Implementation
func (s *UsageBasedBillingStrategy) CalculateCharges(
    ctx context.Context, 
    sub *subscription.Subscription, 
    usage *usage.Usage, 
    periodStart, 
    periodEnd time.Time,
) (*BillingCalculationResult, error) {
    // Use dependencies to implement the strategy
    prices, err := s.priceService.ListByPlanID(ctx, sub.PlanID)
    if err != nil {
        return nil, err
    }
    
    // Calculate charges based on prices and usage
    // ...
    
    return result, nil
}

// Strategy factory with dependencies
type BillingStrategyFactory struct {
    usageBasedStrategy *UsageBasedBillingStrategy
    fixedPriceStrategy *FixedPriceBillingStrategy
    // Other strategies
}

// Constructor with dependencies
func NewBillingStrategyFactory(
    usageBasedStrategy *UsageBasedBillingStrategy,
    fixedPriceStrategy *FixedPriceBillingStrategy,
) *BillingStrategyFactory {
    return &BillingStrategyFactory{
        usageBasedStrategy: usageBasedStrategy,
        fixedPriceStrategy: fixedPriceStrategy,
    }
}

// Get appropriate strategy based on subscription type
func (f *BillingStrategyFactory) GetStrategy(subscriptionType string) BillingCalculationStrategy {
    switch subscriptionType {
    case "usage_based":
        return f.usageBasedStrategy
    case "fixed_price":
        return f.fixedPriceStrategy
    default:
        return f.fixedPriceStrategy // Default strategy
    }
}
```

This approach:
- Encapsulates variant behavior in separate classes
- Makes it easy to add new strategies without modifying existing code
- Improves testability by isolating specific behaviors

## 3. Dependency Flow Between Service Types

The dependency flow between different types of services should follow a clear hierarchy to prevent cyclic dependencies:

### 3.1 Dependency Hierarchy

The general dependency flow should follow this hierarchy (from high-level to low-level):

1. **API Handlers** - Depend on composite services and application services
2. **Composite Services** - Depend on core domain services and command/query handlers
3. **Command/Query Handlers** - Depend on core domain services and strategies
4. **Core Domain Services** - Depend only on repositories
5. **Strategy Implementations** - Depend on core domain services (when needed)

This hierarchy ensures that dependencies flow in one direction, preventing cyclic dependencies and making the system more maintainable.

### 3.2 Dependency Registration in FX

To make this work with your dependency injection framework (fx), you would register these components like this:

```go
// In main.go or a module file
fx.Provide(
    // Core services
    core.NewSubscriptionService,
    core.NewInvoiceService,
    core.NewPriceService,
    core.NewUsageService,
    
    // Strategies
    strategy.NewUsageBasedBillingStrategy,
    strategy.NewFixedPriceBillingStrategy,
    strategy.NewBillingStrategyFactory,
    
    // Query handlers
    query.NewCalculateChargesHandler,
    
    // Command handlers
    command.NewGenerateInvoiceHandler,
    
    // Composite services
    composite.NewBillingService,
)
```

## 4. Directory Structure

Update the service directory structure to reflect the new organization:

```
internal/
├── service/
│   ├── core/           # Core domain services
│   │   ├── subscription.go
│   │   ├── invoice.go
│   │   └── ...
│   ├── composite/      # Composite services
│   │   ├── billing.go
│   │   ├── payment_processor.go
│   │   └── ...
│   ├── application/    # Application services
│   │   ├── auth.go
│   │   ├── tenant.go
│   │   └── ...
│   ├── command/        # Command handlers
│   │   ├── generate_invoice.go
│   │   ├── process_payment.go
│   │   └── ...
│   ├── query/          # Query handlers
│   │   ├── calculate_charges.go
│   │   └── ...
│   ├── strategy/       # Strategy implementations
│   │   ├── billing/
│   │   │   ├── usage_based.go
│   │   │   ├── fixed_price.go
│   │   │   └── ...
│   │   └── ...
│   └── factory.go      # Service factory 
```

## 5. Example Implementation

### 5.1 Billing Service Refactoring

#### Before

```go
// Current implementation
func (s *billingService) PrepareSubscriptionInvoiceRequest(
    ctx context.Context,
    sub *subscription.Subscription,
    periodStart,
    periodEnd time.Time,
    isPreview bool,
) (*dto.CreateInvoiceRequest, error) {
    // 200+ lines of complex logic
    subscriptionService := NewSubscriptionService(s.ServiceParams)
    // More code...
}
```

#### After

```go
// Command definition
type PrepareInvoiceCommand struct {
    SubscriptionID string
    PeriodStart    time.Time
    PeriodEnd      time.Time
    IsPreview      bool
}

// Command handler
type PrepareInvoiceHandler struct {
    subscriptionService core.SubscriptionService
    invoiceService      core.InvoiceService
    billingCalculator   query.CalculateSubscriptionChargesHandler
    // Other dependencies
}

func (h *PrepareInvoiceHandler) Handle(ctx context.Context, cmd PrepareInvoiceCommand) (*dto.CreateInvoiceRequest, error) {
    // Get subscription
    sub, err := h.subscriptionService.Get(ctx, cmd.SubscriptionID)
    if err != nil {
        return nil, err
    }
    
    // Calculate charges
    charges, err := h.billingCalculator.Handle(ctx, query.CalculateSubscriptionChargesQuery{
        SubscriptionID: cmd.SubscriptionID,
        PeriodStart:    cmd.PeriodStart,
        PeriodEnd:      cmd.PeriodEnd,
    })
    if err != nil {
        return nil, err
    }
    
    // Prepare invoice request
    // Smaller, focused implementation
}

// Composite service using the command handler
type billingService struct {
    prepareInvoiceHandler command.PrepareInvoiceHandler
    // Other handlers and dependencies
}

func (s *billingService) PrepareSubscriptionInvoiceRequest(
    ctx context.Context,
    sub *subscription.Subscription,
    periodStart,
    periodEnd time.Time,
    isPreview bool,
) (*dto.CreateInvoiceRequest, error) {
    return s.prepareInvoiceHandler.Handle(ctx, command.PrepareInvoiceCommand{
        SubscriptionID: sub.ID,
        PeriodStart:    periodStart,
        PeriodEnd:      periodEnd,
        IsPreview:      isPreview,
    })
}
```

## 6. Pragmatic Implementation Plan

Given the immediate need to implement advance arrear logic and proration in a short timeframe (20 man-hours), here's a pragmatic approach that balances architectural improvements with delivery needs:

### 6.1 Phased Implementation Approach

#### Phase 0: Immediate Tactical Implementation (Current Sprint)

For the immediate advance arrear logic and proration features:

1. **Implement the features using the current architecture**
   - Focus on delivering the business functionality first
   - Keep the implementation as clean as possible within the current structure
   - Document areas that would benefit from the new architecture

2. **Apply minimal refactoring**
   - Extract complex calculations into well-named helper methods
   - Ensure proper error handling and validation
   - Add comprehensive tests for the new functionality

3. **Prepare for future refactoring**
   - Add TODO comments indicating future refactoring opportunities
   - Document the business rules and logic for later reference

#### Phase 1: Foundation Setup (Next Sprint)

1. **Update Service Constructors and Dependency Injection (2-3 days)**
   - Modify service constructors to accept dependencies explicitly
   - Update fx provider registration
   - Fix any cyclic dependencies
   - This step provides immediate benefits with minimal risk

2. **Create Directory Structure (1 day)**
   - Set up the new directory structure
   - Move existing services to appropriate directories (core, composite, application)
   - Update imports and references

#### Phase 2: Targeted Refactoring (Subsequent Sprints)

1. **Identify High-Value Targets (1 day)**
   - Analyze services with the most complex methods
   - Prioritize services with the most dependencies
   - Focus on areas with planned feature additions

2. **Refactor One Service at a Time**
   - Start with the billing service (3-4 days)
   - Extract command and query handlers for complex operations
   - Implement strategy pattern for variant behavior
   - Ensure comprehensive test coverage

3. **Expand to Other Services**
   - Apply the same patterns to payment processing (2-3 days)
   - Continue with other complex services

#### Phase 3: Continuous Improvement

1. **Apply Patterns to New Features**
   - Use the new architecture for all new feature development
   - Refactor existing code opportunistically

2. **Documentation and Knowledge Sharing**
   - Update development guidelines
   - Conduct knowledge sharing sessions
   - Ensure consistent application of patterns

### 6.2 Immediate Next Steps (For Advance Arrear Logic)

1. **Implement the advance arrear logic using the current architecture**
   - Focus on delivering the business functionality
   - Keep methods as clean as possible
   - Add comprehensive tests

2. **Document refactoring opportunities**
   - Identify specific methods that would benefit from command/query handlers
   - Note potential strategy implementations for different calculation types

3. **Begin Phase 1 immediately after delivery**
   - Start with updating service constructors to accept dependencies
   - This provides immediate benefits with minimal risk

## 7. Benefits

1. **Improved Maintainability**
   - Smaller, focused components
   - Clear separation of concerns
   - Explicit dependencies

2. **Enhanced Testability**
   - Isolated components
   - Easier mocking of dependencies
   - More targeted tests

3. **Better Extensibility**
   - New strategies can be added without modifying existing code
   - New command and query handlers can be added independently
   - Clear extension points

4. **Reduced Complexity**
   - Simpler individual components
   - More explicit flow of control
   - Better organization of business logic

5. **Elimination of Cyclic Dependencies**
   - Explicit dependency injection
   - Clear dependency hierarchy
   - Compile-time dependency checking

## 8. Conclusion

The proposed architectural improvements address the current challenges while maintaining the core strengths of the existing architecture. By enhancing dependency injection, implementing service composition, applying CQRS for complex operations, and using the strategy pattern for variant behavior, the FlexPrice service layer will become more maintainable, testable, and extensible.

The pragmatic implementation plan allows for immediate delivery of critical features while setting the foundation for incremental architectural improvements. This balanced approach ensures business continuity while progressively improving the codebase.

Remember that architectural improvements should serve the business needs, not the other way around. The phased approach allows for continuous delivery while gradually improving the architecture, ensuring that technical debt is managed without impeding business progress. 