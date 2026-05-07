/**
 * FlexPrice TypeScript SDK – Complete example
 *
 * Demonstrates: client init, customer CRUD, pagination, plan creation,
 * subscription lifecycle, event ingestion (single + bulk), invoice listing,
 * idempotency headers, per-request timeouts, and webhook handling.
 *
 * Prerequisites:
 *   FLEXPRICE_API_KEY  – your API key
 *   FLEXPRICE_API_HOST – optional, defaults to us.api.flexprice.io
 */

import { FlexPrice } from "@flexprice/sdk";
import type {
  Customer,
  Subscription,
  Invoice,
  Plan,
  CreateCustomerRequest,
  UpdateCustomerRequest,
  CreatePlanRequest,
  CreateSubscriptionRequest,
  CancelSubscriptionRequest,
  IngestEventRequest,
  BulkIngestEventRequest,
  InvoiceFilter,
  CustomerFilter,
  ListCustomersResponse,
  ListInvoicesResponse,
} from "@flexprice/sdk";
import { SDKError } from "@flexprice/sdk";

// ── Client initialisation ─────────────────────────────────────────────────────

const apiKey = process.env.FLEXPRICE_API_KEY;
const apiHost = process.env.FLEXPRICE_API_HOST ?? "us.api.flexprice.io";

if (!apiKey) {
  console.error("FLEXPRICE_API_KEY is not set. Exiting.");
  process.exit(1);
}

const serverURL = apiHost.startsWith("http") ? apiHost : `https://${apiHost}`;

const flexprice = new FlexPrice({
  serverURL,
  apiKeyAuth: apiKey,
});

// ── Helpers ───────────────────────────────────────────────────────────────────

/** Generate a unique external ID so the example is re-runnable. */
const externalId = (prefix: string): string => `${prefix}-${Date.now()}`;

// ── createOrGetCustomer ───────────────────────────────────────────────────────

/**
 * Creates a new customer, handling a 409 Conflict gracefully by fetching the
 * existing record instead.  Demonstrates idempotency key and optional chaining.
 */
async function createOrGetCustomer(): Promise<Customer> {
  console.log("\n// ── Create customer ──────────────────────────────────────");

  const customerExternalId = externalId("acme-corp");

  const request: CreateCustomerRequest = {
    externalId: customerExternalId,
    name: "Acme Corp",
    email: "billing@acme-corp.example.com",
    addressLine1: "123 Commerce Street",
    addressCity: "San Francisco",
    addressState: "CA",
    addressPostalCode: "94105",
    addressCountry: "US",
    metadata: {
      salesforce_id: "0010X000abcdef",
      tier: "enterprise",
    },
  };

  try {
    // Pass an idempotency key via a custom request header so that retrying the
    // same network call never creates a duplicate customer.
    const customer = await flexprice.customers.createCustomer(request, {
      headers: { "Idempotency-Key": `create-customer-${customerExternalId}` },
    });

    console.log(
      `Created customer  id=${customer.id}  email=${customer.email ?? "N/A"}`
    );
    return customer;
  } catch (err) {
    // A 409 means the external_id already exists – retrieve the existing record.
    if (err instanceof SDKError && err.statusCode === 409) {
      console.log("Customer already exists; fetching by external_id …");
      const result: ListCustomersResponse =
        await flexprice.customers.queryCustomer({
          externalId: customerExternalId,
          limit: 1,
        });
      const existing = result.items?.[0];
      if (!existing) {
        throw new Error(
          `409 returned but customer with externalId=${customerExternalId} not found`
        );
      }
      console.log(
        `Fetched existing customer  id=${existing.id}  name=${existing.name ?? "N/A"}`
      );
      return existing;
    }
    throw err;
  }
}

// ── updateCustomer ────────────────────────────────────────────────────────────

async function updateCustomer(customerId: string): Promise<Customer> {
  console.log("\n// ── Update customer ──────────────────────────────────────");

  const update: UpdateCustomerRequest = {
    metadata: {
      salesforce_id: "0010X000abcdef",
      tier: "enterprise",
      onboarded: "true",
    },
  };

  const updated = await flexprice.customers.updateCustomer(update, customerId);
  console.log(
    `Updated customer  id=${updated.id}  name=${updated.name ?? "N/A"}`
  );
  return updated;
}

// ── paginateCustomers ─────────────────────────────────────────────────────────

/**
 * Iterates through every customer page until all records have been visited.
 * Uses the `pagination.total` field to decide when to stop.
 */
