# Add-ons Accounting Flow Diagram (Invoice-Level Integration)

## Complete System Flow (Minimal Changes Approach)

```mermaid
graph TB
    %% Customer Application Layer
    subgraph "Customer Application"
        CA[Customer App]
        UE[Usage Events]
        CA --> UE
    end

    %% Event Ingestion Layer (UNCHANGED)
    subgraph "Event Ingestion (Unchanged)"
        EI[Event Ingestion Service]
        EV[Event Validation]
        UR[Usage Recording]
        TM[Threshold Monitoring]

        UE --> EI
        EI --> EV
        EV --> UR
        UR --> TM
    end

    %% Data Storage Layer
    subgraph "Data Storage"
        EVENTS[(Events Table)]
        USAGE[(Usage Tracking)]
        ADDONS[(Addons Table)]
        SUB_ADDONS[(Subscription Addons)]
        ENTITLEMENTS[(Entitlements Table)]
        SUB_LINE_ITEMS_TABLE[(Subscription Line Items)]

        UR --> EVENTS
        UR --> USAGE
    end

    %% Subscription & Addon Management
    subgraph "Subscription Management (Enhanced)"
        SUB[Enhanced Subscription Service]
        ADDON_SVC[New Addon Service]
        GET_USAGE[Enhanced GetUsageBySubscription]

        SUB --> SUB_LINE_ITEMS_TABLE
        ADDON_SVC --> SUB_ADDONS
        SUB --> GET_USAGE
    end

    %% Enhanced Usage Calculation (Invoice-Level)
    subgraph "Enhanced Usage Calculation"
        COMBINED_ENT[Calculate Combined Entitlements]
        USAGE_CALC[Usage Against Combined Limits]
        OVERAGE_DETECT[Overage Detection]

        GET_USAGE --> COMBINED_ENT
        COMBINED_ENT --> USAGE_CALC
        USAGE_CALC --> OVERAGE_DETECT
    end

    %% Enhanced Billing Calculation
    subgraph "Enhanced Billing Calculation"
        ENHANCED_BC[Enhanced Billing Service]
        FIXED_CALC[Enhanced CalculateFixedCharges]
        USAGE_CHARGE_CALC[Enhanced CalculateUsageCharges]
        PRORATION[Addon Proration]

        SUB_LINE_ITEMS_TABLE --> FIXED_CALC
        SUB_ADDONS --> FIXED_CALC
        OVERAGE_DETECT --> USAGE_CHARGE_CALC
        COMBINED_ENT --> USAGE_CHARGE_CALC
        FIXED_CALC --> ENHANCED_BC
        USAGE_CHARGE_CALC --> ENHANCED_BC
        PRORATION --> ENHANCED_BC
    end

    %% Invoice Generation (Enhanced)
    subgraph "Invoice Generation (Enhanced)"
        IG[Invoice Generation Service]
        ADDON_LINE_ITEMS[Addon Line Item Creation]
        IA[Invoice Assembly]
        IF[Invoice Finalization]
        PP[Payment Processing]

        ENHANCED_BC --> ADDON_LINE_ITEMS
        ADDON_LINE_ITEMS --> IA
        IA --> IF
        IF --> PP
    end

    %% Webhook & Notifications
    subgraph "Webhooks & Notifications"
        WH[Webhook Service]
        NOT[Notification Service]
        TM --> WH
        IF --> WH
        WH --> NOT
    end

    %% Connections between layers
    SUB_ADDONS --> COMBINED_ENT
    ENTITLEMENTS --> COMBINED_ENT
    ADDONS --> ADDON_SVC

    %% Styling
    classDef customerLayer fill:#e1f5fe
    classDef ingestionLayer fill:#f3e5f5
    classDef storageLayer fill:#e8f5e8
    classDef serviceLayer fill:#fff3e0
    classDef enhancedLayer fill:#fce4ec
    classDef billingLayer fill:#f1f8e9
    classDef invoiceLayer fill:#e0f2f1
    classDef webhookLayer fill:#fafafa

    class CA,UE customerLayer
    class EI,EV,UR,TM ingestionLayer
    class EVENTS,USAGE,ADDONS,SUB_ADDONS,ENTITLEMENTS,SUB_LINE_ITEMS_TABLE storageLayer
    class SUB,ADDON_SVC,GET_USAGE serviceLayer
    class COMBINED_ENT,USAGE_CALC,OVERAGE_DETECT enhancedLayer
    class ENHANCED_BC,FIXED_CALC,USAGE_CHARGE_CALC,PRORATION billingLayer
    class IG,ADDON_LINE_ITEMS,IA,IF,PP invoiceLayer
    class WH,NOT webhookLayer
```

