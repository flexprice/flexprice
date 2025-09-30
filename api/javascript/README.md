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

### Published Package

```bash
npm install @flexprice/sdk --save
```

### Unpublished (Development)

```bash
npm install PATH_TO_GENERATED_PACKAGE --save
```

## Quick Start

### JavaScript (CommonJS)

```javascript
const FlexPrice = require("@flexprice/sdk");
require("dotenv").config();

// Configure the API client
const defaultClient = FlexPrice.ApiClient.instance;
defaultClient.basePath = "https://api.cloud.flexprice.io/v1";

const apiKeyAuth = defaultClient.authentications["ApiKeyAuth"];
apiKeyAuth.apiKey = process.env.FLEXPRICE_API_KEY;
apiKeyAuth.in = "header";
apiKeyAuth.name = "x-api-key";

// Create an event
const eventsApi = new FlexPrice.EventsApi();
const eventRequest = {
  event_name: "Sample Event",
  external_customer_id: "customer-123",
  properties: { source: "javascript_app" },
  source: "javascript_app",
  timestamp: new Date().toISOString(),
};

eventsApi.eventsPost(eventRequest, (error, data, response) => {
  if (error) {
    console.error("Error:", error);
  } else {
    console.log("Event created:", data);
  }
});
```

### TypeScript (ES Modules)

```typescript
import * as FlexPrice from "@flexprice/sdk";
import * as dotenv from "dotenv";

dotenv.config();

// Configure the API client with type safety
const defaultClient = FlexPrice.ApiClient.instance;
defaultClient.basePath = "https://api.cloud.flexprice.io/v1";

const apiKeyAuth = defaultClient.authentications["ApiKeyAuth"];
apiKeyAuth.apiKey = process.env.FLEXPRICE_API_KEY!;
apiKeyAuth.in = "header";
apiKeyAuth.name = "x-api-key";

// Type-safe API calls
const eventsApi = new FlexPrice.EventsApi();
const eventRequest: FlexPrice.DtoIngestEventRequest = {
  eventName: "Sample Event",
  externalCustomerId: "customer-123",
  properties: { source: "typescript_app" },
  source: "typescript_app",
  timestamp: new Date().toISOString(),
};

try {
  const result = await eventsApi.eventsPost({ event: eventRequest });
  console.log("Event created:", result);
} catch (error) {
  console.error("Error:", error);
}
```

### Browser Usage

```html
<script src="https://cdn.jsdelivr.net/npm/@flexprice/sdk/dist/flexprice-sdk.min.js"></script>
<script>
  // Configure the API client
  const defaultClient = FlexPrice.ApiClient.instance;
  defaultClient.basePath = "https://api.cloud.flexprice.io/v1";

  const apiKeyAuth = defaultClient.authentications["ApiKeyAuth"];
  apiKeyAuth.apiKey = "your-api-key-here";
  apiKeyAuth.in = "header";
  apiKeyAuth.name = "x-api-key";

  // Use the SDK
  const eventsApi = new FlexPrice.EventsApi();
  // ... rest of your code
</script>
```

## Environment Support

### Node.js Environments

- Node.js 16+
- Webpack
- Browserify

### Language Levels

- ES5 (requires Promises/A+ library)
- ES6+
- TypeScript 4.5+

### Module Systems

- CommonJS
- ES6 Modules
- UMD (Universal Module Definition)

## API Reference

### Authentication

All API requests require authentication using an API key:

```javascript
const apiKeyAuth = defaultClient.authentications["ApiKeyAuth"];
apiKeyAuth.apiKey = "sk_your_api_key_here";
apiKeyAuth.in = "header";
apiKeyAuth.name = "x-api-key";
```

### Available APIs

- **EventsApi** - Event ingestion and retrieval
- **CustomersApi** - Customer management
- **SubscriptionsApi** - Subscription management
- **InvoicesApi** - Invoice operations
- **PaymentsApi** - Payment processing
- **PlansApi** - Plan management
- **AddonsApi** - Addon management
- **CouponsApi** - Coupon management
- **WebhooksApi** - Webhook management

### Common Patterns

#### Creating Events

```javascript
const eventRequest = {
  eventName: "user_signup",
  externalCustomerId: "customer-123",
  properties: {
    plan: "premium",
    source: "website",
  },
  source: "web_app",
  timestamp: new Date().toISOString(),
};

eventsApi.eventsPost({ event: eventRequest }, callback);
```

#### Creating Customers

```javascript
const customerRequest = {
  externalId: "customer-123",
  email: "user@example.com",
  name: "John Doe",
  metadata: {
    source: "signup_form",
  },
};

customersApi.customersPost({ customer: customerRequest }, callback);
```

#### Error Handling

```javascript
eventsApi.eventsPost(eventRequest, (error, data, response) => {
  if (error) {
    if (error.status === 401) {
      console.error("Authentication failed");
    } else if (error.status === 400) {
      console.error("Bad request:", error.response.body);
    } else {
      console.error("API Error:", error.message);
    }
  } else {
    console.log("Success:", data);
  }
});
```

## Development

### Building

To build and compile the TypeScript sources to JavaScript:

```bash
npm install
npm run build
```

### Publishing

First build the package then run:

```bash
npm publish
```

### Testing

```bash
npm test
```

### Linting

```bash
npm run lint
npm run lint:fix
```

## Examples

### Running JavaScript Example

```bash
cd examples
npm install
npm start
```

### Running TypeScript Example

```bash
cd examples
npm install
npm run start:ts
```

### Building TypeScript Example

```bash
cd examples
npm run build:ts
npm run start:built
```

## Troubleshooting

### Authentication Issues

- Ensure your API key starts with `sk_`
- Verify the key is active and has required permissions
- Check that the `x-api-key` header is being sent correctly

### Property Naming

Use the correct property names based on the TypeScript interfaces:

- ✅ TypeScript: `eventName`, `externalCustomerId`, `externalId`
- ✅ API JSON: `event_name`, `external_customer_id`, `external_id`
- ❌ Mixed: `event_name` in TypeScript or `eventName` in JSON

### Parameter Passing

Pass parameters in the correct format for each API method:

- ✅ `eventsApi.eventsPost({ event: eventRequest }, callback)`
- ✅ `customersApi.customersPost({ customer: customerRequest }, callback)`
- ❌ `eventsApi.eventsPost(eventRequest, callback)` (missing wrapper object)

### TypeScript Issues

- Ensure you have TypeScript 4.5+ installed
- Use proper type imports: `import * as FlexPrice from '@flexprice/sdk'`
- Check that your `tsconfig.json` includes the SDK types

### Network Issues

- Verify your internet connection
- Check firewall/proxy settings
- Ensure the API host is accessible from your environment

## Browser Considerations

When using in browsers:

- CORS restrictions may apply - ensure your domain is whitelisted
- Never expose API keys in client-side code
- Use a proxy or backend service for sensitive operations
- Some features may be limited in browser environments

## TypeScript Definitions

The package includes comprehensive TypeScript definitions that are automatically resolved via `package.json`. All API endpoints, request/response types, and configuration options are fully typed.

## License

This SDK is licensed under the MIT License. See the LICENSE file for details.

## Support

For support and questions:

- Check the [API Documentation](https://docs.flexprice.io)
- Open an issue on [GitHub](https://github.com/flexprice/javascript-sdk)
- Contact support at [support@flexprice.io](mailto:support@flexprice.io)
