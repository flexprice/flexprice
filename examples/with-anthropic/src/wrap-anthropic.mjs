import "dotenv/config";
import Anthropic from "anthropic";
import * as FlexPrice from "@flexprice/sdk";

const {
    ANTHROPIC_API_KEY,
    FLEXPRICE_API_KEY,
    FLEXPRICE_PROJECT_ID,
    FLEXPRICE_API_HOST = "api.cloud.flexprice.io",
    CUSTOMER_ID = "cust_demo_123",
} = process.env;

if (!ANTHROPIC_API_KEY || !FLEXPRICE_API_KEY || !FLEXPRICE_PROJECT_ID) {
    console.error(
        "Missing env vars: ANTHROPIC_API_KEY, FLEXPRICE_API_KEY, FLEXPRICE_PROJECT_ID"
    );
    process.exit(1);
}

// Initialize Flexprice client (aligns with with-nextjs example)
const defaultClient = FlexPrice.ApiClient.instance;
defaultClient.basePath = `https://${FLEXPRICE_API_HOST}/v1`;
const apiKeyAuth = defaultClient.authentications["ApiKeyAuth"];
apiKeyAuth.apiKey = FLEXPRICE_API_KEY;
apiKeyAuth.in = "header";
apiKeyAuth.name = "x-api-key";

const customersApi = new FlexPrice.CustomersApi();
const eventsApi = new FlexPrice.EventsApi();

const wrapCustomersPost = (dtoCreateCustomerRequest) =>
    new Promise((resolve, reject) => {
        customersApi.customersPost({ dtoCreateCustomerRequest }, (err, data) =>
            err ? reject(err) : resolve(data)
        );
    });
const wrapEventsPost = (eventRequest) =>
    new Promise((resolve, reject) => {
        eventsApi.eventsPost(eventRequest, (err, data) =>
            err ? reject(err) : resolve(data)
        );
    });
const wrapEventsGet = (params) =>
    new Promise((resolve, reject) => {
        eventsApi.eventsGet(params, (err, data) =>
            err ? reject(err) : resolve(data)
        );
    });

const anthropic = new Anthropic({ apiKey: ANTHROPIC_API_KEY });

async function ensureCustomer(externalId) {
    try {
        await wrapCustomersPost({
            externalId,
            name: externalId,
            email: `${externalId}@example.com`,
        });
    } catch (e) {}
}

async function main() {
    await ensureCustomer(CUSTOMER_ID);

    const start = Date.now();
    const msg = await anthropic.messages.create({
        model: "claude-3-5-sonnet-latest",
        max_tokens: 100,
        system: "Be brief.",
        messages: [
            { role: "user", content: "Say hello in one short sentence." },
        ],
    });

    const tokens = msg.usage?.output_tokens ?? msg.usage?.input_tokens ?? 0;

    const eventRequest = {
        event_name: "anthropic.messages.create",
        external_customer_id: CUSTOMER_ID,
        properties: {
            units: tokens,
            provider: "anthropic",
            model: msg.model,
            latency_ms: Date.now() - start,
            source: "with-anthropic",
        },
        source: "with-anthropic",
        timestamp: new Date().toISOString(),
    };
    await wrapEventsPost(eventRequest);

    const data = await wrapEventsGet({ external_customer_id: CUSTOMER_ID });
    const events = Array.isArray(data?.events) ? data.events : [];
    const oneHourAgo = Date.now() - 60 * 60 * 1000;
    let total_units = 0;
    for (const e of events) {
        const ts = new Date(e.timestamp || 0).getTime();
        if (Number.isFinite(ts) && ts >= oneHourAgo) {
            total_units += Number(e?.properties?.units ?? 0);
        }
    }

    console.log("Anthropic reply:", msg.content?.[0]?.text ?? "(no text)");
    console.log(
        "Flexprice total units (last hour):",
        Number.isFinite(total_units) ? total_units : 0
    );
}

main().catch((e) => {
    console.error(e);
    process.exit(1);
});
