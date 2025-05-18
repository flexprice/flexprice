# Payment Gateway Integration Framework - Dynamic Credential Management

## Overview

This document outlines a simplified approach to implementing the payment gateway integration framework that addresses the unique challenges of dynamic credential management and lazy initialization of payment gateways in a multi-tenant environment.

## Problem Statement

The original payment gateway integration design follows a static initialization approach where gateway instances are created upfront during service startup. This approach presents several challenges:

1. **Multiple Configurations per Tenant**: A single tenant may have multiple API keys/configurations for the same provider (e.g., different Stripe accounts for different business units or regions)
2. **Dynamic Credential Requirements**: API keys and credentials must be fetched at runtime for each request as they belong to our customers
3. **Optional Provider Availability**: Some tenants may not have credentials for certain payment providers
4. **Credential Security**: We must securely handle and store payment gateway credentials

## Goals

- Enable lazy (on-demand) initialization of gateway clients with appropriate credentials
- Support multiple configurations per provider per tenant using existing Connection entity
- Maintain a clean abstraction layer between FlexPrice services and external payment providers
- Support seamless addition of new payment gateway providers
- Ensure high performance through appropriate caching strategies
- Provide robust error handling for various credential and initialization scenarios
- Keep the implementation simple by using existing entities and structures

## Architecture

### 1. Using Existing Connection Model

We'll leverage the existing Connection entity which already has most properties we need:

```go
// Connection represents a connection to an external service
type Connection struct {
    ID              string          // Unique identifier
    TenantID        string          // The tenant this connection belongs to
    ProviderType    types.SecretProvider // "stripe", "razorpay", etc.
    ConnectionCode  string          // Unique code to identify this connection
    Name            string          // User-defined name
    SecretID        string          // ID of the stored credentials
    Metadata        types.Metadata  // Additional metadata
    // Other fields from BaseModel
}
```

### 2. Gateway Factory Pattern

We'll use a factory pattern to create gateway instances on-demand:

```
┌─────────────┐      ┌───────────────┐      ┌─────────────────┐
│  Services   │──────│ Gateway       │──────│ Gateway         │
│ (Payment,   │      │ Manager       │      │ Factory         │
│  Customer)  │      │ (interface)   │      │ (provider impl) │
└─────────────┘      └───────────────┘      └─────────────────┘
                            │                        │
                            │                        │
                      ┌──────────────┐         ┌──────────────┐
                      │  Connection  │         │  Secret      │
                      │  Repository  │         │  Repository  │
                      └──────────────┘         └──────────────┘
```

### 3. Gateway Manager Service

The Gateway Manager service will:

- Act as a single entry point for services to obtain gateway instances
- Create gateways on-demand using connection codes
- Handle caching for performance optimization
- Provide error handling for credential and initialization failures

## Component Design

### 1. Gateway Manager Interface

```go
// GatewayManager provides access to integration gateways using connection codes
type GatewayManager interface {
    // Get a gateway instance using a specific connection code
    GetGatewayByConnectionCode(ctx context.Context, connectionCode string) (integrations.IntegrationGateway, error)

    // Get a gateway instance using provider type
    GetGatewaysByProvider(ctx context.Context, providerType string) ([]integrations.IntegrationGateway, error)

    // Check if a provider has any configurations
    HasProviderConfiguration(ctx context.Context, providerType string) (bool, error)

}
```

### 2. Factory Registry

```go
// Maps provider types to their respective factory functions
type GatewayFactoryRegistry struct {
    factories map[string]integrations.GatewayFactory
}
```

### 3. Using Existing APIs

We'll leverage the existing Connection management APIs:

```
// Create a new connection
POST /api/v1/connections
Body: {
    "name": "Stripe US",
    "connection_code": "stripe_us",
    "provider_type": "stripe",
    "credentials": {
        "api_key": "sk_test_...",
    }
}

// List connections
GET /api/v1/connections?provider_type=stripe

// Get a specific connection
GET /api/v1/connections/{connection_id}

// Update a connection
PUT /api/v1/connections/{connection_id}

// Delete a connection
DELETE /api/v1/connections/{connection_id}
```

## Detailed Implementation

### 1. Gateway Manager Implementation

