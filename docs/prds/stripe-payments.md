# Payment Gateway Integration Framework

## Overview

This document outlines an extensible framework for integrating external payment gateways into FlexPrice, with Stripe as the first implementation. Rather than building a one-off integration, we'll create a modular system that can support multiple payment providers while maintaining consistency with our core payment processing architecture.

## Goals

- Build an extensible payment gateway integration framework
- Implement Stripe as the first payment gateway provider
- Support entity synchronization between FlexPrice and external systems
- Enable tracking of connection states and syncing histories
- Maintain proper reconciliation with invoices and wallet transactions
- Ensure security and compliance with payment card industry standards

## Current System Architecture

FlexPrice currently supports the following payment methods:
- **Offline/Manual Payments**: Manually recorded payments
- **Wallet Credits**: Using available wallet balance

The payment system is designed around the following core entities:
- `Payment`: Represents a payment transaction from customer to company
- `PaymentAttempt`: Tracks individual payment processing attempts
- `Invoice`: Represents an amount due from a customer
- `Customer`: Represents the entity making the payment

## Proposed Architecture

### 1. Gateway Integration Framework

#### 1.1 Connection Management Layer

We'll introduce a new "Connection" concept to track relationships between FlexPrice entities and external systems:

```
┌─────────────┐      ┌─────────────┐      ┌─────────────┐
│   Entity    │──────│ Connection  │──────│  External   │
│ (Customer,  │      │  (tracks    │      │   System    │
│  Payment)   │      │ sync state) │      │  (Stripe)   │
└─────────────┘      └─────────────┘      └─────────────┘
```

This provides:
- Clear tracking of which entities are synced to which external systems
- Sync state management (pending, synced, failed)
- History of sync attempts and actions
- Error tracking and recovery mechanisms

#### 1.2 Provider Gateway Layer

A dedicated gateway layer will abstract provider-specific functionality using a unified interface:

```
┌─────────────┐      ┌─────────────────────┐      ┌─────────────┐
│             │      │  IntegrationGateway │      │  Provider   │
│  FlexPrice  │──────│   (with Customer,   │──────│  Specific   │
│   Services  │      │  Payment, and other │      │    APIs     │
│             │      │    capabilities)    │      │ (Stripe API)│
└─────────────┘      └─────────────────────┘      └─────────────┘
```

Key components:
- Unified gateway interface with all supported capabilities
- Provider-specific implementations that activate only supported features
- Dynamic provider discovery and initialization
- Error handling and retry logic

### 2. Implementation Details

#### 2.1 Connection Management

1. Create a new domain entity `IntegrationEntity`:

```go
type IntegrationEntity struct {
    ID            string
    TenantID      string
    EntityType    string  // "customer", "payment", etc.
    EntityID      string  // ID of the FlexPrice entity
    ProviderType  string  // "stripe", "razorpay", "hubspot", etc.
    ProviderID    string  // ID in the external system
    SyncStatus    string  // "pending", "synced", "failed"
    LastSyncedAt  *time.Time
    LastErrorMsg  *string
    SyncHistory   []SyncEvent
    Metadata      types.Metadata
    BaseModel
}

type SyncEvent struct {
    Action    string    // "create", "update", "delete"
    Status    string    // "success", "failed"
    Timestamp time.Time
    ErrorMsg  *string
}
```

2. Create connection repository and service:

```
- EntityConnectionRepository: CRUD operations for connections
- SyncService: Manages synchronization logic across providers
```

#### 2.2 Unified Integration Gateway

1. Define a unified gateway interface with capabilities:

```go
// IntegrationCapability represents features an integration can support
type IntegrationCapability string

const (
    CapabilityCustomer      IntegrationCapability = "customer"
    CapabilityPaymentMethod IntegrationCapability = "payment_method"
    CapabilityPayment       IntegrationCapability = "payment"
    CapabilityInvoice       IntegrationCapability = "invoice"
    // Future capabilities can be added here
)

// IntegrationGateway defines the interface for all integration operations
type IntegrationGateway interface {
    // Core methods
    GetProviderName() string
    GetCapabilities() []IntegrationCapability
    SupportsCapability(capability IntegrationCapability) bool
    
    // Customer methods
    CreateCustomer(ctx context.Context, customer *customer.Customer) (string, error)
    UpdateCustomer(ctx context.Context, customer *customer.Customer, providerID string) error
    DeleteCustomer(ctx context.Context, providerID string) error
    GetCustomer(ctx context.Context, providerID string) (map[string]interface{}, error)
    
    // Payment method methods
    CreatePaymentMethod(ctx context.Context, req *dto.CreatePaymentMethodRequest) (string, error)
    ListPaymentMethods(ctx context.Context, customerProviderID string) ([]map[string]interface{}, error)
    DeletePaymentMethod(ctx context.Context, paymentMethodID string) error
    
    // Payment methods
    CreatePayment(ctx context.Context, payment *payment.Payment, options map[string]interface{}) (string, error)
    GetPaymentStatus(ctx context.Context, providerPaymentID string) (types.PaymentStatus, error)
    RefundPayment(ctx context.Context, providerPaymentID string, amount decimal.Decimal) (string, error)
    
    // Webhook methods
    HandleWebhook(ctx context.Context, payload []byte, signature string) error
    
    // Additional methods for other capabilities can be added here
}
```

2. Create a base gateway implementation that returns "not supported" for unsupported features:

```go
// BaseIntegrationGateway provides default implementations that return "not supported" errors
type BaseIntegrationGateway struct {
    ProviderName  string
    Capabilities  []IntegrationCapability
    SecretService secret.Service
    Logger        *logger.Logger
}

func (g *BaseIntegrationGateway) GetProviderName() string {
    return g.ProviderName
}

func (g *BaseIntegrationGateway) GetCapabilities() []IntegrationCapability {
    return g.Capabilities
}

func (g *BaseIntegrationGateway) SupportsCapability(capability IntegrationCapability) bool {
    for _, c := range g.Capabilities {
        if c == capability {
            return true
        }
    }
    return false
}

// Default implementation for unsupported methods
func (g *BaseIntegrationGateway) CreateCustomer(ctx context.Context, customer *customer.Customer) (string, error) {
    return "", ierr.NewError(fmt.Sprintf("%s does not support customer creation", g.ProviderName))
}

// Similar default implementations for all other methods
```

3. Create provider-specific implementations that override only supported features:

```go
// StripeIntegrationGateway implements IntegrationGateway for Stripe
type StripeIntegrationGateway struct {
    BaseIntegrationGateway
    ConnectionRepo EntityConnectionRepository
    // Other dependencies
}

// Initialize with supported capabilities
func NewStripeIntegrationGateway(secretService secret.Service, logger *logger.Logger) *StripeIntegrationGateway {
    return &StripeIntegrationGateway{
        BaseIntegrationGateway: BaseIntegrationGateway{
            ProviderName: "stripe",
            Capabilities: []IntegrationCapability{
                CapabilityCustomer,
                CapabilityPaymentMethod,
                CapabilityPayment,
            },
            SecretService: secretService,
            Logger:        logger,
        },
        // Initialize other dependencies
    }
}

// Override methods for supported capabilities
func (g *StripeIntegrationGateway) CreateCustomer(ctx context.Context, customer *customer.Customer) (string, error) {
    // Stripe-specific implementation
}

// Similarly override other supported methods
```

#### 2.3 Integration Gateway Service

Create a service to discover, initialize and manage all integration gateways:

