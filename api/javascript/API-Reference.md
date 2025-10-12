# CustomerPortal API Reference

## Class: CustomerPortal

The main class for accessing customer dashboard data.

### Constructor

```typescript
constructor(configuration?: runtime.Configuration)
```

**Parameters:**

- `configuration` - Optional FlexPrice API configuration object

**Example:**

```typescript
const configuration = new Configuration({
  apiKey: "your-api-key",
  basePath: "https://api.cloud.flexprice.io",
});

const customerPortal = new CustomerPortal(configuration);
```

## Methods

### getDashboardData(customerExternalId, options?)

Fetches complete customer dashboard data in a single optimized call.

**Signature:**

```typescript
async getDashboardData(
  customerExternalId: string,
  options: DashboardOptions = {}
): Promise<CustomerDashboardData>
```

**Parameters:**

- `customerExternalId: string` - Customer's external ID (the ID you provided when creating the customer)
- `options: DashboardOptions` - Optional configuration object

**Returns:** `Promise<CustomerDashboardData>`

**Example:**

```typescript
const data = await customerPortal.getDashboardData("customer-123", {
  subscriptionLimit: 10,
  invoiceLimit: 5,
  days: 30,
});
```

### getUsage(customerId)

Gets customer usage data.

**Signature:**

```typescript
async getUsage(customerId: string): Promise<UsageResponse>
```

**Parameters:**

- `customerId: string` - Customer ID (internal FlexPrice ID)

**Returns:** `Promise<UsageResponse>`

**Example:**

```typescript
const usage = await customerPortal.getUsage("customer-123");
if (usage.data) {
  console.log("Usage data:", usage.data);
} else if (usage.error) {
  console.error("Error:", usage.error);
}
```

### getEntitlements(customerId)

Gets customer entitlements.

**Signature:**

```typescript
async getEntitlements(customerId: string): Promise<EntitlementsResponse>
```

**Parameters:**

- `customerId: string` - Customer ID (internal FlexPrice ID)

**Returns:** `Promise<EntitlementsResponse>`

**Example:**

```typescript
const entitlements = await customerPortal.getEntitlements("customer-123");
```

### getWalletBalance(customerId)

Gets customer wallet balance.

**Signature:**

```typescript
async getWalletBalance(customerId: string): Promise<WalletBalanceResponse>
```

**Parameters:**

- `customerId: string` - Customer ID (internal FlexPrice ID)

**Returns:** `Promise<WalletBalanceResponse>`

**Example:**

```typescript
const wallet = await customerPortal.getWalletBalance("customer-123");
if (wallet.data) {
  console.log("Balance:", wallet.data.realTimeBalance);
}
```

### getActiveSubscriptions(customerId, limit?, status?, startTime?, endTime?)

Gets active subscriptions for a customer.

**Signature:**

```typescript
async getActiveSubscriptions(
  customerId: string,
  limit: number = 10,
  status?: SubscriptionsGetSubscriptionStatusEnum[],
  startTime?: string,
  endTime?: string
): Promise<ActiveSubscriptionsResponse>
```

**Parameters:**

- `customerId: string` - Customer ID (internal FlexPrice ID)
- `limit: number` - Maximum number of subscriptions (default: 10)
- `status: SubscriptionsGetSubscriptionStatusEnum[]` - Filter by subscription status
- `startTime: string` - Start time filter (ISO string)
- `endTime: string` - End time filter (ISO string)

**Returns:** `Promise<ActiveSubscriptionsResponse>`

**Example:**

```typescript
const subscriptions = await customerPortal.getActiveSubscriptions(
  "customer-123",
  5, // limit
  [SubscriptionsGetSubscriptionStatusEnum.ACTIVE], // status
  "2024-01-01T00:00:00Z", // startTime
  "2024-12-31T23:59:59Z" // endTime
);
```

### getRecentInvoices(customerId, limit?, status?, startTime?, endTime?)

Gets recent invoices for a customer.

**Signature:**

```typescript
async getRecentInvoices(
  customerId: string,
  limit: number = 5,
  status?: InvoicesGetInvoiceStatusEnum[],
  startTime?: string,
  endTime?: string
): Promise<RecentInvoicesResponse>
```

**Parameters:**

- `customerId: string` - Customer ID (internal FlexPrice ID)
- `limit: number` - Maximum number of invoices (default: 5)
- `status: InvoicesGetInvoiceStatusEnum[]` - Filter by invoice status
- `startTime: string` - Start time filter (ISO string)
- `endTime: string` - End time filter (ISO string)

**Returns:** `Promise<RecentInvoicesResponse>`

**Example:**

```typescript
const invoices = await customerPortal.getRecentInvoices(
  "customer-123",
  10, // limit
  [InvoicesGetInvoiceStatusEnum.FINALIZED] // status
);
```

## Interfaces

### DashboardOptions

Configuration options for the `getDashboardData` method.

