# FlexPrice TypeScript / JavaScript SDK

Type-safe TypeScript/JavaScript client for the FlexPrice API: billing, metering, and subscription management for SaaS and usage-based products.

## Requirements

- Node.js 18+ (or see [RUNTIMES.md](RUNTIMES.md) for supported runtimes)

## Installation

```bash
npm i @flexprice/sdk
```

With pnpm, bun, or yarn:

```bash
pnpm add @flexprice/sdk
bun add @flexprice/sdk
yarn add @flexprice/sdk
```

The package supports both CommonJS and ESM.

Then in your code:

```typescript
import { FlexPrice } from "@flexprice/sdk";
```

**All generated modules** (shared models, enums, errors, operation types, SDK types) are re-exported from the package root so you can import everything from one place. Run `npm run build` before publishing so both ESM and CommonJS entry points (`dist/esm/index.js` and `dist/commonjs/index.js`) include these re-exports; then `import` and `require("@flexprice/sdk")` both expose types/enums from the root.

```typescript
import {
  FlexPrice,
  FeatureType,
  Status,
  FlexpriceError,
  CreateCustomerRequest,
  createPageIterator,
} from "@flexprice/sdk";
// No need for: .../dist/sdk/models/shared, .../errors, .../operations, .../types
```

Runnable samples are in the `examples/` directory.

## Environment

| Variable | Required | Description |
| -------- | -------- | ----------- |
| `FLEXPRICE_API_KEY` | Yes | API key |
| `FLEXPRICE_API_HOST` | Optional | Full base URL including `https://` and `/v1` (default: `https://us.api.flexprice.io/v1`). No trailing slash. |

**Integration tests** in [api/tests/ts/test_sdk.ts](../../tests/ts/test_sdk.ts) use a different env shape; see [api/tests/README.md](../../tests/README.md).

## Quick start

Initialize the client, then create a customer and ingest an event:

```typescript
import { FlexPrice } from "@flexprice/sdk";

const apiKey = process.env.FLEXPRICE_API_KEY ?? "YOUR_API_KEY";
const serverURL =
  process.env.FLEXPRICE_API_HOST ?? "https://us.api.flexprice.io/v1";

const flexPrice = new FlexPrice({
  serverURL,
  apiKeyAuth: apiKey,
});

async function main() {
  const externalId = `customer-${Date.now()}`;
  const customer = await flexPrice.customers.createCustomer({
    externalId,
    email: "user@example.com",
    name: "Example Customer",
  });
  console.log(customer);

  const eventResult = await flexPrice.events.ingestEvent({
    eventName: "Sample Event",
    externalCustomerId: externalId,
    properties: { source: "ts_sdk", environment: "test" },
    source: "ts_sdk",
  });
  console.log(eventResult);
}

main();
```

