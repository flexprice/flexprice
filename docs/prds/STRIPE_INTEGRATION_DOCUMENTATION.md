# FlexPrice Stripe Integration Documentation

## Table of Contents
1. [Overview](#overview)
2. [Architecture](#architecture)
3. [Customer Sync](#customer-sync)
4. [Plan Sync](#plan-sync)
5. [Invoice Sync](#invoice-sync)
6. [Subscription Sync](#subscription-sync)
7. [Entity Integration Mapping](#entity-integration-mapping)
8. [Webhook Handling](#webhook-handling)
9. [Configuration](#configuration)
10. [Data Flow Diagrams](#data-flow-diagrams)

## Overview

FlexPrice provides comprehensive bidirectional synchronization with Stripe, enabling seamless integration between FlexPrice's billing system and Stripe's payment processing platform. The integration supports:

- **Customer Management**: Bidirectional sync of customer data
- **Plan Management**: Sync Stripe products as FlexPrice plans
- **Invoice Management**: Sync FlexPrice invoices to Stripe for payment processing
- **Subscription Management**: Bidirectional sync of subscription data
- **Payment Processing**: Handle payments through Stripe checkout and webhooks
- **Real-time Updates**: Webhook-based real-time synchronization

## Architecture

### Core Components

```
┌─────────────────────────────────────────────────────────────────┐
│                    FlexPrice Core System                        │
├─────────────────────────────────────────────────────────────────┤
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐  │
│  │   Customer      │  │      Plan       │  │    Invoice      │  │
│  │   Service       │  │    Service      │  │    Service      │  │
│  └─────────────────┘  └─────────────────┘  └─────────────────┘  │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐  │
│  │ Subscription    │  │    Payment      │  │   Connection    │  │
│  │   Service       │  │    Service      │  │    Service      │  │
│  └─────────────────┘  └─────────────────┘  └─────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
                               │
                               ▼
┌─────────────────────────────────────────────────────────────────┐
│                Integration Factory & Services                   │
├─────────────────────────────────────────────────────────────────┤
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐  │
│  │    Stripe       │  │    Stripe       │  │    Stripe       │  │
│  │   Customer      │  │     Plan        │  │   Invoice       │  │
│  │   Service       │  │   Service       │  │    Sync         │  │
│  └─────────────────┘  └─────────────────┘  └─────────────────┘  │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐  │
│  │    Stripe       │  │    Stripe       │  │    Webhook      │  │
│  │ Subscription    │  │   Payment       │  │    Handler      │  │
│  │   Service       │  │   Service       │  │                 │  │
│  └─────────────────┘  └─────────────────┘  └─────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
                               │
                               ▼
┌─────────────────────────────────────────────────────────────────┐
│                    Entity Integration Mapping                   │
├─────────────────────────────────────────────────────────────────┤
│  Maps FlexPrice entities to Stripe entities for sync tracking  │
│  - Customer ID ↔ Stripe Customer ID                            │
│  - Plan ID ↔ Stripe Product ID                                 │
│  - Invoice ID ↔ Stripe Invoice ID                              │
│  - Subscription ID ↔ Stripe Subscription ID                    │
└─────────────────────────────────────────────────────────────────┘
                               │
                               ▼
┌─────────────────────────────────────────────────────────────────┐
│                        Stripe API                              │
└─────────────────────────────────────────────────────────────────┘
```

### Integration Factory Pattern

The integration uses a factory pattern to provide configured Stripe services:

```go
type StripeIntegration struct {
    Client         *stripe.Client
    CustomerSvc    *stripe.CustomerService
    PaymentSvc     *stripe.PaymentService
    InvoiceSyncSvc *stripe.InvoiceSyncService
    WebhookHandler *webhook.Handler
}
```

## Customer Sync

### Outbound Sync (FlexPrice → Stripe)

#### Flow Diagram
```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   FlexPrice     │    │      Check      │    │    Create       │
│   Customer      │───▶│   Existing      │───▶│   Customer      │
│   Created       │    │   Mapping       │    │   in Stripe     │
└─────────────────┘    └─────────────────┘    └─────────────────┘
                                │                       │
                                ▼                       ▼
                       ┌─────────────────┐    ┌─────────────────┐
                       │   Customer      │    │    Update       │
                       │   Already       │    │   Customer      │
                       │   Synced        │    │   Metadata      │
                       └─────────────────┘    └─────────────────┘
                                                       │
                                                       ▼
                                              ┌─────────────────┐
                                              │    Create       │
                                              │   Integration   │
                                              │    Mapping      │
                                              └─────────────────┘
```

#### Key Functions

**EnsureCustomerSyncedToStripe**
```go
func (s *CustomerService) EnsureCustomerSyncedToStripe(ctx context.Context, customerID string, customerService interfaces.CustomerService) (*dto.CustomerResponse, error)
```

**Process:**
1. Check if customer has `stripe_customer_id` in metadata
2. Check entity integration mapping table
3. If not synced, create customer in Stripe
4. Update FlexPrice customer metadata with Stripe ID
5. Create entity integration mapping

**CreateCustomerInStripe**
```go
func (s *CustomerService) CreateCustomerInStripe(ctx context.Context, customerID string, customerService interfaces.CustomerService) error
```

**Stripe Customer Payload:**
```go
params := &stripe.CustomerCreateParams{
    Name:  stripe.String(ourCustomer.Name),
    Email: stripe.String(ourCustomer.Email),
    Metadata: map[string]string{
        "flexprice_customer_id": ourCustomer.ID,
        "flexprice_environment": ourCustomer.EnvironmentID,
        "external_id":           ourCustomer.ExternalID,
    },
}
```

### Inbound Sync (Stripe → FlexPrice)

#### Webhook Flow
```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│     Stripe      │    │    Webhook      │    │     Check       │
│   customer.     │───▶│    Handler      │───▶│   Sync Config   │
│   created       │    │                 │    │   Enabled       │
└─────────────────┘    └─────────────────┘    └─────────────────┘
                                                       │
                                                       ▼
                                              ┌─────────────────┐
                                              │    Create       │
                                              │   Customer      │
                                              │  in FlexPrice   │
                                              └─────────────────┘
                                                       │
                                                       ▼
                                              ┌─────────────────┐
                                              │    Create       │
                                              │   Integration   │
                                              │    Mapping      │
                                              └─────────────────┘
```

**CreateCustomerFromStripe**
```go
func (s *CustomerService) CreateCustomerFromStripe(ctx context.Context, stripeCustomer *stripe.Customer, environmentID string, customerService interfaces.CustomerService) error
```

**Process:**
1. Check if customer already exists by `flexprice_customer_id` metadata
2. Create new customer in FlexPrice with Stripe data
3. Set `stripe_customer_id` in customer metadata
4. Create entity integration mapping

## Plan Sync

### Inbound Sync (Stripe → FlexPrice)

#### Flow Diagram
```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│     Stripe      │    │    Webhook      │    │     Check       │
│   product.      │───▶│    Handler      │───▶│   Existing      │
│   created       │    │                 │    │    Mapping      │
└─────────────────┘    └─────────────────┘    └─────────────────┘
                                                       │
                                                       ▼
                                              ┌─────────────────┐
                                              │     Fetch       │
                                              │    Product      │
                                              │  from Stripe    │
                                              └─────────────────┘
                                                       │
                                                       ▼
                                              ┌─────────────────┐
                                              │    Create       │
                                              │     Plan        │
                                              │  in FlexPrice   │
                                              └─────────────────┘
                                                       │
                                                       ▼
                                              ┌─────────────────┐
                                              │    Create       │
                                              │   Integration   │
                                              │    Mapping      │
                                              └─────────────────┘
```

#### Key Functions

**CreatePlan**
```go
func (s *stripePlanService) CreatePlan(ctx context.Context, planID string, services *ServiceDependencies) (string, error)
```

**Process:**
1. Check if plan already exists in entity mapping
2. Fetch Stripe product details
3. Create FlexPrice plan with Stripe product data
4. Create entity integration mapping

**Plan Creation Payload:**
```go
createPlanReq := dto.CreatePlanRequest{
    Name:         stripeProduct.Name,
    Description:  stripeProduct.Description,
    LookupKey:    planID,
    Prices:       []dto.CreatePlanPriceRequest{},
    Entitlements: []dto.CreatePlanEntitlementRequest{},
    CreditGrants: []dto.CreateCreditGrantRequest{},
    Metadata: types.Metadata{
        "source":            "stripe",
        "stripe_plan_id":    planID,
        "stripe_product_id": stripeProduct.ID,
    },
}
```

**UpdatePlan & DeletePlan**
- Similar flows for product.updated and product.deleted webhooks
- Update/delete FlexPrice plan based on Stripe product changes
- Maintain entity integration mappings

## Invoice Sync

### Outbound Sync (FlexPrice → Stripe)

#### Flow Diagram
```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   FlexPrice     │    │     Check       │    │    Ensure       │
│   Invoice       │───▶│   Stripe        │───▶│   Customer      │
│   Created       │    │  Connection     │    │    Synced       │
└─────────────────┘    └─────────────────┘    └─────────────────┘
                                                       │
                                                       ▼
                                              ┌─────────────────┐
                                              │     Check       │
                                              │   Existing      │
                                              │    Mapping      │
                                              └─────────────────┘
                                                       │
                                                       ▼
                                              ┌─────────────────┐
                                              │    Create       │
                                              │     Draft       │
                                              │   Invoice       │
                                              └─────────────────┘
                                                       │
                                                       ▼
                                              ┌─────────────────┐
                                              │     Sync        │
                                              │   Line Items    │
                                              │   to Stripe     │
                                              └─────────────────┘
                                                       │
                                                       ▼
                                              ┌─────────────────┐
                                              │   Finalize      │
                                              │    Invoice      │
                                              │   in Stripe     │
                                              └─────────────────┘
```

#### Key Functions

**SyncInvoiceToStripe**
```go
func (s *InvoiceSyncService) SyncInvoiceToStripe(ctx context.Context, req StripeInvoiceSyncRequest, customerService interfaces.CustomerService) (*StripeInvoiceSyncResponse, error)
```

**7-Step Process:**
1. **Check Stripe Connection** - Verify Stripe integration is configured
2. **Get FlexPrice Invoice** - Retrieve invoice details
3. **Check Existing Mapping** - Avoid duplicate syncing
4. **Create Draft Invoice** - Create draft invoice in Stripe
5. **Create Entity Mapping** - Track sync relationship
6. **Sync Line Items** - Add all invoice line items
7. **Finalize Invoice** - Make invoice ready for payment

**Invoice Creation Parameters:**
```go
params := &stripe.InvoiceCreateParams{
    Customer:    stripe.String(stripeCustomerID),
    Currency:    stripe.String(strings.ToLower(flexInvoice.Currency)),
    AutoAdvance: stripe.Bool(true),
    Description: stripe.String(flexInvoice.Description),
    Metadata: map[string]string{
        "flexprice_invoice_id":     flexInvoice.ID,
        "flexprice_customer_id":    flexInvoice.CustomerID,
        "flexprice_invoice_number": flexInvoice.InvoiceNumber,
        "sync_source":              "flexprice",
    },
}
```

**Collection Methods:**
- `charge_automatically` - Automatic payment attempt
- `send_invoice` - Email invoice to customer

### Payment Reconciliation

#### External Payment Processing
```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│     Stripe      │    │    Webhook      │    │     Find        │
│   invoice.      │───▶│    Handler      │───▶│   FlexPrice     │
│     paid        │    │                 │    │    Invoice      │
└─────────────────┘    └─────────────────┘    └─────────────────┘
                                                       │
                                                       ▼
                                              ┌─────────────────┐
                                              │    Create       │
                                              │    Payment      │
                                              │    Record       │
                                              └─────────────────┘
                                                       │
                                                       ▼
                                              ┌─────────────────┐
                                              │   Reconcile     │
                                              │   with Invoice  │
                                              │     Balance     │
                                              └─────────────────┘
```

## Subscription Sync

### Inbound Sync (Stripe → FlexPrice)

#### Flow Diagram
```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│     Stripe      │    │    Webhook      │    │    Create/      │
│ subscription.   │───▶│    Handler      │───▶│     Find        │
│   created       │    │                 │    │   Customer      │
└─────────────────┘    └─────────────────┘    └─────────────────┘
                                                       │
                                                       ▼
                                              ┌─────────────────┐
                                              │    Create/      │
                                              │     Find        │
                                              │     Plan        │
                                              └─────────────────┘
                                                       │
                                                       ▼
                                              ┌─────────────────┐
                                              │    Create       │
                                              │ Subscription    │
                                              │  in FlexPrice   │
                                              └─────────────────┘
                                                       │
                                                       ▼
                                              ┌─────────────────┐
                                              │    Create       │
                                              │   Integration   │
                                              │    Mapping      │
                                              └─────────────────┘
```

#### Key Functions

**CreateSubscription**
```go
func (s *stripeSubscriptionService) CreateSubscription(ctx context.Context, stripeSubscriptionID string, services *ServiceDependencies) (*dto.SubscriptionResponse, error)
```

**Process:**
1. Fetch Stripe subscription data
2. Check for existing mapping
3. Create or find customer
4. Create or find plan
5. Create FlexPrice subscription
6. Create entity integration mapping

**Billing Cycle Calculation:**
```go
func (s *stripeSubscriptionService) calculateBillingCycle(stripeSub *stripe.Subscription) types.BillingCycle {
    // Calendar billing vs Anniversary billing based on billing_cycle_anchor
    if stripeSub.BillingCycleAnchor != 0 {
        subscriptionStart := time.Unix(stripeSub.StartDate, 0)
        billingAnchor := time.Unix(stripeSub.BillingCycleAnchor, 0)
        
        if !subscriptionStart.Equal(billingAnchor) {
            return types.BillingCycleCalendar
        }
    }
    return types.BillingCycleAnniversary
}
```

### Plan Change Handling

#### Plan Change Flow
```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│     Stripe      │    │     Check       │    │    Cancel       │
│ subscription.   │───▶│     Plan        │───▶│    Existing     │
│   updated       │    │    Change       │    │  Subscription   │
└─────────────────┘    └─────────────────┘    └─────────────────┘
                                                       │
                                                       ▼
                                              ┌─────────────────┐
                                              │    Create       │
                                              │      New        │
                                              │  Subscription   │
                                              └─────────────────┘
                                                       │
                                                       ▼
                                              ┌─────────────────┐
                                              │    Update       │
                                              │   Integration   │
                                              │    Mapping      │
                                              └─────────────────┘
```

**Plan Change Detection:**
```go
func (s *stripeSubscriptionService) isPlanChange(ctx context.Context, existingSubscription *dto.SubscriptionResponse, stripeSubscription *stripe.Subscription, services *ServiceDependencies) (bool, error)
```

## Entity Integration Mapping

### Purpose
Entity Integration Mapping provides a bidirectional lookup table between FlexPrice entities and Stripe entities, enabling:
- Sync status tracking
- Avoiding duplicate syncing
- Efficient lookups during webhook processing
- Metadata storage for sync context

### Structure
```go
type EntityIntegrationMapping struct {
    ID               string                     // Unique mapping ID
    EntityID         string                     // FlexPrice entity ID
    EntityType       types.IntegrationEntityType // customer, plan, invoice, subscription
    ProviderType     string                     // "stripe"
    ProviderEntityID string                     // Stripe entity ID
    Metadata         map[string]interface{}     // Sync metadata
    EnvironmentID    string                     // Environment context
}
```

### Supported Entity Types
- `customer` - FlexPrice Customer ↔ Stripe Customer
- `plan` - FlexPrice Plan ↔ Stripe Product
- `invoice` - FlexPrice Invoice ↔ Stripe Invoice
- `subscription` - FlexPrice Subscription ↔ Stripe Subscription

### Usage Examples

**Creating Mapping:**
```go
mapping := &entityintegrationmapping.EntityIntegrationMapping{
    ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
    EntityID:         flexpriceCustomerID,
    EntityType:       types.IntegrationEntityTypeCustomer,
    ProviderType:     "stripe",
    ProviderEntityID: stripeCustomerID,
    Metadata: map[string]interface{}{
        "created_via":           "flexprice_to_provider",
        "stripe_customer_email": customer.Email,
        "synced_at":             time.Now().UTC().Format(time.RFC3339),
    },
}
```

**Finding Mapping:**
```go
filter := &types.EntityIntegrationMappingFilter{
    EntityID:      customerID,
    EntityType:    types.IntegrationEntityTypeCustomer,
    ProviderTypes: []string{"stripe"},
}
mappings, err := repo.List(ctx, filter)
```

## Webhook Handling

### Supported Webhook Events

| Stripe Event | Handler Function | Description |
|--------------|------------------|-------------|
| `customer.created` | `handleCustomerCreated` | Create customer in FlexPrice |
| `product.created` | `handleProductCreated` | Create plan in FlexPrice |
| `product.updated` | `handleProductUpdated` | Update plan in FlexPrice |
| `product.deleted` | `handleProductDeleted` | Delete plan in FlexPrice |
| `subscription.created` | `handleSubscriptionCreated` | Create subscription in FlexPrice |
| `subscription.updated` | `handleSubscriptionUpdated` | Update subscription in FlexPrice |
| `subscription.deleted` | `handleSubscriptionCancellation` | Cancel subscription in FlexPrice |
| `payment_intent.succeeded` | `handlePaymentIntentSucceeded` | Process successful payment |
| `payment_intent.payment_failed` | `handlePaymentIntentPaymentFailed` | Handle failed payment |
| `invoice.paid` | `handleInvoicePaymentPaid` | Process external invoice payment |
| `setup_intent.succeeded` | `handleSetupIntentSucceeded` | Handle payment method setup |

### Webhook Processing Flow
```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│     Stripe      │    │    Webhook      │    │     Check       │
│    Webhook      │───▶│   Signature     │───▶│   Sync Config   │
│     Event       │    │  Verification   │    │    Settings     │
└─────────────────┘    └─────────────────┘    └─────────────────┘
                                                       │
                                                       ▼
                                              ┌─────────────────┐
                                              │     Route       │
                                              │      to         │
                                              │    Handler      │
                                              └─────────────────┘
                                                       │
                                                       ▼
                                              ┌─────────────────┐
                                              │    Process      │
                                              │     Event       │
                                              │     Data        │
                                              └─────────────────┘
```

### Sync Configuration

Each webhook handler checks sync configuration before processing:

```go
conn, err := h.getConnection(ctx)
if !conn.IsCustomerInboundEnabled() {
    // Skip processing
    return nil
}
```

**Configuration Options:**
- `IsCustomerInboundEnabled()` - Stripe → FlexPrice customer sync
- `IsPlanInboundEnabled()` - Stripe → FlexPrice plan sync
- `IsSubscriptionInboundEnabled()` - Stripe → FlexPrice subscription sync
- `IsInvoiceInboundEnabled()` - Stripe → FlexPrice invoice sync
- `IsInvoiceOutboundEnabled()` - FlexPrice → Stripe invoice sync

## Configuration

### Connection Setup

**Stripe Connection Metadata:**
```go
type StripeConnectionMetadata struct {
    PublishableKey string `json:"publishable_key"`
    SecretKey      string `json:"secret_key"`
    WebhookSecret  string `json:"webhook_secret"`
    AccountID      string `json:"account_id,omitempty"`
}
```

**Sync Configuration:**
```go
type SyncConfig struct {
    CustomerInboundEnabled    bool `json:"customer_inbound_enabled"`
    CustomerOutboundEnabled   bool `json:"customer_outbound_enabled"`
    PlanInboundEnabled        bool `json:"plan_inbound_enabled"`
    PlanOutboundEnabled       bool `json:"plan_outbound_enabled"`
    InvoiceInboundEnabled     bool `json:"invoice_inbound_enabled"`
    InvoiceOutboundEnabled    bool `json:"invoice_outbound_enabled"`
    SubscriptionInboundEnabled  bool `json:"subscription_inbound_enabled"`
    SubscriptionOutboundEnabled bool `json:"subscription_outbound_enabled"`
}
```

### Environment Variables
- Stripe API keys are stored encrypted in the connection configuration
- Webhook endpoints are configured in Stripe dashboard
- Environment-specific configurations supported

## Data Flow Diagrams

### Complete Integration Flow

```
┌─────────────────────────────────────────────────────────────────┐
│                        FlexPrice System                         │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐              │
│  │  Customer   │  │    Plan     │  │   Invoice   │              │
│  │   Created   │  │   Created   │  │   Created   │              │
│  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘              │
│         │                │                │                     │
│         ▼                ▼                ▼                     │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐              │
│  │   Ensure    │  │  Plan Sync  │  │   Invoice   │              │
│  │  Customer   │  │   (N/A)     │  │    Sync     │              │
│  │   Synced    │  │             │  │  to Stripe  │              │
│  └──────┬──────┘  └─────────────┘  └──────┬──────┘              │
│         │                                 │                     │
│         ▼                                 ▼                     │
│  ┌─────────────┐                   ┌─────────────┐              │
│  │   Create    │                   │   Create    │              │
│  │  Customer   │                   │   Draft     │              │
│  │ in Stripe   │                   │  Invoice    │              │
│  └──────┬──────┘                   └──────┬──────┘              │
│         │                                 │                     │
│         ▼                                 ▼                     │
│  ┌─────────────┐                   ┌─────────────┐              │
│  │   Update    │                   │    Add      │              │
│  │  Metadata   │                   │ Line Items  │              │
│  │ & Mapping   │                   │ & Finalize  │              │
│  └─────────────┘                   └──────┬──────┘              │
│                                           │                     │
│                                           ▼                     │
│                                    ┌─────────────┐              │
│                                    │   Create    │              │
│                                    │  Mapping    │              │
│                                    └─────────────┘              │
└─────────────────────────────────────────────────────────────────┘
                               │
                               ▼
┌─────────────────────────────────────────────────────────────────┐
│                         Stripe System                          │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐              │
│  │  Customer   │  │   Product   │  │   Invoice   │              │
│  │   Exists    │  │   Created   │  │   Created   │              │
│  └─────────────┘  └──────┬──────┘  └─────────────┘              │
│                          │                                      │
│                          ▼                                      │
│                   ┌─────────────┐                               │
│                   │   Webhook   │                               │
│                   │   Fired     │                               │
│                   └──────┬──────┘                               │
│                          │                                      │
│                          ▼                                      │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐              │
│  │ Subscription│  │   Payment   │  │   Invoice   │              │
│  │   Created   │  │  Processed  │  │    Paid     │              │
│  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘              │
│         │                │                │                     │
│         ▼                ▼                ▼                     │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐              │
│  │   Webhook   │  │   Webhook   │  │   Webhook   │              │
│  │   to FP     │  │   to FP     │  │   to FP     │              │
│  └─────────────┘  └─────────────┘  └─────────────┘              │
└─────────────────────────────────────────────────────────────────┘
                               │
                               ▼
┌─────────────────────────────────────────────────────────────────┐
│                    FlexPrice Webhook Handler                    │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐              │
│  │   Create    │  │   Create    │  │   Create    │              │
│  │Subscription │  │   Payment   │  │   Payment   │              │
│  │ in FlexPrice│  │   Record    │  │   Record    │              │
│  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘              │
│         │                │                │                     │
│         ▼                ▼                ▼                     │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐              │
│  │   Create    │  │  Reconcile  │  │  Reconcile  │              │
│  │  Mapping    │  │    with     │  │    with     │              │
│  └─────────────┘  │   Invoice   │  │   Invoice   │              │
│                   └─────────────┘  └─────────────┘              │
└─────────────────────────────────────────────────────────────────┘
```

### Payment Processing Flow

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   FlexPrice     │    │     Stripe      │    │   FlexPrice     │
│    Invoice      │───▶│    Checkout     │───▶│    Payment      │
│    Created      │    │    Session      │    │    Intent       │
└─────────────────┘    └─────────────────┘    └─────────────────┘
                                                       │
                                                       ▼
                                              ┌─────────────────┐
                                              │   Customer      │
                                              │   Completes     │
                                              │   Payment       │
                                              └─────────────────┘
                                                       │
                                                       ▼
                                              ┌─────────────────┐
                                              │     Stripe      │
                                              │   Webhook:      │
                                              │ payment_intent. │
                                              │   succeeded     │
                                              └─────────────────┘
                                                       │
                                                       ▼
                                              ┌─────────────────┐
                                              │    Update       │
                                              │   Payment       │
                                              │    Status       │
                                              └─────────────────┘
                                                       │
                                                       ▼
                                              ┌─────────────────┐
                                              │   Reconcile     │
                                              │   Payment       │
                                              │ with Invoice    │
                                              └─────────────────┘
```

## Error Handling and Resilience

### Graceful Degradation
- Sync failures don't break core FlexPrice functionality
- Retry mechanisms for transient failures
- Comprehensive logging for debugging

### Error Scenarios
1. **Network Failures** - Retry with exponential backoff
2. **API Rate Limits** - Respect Stripe rate limits
3. **Invalid Data** - Log and skip invalid records
4. **Mapping Conflicts** - Handle duplicate mappings gracefully
5. **Webhook Failures** - Idempotent processing to handle replays

### Monitoring and Observability
- Structured logging with correlation IDs
- Metrics for sync success/failure rates
- Alerts for critical sync failures
- Audit trails for all sync operations

## Security Considerations

### Data Protection
- Stripe API keys encrypted at rest
- Webhook signature verification
- Environment isolation
- PCI compliance for payment data

### Access Control
- Service-to-service authentication
- Environment-based access controls
- Audit logging for all operations

## Performance Optimization

### Batching and Efficiency
- Bulk operations where possible
- Efficient database queries
- Connection pooling
- Caching for frequently accessed data

### Scalability
- Horizontal scaling support
- Queue-based processing for high volume
- Database optimization
- Rate limit management

---

This documentation provides a comprehensive overview of the FlexPrice Stripe integration architecture, covering all major components, data flows, and operational considerations. The integration ensures reliable, bidirectional synchronization between FlexPrice and Stripe while maintaining data consistency and system reliability.
