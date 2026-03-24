/**
 * FlexPrice TypeScript SDK – Quick start example
 *
 * Creates a client, creates a customer, and ingests one event.
 * Set FLEXPRICE_API_KEY (and optionally FLEXPRICE_API_HOST) in .env or environment.
 */

import { FlexPrice } from "@flexprice/sdk";

const apiKey = process.env.FLEXPRICE_API_KEY;
const apiHost = process.env.FLEXPRICE_API_HOST ?? "us.api.flexprice.io";

if (!apiKey) {
  console.error("Set FLEXPRICE_API_KEY in .env or environment");
  process.exit(1);
}

const serverURL = apiHost.startsWith("http") ? apiHost : `https://${apiHost}`;

const flexPrice = new FlexPrice({
  serverURL,
  apiKeyAuth: apiKey,
});

async function main() {
  const customerId = `sample-customer-${Date.now()}`;

  const customer = await flexPrice.customers.createCustomer({
    externalId: customerId,
    email: "user@example.com",
    name: "Example Customer",
  });
  console.log("Customer:", customer);

  const eventResult = await flexPrice.events.ingestEvent({
    eventName: "Sample Event",
    externalCustomerId: customerId,
    properties: { source: "ts_sdk", environment: "test" },
    source: "ts_sdk",
  });
  console.log("Event ingested:", eventResult);
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