For more examples and all API operations, see the [API reference](https://docs.flexprice.io) and the [examples](examples/) in this repo.

## Property names (snake_case)

For request bodies, the API often expects **snake_case** field names. The SDK may accept camelCase and serialize to snake_case; if you see validation errors, use the API shape:

- Prefer: `event_name`, `external_customer_id`, `page_size`
- Avoid using only camelCase in raw payloads if the API spec uses snake_case

Check the [API reference](https://docs.flexprice.io) for the exact request shapes.

## TypeScript

The package ships with TypeScript definitions. Use the client with full type safety:

```typescript
import { FlexPrice } from "@flexprice/sdk";

const serverURL =
  process.env.FLEXPRICE_API_HOST ?? "https://us.api.flexprice.io/v1";

const flexPrice = new FlexPrice({
  serverURL,
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

The SDK supports two credentials. Set one of them, not both.

| Option | Header | Use it for |
| ------ | ------ | ---------- |
| `apiKeyAuth` | `x-api-key` | Servers and scripts |
| `bearerAuth` | `Authorization: Bearer <token>` | A browser app acting as a signed-in FlexPrice user |

**API key (server side).** Set the key via `apiKeyAuth`. Keys created in the dashboard are bound to one environment, so no other option is needed.

```typescript
import { Flexprice } from "@flexprice/sdk";

const flexprice = new Flexprice({ apiKeyAuth: process.env.FLEXPRICE_API_KEY! });
```

**Signed-in user (browser).** Pass the user's JWT via `bearerAuth`, and the active environment via `environmentId`, which is sent as `X-Environment-ID`. Most endpoints are scoped to an environment and return 403 without it.

```typescript
import { Flexprice } from "@flexprice/sdk";

const flexprice = new Flexprice({
  serverURL: "https://us.api.flexprice.io/v1",
  bearerAuth: async () => getAccessToken(),
  environmentId: () => getActiveEnvironmentId(),
});
```

- Both options take a string or a function, so a refreshed token or a switched environment applies without creating a new client. The SDK does not refresh tokens itself.
- A `bearerAuth` function is called before every attempt, retries included. An `environmentId` function is called once per API call, so a retry never switches environments.
- If the value or the function result is empty, that header is not sent.
- Headers passed on a single call (`{ headers: { ... } }`) take precedence over these options, and the matching option's function is not called.
- `environmentId` also works with `apiKeyAuth`. The API ignores it for keys bound to an environment.
- Setting both `apiKeyAuth` and `bearerAuth` fails every call with an `UnexpectedClientError` before anything is sent.

Set `FLEXPRICE_API_HOST` to a full URL (see [Environment](#environment)) or rely on the default `https://us.api.flexprice.io/v1`.

## Features

- Full API coverage (customers, plans, events, invoices, payments, entitlements, etc.)
- TypeScript types for requests and responses
- Built-in retries and error handling
- ESM and CommonJS support

For a full list of operations, see the [API reference](https://docs.flexprice.io) and the [examples](examples/) in this repo.

## Troubleshooting

- **Missing or invalid API key:** Ensure `apiKeyAuth` is set and the key is active. Use server-side only.
- **401 with `bearerAuth`:** The token is missing or expired. Check that your token function returns a current JWT.
- **403 with `bearerAuth`:** Set `environmentId`. Dashboard login tokens are not bound to an environment.
- **"set either apiKeyAuth or bearerAuth, not both":** Remove one of the two options from the constructor.
- **`debugLogger`:** It prints every request header, including `Authorization` and `x-api-key`. Use it only for local debugging.
- **Wrong server URL:** Use a full URL such as `https://us.api.flexprice.io/v1` (include `/v1`; no trailing slash).
- **Validation or 4xx errors:** Confirm request body field names (snake_case vs camelCase) and required fields against the [API docs](https://docs.flexprice.io).
- **Parameter passing:** Pass the request object directly to methods (e.g. `ingestEvent({ ... })`), not wrapped in an extra key, unless the SDK docs say otherwise.

## Handling Webhooks

Flexprice sends webhook events to your server for async updates on payments, invoices, subscriptions, wallets, and more.

**Flow:**
1. Register your endpoint URL in the Flexprice dashboard
2. Receive `POST` with raw JSON body
3. Read `event_type` to route
4. Parse payload using SDK helpers
5. Handle business logic idempotently
6. Return `200` quickly

```typescript
import {
  WebhookEventName,
  webhookDtoPaymentWebhookPayloadFromJSON,
  webhookDtoSubscriptionWebhookPayloadFromJSON,
  webhookDtoInvoiceWebhookPayloadFromJSON,
} from "@flexprice/sdk";

function handleWebhook(rawBody: string): void {
  const env = JSON.parse(rawBody) as { event_type: string };

  switch (env.event_type as WebhookEventName) {
    case WebhookEventName.PaymentSuccess:
    case WebhookEventName.PaymentFailed:
    case WebhookEventName.PaymentUpdated: {
      const result = webhookDtoPaymentWebhookPayloadFromJSON(rawBody);
      if (!result.ok) { console.error("parse error", result.error); break; }
      const { payment } = result.value;
      console.log("payment", payment?.id);
      // TODO: update payment record
      break;
    }

    case WebhookEventName.SubscriptionActivated:
    case WebhookEventName.SubscriptionCancelled:
    case WebhookEventName.SubscriptionUpdated: {
      const result = webhookDtoSubscriptionWebhookPayloadFromJSON(rawBody);
      if (!result.ok) { console.error("parse error", result.error); break; }
      console.log("subscription", result.value.subscription?.id);
      break;
    }

    case WebhookEventName.InvoiceUpdateFinalized:
    case WebhookEventName.InvoicePaymentOverdue: {
      const result = webhookDtoInvoiceWebhookPayloadFromJSON(rawBody);
      if (!result.ok) { console.error("parse error", result.error); break; }
      console.log("invoice", result.value.invoice?.id);
      break;
    }

    default:
      console.log("unhandled event:", env.event_type);
  }
}
```

> Fields are auto-camelCased by the SDK: `event_type` → `eventType`, `invoice_status` → `invoiceStatus`. The `fromJSON` helpers return a `SafeParseResult<T>` — always check `.ok` before accessing `.value`.

### Event types

| Category | Events |
|---|---|
| **Payment** | `payment.created` · `payment.updated` · `payment.success` · `payment.failed` · `payment.pending` |
| **Invoice** | `invoice.create.drafted` · `invoice.update` · `invoice.update.finalized` · `invoice.update.payment` · `invoice.update.voided` · `invoice.payment.overdue` · `invoice.communication.triggered` |
| **Subscription** | `subscription.created` · `subscription.draft.created` · `subscription.activated` · `subscription.updated` · `subscription.paused` · `subscription.resumed` · `subscription.cancelled` · `subscription.renewal.due` |
| **Subscription Phase** | `subscription.phase.created` · `subscription.phase.updated` · `subscription.phase.deleted` |
| **Customer** | `customer.created` · `customer.updated` · `customer.deleted` |
| **Wallet** | `wallet.created` · `wallet.updated` · `wallet.terminated` · `wallet.transaction.created` · `wallet.transaction.updated` · `wallet.credit_balance.dropped` · `wallet.credit_balance.recovered` · `wallet.ongoing_balance.dropped` · `wallet.ongoing_balance.recovered` · `wallet.ongoing_balance.updated` |
| **Feature / Entitlement** | `feature.created` · `feature.updated` · `feature.deleted` · `feature.wallet_balance.alert` · `entitlement.created` · `entitlement.updated` · `entitlement.deleted` |
| **Credit Note** | `credit_note.created` · `credit_note.updated` |

**Production rules:**
- Keep handlers idempotent — Flexprice retries on non-`2xx`
- Return `200` for unknown event types — prevents unnecessary retries
- Do heavy processing async — respond fast, queue the work

## Documentation

- [FlexPrice API documentation](https://docs.flexprice.io)
- [TypeScript SDK examples](examples/) in this repo
- [SDK integration tests](../../tests/README.md) — different `FLEXPRICE_API_HOST` shape for automated tests