```go
type gatewayManager struct {
    connectionRepo domainConnection.Repository
    secretRepo     secret.Repository
    logger         *logger.Logger
    cache          *cache.Cache

    // Map of provider types to factory functions
    factories      map[string]integrations.GatewayFactory
}

// NewGatewayManager creates a gateway manager with registered factories
func NewGatewayManager(
    connectionRepo domainConnection.Repository,
    secretRepo secret.Repository,
    logger *logger.Logger,
) GatewayManager {
    manager := &gatewayManager{
        connectionRepo: connectionRepo,
        secretRepo:     secretRepo,
        logger:         logger,
        cache:          cache.New(15*time.Minute, 5*time.Minute),
        factories:      make(map[string]integrations.GatewayFactory),
    }

    // Register factories
    manager.factories["stripe"] = stripe.NewStripeGatewayFactory()
    // Add other providers as needed

    return manager
}

func (m *gatewayManager) GetGatewayByCode(ctx context.Context, connectionCode string) (integrations.IntegrationGateway, error) {
    // Get connection by code
    conn, err := m.connectionRepo.GetByConnectionCode(ctx, connectionCode)
    if err != nil {
        return nil, err
    }

    // Get factory for provider type
    factory, exists := m.factories[string(conn.ProviderType)]
    if !exists {
        return nil, ierr.NewError(fmt.Sprintf("Unsupported provider type: %s", conn.ProviderType)).
            Mark(ierr.ErrValidation)
    }

    // Get credentials from secret service
    secret, err := m.secretRepo.Get(ctx, conn.SecretID)
    if err != nil {
        return nil, err
    }

    // Create gateway instance
    gateway, err := factory(secret.Credentials, m.logger)
    if err != nil {
        return nil, err
    }

    // Cache gateway instance
    m.cache.Set(cacheKey, gateway, cache.DefaultExpiration)

    return gateway, nil
}

func (m *gatewayManager) GetGatewayByProvider(ctx context.Context, providerType string) (integrations.IntegrationGateway, error) {
    // Get connection by provider type
    conn, err := m.connectionRepo.GetByProviderType(ctx, types.SecretProvider(providerType))
    if err != nil {
        return nil, err
    }

    return m.GetGatewayByCode(ctx, conn.ConnectionCode)
}

func (m *gatewayManager) HasProviderConfiguration(ctx context.Context, providerType string) (bool, error) {
    filter := &types.ConnectionFilter{
        ProviderType: types.SecretProvider(providerType),
        QueryFilter: types.QueryFilter{
            Limit: 1,
        },
    }

    connections, err := m.connectionRepo.List(ctx, filter)
    if err != nil {
        return false, err
    }

    return len(connections) > 0, nil
}


```

### 2. Usage in Payment Processing

```go
func (p *paymentProcessor) processPayment(ctx context.Context, payment *payment.Payment, connectionCode string) error {
    // Get gateway for the specified connection
    gateway, err := p.gatewayManager.GetGatewayByCode(ctx, connectionCode)
    if err != nil {
        return err
    }

    // Use the gateway to process payment
    paymentOptions := p.buildPaymentOptions(payment)
    gatewayPaymentID, err := gateway.CreatePayment(ctx, payment, paymentOptions)
    if err != nil {
        return err
    }

    // Update payment with gateway information
    payment.GatewayPaymentID = gatewayPaymentID
    payment.PaymentGateway = string(gateway.GetProviderName())

    return p.paymentRepository.Update(ctx, payment)
}

// Alternative method that automatically selects a connection
func (p *paymentProcessor) processPaymentUsingProvider(ctx context.Context, payment *payment.Payment, providerType string) error {
    // Check if tenant has any configuration for this provider
    hasConfig, err := p.gatewayManager.HasProviderConfiguration(ctx, providerType)
    if err != nil {
        return err
    }

    if !hasConfig {
        return ierr.NewError(fmt.Sprintf("No %s integration configured", providerType)).
            WithHint(fmt.Sprintf("Configure a %s integration first", providerType)).
            Mark(ierr.ErrNotFound)
    }

    // Get gateway instance for this provider
    gateway, err := p.gatewayManager.GetGatewayByProvider(ctx, providerType)
    if err != nil {
        return err
    }

    // Rest of the implementation is the same
    // ...
}
```

### 3. Initializing the Gateway Manager

```go
// Wire up the gateway manager in your dependency injection system
func initPaymentServices(
    connectionRepo domainConnection.Repository,
    secretRepo secret.Repository,
    logger *logger.Logger,
    // ... other dependencies
) {
    // Create the gateway manager
    gatewayManager := NewGatewayManager(connectionRepo, secretRepo, logger)

    // Inject it into services that need it
    paymentProcessor := NewPaymentProcessor(
        paymentRepo,
        gatewayManager,
        logger,
        // ... other dependencies
    )

    // ... rest of your initialization
}
```

## Data Storage

The solution leverages existing tables:

1. **Connections Table**: Using the existing connections table
2. **Secrets Table**: Using the existing secrets table for credentials

## Security Considerations

1. **Encryption at Rest**: Ensure all credentials are encrypted at rest using the existing Secret service
2. **Access Control**: Implement proper access controls for connection management
3. **Audit Logging**: Log all access and changes to connections and credentials
4. **Credential Rotation**: Support credential rotation without service disruption
5. **Minimal Privilege**: Cache only what is needed, avoid storing sensitive data in memory for longer than necessary

## Error Handling

The system should handle common error scenarios:

1. **Missing Connection**: When a connection code doesn't exist
2. **Invalid Credentials**: When credentials are invalid or expired
3. **Rate Limiting**: When a provider enforces rate limits
4. **Provider Downtime**: When a provider service is unavailable

## Performance Optimization

1. **Gateway Caching**: Cache gateway instances to avoid repeated credential lookups
2. **Connection Pooling**: Use connection pooling for provider API clients where applicable

## Implementation Plan

### Phase 1: Core Implementation

1. Implement the GatewayManager service
2. Register factory functions for supported providers

### Phase 2: Service Integration

1. Update payment service to use gateway manager
2. Update customer service to use gateway manager where needed
3. Implement caching with proper invalidation

### Phase 3: Testing and Deployment

1. Create integration tests for the full flow
2. Implement monitoring and observability
3. Prepare deployment and migration plan

## Conclusion

This simplified design leverages existing Connection and Secret entities while providing the flexibility of on-demand gateway initialization. By passing connection codes explicitly to the services that need them, we avoid the complexity of multiple configurations per provider while still supporting that capability when needed.

The implementation is minimally invasive to the existing codebase and provides a clean abstraction for accessing different payment gateways with the appropriate credentials.