```go
// IntegrationGatewayService manages all integration gateways
type IntegrationGatewayService interface {
    // Initialize all available gateways based on linked integrations
    InitializeGateways(ctx context.Context) error
    
    // Get a specific gateway by provider name
    GetGateway(ctx context.Context, provider string) (IntegrationGateway, error)
    
    // List available gateways and their capabilities
    ListAvailableGateways(ctx context.Context) ([]GatewayInfo, error)
    
    // Check if a provider is configured
    IsProviderConfigured(ctx context.Context, provider string) bool
}

type GatewayInfo struct {
    ProviderName string
    Capabilities []IntegrationCapability
}

// Implementation
type integrationGatewayService struct {
    secretService secret.Service
    gateways      map[string]IntegrationGateway
    logger        *logger.Logger
}

func NewIntegrationGatewayService(secretService secret.Service, logger *logger.Logger) IntegrationGatewayService {
    return &integrationGatewayService{
        secretService: secretService,
        gateways:      make(map[string]IntegrationGateway),
        logger:        logger,
    }
}

func (s *integrationGatewayService) InitializeGateways(ctx context.Context) error {
    // Get all linked integrations using the SecretService
    providers, err := s.secretService.ListLinkedIntegrations(ctx)
    if err != nil {
        return err
    }
    
    // Initialize gateways for each provider
    for _, provider := range providers {
        switch provider {
        case "stripe":
            s.gateways[provider] = NewStripeIntegrationGateway(s.secretService, s.logger)
        // Add cases for other providers as they're implemented
        default:
            s.logger.Infow("unsupported integration provider", "provider", provider)
        }
    }
    
    return nil
}

func (s *integrationGatewayService) GetGateway(ctx context.Context, provider string) (IntegrationGateway, error) {
    gateway, exists := s.gateways[provider]
    if !exists {
        return nil, ierr.NewError(fmt.Sprintf("integration gateway for %s not found", provider))
    }
    return gateway, nil
}

// Implement other methods
```

#### 2.4 Integration with Existing Payment Service

Enhance the payment processor to work with integration gateways:

```go
func (p *paymentProcessor) handleCardPayment(ctx context.Context, paymentObj *payment.Payment) error {
    // Get the integration gateway service
    integrationService := p.IntegrationGatewayService
    
    // Default to Stripe for card payments
    providerName := "stripe"
    
    // Check if the provider is configured
    if !integrationService.IsProviderConfigured(ctx, providerName) {
        return ierr.NewError(fmt.Sprintf("%s gateway not configured", providerName))
    }
    
    // Get the gateway
    gateway, err := integrationService.GetGateway(ctx, providerName)
    if err != nil {
        return err
    }
    
    // Check if the gateway supports payment capability
    if !gateway.SupportsCapability(CapabilityPayment) {
        return ierr.NewError(fmt.Sprintf("%s does not support payment processing", providerName))
    }
    
    // Process payment through gateway
    gatewayPaymentID, err := gateway.CreatePayment(ctx, paymentObj, nil)
    if err != nil {
        return err
    }
    
    // Create connection record
    connection := &IntegrationEntity{
        EntityType:   "payment",
        EntityID:     paymentObj.ID,
        ProviderType: providerName,
        ProviderID:   gatewayPaymentID,
        SyncStatus:   "synced",
        LastSyncedAt: time.Now().UTC(),
    }
    
    // Save connection
    err = p.ConnectionRepo.Create(ctx, connection)
    if err != nil {
        // Log error but continue since payment was processed
        p.Logger.Errorw("failed to create connection record", "error", err)
    }
    
    // Update payment with gateway ID
    paymentObj.GatewayPaymentID = gatewayPaymentID
    paymentObj.PaymentGateway = providerName
    
    return nil
}
```

### 3. Stripe-Specific Implementation

Implement the Stripe gateway by overriding the base methods:

```go
func (g *StripeIntegrationGateway) CreateCustomer(ctx context.Context, customer *customer.Customer) (string, error) {
    // Get Stripe client
    stripeClient, err := g.getStripeClient(ctx)
    if err != nil {
        return "", err
    }
    
    // Create customer in Stripe
    params := &stripe.CustomerParams{
        Name:  stripe.String(customer.Name),
        Email: stripe.String(customer.Email),
        Metadata: map[string]string{
            "flexPrice_customer_id": customer.ID,
        },
    }
    
    // Add address if available
    if customer.AddressLine1 != "" {
        params.Address = &stripe.AddressParams{
            Line1:      stripe.String(customer.AddressLine1),
            Line2:      stripe.String(customer.AddressLine2),
            City:       stripe.String(customer.AddressCity),
            State:      stripe.String(customer.AddressState),
            PostalCode: stripe.String(customer.AddressPostalCode),
            Country:    stripe.String(customer.AddressCountry),
        }
    }
    
    stripeCustomer, err := stripeClient.Customers.New(params)
    if err != nil {
        return "", g.convertStripeError(err)
    }
    
    return stripeCustomer.ID, nil
}

func (g *StripeIntegrationGateway) HandleWebhook(ctx context.Context, payload []byte, signature string) error {
    // Verify webhook signature
    event, err := g.verifyStripeWebhook(payload, signature)
    if err != nil {
        return err
    }
    
    // Handle different event types
    switch event.Type {
    case "payment_intent.succeeded":
        return g.handlePaymentIntentSucceeded(ctx, event.Data.Object)
    case "payment_intent.payment_failed":
        return g.handlePaymentIntentFailed(ctx, event.Data.Object)
    case "charge.refunded":
        return g.handleChargeRefunded(ctx, event.Data.Object)
    }
    
    return nil
}

func (g *StripeIntegrationGateway) handlePaymentIntentSucceeded(ctx context.Context, data stripe.PaymentIntent) error {
    // Find connection by provider ID
    conn, err := g.connectionRepo.GetByProviderID(ctx, "payment", data.ID, "stripe")
    if err != nil {
        return err
    }
    
    // Update payment status
    payment, err := g.paymentRepo.Get(ctx, conn.EntityID)
    if err != nil {
        return err
    }
    
    payment.PaymentStatus = types.PaymentStatusSucceeded
    succeededAt := time.Now().UTC()
    payment.SucceededAt = &succeededAt
    
    // Update payment
    if err := g.paymentRepo.Update(ctx, payment); err != nil {
        return err
    }
    
    // Handle post-processing (delegate to payment processor)
    return g.paymentProcessor.handlePostProcessing(ctx, payment)
}
```

### 4. Entity Synchronization

Create an entity synchronization service that can work in both synchronous and asynchronous modes:

```go
// EntitySyncService handles entity synchronization with external systems
type EntitySyncService interface {
    // Sync an entity immediately (synchronous)
    SyncEntity(ctx context.Context, entityType string, entityID string, provider string) error
    
    // Queue an entity for synchronization (asynchronous)
    QueueEntitySync(ctx context.Context, entityType string, entityID string, provider string) error
    
    // Process sync queue (for background workers)
    ProcessSyncQueue(ctx context.Context) error
    
    // Retry failed syncs
    RetryFailedSyncs(ctx context.Context, limit int) error
}

// Implementation
type entitySyncService struct {
    connectionRepo         EntityConnectionRepository
    integrationGatewayService IntegrationGatewayService
    // Repositories for different entity types
    customerRepo           customer.Repository
    paymentRepo            payment.Repository
    // Temporal client for async workflows (future)
    // temporalClient         client.Client
    logger                 *logger.Logger
}

func (s *entitySyncService) SyncEntity(ctx context.Context, entityType string, entityID string, provider string) error {
    // Get the entity
    entity, err := s.getEntityByType(ctx, entityType, entityID)
    if err != nil {
        return err
    }
    
    // Get the integration gateway
    gateway, err := s.integrationGatewayService.GetGateway(ctx, provider)
    if err != nil {
        return err
    }
    
    // Check capability support
    capability := IntegrationCapability(entityType)
    if !gateway.SupportsCapability(capability) {
        return ierr.NewError(fmt.Sprintf("%s does not support %s synchronization", provider, entityType))
    }
    
    // Get existing connection or create new one
    connection, err := s.connectionRepo.GetByEntityAndProvider(ctx, entityType, entityID, provider)
    isNew := err != nil && ierr.IsNotFound(err)
    if isNew {
        connection = &IntegrationEntity{
            ID:           types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CONNECTION),
            EntityType:   entityType,
            EntityID:     entityID,
            ProviderType: provider,
            SyncStatus:   "pending",
            SyncHistory:  []SyncEvent{},
            Metadata:     types.Metadata{},
            BaseModel:    types.GetDefaultBaseModel(ctx),
        }
    } else if err != nil {
        return err
    }
    
    // Perform sync based on entity type
    var providerID string
    syncErr := error(nil)
    action := "create"
    
    if !isNew && connection.ProviderID != "" {
        action = "update"
    }
    
    // Perform the appropriate action based on entity type
    if entityType == "customer" {
        if isNew || connection.ProviderID == "" {
            providerID, syncErr = gateway.CreateCustomer(ctx, entity.(*customer.Customer))
        } else {
            syncErr = gateway.UpdateCustomer(ctx, entity.(*customer.Customer), connection.ProviderID)
            providerID = connection.ProviderID
        }
    } else if entityType == "payment" {
        // Similar logic for payments, etc.
    }
    
    // Update connection record
    now := time.Now().UTC()
    syncEvent := SyncEvent{
        Action:    action,
        Timestamp: now,
        Status:    "success",
    }
    
    if syncErr != nil {
        syncEvent.Status = "failed"
        syncEvent.ErrorMsg = lo.ToPtr(syncErr.Error())
        connection.SyncStatus = "failed"
        connection.LastErrorMsg = lo.ToPtr(syncErr.Error())
    } else {
        connection.SyncStatus = "synced"
        connection.ProviderID = providerID
        connection.LastSyncedAt = &now
    }
    
    connection.SyncHistory = append(connection.SyncHistory, syncEvent)
    
    // Save or update the connection
    if isNew {
        err = s.connectionRepo.Create(ctx, connection)
    } else {
        err = s.connectionRepo.Update(ctx, connection)
    }
    
    if err != nil {
        return err
    }
    
    // Return the sync error if any
    return syncErr
}

// Future: Implement async processing using Temporal
func (s *entitySyncService) QueueEntitySync(ctx context.Context, entityType string, entityID string, provider string) error {
    // For now, just do synchronous processing
    // In the future, this will queue a task in Temporal
    return s.SyncEntity(ctx, entityType, entityID, provider)
}
```

### 5. Integration Plan

#### Phase 1: Framework and Connection Model
- Create IntegrationEntity entity and repository
- Implement the base IntegrationGateway interface
- Create the IntegrationGatewayService for dynamic gateway discovery
- Set up EntitySyncService for synchronous entity syncs

#### Phase 2: Stripe Integration
- Implement StripeIntegrationGateway with all capabilities
- Create webhook handling for Stripe events
- Integrate with customer service for Stripe customer creation
- Add payment method management

#### Phase 3: Integration with Payment Processor
- Enhance payment processor to use IntegrationGateways
- Update payment handling for card payments
- Implement connection tracking for payments
- Add webhook API endpoint for Stripe events

#### Phase 4: Entity Synchronization
- Implement entity synchronization for customers and payments
- Add APIs for manual sync triggers
- Build sync status tracking and reporting
- Create retry mechanisms for failed syncs

#### Phase 5: Testing and Deployment
- Implement integration tests for Stripe flows
- Set up monitoring and error reporting
- Deploy to staging environment
- Conduct user acceptance testing

#### Future Phase: Asynchronous Processing
- Integrate with Temporal for asynchronous processing
- Convert entity syncs to use workflow-based approach
- Implement background workers for sync queue processing
- Add monitoring and observability for async workflows

## Technical Considerations

### 1. Error Handling and Recovery

The connection model allows for tracking sync status and recovering from errors:

```go
func (s *entitySyncService) RetryFailedSyncs(ctx context.Context, limit int) error {
    filter := &IntegrationEntityFilter{
        SyncStatus: "failed",
        Limit:      limit,
    }
    
    connections, err := s.connectionRepo.List(ctx, filter)
    if err != nil {
        return err
    }
    
    for _, conn := range connections {
        s.logger.Infow("retrying failed sync", 
            "entity_type", conn.EntityType, 
            "entity_id", conn.EntityID,
            "provider", conn.ProviderType)
            
        err := s.SyncEntity(ctx, conn.EntityType, conn.EntityID, conn.ProviderType)
        if err != nil {
            s.logger.Errorw("retry failed", 
                "error", err,
                "entity_type", conn.EntityType, 
                "entity_id", conn.EntityID)
        }
    }
    
    return nil
}
```

### 2. Idempotency

Ensure operations are idempotent across systems:

```go
func (g *StripeIntegrationGateway) CreatePayment(ctx context.Context, payment *payment.Payment, options map[string]interface{}) (string, error) {
    // ... other code ...
    
    // Generate idempotency key if not present
    idempotencyKey := payment.IdempotencyKey
    if idempotencyKey == nil || *idempotencyKey == "" {
        key := fmt.Sprintf("payment_%s_%s", payment.ID, time.Now().UTC().Format(time.RFC3339))
        idempotencyKey = &key
    }
    
    params := &stripe.PaymentIntentParams{
        // ... other params ...
    }
    
    // Set idempotency key to avoid duplicate charges
    params.IdempotencyKey = stripe.String(*idempotencyKey)
    
    // ... rest of the function ...
}
```

### 3. Transaction Management

Maintain consistency between systems:

```go
func (s *customerService) CreateCustomerWithIntegrations(ctx context.Context, req dto.CreateCustomerRequest) (*dto.CustomerResponse, error) {
    var customerResp *dto.CustomerResponse
    
    // Use transaction to ensure consistency
    err := s.db.WithTx(ctx, func(txCtx context.Context) error {
        // Create customer in FlexPrice
        cust := req.ToCustomer(txCtx)
        if err := s.repo.Create(txCtx, cust); err != nil {
            return err
        }
        
        // Get available gateways that support customer capability
        gateways, err := s.integrationGatewayService.ListAvailableGateways(txCtx)
        if err != nil {
            return err
        }
        
        // Queue customer sync for each supported gateway
        for _, gwInfo := range gateways {
            if lo.Contains(gwInfo.Capabilities, CapabilityCustomer) {
                if err := s.entitySyncService.QueueEntitySync(txCtx, "customer", cust.ID, gwInfo.ProviderName); err != nil {
                    s.logger.Warnw("failed to queue customer sync", 
                        "error", err, 
                        "customer_id", cust.ID, 
                        "provider", gwInfo.ProviderName)
                    // Continue with other providers even if one fails
                }
            }
        }
        
        customerResp = &dto.CustomerResponse{Customer: cust}
        return nil
    })
    
    return customerResp, err
}
```

## Security Considerations

1. **API Key Management**: Continue using the Secret service for secure API key storage
2. **PCI Compliance**: Never handle raw card data in the FlexPrice backend
3. **Webhook Signature Verification**: Validate all incoming webhooks
4. **Audit Logging**: Track all sync operations and payment activities
5. **Error Handling**: Ensure sensitive data isn't exposed in error messages

## Future Extensibility

This architecture can be extended to support:

1. **Additional Integration Types**: Beyond payment gateways to CRM, marketing, etc.
2. **Asynchronous Processing**: Move from synchronous to asynchronous entity syncs
3. **Multi-Gateway Support**: Allow different customers to use different payment providers
4. **Conflict Resolution**: Add sophisticated logic to handle conflicts between systems
5. **Bidirectional Sync**: Support updates originating from external systems

## Metrics and Monitoring

1. **Sync Status**: Track percentage of entities successfully synced
2. **Gateway Performance**: Monitor API call latency and error rates
3. **Payment Processing**: Track success/failure rates per gateway
4. **Connection Health**: Monitor connection status across providers
5. **Error Categorization**: Track common failure patterns
