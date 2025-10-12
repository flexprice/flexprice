# Integration Architecture & Stripe Integration PRD

## Table of Contents
1. [Current Integration Architecture](#current-integration-architecture)
2. [Stripe Integration Overview](#stripe-integration-overview)
3. [Payment & Invoice Sync Behavior Matrix](#payment--invoice-sync-behavior-matrix)
4. [Flow Diagrams](#flow-diagrams)
5. [Integration Scenarios](#integration-scenarios)

---

## Current Integration Architecture

### Overview
FlexPrice's integration system is designed to provide seamless connectivity with external payment providers and billing systems. The architecture follows a provider-agnostic approach with pluggable integrations.

### Core Components

```
┌─────────────────────────────────────────────────────────────────┐
│                    FlexPrice Core System                        │
├─────────────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐             │
│  │  Customers  │  │  Invoices   │  │  Payments   │             │
│  └─────────────┘  └─────────────┘  └─────────────┘             │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐             │
│  │ Subscriptions│  │    Plans    │  │   Wallets   │             │
│  └─────────────┘  └─────────────┘  └─────────────┘             │
└─────────────────────────────────────────────────────────────────┘
                                │
                                ▼
┌─────────────────────────────────────────────────────────────────┐
│                 Integration Layer                               │
├─────────────────────────────────────────────────────────────────┤
│  ┌─────────────────────────────────────────────────────────────┐ │
│  │              Entity Integration Mapping                     │ │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐         │ │
│  │  │ FlexPrice   │◄─┤   Mapping   │─►│  Provider   │         │ │
│  │  │ Entity ID   │  │    Table    │  │  Entity ID  │         │ │
│  │  └─────────────┘  └─────────────┘  └─────────────┘         │ │
│  └─────────────────────────────────────────────────────────────┘ │
│  ┌─────────────────────────────────────────────────────────────┐ │
│  │                Connection Management                        │ │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐         │ │
│  │  │   Provider  │  │   Config    │  │   Sync      │         │ │
│  │  │   Type      │  │   Settings  │  │   Control   │         │ │
│  │  └─────────────┘  └─────────────┘  └─────────────┘         │ │
│  └─────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────┘
                                │
                                ▼
┌─────────────────────────────────────────────────────────────────┐
│                    Provider Integrations                        │
├─────────────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐             │
│  │   Stripe    │  │   PayPal    │  │   Others    │             │
│  │ Integration │  │ Integration │  │ Integration │             │
│  └─────────────┘  └─────────────┘  └─────────────┘             │
└─────────────────────────────────────────────────────────────────┘
```

### Key Features
- **Bidirectional Sync**: Data flows both ways between FlexPrice and external providers
- **Granular Control**: Per-entity-type sync configuration (customers, invoices, plans, subscriptions)
- **Webhook Processing**: Real-time event handling from external providers
- **Entity Mapping**: Maintains relationships between FlexPrice and provider entities
- **Connection Management**: Secure storage and management of provider credentials

---

## Stripe Integration Overview

### Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    FlexPrice System                             │
├─────────────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐             │
│  │  Customer   │  │  Invoice    │  │  Payment    │             │
│  │  Service    │  │  Service    │  │  Service    │             │
│  └─────────────┘  └─────────────┘  └─────────────┘             │
└─────────────────────────────────────────────────────────────────┘
                                │
                                ▼
┌─────────────────────────────────────────────────────────────────┐
│                Stripe Integration Layer                         │
├─────────────────────────────────────────────────────────────────┤
│  ┌─────────────────────────────────────────────────────────────┐ │
│  │              Stripe Client & Configuration                  │ │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐         │ │
│  │  │   API       │  │   Webhook   │  │   Security  │         │ │
│  │  │   Client    │  │   Handler   │  │   (Keys)    │         │ │
│  │  └─────────────┘  └─────────────┘  └─────────────┘         │ │
│  └─────────────────────────────────────────────────────────────┘ │
│  ┌─────────────────────────────────────────────────────────────┐ │
│  │                Sync Services                                │ │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐         │ │
│  │  │  Customer   │  │  Invoice    │  │  Payment    │         │ │
│  │  │  Sync       │  │  Sync       │  │  Sync       │         │ │
│  │  └─────────────┘  └─────────────┘  └─────────────┘         │ │
│  └─────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────┘
                                │
                                ▼
┌─────────────────────────────────────────────────────────────────┐
│                      Stripe Platform                           │
├─────────────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐             │
│  │  Customers  │  │  Invoices   │  │  Payments   │             │
│  │  (Stripe)   │  │  (Stripe)   │  │  (Stripe)   │             │
│  └─────────────┘  └─────────────┘  └─────────────┘             │
└─────────────────────────────────────────────────────────────────┘
```

### Sync Configuration Structure

```json
{
  "customer": {
    "inbound": true,    // Stripe → FlexPrice
    "outbound": true    // FlexPrice → Stripe
  },
  "invoice": {
    "inbound": true,    // Stripe → FlexPrice
    "outbound": true    // FlexPrice → Stripe
  },
  "plan": {
    "inbound": true,    // Stripe → FlexPrice
    "outbound": true    // FlexPrice → Stripe
  },
  "subscription": {
    "inbound": true,    // Stripe → FlexPrice
    "outbound": true    // FlexPrice → Stripe
  }
}
```

---

## Payment & Invoice Sync Behavior Matrix

### Sync Configuration Behavior Table

| Connection | Sync Config | Payment Processing | Invoice Sync | Customer Sync | Plan/Sub Sync | Notes |
|------------|-------------|-------------------|--------------|---------------|---------------|-------|
| **OFF** | N/A | ❌ Card fails, ✅ Wallet/Offline | ❌ No sync | ❌ No sync | ❌ No sync | Standalone mode |
| **ON** | None | ✅ All methods work | ❌ No sync | ❌ No sync | ❌ No sync | Payments only |
| **ON** | `invoice.inbound: true` | ✅ All methods work | ✅ Stripe → FlexPrice | ❌ No sync | ❌ No sync | Invoice inbound only |
| **ON** | `invoice.outbound: true` | ✅ All methods work | ✅ FlexPrice → Stripe | ❌ No sync | ❌ No sync | Invoice outbound only |
| **ON** | `invoice: bidirectional` | ✅ All methods work | ✅ Bidirectional | ❌ No sync | ❌ No sync | Full invoice sync |
| **ON** | `customer.inbound: true` | ✅ All methods work | ❌ No sync | ✅ Stripe → FlexPrice | ❌ No sync | Customer inbound only |
| **ON** | `customer.outbound: true` | ✅ All methods work | ❌ No sync | ✅ FlexPrice → Stripe | ❌ No sync | Customer outbound only |
| **ON** | `customer: bidirectional` | ✅ All methods work | ❌ No sync | ✅ Bidirectional | ❌ No sync | Full customer sync |
| **ON** | `plan.inbound: true` | ✅ All methods work | ❌ No sync | ❌ No sync | ✅ Stripe → FlexPrice | Plan inbound only |
| **ON** | `plan.outbound: true` | ✅ All methods work | ❌ No sync | ❌ No sync | ✅ FlexPrice → Stripe | Plan outbound only |
| **ON** | `plan: bidirectional` | ✅ All methods work | ❌ No sync | ❌ No sync | ✅ Bidirectional | Full plan sync |
| **ON** | `subscription.inbound: true` | ✅ All methods work | ❌ No sync | ❌ No sync | ✅ Stripe → FlexPrice | Sub inbound only |
| **ON** | `subscription.outbound: true` | ✅ All methods work | ❌ No sync | ❌ No sync | ✅ FlexPrice → Stripe | Sub outbound only |
| **ON** | `subscription: bidirectional` | ✅ All methods work | ❌ No sync | ❌ No sync | ✅ Bidirectional | Full sub sync |
| **ON** | All enabled | ✅ All methods work | ✅ Bidirectional | ✅ Bidirectional | ✅ Bidirectional | Full integration |

---

## Flow Diagrams

### 1. Payment Processing Flow

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   FlexPrice     │    │   Stripe        │    │   Customer      │
│   System        │    │   Integration   │    │   Browser       │
└─────────────────┘    └─────────────────┘    └─────────────────┘
         │                       │                       │
         │ 1. Create Payment     │                       │
         ├──────────────────────►│                       │
         │                       │                       │
         │                       │ 2. Create Payment     │
         │                       │    Intent             │
         │                       ├──────────────────────►│
         │                       │                       │
         │                       │ 3. Payment            │
         │                       │    Confirmation       │
         │                       │◄──────────────────────┤
         │                       │                       │
         │ 4. Webhook:           │                       │
         │    payment_intent.    │                       │
         │    succeeded          │                       │
         │◄──────────────────────┤                       │
         │                       │                       │
         │ 5. Update Payment     │                       │
         │    Status             │                       │
         │                       │                       │
         │ 6. Reconcile with     │                       │
         │    Invoice            │                       │
         │                       │                       │
```

### 2. Invoice Sync Flow (FlexPrice → Stripe)

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   FlexPrice     │    │   Stripe        │    │   Stripe        │
│   Invoice       │    │   Integration   │    │   Platform      │
│   Service       │    │   Service       │    │                 │
└─────────────────┘    └─────────────────┘    └─────────────────┘
         │                       │                       │
         │ 1. Invoice            │                       │
         │    Finalized          │                       │
         │                       │                       │
         │ 2. Check Sync Config  │                       │
         │    (outbound: true)   │                       │
         │                       │                       │
         │ 3. Sync Invoice       │                       │
         ├──────────────────────►│                       │
         │                       │                       │
         │                       │ 4. Create Stripe      │
         │                       │    Invoice            │
         │                       ├──────────────────────►│
         │                       │                       │
         │                       │ 5. Stripe Invoice     │
         │                       │    Created            │
         │                       │◄──────────────────────┤
         │                       │                       │
         │ 6. Create Entity      │                       │
         │    Mapping            │                       │
         │                       │                       │
```

### 3. Customer Sync Flow (Stripe → FlexPrice)

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   Stripe        │    │   FlexPrice     │    │   FlexPrice     │
│   Platform      │    │   Webhook       │    │   Customer      │
│                 │    │   Handler       │    │   Service       │
└─────────────────┘    └─────────────────┘    └─────────────────┘
         │                       │                       │
         │ 1. Customer           │                       │
         │    Created            │                       │
         │                       │                       │
         │ 2. Webhook:           │                       │
         │    customer.created   │                       │
         ├──────────────────────►│                       │
         │                       │                       │
         │                       │ 3. Check Sync Config  │
         │                       │    (inbound: true)    │
         │                       │                       │
         │                       │ 4. Create Customer    │
         │                       ├──────────────────────►│
         │                       │                       │
         │                       │ 5. Customer Created   │
         │                       │◄──────────────────────┤
         │                       │                       │
         │                       │ 6. Create Entity      │
         │                       │    Mapping            │
         │                       │                       │
```

---

## Integration Scenarios

### Scenario 1: Stripe Connection OFF
**Use Case**: Standalone FlexPrice operation without external payment processing

**Behavior**:
- ✅ Wallet/Credits payments work normally
- ✅ Offline payments work normally  
- ❌ Card payments fail (no Stripe integration)
- ❌ Payment links may fail
- ❌ No data synchronization with Stripe
- ✅ All FlexPrice features work independently

**Best For**: Development environments, testing, or businesses using only internal payment methods

### Scenario 2: Stripe Connection ON - Payments Only
**Use Case**: Using Stripe for payment processing but maintaining separate data systems

**Behavior**:
- ✅ All payment methods work (cards, wallets, offline)
- ✅ Payment links work
- ✅ Automatic payment processing
- ❌ No customer data sync
- ❌ No invoice data sync
- ❌ No plan/subscription sync
- ✅ Independent data management

**Best For**: Businesses that want Stripe payments but prefer to manage customer/invoice data separately

### Scenario 3: Invoice Sync Only (Outbound)
**Use Case**: FlexPrice manages invoices and syncs them to Stripe for payment collection

**Behavior**:
- ✅ All payment methods work
- ✅ FlexPrice invoices sync to Stripe
- ✅ Stripe can collect payments on synced invoices
- ❌ Stripe invoices don't sync back to FlexPrice
- ❌ No customer sync
- ❌ No plan/subscription sync

**Best For**: Businesses using FlexPrice as primary billing system with Stripe as payment processor

### Scenario 4: Invoice Sync Only (Inbound)
**Use Case**: Stripe manages invoices and FlexPrice receives them for reporting/analytics

**Behavior**:
- ✅ All payment methods work
- ✅ Stripe invoices sync to FlexPrice
- ✅ FlexPrice can track payments and provide analytics
- ❌ FlexPrice invoices don't sync to Stripe
- ❌ No customer sync
- ❌ No plan/subscription sync

**Best For**: Businesses using Stripe as primary billing system with FlexPrice for analytics

### Scenario 5: Full Bidirectional Sync
**Use Case**: Complete integration between FlexPrice and Stripe

**Behavior**:
- ✅ All payment methods work
- ✅ Bidirectional invoice sync
- ✅ Bidirectional customer sync
- ✅ Bidirectional plan sync
- ✅ Bidirectional subscription sync
- ✅ Real-time data consistency
- ✅ Unified billing experience

**Best For**: Businesses wanting complete integration and data consistency between systems

### Scenario 6: Customer Sync Only
**Use Case**: Synchronizing customer data while managing other entities separately

**Behavior**:
- ✅ All payment methods work
- ✅ Customer data stays synchronized
- ❌ No invoice sync
- ❌ No plan/subscription sync
- ✅ Independent invoice and plan management

**Best For**: Businesses that want customer data consistency but prefer separate billing workflows

---

## Key Benefits

### Flexibility
- **Granular Control**: Enable/disable sync per entity type
- **Directional Control**: Choose inbound, outbound, or bidirectional sync
- **Independent Operation**: System works with or without integrations

### Reliability
- **Graceful Degradation**: System continues working if integration fails
- **Error Handling**: Failed syncs don't break core functionality
- **Webhook Processing**: Real-time event handling with proper error recovery

### Scalability
- **Provider Agnostic**: Easy to add new payment providers
- **Modular Design**: Each integration component is independent
- **Entity Mapping**: Efficient relationship management between systems

### Security
- **Encrypted Credentials**: Secure storage of provider API keys
- **Webhook Verification**: Proper signature validation
- **Context Isolation**: Proper tenant and environment separation

---

## Conclusion

The FlexPrice integration architecture provides a robust, flexible foundation for connecting with external payment providers and billing systems. The Stripe integration demonstrates how this architecture enables various sync scenarios while maintaining system reliability and data consistency.

The granular sync configuration allows businesses to choose the level of integration that best fits their needs, from simple payment processing to full bidirectional data synchronization.