async function paginateCustomers(): Promise<void> {
  console.log(
    "\n// ── Paginate all customers ────────────────────────────────"
  );

  const PAGE_SIZE = 50;
  let offset = 0;
  let total: number | undefined;
  let page = 1;

  do {
    const filter: CustomerFilter = {
      limit: PAGE_SIZE,
      offset,
      order: "desc",
    };

    const response: ListCustomersResponse =
      await flexprice.customers.queryCustomer(filter);

    total = response.pagination?.total;
    const items = response.items ?? [];

    console.log(
      `  Page ${page}: fetched ${items.length} customers` +
        (total !== undefined ? `  (total=${total})` : "")
    );

    for (const c of items) {
      console.log(`    • ${c.id}  ${c.email ?? "no-email"}`);
    }

    offset += items.length;
    page++;

    // Stop if we received fewer items than the page size (last page) or once
    // we have consumed everything reported by the server.
  } while (
    (total === undefined || offset < total) &&
    (response.items?.length ?? 0) === PAGE_SIZE
  );
}

// ── createPlan ────────────────────────────────────────────────────────────────

async function createPlan(): Promise<Plan> {
  console.log("\n// ── Create plan ──────────────────────────────────────────");

  const request: CreatePlanRequest = {
    name: "Pro Plan",
    description: "Usage-based plan for professional teams",
    lookupKey: `pro-plan-${Date.now()}`,
    metadata: {
      internal_code: "PLAN_PRO_V1",
    },
  };

  const plan = await flexprice.plans.createPlan(request);
  console.log(`Created plan  id=${plan.id}  name=${plan.name}`);
  return plan;
}

// ── createSubscription ────────────────────────────────────────────────────────

async function createSubscription(
  customerId: string,
  planId: string
): Promise<Subscription> {
  console.log(
    "\n// ── Create subscription ──────────────────────────────────"
  );

  const request: CreateSubscriptionRequest = {
    customerId,
    planId,
    billingCadence: "RECURRING",
    billingPeriod: "MONTHLY",
    currency: "USD",
    collectionMethod: "charge_automatically",
    startDate: new Date(),
    metadata: {
      source: "ts_sdk_example",
    },
  };

  // Per-request timeout – useful for operations that trigger async work on the
  // server (e.g. proration calculations) and may take longer than the default.
  const subscription = await flexprice.subscriptions.createSubscription(
    request,
    { timeoutMs: 15000 }
  );

  console.log(
    `Created subscription  id=${subscription.id}` +
      `  status=${subscription.subscriptionStatus ?? "unknown"}`
  );
  return subscription;
}

// ── ingestEvents ──────────────────────────────────────────────────────────────

async function ingestEvents(externalCustomerId: string): Promise<void> {
  console.log(
    "\n// ── Ingest single event ──────────────────────────────────"
  );

  // Single event ingest (returns void on HTTP 202)
  const singleEvent: IngestEventRequest = {
    eventName: "api_request",
    externalCustomerId,
    eventId: `evt-single-${Date.now()}`,
    timestamp: new Date().toISOString(),
    source: "ts_sdk_example",
    properties: {
      endpoint: "/v1/completions",
      model: "gpt-4o",
      tokens_used: "1500",
    },
  };

  await flexprice.events.ingestEvent(singleEvent);
  console.log("Single event accepted (202)");

  // ── Bulk event ingest ───────────────────────────────────────────────────────
  console.log(
    "\n// ── Ingest events bulk ───────────────────────────────────"
  );

  const bulkPayload: BulkIngestEventRequest = {
    events: [
      {
        eventName: "api_request",
        externalCustomerId,
        eventId: `evt-bulk-1-${Date.now()}`,
        timestamp: new Date().toISOString(),
        source: "ts_sdk_example",
        properties: {
          endpoint: "/v1/embeddings",
          model: "text-embedding-3-small",
          tokens_used: "800",
        },
      },
      {
        eventName: "storage_write",
        externalCustomerId,
        eventId: `evt-bulk-2-${Date.now()}`,
        timestamp: new Date().toISOString(),
        source: "ts_sdk_example",
        properties: {
          bucket: "user-uploads",
          bytes_written: "204800",
        },
      },
      {
        eventName: "storage_read",
        externalCustomerId,
        eventId: `evt-bulk-3-${Date.now()}`,
        timestamp: new Date().toISOString(),
        source: "ts_sdk_example",
        properties: {
          bucket: "user-uploads",
          bytes_read: "102400",
        },
      },
    ],
  };

  await flexprice.events.ingestEventsBulk(bulkPayload);
  console.log(`Bulk ingest accepted (202)  count=${bulkPayload.events.length}`);
}

// ── cancelSubscription ────────────────────────────────────────────────────────

