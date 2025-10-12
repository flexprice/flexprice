# FlexPrice Customer Portal SDK

A comprehensive TypeScript SDK for building customer dashboards with FlexPrice. This SDK provides a clean, type-safe interface for fetching customer data including usage, entitlements, wallet balance, subscriptions, and invoices.

## Quick Start

```typescript
import { CustomerPortal, Configuration } from "@flexprice/javascript-sdk";

// Initialize with your API configuration
const configuration = new Configuration({
  apiKey: "your-api-key",
  basePath: "https://api.cloud.flexprice.io",
});

// Create CustomerPortal instance
const customerPortal = new CustomerPortal(configuration);

// Get complete dashboard data
const dashboardData = await customerPortal.getDashboardData("customer-123", {
  subscriptionLimit: 10,
  invoiceLimit: 5,
  days: 30,
});
```

## Main Methods

### getDashboardData(customerExternalId, options?)

Fetches complete customer dashboard data in a single optimized call.

**Parameters:**

- `customerExternalId: string` - Customer's external ID
- `options: DashboardOptions` - Optional configuration

**Returns:** `Promise<CustomerDashboardData>`

### Individual Methods

```typescript
// Get specific data types
const usage = await customerPortal.getUsage("customer-123");
const entitlements = await customerPortal.getEntitlements("customer-123");
const wallet = await customerPortal.getWalletBalance("customer-123");
const subscriptions = await customerPortal.getActiveSubscriptions(
  "customer-123"
);
const invoices = await customerPortal.getRecentInvoices("customer-123");
```

## Response Format

### CustomerDashboardData

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

## Customization Options

### DashboardOptions

```typescript
interface DashboardOptions {
  // Pagination
  subscriptionLimit?: number; // Default: 10
  invoiceLimit?: number; // Default: 5
  entitlementLimit?: number; // Default: 50

  // Status filters
  subscriptionStatus?: SubscriptionsGetSubscriptionStatusEnum[];
  invoiceStatus?: InvoicesGetInvoiceStatusEnum[];

  // Time range
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

## Examples

### Basic Usage

```typescript
// Get basic dashboard data
const data = await customerPortal.getDashboardData("customer-123");

console.log("Customer:", data.customer?.name);
console.log("Active Subscriptions:", data.activeSubscriptions?.length);
console.log("Wallet Balance:", data.walletBalance?.realTimeBalance);
```

### Advanced Filtering

```typescript
// Get dashboard with custom filters
const data = await customerPortal.getDashboardData("customer-123", {
  subscriptionLimit: 20,
  invoiceLimit: 10,
  subscriptionStatus: [SubscriptionsGetSubscriptionStatusEnum.ACTIVE],
  invoiceStatus: [InvoicesGetInvoiceStatusEnum.FINALIZED],
  days: 90,
  includeFeatures: true,
  featureIds: ["feature-1", "feature-2"],
});
```

### Error Handling

```typescript
const dashboardData = await customerPortal.getDashboardData("customer-123");

// Check for errors
if (dashboardData.metadata.errors) {
  console.error("Errors occurred:", dashboardData.metadata.errors);
}

// Check individual method responses
const usage = await customerPortal.getUsage("customer-123");
if (usage.error) {
  console.error("Usage fetch failed:", usage.error);
}
```

## Available Enums

### Subscription Status

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

### Invoice Status

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

## React Example

```typescript
import React, { useEffect, useState } from "react";
import {
  CustomerPortal,
  Configuration,
  CustomerDashboardData,
} from "@flexprice/javascript-sdk";

const CustomerDashboard: React.FC<{ customerId: string }> = ({
  customerId,
}) => {
  const [data, setData] = useState<CustomerDashboardData | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    const fetchData = async () => {
      const configuration = new Configuration({
        apiKey: process.env.REACT_APP_FLEXPRICE_API_KEY,
      });

      const customerPortal = new CustomerPortal(configuration);

      try {
        const dashboardData = await customerPortal.getDashboardData(
          customerId,
          {
            subscriptionLimit: 10,
            invoiceLimit: 5,
            includeFeatures: true,
          }
        );

        setData(dashboardData);
      } catch (error) {
        console.error("Failed to fetch dashboard data:", error);
      } finally {
        setLoading(false);
      }
    };

    fetchData();
  }, [customerId]);

  if (loading) return <div>Loading...</div>;
  if (!data) return <div>Failed to load data</div>;

  return (
    <div>
      <h1>Customer Dashboard</h1>

      {/* Customer Info */}
      {data.customer && (
        <div>
          <h2>{data.customer.name}</h2>
          <p>Email: {data.customer.email}</p>
        </div>
      )}

      {/* Wallet Balance */}
      {data.walletBalance && (
        <div>
          <h3>Wallet Balance</h3>
          <p>${data.walletBalance.realTimeBalance}</p>
        </div>
      )}

      {/* Active Subscriptions */}
      <div>
        <h3>Active Subscriptions ({data.activeSubscriptions?.length || 0})</h3>
        {data.activeSubscriptions?.map((sub) => (
          <div key={sub.id}>
            <p>
              {sub.planName} - {sub.status}
            </p>
          </div>
        ))}
      </div>

      {/* Recent Invoices */}
      <div>
        <h3>Recent Invoices ({data.invoices?.length || 0})</h3>
        {data.invoices?.map((invoice) => (
          <div key={invoice.id}>
            <p>
              Invoice #{invoice.invoiceNumber} - ${invoice.total}
            </p>
          </div>
        ))}
      </div>

      {/* Error Display */}
      {data.metadata.errors && (
        <div style={{ color: "red" }}>
          <h4>Errors:</h4>
          <ul>
            {data.metadata.errors.map((error, index) => (
              <li key={index}>{error}</li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
};

export default CustomerDashboard;
```

## Best Practices

1. **Use `getDashboardData()` for most cases** - it's optimized for parallel data fetching
2. **Handle errors gracefully** - check `metadata.errors` and individual response `error` fields
3. **Use enums for status filtering** - provides type safety and prevents typos
4. **Configure options based on your needs** - only fetch data you actually use
5. **Use TypeScript** - take advantage of the full type system

## Troubleshooting

### Common Issues

1. **API Key Issues**: Ensure your API key is valid and has necessary permissions
2. **Customer Not Found**: Verify the customer external ID exists
3. **Rate Limiting**: Implement retry logic with exponential backoff
4. **Network Issues**: Handle network failures gracefully

### Debug Mode

```typescript
const configuration = new Configuration({
  apiKey: "your-api-key",
  middleware: [
    {
      pre: (context) => {
        console.log("API Request:", context.url);
        return context;
      },
    },
  ],
});
```

## Files in this Directory

- `src/apis/CustomerPortal.ts` - Main CustomerPortal class implementation
- `CustomerPortal-Usage.md` - Detailed usage guide
- `API-Reference.md` - Complete API reference
- `example-usage.ts` - Code examples and patterns
- `README.md` - This file

## Integration

The CustomerPortal is automatically exported from the main SDK when you import from `@flexprice/javascript-sdk`. No additional setup is required.

```typescript
import { CustomerPortal, Configuration } from "@flexprice/javascript-sdk";
```

## Support

For additional support and documentation, visit:

- [FlexPrice Documentation](https://docs.flexprice.com)
- [API Reference](https://api.cloud.flexprice.io/docs)
- [GitHub Repository](https://github.com/flexprice/javascript-sdk)
