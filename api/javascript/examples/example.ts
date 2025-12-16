import { Flexprice } from "@flexprice/sdk";

const apiKey = process.env.FLEXPRICE_API_KEY;
const apiHost = process.env.FLEXPRICE_API_HOST || "https://api.cloud.flexprice.io";

if (!apiKey) {
  console.error("Error: FLEXPRICE_API_KEY required");
  process.exit(1);
}

console.log("FlexPrice TypeScript SDK Example");
console.log("=".repeat(50));

const sdk = new Flexprice({
  apiKeyAuth: apiKey,
  serverURL: apiHost,
});

// Send event
console.log("\n1. Sending event...");
sdk.events.ingest({
  externalCustomerId: "customer_123",
  eventName: "api_call",
  properties: { method: "GET" },
  timestamp: new Date().toISOString(),
}).then(result => {
  console.log("✓ Event sent:", result.object);
}).catch(error => {
  console.error("Error:", error);
});

console.log("\nExample completed!");