async function cancelSubscription(subscriptionId: string): Promise<void> {
  console.log(
    "\n// ── Cancel subscription ──────────────────────────────────"
  );

  const request: CancelSubscriptionRequest = {
    cancellationType: "end_of_period",
    reason: "Customer requested cancellation via account settings",
  };

  const cancelled = await flexprice.subscriptions.cancelSubscription(
    subscriptionId,
    request
  );

  console.log(
    `Subscription cancellation scheduled  id=${cancelled.id}` +
      `  status=${cancelled.subscriptionStatus ?? "unknown"}`
  );
}

// ── listInvoices ──────────────────────────────────────────────────────────────

async function listInvoices(customerId: string): Promise<void> {
  console.log("\n// ── List invoices ────────────────────────────────────────");

  const filter: InvoiceFilter = {
    customerId,
    invoiceStatus: ["DRAFT", "FINALIZED"],
    limit: 20,
    offset: 0,
    order: "desc",
  };

  const response: ListInvoicesResponse =
    await flexprice.invoices.queryInvoice(filter);

  const invoices: Invoice[] = response.items ?? [];
  console.log(
    `Found ${invoices.length} invoice(s)` +
      (response.pagination?.total !== undefined
        ? `  (total=${response.pagination.total})`
        : "")
  );

  for (const inv of invoices) {
    console.log(
      `  • ${inv.id}` +
        `  status=${inv.invoiceStatus ?? "unknown"}` +
        `  amount_due=${inv.amountDue ?? "0"} ${inv.currency ?? ""}`
    );
  }
}

// ── handleWebhook ─────────────────────────────────────────────────────────────

/**
 * Express-style webhook handler that reads the FlexPrice event type and
 * type-narrows the payload.  Mount with:
 *
 *   app.post("/webhooks/flexprice", express.raw({ type: "*\/*" }), handleWebhook);
 */
async function handleWebhook(
  req: { body: Buffer | string; headers: Record<string, string | string[] | undefined> },
  res: { status: (code: number) => { send: (body: string) => void } }
): Promise<void> {
  const rawBody =
    req.body instanceof Buffer ? req.body.toString("utf8") : req.body;

  let event: Record<string, unknown>;
  try {
    event = JSON.parse(rawBody) as Record<string, unknown>;
  } catch {
    res.status(400).send("Invalid JSON");
    return;
  }

  const eventType = event["event_type"] as string | undefined;

  switch (eventType) {
    case "invoice.created": {
      const invoice = event["data"] as Invoice;
      console.log(
        `Webhook: invoice created  id=${invoice?.id}` +
          `  amount_due=${invoice?.amountDue ?? "0"}`
      );
      break;
    }

    case "invoice.finalized": {
      const invoice = event["data"] as Invoice;
      console.log(
        `Webhook: invoice finalised  id=${invoice?.id}` +
          `  status=${invoice?.invoiceStatus ?? "unknown"}`
      );
      break;
    }

    case "subscription.created":
    case "subscription.updated":
    case "subscription.cancelled": {
      const sub = event["data"] as Subscription;
      console.log(
        `Webhook: ${eventType}  id=${sub?.id}` +
          `  status=${sub?.subscriptionStatus ?? "unknown"}`
      );
      break;
    }

    default:
      console.log(`Webhook: unhandled event_type="${eventType ?? ""}"`);
  }

  res.status(200).send("OK");
}

// ── main ──────────────────────────────────────────────────────────────────────

async function main(): Promise<void> {
  console.log("FlexPrice TypeScript SDK – complete example");
  console.log(`Server: ${serverURL}\n`);

  // 1. Customer lifecycle
  const customer = await createOrGetCustomer();
  await updateCustomer(customer.id);

  // 2. Pagination
  await paginateCustomers();

  // 3. Plan
  const plan = await createPlan();

  // 4. Subscription (requires a plan with at least one price; this example
  //    assumes the plan already has prices attached via the dashboard / API)
  const subscription = await createSubscription(customer.id, plan.id);

  // 5. Usage events
  const extId = customer.externalId ?? customer.id;
  await ingestEvents(extId);

  // 6. Invoices
  await listInvoices(customer.id);

  // 7. Cancel at end of current billing period
  await cancelSubscription(subscription.id);

  // 8. Webhook handler – simulated invocation (would normally be wired into
  //    an HTTP server; shown here to demonstrate the typing pattern)
  console.log("\n// ── Webhook handler (simulated) ──────────────────────────");
  await handleWebhook(
    {
      body: JSON.stringify({
        event_type: "invoice.created",
        data: { id: "inv_demo", amountDue: "49.00", currency: "USD" },
      }),
      headers: { "content-type": "application/json" },
    },
    {
      status: (code: number) => ({
        send: (body: string) =>
          console.log(`  Webhook response: ${code} ${body}`),
      }),
    }
  );

  console.log("\nAll examples completed successfully.");
}

main().catch((err: unknown) => {
  console.error("Fatal error:", err);
  process.exit(1);
});
