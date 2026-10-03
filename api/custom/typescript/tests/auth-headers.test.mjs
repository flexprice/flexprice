// Tests for the bearerAuth and environmentId options. Run by `make ts-sdk-check` against the compiled esm/ output.
import assert from "node:assert/strict";
import { test } from "node:test";

import * as esm from "../esm/index.js";

const { CustomerPortal, HTTPClient } = esm;
// White-label builds name the SDK class after the client.
const sdkClassName = process.env.WL_SDK_CLASS_NAME || "Flexprice";
const SDK = esm[sdkClassName];
assert.equal(typeof SDK, "function", `esm/index.js exports no ${sdkClassName} class`);

const serverURL = "https://api.test/v1";

// Records requests instead of sending them. Answers with the queued statuses, then 200.
function recorder(statuses = []) {
  const requests = [];
  const httpClient = new HTTPClient({
    fetcher: async (input, init) => {
      requests.push(input instanceof Request ? input : new Request(input, init));
      return new Response("{}", {
        status: statuses.shift() ?? 200,
        headers: { "content-type": "application/json" },
      });
    },
  });
  return { httpClient, requests };
}

function client(options, statuses) {
  const { httpClient, requests } = recorder(statuses);
  return { sdk: new SDK({ serverURL, httpClient, ...options }), requests };
}

function counter(prefix) {
  let calls = 0;
  return () => `${prefix}-${++calls}`;
}

// Per-call options that retry a 503 once, with near-zero backoff.
const retryOn503 = {
  retries: {
    strategy: "backoff",
    backoff: { initialInterval: 1, maxInterval: 5, exponent: 1, maxElapsedTime: 5000 },
  },
  retryCodes: ["503"],
};

test("bearerAuth and environmentId send Authorization and X-Environment-ID", async () => {
  const { sdk, requests } = client({ bearerAuth: "jwt", environmentId: "env_1" });
  await sdk.customers.getCustomer("cust_1");

  assert.equal(requests.length, 1);
  assert.equal(requests[0].headers.get("authorization"), "Bearer jwt");
  assert.equal(requests[0].headers.get("x-environment-id"), "env_1");
  assert.equal(requests[0].headers.has("x-api-key"), false);
});

test("apiKeyAuth alone sends only x-api-key", async () => {
  const { sdk, requests } = client({ apiKeyAuth: "sk_test" });
  await sdk.customers.getCustomer("cust_1");

  assert.equal(requests[0].headers.get("x-api-key"), "sk_test");
  assert.equal(requests[0].headers.has("authorization"), false);
  assert.equal(requests[0].headers.has("x-environment-id"), false);
});

test("apiKeyAuth with bearerAuth rejects before sending", async () => {
  const { sdk, requests } = client({ apiKeyAuth: "sk_test", bearerAuth: "jwt" });

  await assert.rejects(sdk.customers.getCustomer("cust_1"), (err) => {
    assert.equal(err.name, "UnexpectedClientError");
    assert.match(err.message, /set either apiKeyAuth or bearerAuth, not both\.$/);
    assert.doesNotMatch(err.message, /flexprice/i);
    return true;
  });
  assert.equal(requests.length, 0);
});

test("per-call headers win over the options", async () => {
  const { sdk, requests } = client({ bearerAuth: "jwt", environmentId: "env_1" });
  await sdk.customers.getCustomer("cust_1", {
    headers: { Authorization: "Bearer call-jwt", "X-Environment-ID": "env_call" },
  });

  assert.equal(requests[0].headers.get("authorization"), "Bearer call-jwt");
  assert.equal(requests[0].headers.get("x-environment-id"), "env_call");
});

test("per-call headers skip the option functions, even ones that reject", async () => {
  const reject = async () => {
    throw new Error("not logged in");
  };
  const { sdk, requests } = client({ bearerAuth: reject, environmentId: reject });
  await sdk.customers.getCustomer("cust_1", {
    headers: { Authorization: "Bearer call-jwt", "X-Environment-ID": "env_call" },
  });

  assert.equal(requests.length, 1);
  assert.equal(requests[0].headers.get("authorization"), "Bearer call-jwt");
  assert.equal(requests[0].headers.get("x-environment-id"), "env_call");
});

test("a token function is called for each request", async () => {
  const { sdk, requests } = client({ bearerAuth: counter("jwt") });
  await sdk.customers.getCustomer("cust_1");
  await sdk.customers.getCustomer("cust_1");

  assert.equal(requests[0].headers.get("authorization"), "Bearer jwt-1");
  assert.equal(requests[1].headers.get("authorization"), "Bearer jwt-2");
});

test("a retried attempt sends a freshly resolved token", async () => {
  const nextToken = counter("jwt");
  const { sdk, requests } = client({ bearerAuth: async () => nextToken() }, [503, 200]);
  await sdk.customers.getCustomer("cust_1", retryOn503);

  assert.equal(requests.length, 2);
  assert.equal(requests[0].headers.get("authorization"), "Bearer jwt-1");
  assert.equal(requests[1].headers.get("authorization"), "Bearer jwt-2");
});

test("a retried call keeps its environment, and the next call resolves it again", async () => {
  const { sdk, requests } = client({ bearerAuth: "jwt", environmentId: counter("env") }, [503, 200]);
  await sdk.customers.getCustomer("cust_1", retryOn503);
  await sdk.customers.getCustomer("cust_1");

  assert.equal(requests.length, 3);
  assert.equal(requests[0].headers.get("x-environment-id"), "env-1");
  assert.equal(requests[1].headers.get("x-environment-id"), "env-1");
  assert.equal(requests[2].headers.get("x-environment-id"), "env-2");
});

test("an empty token or environment omits that header", async () => {
  const { sdk, requests } = client({ bearerAuth: async () => undefined, environmentId: "" });
  await sdk.customers.getCustomer("cust_1");

  assert.equal(requests[0].headers.has("authorization"), false);
  assert.equal(requests[0].headers.has("x-environment-id"), false);
});

test("environmentId with apiKeyAuth sends both headers", async () => {
  const { sdk, requests } = client({ apiKeyAuth: "sk_test", environmentId: () => "env_1" });
  await sdk.customers.getCustomer("cust_1");

  assert.equal(requests[0].headers.get("x-api-key"), "sk_test");
  assert.equal(requests[0].headers.get("x-environment-id"), "env_1");
});

test("CustomerPortal built with bearerAuth sends the JWT", async () => {
  const { httpClient, requests } = recorder();
  const portal = new CustomerPortal({ serverURL, httpClient, bearerAuth: "jwt" });
  await portal.getDashboardData("ext_1");

  assert.ok(requests.length > 0);
  for (const request of requests) {
    assert.equal(request.headers.get("authorization"), "Bearer jwt");
  }
});
