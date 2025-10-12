# FlexPrice JavaScript/TypeScript SDK

[![npm version](https://badge.fury.io/js/%40flexprice%2Fsdk.svg)](https://badge.fury.io/js/%40flexprice%2Fsdk)
[![TypeScript](https://img.shields.io/badge/TypeScript-Ready-blue.svg)](https://www.typescriptlang.org/)

Official TypeScript/JavaScript SDK for the FlexPrice API with modern ES7 module support and comprehensive type safety.

## Features

- ✅ **Full TypeScript Support** - Complete type definitions for all API endpoints
- ✅ **Modern ES7 Modules** - Native ES modules with CommonJS fallback
- ✅ **Fetch API** - Built on modern web standards
- ✅ **Browser Compatible** - Works in Node.js, Webpack, and Browserify
- ✅ **Promise & Callback Support** - Flexible async patterns
- ✅ **Comprehensive Documentation** - Auto-generated from OpenAPI specs
- ✅ **Error Handling** - Detailed error messages and status codes

## Installation

```bash
npm install @flexprice/sdk --save
```

## Quick Start

```typescript
import { Configuration, EventsApi, CustomersApi } from "@flexprice/sdk";

// Configure the API client
const config = new Configuration({
  basePath: "https://api.cloud.flexprice.io/v1",
  apiKey: "your_api_key_here",
  headers: {
    "X-Environment-ID": "your_environment_id_here",
  },
});

// Create API instances
const eventsApi = new EventsApi(config);
const customersApi = new CustomersApi(config);

// Use APIs directly
const eventRequest = {
  eventName: "user_signup",
  externalCustomerId: "customer-123",
  properties: {
    plan: "premium",
    source: "website",
  },
};

await eventsApi.eventsPost({ event: eventRequest });
```

## Environment Setup

### Environment Variables

Create a `.env` file in your project root:

```bash
# FlexPrice Configuration
FLEXPRICE_API_KEY=sk_your_api_key_here
FLEXPRICE_BASE_URL=https://api.cloud.flexprice.io/v1
FLEXPRICE_ENVIRONMENT_ID=env_your_environment_id_here
```

### Vite/React Applications

For Vite applications, prefix environment variables with `VITE_`:

```bash
# .env
VITE_FLEXPRICE_API_KEY=sk_your_api_key_here
VITE_FLEXPRICE_BASE_URL=https://api.cloud.flexprice.io/v1
VITE_FLEXPRICE_ENVIRONMENT_ID=env_your_environment_id_here
```

```typescript
// config.ts
import { Configuration, EventsApi, CustomersApi } from "@flexprice/sdk";

const API_KEY = import.meta.env.VITE_FLEXPRICE_API_KEY;
const BASE_PATH = import.meta.env.VITE_FLEXPRICE_BASE_URL;
const ENVIRONMENT_ID = import.meta.env.VITE_FLEXPRICE_ENVIRONMENT_ID;

const config = new Configuration({
  basePath: BASE_PATH,
  apiKey: API_KEY,
  headers: {
    "X-Environment-ID": ENVIRONMENT_ID,
  },
});

// Export configured API instances
export const eventsApi = new EventsApi(config);
export const customersApi = new CustomersApi(config);
```

## Available APIs

- **EventsApi** - Event ingestion and analytics
- **CustomersApi** - Customer management
- **AuthApi** - Authentication and user management
- **PlansApi** - Subscription plan management
- **FeaturesApi** - Feature management
- **InvoicesApi** - Invoice operations
- **SubscriptionsApi** - Subscription management
- **AddonsApi** - Addon management
- **CouponsApi** - Coupon management
- **CreditNotesApi** - Credit note management
- **EntitlementsApi** - Feature access control
- **UsersApi** - User management

## API Examples

### Events API

```typescript
import { EventsApi } from "@flexprice/sdk";

const eventsApi = new EventsApi(config);

// Ingest a single event
await eventsApi.eventsPost({
  event: {
    eventName: "api_call",
    externalCustomerId: "customer-123",
    properties: {
      endpoint: "/api/users",
      method: "GET",
      responseTime: 150,
    },
  },
});

// Query events
const events = await eventsApi.eventsQueryPost({
  request: {
    externalCustomerId: "customer-123",
    eventName: "api_call",
    startTime: "2024-01-01T00:00:00Z",
    endTime: "2024-01-31T23:59:59Z",
    limit: 100,
  },
});

// Get usage analytics
const analytics = await eventsApi.eventsAnalyticsPost({
  request: {
    externalCustomerId: "customer-123",
    startTime: "2024-01-01T00:00:00Z",
    endTime: "2024-01-31T23:59:59Z",
    windowSize: "day",
  },
});
```

### Customers API

```typescript
import { CustomersApi } from "@flexprice/sdk";

const customersApi = new CustomersApi(config);

// Create a customer
const customer = await customersApi.customersPost({
  customer: {
    externalId: "customer-123",
    email: "user@example.com",
    name: "John Doe",
    metadata: {
      source: "signup_form",
    },
  },
});

// Get customer
const customerData = await customersApi.customersIdGet({
  id: "customer-123",
});

// Update customer
const updatedCustomer = await customersApi.customersIdPut({
  id: "customer-123",
  customer: {
    name: "John Smith",
    metadata: { plan: "premium" },
  },
});

// List customers
const customers = await customersApi.customersGet({
  limit: 50,
  offset: 0,
  status: "active",
});
```

### Authentication API

```typescript
import { AuthApi } from "@flexprice/sdk";

const authApi = new AuthApi(config);

// Login user
const authResponse = await authApi.authLoginPost({
  login: {
    email: "user@example.com",
    password: "password123",
  },
});

// Sign up new user
const signupResponse = await authApi.authSignupPost({
  signup: {
    email: "newuser@example.com",
    password: "password123",
    name: "New User",
  },
});
```

## React Integration

### With React Query

```typescript
import { useMutation, useQuery } from "@tanstack/react-query";
import { eventsApi } from "./config";

// Fetch events
const { data: events, isLoading } = useQuery({
  queryKey: ["events"],
  queryFn: () =>
    eventsApi.eventsQueryPost({
      request: {
        externalCustomerId: "customer-123",
        limit: 100,
      },
    }),
});

// Fire an event
const { mutate: fireEvent } = useMutation({
  mutationFn: (eventData) => eventsApi.eventsPost({ event: eventData }),
  onSuccess: () => {
    toast.success("Event fired successfully");
  },
  onError: (error) => {
    toast.error("Failed to fire event");
  },
});
```

### With useEffect

```typescript
import { useEffect, useState } from "react";
import { eventsApi } from "./config";

const UsageComponent = () => {
  const [usage, setUsage] = useState(null);

  useEffect(() => {
    const fetchUsage = async () => {
      try {
        const data = await eventsApi.eventsUsagePost({
          request: {
            externalCustomerId: "customer-123",
            startTime: "2024-01-01",
            endTime: "2024-01-31",
          },
        });
        setUsage(data);
      } catch (error) {
        console.error("Failed to fetch usage:", error);
      }
    };

    fetchUsage();
  }, []);

  return <div>{/* Render usage data */}</div>;
};
```

## Error Handling

```typescript
try {
  await eventsApi.eventsPost({ event: eventData });
} catch (error) {
  if (error.status === 401) {
    console.error("Authentication failed");
  } else if (error.status === 400) {
    console.error("Bad request:", error.response?.body);
  } else {
    console.error("API Error:", error.message);
  }
}
```

## TypeScript Support

The SDK includes comprehensive TypeScript definitions:

```typescript
import type {
  DtoIngestEventRequest,
  DtoGetUsageRequest,
  DtoCreateCustomerRequest,
  DtoCustomerResponse,
  // ... many more types
} from "@flexprice/sdk";

// Type-safe event creation
const event: DtoIngestEventRequest = {
  eventName: "llm_usage",
  externalCustomerId: "user_123",
  properties: {
    tokens: 150,
    model: "gpt-4",
  },
};
```

## Browser Usage

```html
<script src="https://cdn.jsdelivr.net/npm/@flexprice/sdk/dist/flexprice-sdk.min.js"></script>
<script>
  // Configure the API client
  const config = new FlexPrice.Configuration({
    basePath: "https://api.cloud.flexprice.io/v1",
    apiKey: "your-api-key-here",
    headers: {
      "X-Environment-ID": "your-environment-id-here",
    },
  });

  // Create API instance
  const eventsApi = new FlexPrice.EventsApi(config);

  // Use the SDK
  eventsApi.eventsPost({
    event: {
      eventName: "page_view",
      externalCustomerId: "user_123",
      properties: { page: "/home" },
    },
  });
</script>
```

## Troubleshooting

### Authentication Issues

- Verify the key is active and has required permissions
- Check that the `x-api-key` header is being sent correctly
- Verify the `X-Environment-ID` header is included

## Support

For support and questions:

- Check the [API Documentation](https://docs.flexprice.io)
- Contact support at [support@flexprice.io](mailto:support@flexprice.io)
