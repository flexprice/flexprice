# FlexPrice TypeScript / JavaScript SDK

Type-safe TypeScript/JavaScript client for the FlexPrice API: billing, metering, and subscription management for SaaS and usage-based products.

## Requirements

- Node.js 18+ (or see [RUNTIMES.md](RUNTIMES.md) for supported runtimes)

## Installation

```bash
npm add flexprice-ts
```

With pnpm, bun, or yarn:

```bash
pnpm add flexprice-ts
bun add flexprice-ts
yarn add flexprice-ts
```

The package supports both CommonJS and ESM.

## Quick start

Initialize the client with your server URL and API key, then create a customer and ingest an event:

```typescript
import { FlexPrice } from "flexprice-ts";

const flexPrice = new FlexPrice({
  serverURL: "https://us.api.flexprice.io",
  apiKeyAuth: process.env.FLEXPRICE_API_KEY ?? "YOUR_API_KEY",
});

async function main() {
  // Create a customer
  const customer = await flexPrice.customers.createCustomer({
    externalId: "customer-123",
    email: "user@example.com",
    name: "Example Customer",
  });
  console.log(customer);

  // Ingest an event (use snake_case for request body fields where required by the API)
  const eventResult = await flexPrice.events.ingestEvent({
    eventName: "Sample Event",
    externalCustomerId: "customer-123",
    properties: { source: "ts_sdk", environment: "test" },
    source: "ts_sdk",
  });
  console.log(eventResult);
}

main();
```

## Property names (snake_case)

For request bodies, the API often expects **snake_case** field names. The SDK may accept camelCase and serialize to snake_case; if you see validation errors, use the API shape:

- Prefer: `event_name`, `external_customer_id`, `page_size`
- Avoid using only camelCase in raw payloads if the API spec uses snake_case

Check the [API reference](https://docs.flexprice.io) or generated types for the exact request shapes.

## TypeScript

The package ships with TypeScript definitions. Use the client with full type safety:

```typescript
import { FlexPrice } from "flexprice-ts";

const flexPrice = new FlexPrice({
  serverURL: "https://us.api.flexprice.io",
  apiKeyAuth: process.env.FLEXPRICE_API_KEY!,
});

const result = await flexPrice.events.ingestEvent({
  eventName: "usage",
  externalCustomerId: "cust_123",
  properties: { units: 10 },
  source: "backend",
});
```

## Authentication

- Set the API key via `apiKeyAuth` when constructing `FlexPrice`. The SDK sends it in the `x-api-key` header.
- Use environment variables (e.g. `FLEXPRICE_API_KEY`) and never expose keys in client-side or public code. Get keys from your [FlexPrice dashboard](https://app.flexprice.io) or docs.

## Features

- Full API coverage (customers, plans, events, invoices, payments, entitlements, etc.)
- TypeScript types for requests and responses
- Built-in retries and error handling
- ESM and CommonJS support

For a full list of operations, see the [API reference](https://docs.flexprice.io) or the generated `docs/sdks/` in this repo.

## Troubleshooting

- **Missing or invalid API key:** Ensure `apiKeyAuth` is set and the key is active. Use server-side only.
- **Wrong server URL:** Use `https://us.api.flexprice.io` (no trailing slash).
- **Validation or 4xx errors:** Confirm request body field names (snake_case vs camelCase) and required fields against the [API docs](https://docs.flexprice.io).
- **Parameter passing:** Pass the request object directly to methods (e.g. `ingestEvent({ ... })`), not wrapped in an extra key, unless the SDK docs say otherwise.

## Documentation

- [FlexPrice API documentation](https://docs.flexprice.io)
- [TypeScript SDK examples](src/) in this repo