## Invoice-Level Integration Flow

```mermaid
sequenceDiagram
    participant CA as Customer App
    participant EI as Event Ingestion (Unchanged)
    participant UR as Usage Recording
    participant SUB as Enhanced Subscription Service
    participant USAGE as Enhanced GetUsageBySubscription
    participant BC as Enhanced Billing Service
    participant IG as Invoice Generation

    Note over EI: Existing event processing unchanged
    CA->>EI: Usage Event
    EI->>UR: Standard Usage Recording

    Note over SUB: Addon integration happens here
    SUB->>USAGE: Get Usage with Addons
    USAGE->>USAGE: Calculate Combined Entitlements
    USAGE->>USAGE: Apply Usage Against Combined Limits
    USAGE->>USAGE: Generate Overage for Excess

    Note over BC: Enhanced billing calculation
    BC->>BC: Process Plan + Addon Line Items
    BC->>BC: Calculate Fixed Charges with Proration
    BC->>BC: Calculate Usage Charges with Combined Limits

    Note over IG: Invoice generation with addon line items
    BC->>IG: Enhanced Billing Calculation
    IG->>IG: Create Separate Addon Line Items
    IG->>CA: Invoice with Plan + Addon Charges
```

## Entitlement Aggregation Flow

```mermaid
flowchart TD
    subgraph "Input Sources"
        SUB[Subscription]
        PLAN_ENT[Plan Entitlements]
        ADDON_ENT[Addon Entitlements]
    end

    subgraph "Aggregation Process"
        GET_PLAN[Get Plan Entitlements]
        GET_ADDON[Get Addon Entitlements]
        MERGE[Merge Entitlements]
        VALIDATE[Validate Compatibility]
    end

    subgraph "Output"
        COMBINED[Combined Entitlements]
        SOURCES[Source Attribution]
        LIMITS[Combined Limits]
    end

    SUB --> GET_PLAN
    SUB --> GET_ADDON
    GET_PLAN --> MERGE
    GET_ADDON --> MERGE
    MERGE --> VALIDATE
    VALIDATE --> COMBINED
    COMBINED --> SOURCES
    COMBINED --> LIMITS

    %% Styling
    classDef input fill:#e3f2fd
    classDef process fill:#f3e5f5
    classDef output fill:#e8f5e8

    class SUB,PLAN_ENT,ADDON_ENT input
    class GET_PLAN,GET_ADDON,MERGE,VALIDATE process
    class COMBINED,SOURCES,LIMITS output
```

## Billing Calculation Flow

```mermaid
flowchart LR
    subgraph "Input Data"
        SUB_LI[Subscription Line Items]
        ADDON_LI[Addon Line Items]
        USAGE_DATA[Usage Data]
        ENTITLEMENTS[Combined Entitlements]
    end

    subgraph "Fixed Charges"
        PLAN_FC[Plan Fixed Charges]
        ADDON_FC[Addon Fixed Charges]
        PRORATION[Proration Charges]
    end

    subgraph "Usage Charges"
        PLAN_UC[Plan Usage Charges]
        ADDON_UC[Addon Usage Charges]
        OVERAGE[Overage Charges]
    end

    subgraph "Invoice Assembly"
        LINE_ITEMS[Line Items]
        INVOICE[Invoice]
        PAYMENT[Payment Processing]
    end

    SUB_LI --> PLAN_FC
    ADDON_LI --> ADDON_FC
    USAGE_DATA --> PLAN_UC
    ENTITLEMENTS --> ADDON_UC
    USAGE_DATA --> OVERAGE

    PLAN_FC --> LINE_ITEMS
    ADDON_FC --> LINE_ITEMS
    PRORATION --> LINE_ITEMS
    PLAN_UC --> LINE_ITEMS
    ADDON_UC --> LINE_ITEMS
    OVERAGE --> LINE_ITEMS

    LINE_ITEMS --> INVOICE
    INVOICE --> PAYMENT

    %% Styling
    classDef input fill:#e3f2fd
    classDef fixed fill:#f3e5f5
    classDef usage fill:#e8f5e8
    classDef invoice fill:#fff3e0

    class SUB_LI,ADDON_LI,USAGE_DATA,ENTITLEMENTS input
    class PLAN_FC,ADDON_FC,PRORATION fixed
    class PLAN_UC,ADDON_UC,OVERAGE usage
    class LINE_ITEMS,INVOICE,PAYMENT invoice
```

