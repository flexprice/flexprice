import "dotenv/config";
import OpenAI from "openai";
import * as FlexPrice from "@flexprice/sdk";

const {
    OPENAI_API_KEY,
    FLEXPRICE_API_KEY,
    FLEXPRICE_PROJECT_ID,
    FLEXPRICE_API_HOST = "api.cloud.flexprice.io",
    CUSTOMER_ID = "cust_demo_123",
} = process.env;

if (!OPENAI_API_KEY || !FLEXPRICE_API_KEY || !FLEXPRICE_PROJECT_ID) {
    console.error(
        "Missing env vars: OPENAI_API_KEY, FLEXPRICE_API_KEY, FLEXPRICE_PROJECT_ID"
    );
    process.exit(1);
}

// Initialize Flexprice client (matches with-nextjs pattern)
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

const openai = new OpenAI({ apiKey: OPENAI_API_KEY });

async function ensureCustomer(externalId) {
    try {
        await wrapCustomersPost({
            externalId,
            name: externalId,
            email: `${externalId}@example.com`,
        });
    } catch (e) {
        // ignore if exists
    }
}

async function main() {
    await ensureCustomer(CUSTOMER_ID);

    // 1) Call OpenAI
    const start = Date.now();
    const completion = await openai.chat.completions.create({
        model: "gpt-4o-mini",
        messages: [
            { role: "system", content: "Be concise." },
            { role: "user", content: "Say hello and count to 3." },
        ],
    });
    const usage = completion.usage || {
        prompt_tokens: 0,
        completion_tokens: 0,
        total_tokens: 0,
    };

    // 2) Report usage to Flexprice
    const eventRequest = {
        event_name: "openai.chat.completion",
        external_customer_id: CUSTOMER_ID,
        properties: {
            units: usage.total_tokens ?? 0,
            provider: "openai",
            model: completion.model,
            prompt_tokens: usage.prompt_tokens,
            completion_tokens: usage.completion_tokens,
            latency_ms: Date.now() - start,
            source: "with-openai",
        },
        source: "with-openai",
        timestamp: new Date().toISOString(),
    };
    await wrapEventsPost(eventRequest);

    // 3) Summary (last hour, client-side aggregation)
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

    console.log("OpenAI response:", completion.choices?.[0]?.message?.content ?? "No response content");
    console.log(
        "Flexprice total units (last hour):",
        Number.isFinite(total_units) ? total_units : 0
    );
}

main().catch((e) => {
    console.error(e);
    process.exit(1);
});