```typescript
interface DashboardOptions {
  // Pagination limits
  subscriptionLimit?: number; // Default: 10
  invoiceLimit?: number; // Default: 5
  entitlementLimit?: number; // Default: 50

  // Status filters
  subscriptionStatus?: SubscriptionsGetSubscriptionStatusEnum[];
  invoiceStatus?: InvoicesGetInvoiceStatusEnum[];

  // Time range filters
  days?: number; // Last N days
  startDate?: string; // Start date (ISO string)
  endDate?: string; // End date (ISO string)

  // What to include
  includeCustomer?: boolean; // Default: true
  includeSubscriptions?: boolean; // Default: true
  includeInvoices?: boolean; // Default: true
  includeUsage?: boolean; // Default: true
  includeEntitlements?: boolean; // Default: true
  includeSummary?: boolean; // Default: true
  includeAnalytics?: boolean; // Default: false
  includeFeatures?: boolean; // Default: false
  includeWalletBalance?: boolean; // Default: true

  // Advanced filtering
  featureIds?: string[]; // Filter by specific features
  subscriptionIds?: string[]; // Filter by specific subscriptions
}
```

### CustomerDashboardData

Main response object from `getDashboardData()`.

```typescript
interface CustomerDashboardData {
  // Core data
  customer?: DtoCustomerResponse;
  usage?: DtoCustomerUsageSummaryResponse;
  entitlements?: DtoCustomerEntitlementsResponse;
  walletBalance?: DtoWalletBalanceResponse;
  activeSubscriptions?: DtoSubscriptionResponse[];
  invoices?: DtoInvoiceResponse[];
  summary?: DtoCustomerMultiCurrencyInvoiceSummary;
  analytics?: DtoGetUsageAnalyticsResponse;
  features?: DtoFeatureResponse[];

  // Metadata
  metadata: {
    fetchedAt: string;
    customerId: string;
    totalSubscriptions?: number;
    totalInvoices?: number;
    totalWallets?: number;
    totalFeatures?: number;
    errors?: string[];
    warnings?: string[];
  };
}
```

### Individual Method Response Types

#### UsageResponse

```typescript
interface UsageResponse {
  data?: DtoCustomerUsageSummaryResponse;
  error?: string;
}
```

#### EntitlementsResponse

```typescript
interface EntitlementsResponse {
  data?: DtoCustomerEntitlementsResponse;
  error?: string;
}
```

#### WalletBalanceResponse

```typescript
interface WalletBalanceResponse {
  data?: DtoWalletBalanceResponse;
  error?: string;
}
```

#### ActiveSubscriptionsResponse

```typescript
interface ActiveSubscriptionsResponse {
  data?: DtoSubscriptionResponse[];
  error?: string;
}
```

#### RecentInvoicesResponse

```typescript
interface RecentInvoicesResponse {
  data?: DtoInvoiceResponse[];
  error?: string;
}
```

## Enums

### ApiOperationType

Used internally for type-safe API operation tracking.

```typescript
enum ApiOperationType {
  CUSTOMER_LOOKUP = "Customer Lookup",
  USAGE = "Usage",
  ENTITLEMENTS = "Entitlements",
  WALLET = "Wallet",
  SUBSCRIPTIONS = "Subscriptions",
  INVOICES = "Invoices",
  SUMMARY = "Summary",
  FEATURES = "Features",
}
```

### SubscriptionsGetSubscriptionStatusEnum

Subscription status values.

```typescript
enum SubscriptionsGetSubscriptionStatusEnum {
  ACTIVE = "active",
  PAUSED = "paused",
  CANCELLED = "cancelled",
  INCOMPLETE = "incomplete",
  INCOMPLETE_EXPIRED = "incomplete_expired",
  PAST_DUE = "past_due",
  TRIALING = "trialing",
  UNPAID = "unpaid",
}
```

### InvoicesGetInvoiceStatusEnum

Invoice status values.

```typescript
enum InvoicesGetInvoiceStatusEnum {
  DRAFT = "draft",
  FINALIZED = "finalized",
  VOID = "void",
  PAID = "paid",
  UNPAID = "unpaid",
  OVERPAID = "overpaid",
}
```

## Error Handling

### Error Collection

Errors are collected in two ways:

1. **Global errors** in `CustomerDashboardData.metadata.errors`
2. **Individual method errors** in response `error` fields

### Safe API Calls

All API calls are wrapped in a `safeCall` function that:

- Catches and logs errors
- Returns `undefined` for failed calls
- Collects errors in metadata
- Uses typed operation names for better debugging

### Example Error Handling

```typescript
const dashboardData = await customerPortal.getDashboardData("customer-123");

// Check for global errors
if (dashboardData.metadata.errors) {
  console.error("Global errors:", dashboardData.metadata.errors);
}

// Check individual method responses
const usage = await customerPortal.getUsage("customer-123");
if (usage.error) {
  console.error("Usage fetch failed:", usage.error);
} else {
  console.log("Usage data:", usage.data);
}
```

## TypeScript Support

The SDK is fully typed with TypeScript definitions. All interfaces, enums, and method signatures are available for autocomplete and type checking.

### Key Features

- Compile-time type checking
- IntelliSense support
- Enum-based status filtering
- Generic response types
- Optional chaining support

### Import Examples

```typescript
import {
  CustomerPortal,
  Configuration,
  DashboardOptions,
  CustomerDashboardData,
  SubscriptionsGetSubscriptionStatusEnum,
  InvoicesGetInvoiceStatusEnum,
} from "@flexprice/javascript-sdk";
```