## Invoice Line Item Structure

```mermaid
graph TD
    subgraph "Invoice Line Items"
        subgraph "Fixed Charges"
            PLAN_FIXED[Plan Fixed Charge<br/>plan_id: plan_123<br/>amount: 29.99]
            ADDON_FIXED[Addon Fixed Charge<br/>addon_id: addon_456<br/>amount: 50.00]
            PRORATION_CHARGE[Proration Charge<br/>addon_id: addon_456<br/>amount: 25.00]
        end

        subgraph "Usage Charges"
            PLAN_USAGE[Plan Usage Charge<br/>plan_id: plan_123<br/>usage: 1000 calls<br/>amount: 10.00]
            ADDON_USAGE[Addon Usage Charge<br/>addon_id: addon_456<br/>usage: 500 calls<br/>amount: 5.00]
            OVERAGE_CHARGE[Overage Charge<br/>source: addon_456<br/>usage: 200 calls<br/>amount: 4.00]
        end
    end

    subgraph "Invoice Summary"
        TOTAL[Total Amount: 123.99]
        BREAKDOWN[Breakdown:<br/>Plan Fixed: 29.99<br/>Addon Fixed: 50.00<br/>Proration: 25.00<br/>Plan Usage: 10.00<br/>Addon Usage: 5.00<br/>Overage: 4.00]
    end

    PLAN_FIXED --> TOTAL
    ADDON_FIXED --> TOTAL
    PRORATION_CHARGE --> TOTAL
    PLAN_USAGE --> TOTAL
    ADDON_USAGE --> TOTAL
    OVERAGE_CHARGE --> TOTAL
    TOTAL --> BREAKDOWN

    %% Styling
    classDef fixed fill:#e3f2fd
    classDef usage fill:#f3e5f5
    classDef summary fill:#e8f5e8

    class PLAN_FIXED,ADDON_FIXED,PRORATION_CHARGE fixed
    class PLAN_USAGE,ADDON_USAGE,OVERAGE_CHARGE usage
    class TOTAL,BREAKDOWN summary
```

## Usage Tracking with Source Attribution

```mermaid
graph TB
    subgraph "Usage Event"
        EVENT[Usage Event<br/>meter_id: api_calls<br/>usage: 1500 calls<br/>timestamp: 2024-01-15]
    end

    subgraph "Entitlement Sources"
        PLAN_ENT[Plan Entitlement<br/>limit: 1000 calls<br/>used: 1000 calls]
        ADDON_ENT[Addon Entitlement<br/>limit: 500 calls<br/>used: 500 calls]
    end

    subgraph "Usage Calculation"
        PLAN_USAGE[Plan Usage<br/>1000 calls (within limit)]
        ADDON_USAGE[Addon Usage<br/>500 calls (within limit)]
        OVERAGE_USAGE[Overage Usage<br/>0 calls (no overage)]
    end

    subgraph "Billing Impact"
        PLAN_CHARGE[Plan Charge<br/>1000 calls × 0.01 = 10.00]
        ADDON_CHARGE[Addon Charge<br/>500 calls × 0.01 = 5.00]
        OVERAGE_CHARGE[Overage Charge<br/>0 calls × 0.02 = 0.00]
    end

    EVENT --> PLAN_ENT
    EVENT --> ADDON_ENT
    PLAN_ENT --> PLAN_USAGE
    ADDON_ENT --> ADDON_USAGE
    PLAN_USAGE --> PLAN_CHARGE
    ADDON_USAGE --> ADDON_CHARGE
    OVERAGE_USAGE --> OVERAGE_CHARGE

    %% Styling
    classDef event fill:#e3f2fd
    classDef entitlement fill:#f3e5f5
    classDef usage fill:#e8f5e8
    classDef billing fill:#fff3e0

    class EVENT event
    class PLAN_ENT,ADDON_ENT entitlement
    class PLAN_USAGE,ADDON_USAGE,OVERAGE_USAGE usage
    class PLAN_CHARGE,ADDON_CHARGE,OVERAGE_CHARGE billing
```

