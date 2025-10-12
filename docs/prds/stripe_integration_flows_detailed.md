# Stripe Integration Flows - Detailed Technical Documentation

## Overview

This document provides comprehensive technical documentation for FlexPrice's Stripe integration, covering customer synchronization, payment processing flows, and invoice synchronization. Each flow includes detailed Mermaid diagrams and step-by-step explanations.

## Table of Contents

1. [Customer Sync Flows](#1-customer-sync-flows)
2. [Payment Processing Flows](#2-payment-processing-flows)
3. [Invoice Sync Flows](#3-invoice-sync-flows)
4. [Webhook Event Handling](#4-webhook-event-handling)
5. [Error Handling and Edge Cases](#5-error-handling-and-edge-cases)

---

## 1. Customer Sync Flows

### 1.1 Customer Sync Overview

The customer sync system ensures bidirectional synchronization between FlexPrice customers and Stripe customers. It supports multiple sync scenarios and maintains data consistency across both platforms.

### 1.2 Customer Sync Flow Diagram

```mermaid
graph TB
    A[Customer Sync Request] --> B{Check Customer Exists in FlexPrice}
    B -->|No| C[Create Customer in FlexPrice]
    B -->|Yes| D{Check Stripe ID in Metadata}
    
    D -->|Exists| E[Return Customer with Stripe ID]
    D -->|Not Exists| F{Check Integration Mapping Table}
    
    F -->|Mapping Found| G[Update Customer Metadata with Stripe ID]
    F -->|No Mapping| H[Create Customer in Stripe]
    
    H --> I[Create Stripe Customer]
    I --> J[Create Entity Integration Mapping]
    J --> K[Update FlexPrice Customer Metadata]
    K --> L[Return Updated Customer]
    
    G --> M[Return Updated Customer]
    
    C --> N[Create Customer in Stripe]
    N --> O[Create Entity Integration Mapping]
    O --> P[Update FlexPrice Customer Metadata]
    P --> Q[Return New Customer]
    
    style A fill:#e1f5fe
    style E fill:#c8e6c9
    style L fill:#c8e6c9
    style M fill:#c8e6c9
    style Q fill:#c8e6c9
```

### 1.3 Customer Sync Methods

#### 1.3.1 EnsureCustomerSyncedToStripe

**Purpose**: Ensures a FlexPrice customer is synced to Stripe, creating the mapping if it doesn't exist.

**Flow Steps**:
1. Get FlexPrice customer by ID
2. Check if customer already has `stripe_customer_id` in metadata
3. If not found, check integration mapping table
4. If mapping exists, update customer metadata
5. If no mapping, create customer in Stripe
6. Create entity integration mapping
7. Update customer metadata with Stripe ID

**Key Functions**:
- `EnsureCustomerSyncedToStripe(ctx, customerID, customerService)`
- `CreateCustomerInStripe(ctx, customerID, customerService)`
- `CreateCustomerFromStripe(ctx, stripeCustomer, environmentID, customerService)`

#### 1.3.2 CreateCustomerInStripe

**Purpose**: Creates a new customer in Stripe and establishes the mapping.

**Stripe Customer Creation Parameters**:
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

#### 1.3.3 CreateCustomerFromStripe

**Purpose**: Creates a FlexPrice customer from Stripe webhook data.

**Flow**:
1. Check for existing customer by external ID
2. If exists, update with Stripe ID
3. If not exists, create new customer
4. Create entity integration mapping

### 1.4 Customer Sync Webhook Handling

```mermaid
sequenceDiagram
    participant S as Stripe
    participant W as Webhook Handler
    participant CS as Customer Service
    participant DB as Database
    
    S->>W: customer.created webhook
    W->>W: Verify webhook signature
    W->>W: Check sync configuration
    W->>W: Parse customer data
    W->>CS: CreateCustomerFromStripe()
    CS->>DB: Check existing customer
    alt Customer exists
        CS->>DB: Update customer metadata
    else Customer doesn't exist
        CS->>DB: Create new customer
        CS->>DB: Create integration mapping
    end
    CS->>W: Return success
    W->>S: 200 OK
```

---

## 2. Payment Processing Flows

### 2.1 Payment Flow Overview

FlexPrice supports multiple payment methods through Stripe integration:
1. **Payment Links** - Stripe Checkout sessions
2. **Saved Payment Methods** - Off-session payments
3. **External Payments** - Payments made directly in Stripe
4. **Setup Intents** - Payment method collection

### 2.2 Payment Flow Architecture

```mermaid
graph TB
    A[Payment Request] --> B{Payment Type}
    
    B -->|Payment Link| C[CreatePaymentLink]
    B -->|Saved Method| D[ChargeSavedPaymentMethod]
    B -->|Setup Intent| E[SetupIntent]
    B -->|External| F[ProcessExternalPayment]
    
    C --> G[Validate Invoice]
    G --> H[Ensure Customer Synced]
    H --> I[Create Checkout Session]
    I --> J[Return Payment URL]
    
    D --> K[Validate Payment Method]
    K --> L[Create PaymentIntent]
    L --> M[Confirm Payment]
    M --> N[Attach to Invoice]
    
    E --> O[Create SetupIntent]
    O --> P[Create Checkout Session]
    P --> Q[Return Setup URL]
    
    F --> R[Parse Webhook Data]
    R --> S[Find FlexPrice Invoice]
    S --> T[Create Payment Record]
    T --> U[Reconcile Invoice]
    
    style A fill:#e1f5fe
    style J fill:#c8e6c9
    style N fill:#c8e6c9
    style Q fill:#c8e6c9
    style U fill:#c8e6c9
```

### 2.3 Payment Link Flow

```mermaid
sequenceDiagram
    participant C as Client
    participant PS as Payment Service
    participant IS as Invoice Service
    participant CS as Customer Service
    participant S as Stripe
    
    C->>PS: CreatePaymentLink Request
    PS->>IS: GetInvoice()
    IS->>PS: Invoice Details
    PS->>PS: Validate Invoice Status
    PS->>PS: Validate Payment Amount
    PS->>CS: EnsureCustomerSyncedToStripe()
    CS->>S: Create/Get Stripe Customer
    S->>CS: Stripe Customer ID
    CS->>PS: Customer with Stripe ID
    PS->>S: Create Checkout Session
    S->>PS: Checkout Session
    PS->>C: Payment URL + Session ID
```

### 2.4 Saved Payment Method Flow

```mermaid
sequenceDiagram
    participant C as Client
    participant PS as Payment Service
    participant CS as Customer Service
    participant IS as Invoice Service
    participant S as Stripe
    
    C->>PS: ChargeSavedPaymentMethod Request
    PS->>CS: EnsureCustomerSyncedToStripe()
    CS->>PS: Customer with Stripe ID
    PS->>IS: GetInvoice()
    IS->>PS: Invoice Details
    PS->>PS: Validate Payment Amount
    PS->>S: Create PaymentIntent
    Note over S: Off-session payment
    S->>PS: PaymentIntent Result
    PS->>S: AttachPaymentToStripeInvoice()
    PS->>PS: ReconcilePaymentWithInvoice()
    PS->>C: Payment Result
```

### 2.5 External Payment Processing Flow

```mermaid
sequenceDiagram
    participant S as Stripe
    participant W as Webhook Handler
    participant PS as Payment Service
    participant IS as Invoice Service
    participant DB as Database
    
    S->>W: payment_intent.succeeded webhook
    W->>W: Parse webhook data
    W->>W: Extract invoice ID
    W->>PS: HandleExternalStripePaymentFromWebhook()
    PS->>PS: GetFlexPriceInvoiceID()
    PS->>DB: Create Payment Record
    PS->>IS: ReconcilePaymentStatus()
    IS->>DB: Update Invoice Status
    PS->>W: Success
    W->>S: 200 OK
```

### 2.6 Webhook Event Handling

#### 2.6.1 Supported Webhook Events

| Event Type | Handler | Purpose |
|------------|---------|---------|
| `customer.created` | `handleCustomerCreated` | Create customer in FlexPrice |
| `payment_intent.succeeded` | `handlePaymentIntentSucceeded` | Process successful payment |
| `payment_intent.payment_failed` | `handlePaymentIntentPaymentFailed` | Handle failed payment |
| `setup_intent.succeeded` | `handleSetupIntentSucceeded` | Handle payment method setup |
| `invoice_payment.paid` | `handleInvoicePaymentPaid` | Process external invoice payment |
| `product.created` | `handleProductCreated` | Create plan in FlexPrice |
| `product.updated` | `handleProductUpdated` | Update plan in FlexPrice |
| `product.deleted` | `handleProductDeleted` | Delete plan in FlexPrice |
| `customer.subscription.created` | `handleSubscriptionCreated` | Create subscription in FlexPrice |
| `customer.subscription.updated` | `handleSubscriptionUpdated` | Update subscription in FlexPrice |
| `customer.subscription.deleted` | `handleSubscriptionCancellation` | Cancel subscription in FlexPrice |

#### 2.6.2 Webhook Processing Flow

```mermaid
graph TB
    A[Stripe Webhook] --> B[Verify Signature]
    B --> C[Parse Event Type]
    C --> D{Event Type}
    
    D -->|customer.created| E[handleCustomerCreated]
    D -->|payment_intent.succeeded| F[handlePaymentIntentSucceeded]
    D -->|payment_intent.payment_failed| G[handlePaymentIntentPaymentFailed]
    D -->|setup_intent.succeeded| H[handleSetupIntentSucceeded]
    D -->|invoice_payment.paid| I[handleInvoicePaymentPaid]
    D -->|product.*| J[handleProduct*]
    D -->|subscription.*| K[handleSubscription*]
    
    E --> L[Check Sync Config]
    L --> M[Create Customer]
    
    F --> N{Check Payment Source}
    N -->|FlexPrice| O[HandleFlexPriceCheckoutPayment]
    N -->|External| P[HandleExternalStripePaymentFromWebhook]
    
    G --> Q[Update Payment Status]
    H --> R[Save Payment Method]
    I --> S[Process External Payment]
    J --> T[Sync Product/Plan]
    K --> U[Sync Subscription]
    
    style A fill:#e1f5fe
    style M fill:#c8e6c9
    style O fill:#c8e6c9
    style P fill:#c8e6c9
    style Q fill:#ffcdd2
    style R fill:#c8e6c9
    style S fill:#c8e6c9
    style T fill:#c8e6c9
    style U fill:#c8e6c9
```

---

## 3. Invoice Sync Flows

### 3.1 Invoice Sync Overview

The invoice sync system enables bidirectional synchronization of invoices between FlexPrice and Stripe, supporting both outbound (FlexPrice → Stripe) and inbound (Stripe → FlexPrice) flows.

### 3.2 Invoice Sync Flow Diagram

```mermaid
graph TB
    A[Invoice Sync Request] --> B{Check Stripe Connection}
    B -->|No Connection| C[Return Error]
    B -->|Connection Exists| D[Get FlexPrice Invoice]
    
    D --> E{Check Existing Mapping}
    E -->|Mapping Exists| F[Use Existing Stripe Invoice]
    E -->|No Mapping| G[Create Draft Invoice in Stripe]
    
    G --> H[Create Entity Integration Mapping]
    H --> I[Sync Line Items to Stripe]
    I --> J[Finalize Invoice in Stripe]
    
    J --> K{Collection Method}
    K -->|charge_automatically| L[Auto-advance Invoice]
    K -->|send_invoice| M[Send Invoice to Customer]
    
    F --> N[Sync Line Items]
    N --> O[Update Invoice if Needed]
    
    L --> P[Return Success]
    M --> P
    O --> P
    
    style A fill:#e1f5fe
    style C fill:#ffcdd2
    style P fill:#c8e6c9
```

### 3.3 Invoice Sync Process Details

#### 3.3.1 SyncInvoiceToStripe - 7-Step Process

1. **Check Stripe Connection** - Verify Stripe integration is configured
2. **Get FlexPrice Invoice** - Retrieve invoice details from database
3. **Check Existing Mapping** - Avoid duplicate syncing via integration mapping table
4. **Create Draft Invoice** - Create draft invoice in Stripe with metadata
5. **Create Entity Mapping** - Track sync relationship in integration mapping table
6. **Sync Line Items** - Add all invoice line items to Stripe invoice
7. **Finalize Invoice** - Make invoice ready for payment

#### 3.3.2 Invoice Creation Parameters

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

#### 3.3.3 Collection Methods

- **`charge_automatically`** - Automatic payment attempt using customer's default payment method
- **`send_invoice`** - Email invoice to customer for manual payment

### 3.4 Invoice Sync Sequence Diagram

```mermaid
sequenceDiagram
    participant IS as Invoice Service
    participant ISS as Invoice Sync Service
    participant CS as Customer Service
    participant S as Stripe
    participant DB as Database
    
    IS->>ISS: SyncInvoiceToStripe()
    ISS->>ISS: Check Stripe Connection
    ISS->>DB: Get FlexPrice Invoice
    ISS->>DB: Check Existing Mapping
    
    alt No Mapping
        ISS->>CS: EnsureCustomerSyncedToStripe()
        CS->>ISS: Stripe Customer ID
        ISS->>S: Create Draft Invoice
        S->>ISS: Stripe Invoice ID
        ISS->>DB: Create Entity Mapping
        ISS->>S: Add Line Items
        ISS->>S: Finalize Invoice
        S->>ISS: Finalized Invoice
        ISS->>S: Send Invoice (if needed)
    else Mapping Exists
        ISS->>S: Get Existing Invoice
        ISS->>S: Update Line Items
    end
    
    ISS->>IS: Sync Success
```

### 3.5 Invoice Reconciliation

#### 3.5.1 External Payment Reconciliation

When payments are made directly in Stripe (external to FlexPrice), the system reconciles these payments with FlexPrice invoices:

```mermaid
sequenceDiagram
    participant S as Stripe
    participant W as Webhook Handler
    participant PS as Payment Service
    participant IS as Invoice Service
    participant DB as Database
    
    S->>W: invoice_payment.paid webhook
    W->>W: Parse webhook data
    W->>PS: ProcessExternalStripePayment()
    PS->>PS: GetFlexPriceInvoiceID()
    PS->>DB: Create Payment Record
    PS->>IS: ReconcilePaymentStatus()
    IS->>DB: Update Invoice Amounts
    IS->>DB: Update Payment Status
    PS->>W: Success
    W->>S: 200 OK
```

#### 3.5.2 Payment Status Calculation

The system calculates new payment status based on amounts:

```go
newAmountPaid := invoiceResp.AmountPaid.Add(paymentAmount)
newAmountRemaining := invoiceResp.AmountDue.Sub(newAmountPaid)

var newPaymentStatus types.PaymentStatus
if newAmountRemaining.IsZero() {
    newPaymentStatus = types.PaymentStatusSucceeded // Fully paid
} else if newAmountRemaining.IsNegative() {
    newPaymentStatus = types.PaymentStatusOverpaid // Overpaid
} else {
    newPaymentStatus = types.PaymentStatusPending // Partial payment
}
```

---

## 4. Webhook Event Handling

### 4.1 Webhook Processing Architecture

```mermaid
graph TB
    A[Stripe Webhook] --> B[API Gateway]
    B --> C[Webhook Handler]
    C --> D[Verify Signature]
    D --> E[Parse Event]
    E --> F[Check Sync Configuration]
    F --> G{Route by Event Type}
    
    G -->|Customer Events| H[Customer Handlers]
    G -->|Payment Events| I[Payment Handlers]
    G -->|Invoice Events| J[Invoice Handlers]
    G -->|Product Events| K[Product Handlers]
    G -->|Subscription Events| L[Subscription Handlers]
    
    H --> M[Update Database]
    I --> N[Process Payment]
    J --> O[Sync Invoice]
    K --> P[Sync Product/Plan]
    L --> Q[Sync Subscription]
    
    M --> R[Return Success]
    N --> R
    O --> R
    P --> R
    Q --> R
    
    style A fill:#e1f5fe
    style R fill:#c8e6c9
```

### 4.2 Webhook Security

1. **Signature Verification** - All webhooks are verified using Stripe's webhook signature
2. **Idempotency** - Duplicate events are handled gracefully
3. **Error Handling** - Failed webhooks are logged and can be retried
4. **Configuration Checks** - Sync settings are validated before processing

### 4.3 Webhook Event Details

#### 4.3.1 Payment Intent Succeeded Handler

```mermaid
flowchart TD
    A[payment_intent.succeeded] --> B[Parse Payment Intent]
    B --> C[Fetch Latest from Stripe API]
    C --> D{Check flexprice_payment_id}
    
    D -->|Exists| E[Get FlexPrice Payment]
    E --> F{Payment Status}
    F -->|Already Succeeded| G[Skip Processing]
    F -->|Not Succeeded| H[HandleFlexPriceCheckoutPayment]
    
    D -->|Not Exists| I[HandleExternalStripePaymentFromWebhook]
    I --> J[Parse Webhook Data]
    J --> K[Extract Invoice ID]
    K --> L[Process External Payment]
    
    H --> M[Update Payment Status]
    L --> N[Create Payment Record]
    M --> O[Reconcile Invoice]
    N --> O
    O --> P[Return Success]
    
    style A fill:#e1f5fe
    style G fill:#fff3e0
    style P fill:#c8e6c9
```

---

## 5. Error Handling and Edge Cases

### 5.1 Common Error Scenarios

1. **Customer Sync Failures**
   - Stripe API errors
   - Duplicate customer creation
   - Metadata update failures

2. **Payment Processing Errors**
   - Invalid payment methods
   - Insufficient funds
   - Authentication required

3. **Invoice Sync Issues**
   - Missing line items
   - Currency mismatches
   - Collection method conflicts

4. **Webhook Processing Failures**
   - Invalid signatures
   - Malformed payloads
   - Service unavailability

### 5.2 Error Recovery Strategies

1. **Retry Mechanisms** - Automatic retry for transient failures
2. **Fallback Processing** - Alternative flows for critical operations
3. **Manual Intervention** - Admin tools for error resolution
4. **Monitoring and Alerting** - Real-time error detection

### 5.3 Data Consistency

1. **Idempotency Keys** - Prevent duplicate processing
2. **Transaction Management** - Ensure atomic operations
3. **Reconciliation Jobs** - Periodic data consistency checks
4. **Audit Logging** - Complete operation tracking

---

## Conclusion

This document provides comprehensive technical documentation for FlexPrice's Stripe integration. The system supports complex bidirectional synchronization scenarios while maintaining data consistency and providing robust error handling. The modular architecture allows for easy extension and maintenance of integration features.

For implementation details, refer to the specific service files in the `internal/integration/stripe/` directory.