## Mid-Cycle Addon Management Flow

```mermaid
sequenceDiagram
    participant C as Customer
    participant AS as Addon Service
    participant SUB as Subscription Service
    participant BC as Billing Service
    participant IG as Invoice Service

    Note over C,IG: Addon Purchase Mid-Cycle
    C->>AS: Add Addon Request
    AS->>AS: Validate Addon Compatibility
    AS->>SUB: Create Subscription Addon Record
    AS->>BC: Calculate Proration
    BC->>BC: Calculate Daily Rate × Remaining Days
    BC->>IG: Create Proration Invoice
    IG->>C: Immediate Proration Invoice

    Note over C,IG: Regular Invoice Generation with Addons
    IG->>SUB: Generate Regular Invoice
    SUB->>SUB: Get Plan + Addon Line Items
    SUB->>BC: Enhanced Billing Calculation
    BC->>BC: Process Fixed + Usage Charges
    BC->>IG: Plan + Addon Line Items
    IG->>C: Regular Invoice with Addons

    Note over C,IG: Addon Cancellation Mid-Cycle
    C->>AS: Cancel Addon Request
    AS->>AS: Set Addon End Date
    AS->>BC: Calculate Credit for Unused Period
    BC->>IG: Create Credit Note
    IG->>C: Credit for Unused Addon Period
```

## Invoice Generation During Addon Changes

```mermaid
graph TD
    subgraph "Addon State at Invoice Time"
        STATE[Snapshot Addon State]
        ACTIVE[Active Addons]
        PARTIAL[Partial Period Addons]
        CANCELLED[Recently Cancelled]
    end

    subgraph "Line Item Generation"
        PLAN_ITEMS[Plan Line Items]
        ADDON_ITEMS[Addon Line Items]
        PRORATION_ITEMS[Proration Line Items]
        CREDIT_ITEMS[Credit Line Items]
    end

    subgraph "Invoice Assembly"
        COMBINE[Combine All Line Items]
        VALIDATE[Validate Total]
        GENERATE[Generate Final Invoice]
    end

    STATE --> ACTIVE
    STATE --> PARTIAL
    STATE --> CANCELLED

    ACTIVE --> ADDON_ITEMS
    PARTIAL --> PRORATION_ITEMS
    CANCELLED --> CREDIT_ITEMS
    PLAN_ITEMS --> COMBINE
    ADDON_ITEMS --> COMBINE
    PRORATION_ITEMS --> COMBINE
    CREDIT_ITEMS --> COMBINE

    COMBINE --> VALIDATE
    VALIDATE --> GENERATE

    %% Styling
    classDef state fill:#e3f2fd
    classDef lineitem fill:#f3e5f5
    classDef assembly fill:#e8f5e8

    class STATE,ACTIVE,PARTIAL,CANCELLED state
    class PLAN_ITEMS,ADDON_ITEMS,PRORATION_ITEMS,CREDIT_ITEMS lineitem
    class COMBINE,VALIDATE,GENERATE assembly
```

## Error Handling and Edge Cases

```mermaid
flowchart TD
    subgraph "Error Scenarios"
        E1[Usage Limit Conflict]
        E2[Reset Period Mismatch]
        E3[Proration Edge Case]
        E4[Overage Calculation Conflict]
    end

    subgraph "Resolution Strategies"
        R1[Sum Limits for Metered Features]
        R2[Enforce Same Reset Period]
        R3[Calculate Daily Rates]
        R4[Apply Source-Specific Overage]
    end

    subgraph "Fallback Actions"
        F1[Return Error with Details]
        F2[Use Default Values]
        F3[Log Warning and Continue]
        F4[Manual Review Required]
    end

    E1 --> R1
    E2 --> R2
    E3 --> R3
    E4 --> R4

    R1 --> F1
    R2 --> F2
    R3 --> F3
    R4 --> F4

    %% Styling
    classDef error fill:#ffebee
    classDef resolution fill:#e8f5e8
    classDef fallback fill:#fff3e0

    class E1,E2,E3,E4 error
    class R1,R2,R3,R4 resolution
    class F1,F2,F3,F4 fallback
```

## Performance Optimization Points

```mermaid
graph LR
    subgraph "Caching Strategy"
        C1[Entitlement Cache]
        C2[Usage Cache]
        C3[Invoice Template Cache]
    end

    subgraph "Batch Processing"
        B1[Event Batching]
        B2[Usage Aggregation]
        B3[Invoice Generation]
    end

    subgraph "Database Optimization"
        D1[Usage Indexes]
        D2[Entitlement Indexes]
        D3[Addon Indexes]
    end

    subgraph "Parallel Processing"
        P1[Usage Calculation]
        P2[Entitlement Aggregation]
        P3[Line Item Generation]
    end

    C1 --> B1
    C2 --> B2
    C3 --> B3
    B1 --> D1
    B2 --> D2
    B3 --> D3
    D1 --> P1
    D2 --> P2
    D3 --> P3

    %% Styling
    classDef cache fill:#e3f2fd
    classDef batch fill:#f3e5f5
    classDef db fill:#e8f5e8
    classDef parallel fill:#fff3e0

    class C1,C2,C3 cache
    class B1,B2,B3 batch
    class D1,D2,D3 db
    class P1,P2,P3 parallel
```

## Enhanced Function Integration Points

```mermaid
graph LR
    subgraph "Current Functions (Enhanced)"
        CALC_FIXED[CalculateFixedCharges<br/>+ Addon Line Items<br/>+ Proration Logic]
        CALC_USAGE[CalculateUsageCharges<br/>+ Combined Entitlements<br/>+ Overage Attribution]
        GET_USAGE[GetUsageBySubscription<br/>+ Addon Entitlement Merge<br/>+ Overage Detection]
        GET_ENTITLEMENTS[GetCustomerEntitlements<br/>+ Plan + Addon Merge<br/>+ Source Attribution]
    end

    subgraph "New Functions"
        ADDON_SVC[AddonService<br/>• AddAddonToSubscription<br/>• RemoveAddonFromSubscription<br/>• CalculateProration]
        ADDON_REPO[AddonRepository<br/>• GetSubscriptionAddons<br/>• CreateSubscriptionAddon<br/>• UpdateAddonStatus]
    end

    subgraph "Data Sources"
        PLAN_DATA[(Plan Line Items)]
        ADDON_DATA[(Subscription Addons)]
        ENTITLE_DATA[(Plan + Addon Entitlements)]
    end

    subgraph "Invoice Output"
        PLAN_LINES[Plan Line Items]
        ADDON_LINES[Addon Line Items]
        PRORATION_LINES[Proration Line Items]
        OVERAGE_LINES[Overage Line Items]
    end

    PLAN_DATA --> CALC_FIXED
    ADDON_DATA --> CALC_FIXED
    ENTITLE_DATA --> CALC_USAGE
    ADDON_DATA --> GET_USAGE
    ENTITLE_DATA --> GET_ENTITLEMENTS

    CALC_FIXED --> PLAN_LINES
    CALC_FIXED --> ADDON_LINES
    CALC_FIXED --> PRORATION_LINES
    CALC_USAGE --> OVERAGE_LINES

    ADDON_SVC --> ADDON_DATA
    ADDON_REPO --> ADDON_DATA

    %% Styling
    classDef enhanced fill:#fff3e0
    classDef new fill:#e8f5e8
    classDef data fill:#e3f2fd
    classDef output fill:#f3e5f5

    class CALC_FIXED,CALC_USAGE,GET_USAGE,GET_ENTITLEMENTS enhanced
    class ADDON_SVC,ADDON_REPO new
    class PLAN_DATA,ADDON_DATA,ENTITLE_DATA data
    class PLAN_LINES,ADDON_LINES,PRORATION_LINES,OVERAGE_LINES output
```

## Summary: Minimal Integration Approach

This approach provides comprehensive addon functionality while:

1. **Preserving Event System**: No changes to event ingestion
2. **Enhancing Key Functions**: Minimal changes to existing billing functions
3. **Adding New Services**: Clean addon management layer
4. **Invoice-Level Integration**: All addon logic handled during invoice generation
5. **Mid-Cycle Flexibility**: Full support for addon changes with proper proration

The system maintains backward compatibility while adding powerful addon capabilities through strategic enhancements to existing functions and addition of focused addon management services.
