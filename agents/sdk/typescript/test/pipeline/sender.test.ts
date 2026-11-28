import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { SendError, createHttpSender } from "../../src/pipeline/sender.js";
import type { UsageEvent } from "../../src/types.js";
import { type FakeFlexprice, startFakeFlexprice } from "../helpers/fake-flexprice.js";

const event = (id: string): UsageEvent => ({
  event_name: "model.usage",
  event_id: id,
  external_customer_id: "cust_42",
  timestamp: "2026-11-23T09:15:02Z",
  source: "flexprice-agent-sdk",
  properties: { input_tokens: "1200" },
});

/** Runs a send that should fail, and returns its error. */
async function failureOf(send: Promise<void>): Promise<SendError> {
  try {
    await send;
  } catch (error) {
    expect(error).toBeInstanceOf(SendError);
    return error as SendError;
  }
  throw new Error("the send was expected to fail, and it succeeded");
}

describe("createHttpSender", () => {
  let server: FakeFlexprice;

  beforeEach(async () => {
    server = await startFakeFlexprice();
  });

  afterEach(async () => {
    await server.close();
  });

  it("posts the events to /events/bulk with the API key", async () => {
    const send = createHttpSender({ apiKey: "sk_test_123", baseUrl: server.baseUrl, timeoutMs: 1000 });

    await send([event("a"), event("b")]);

    expect(server.requests).toHaveLength(1);
    const request = server.requests[0];
    expect(request?.method).toBe("POST");
    expect(request?.path).toBe("/v1/events/bulk");
    expect(request?.headers["x-api-key"]).toBe("sk_test_123");
    expect(request?.headers["content-type"]).toBe("application/json");
    expect(request?.body).toStrictEqual({ events: [event("a"), event("b")] });
  });

  it("accepts a base URL that ends with a slash", async () => {
    const send = createHttpSender({
      apiKey: "sk_test_123",
      baseUrl: `${server.baseUrl}/`,
      timeoutMs: 1000,
    });

    await send([event("a")]);

    expect(server.requests[0]?.path).toBe("/v1/events/bulk");
  });

  it("sends nothing for an empty list", async () => {
    const send = createHttpSender({ apiKey: "sk_test_123", baseUrl: server.baseUrl, timeoutMs: 1000 });

    await send([]);

    expect(server.requests).toHaveLength(0);
  });

  it("fails with the status when FlexPrice answers with an error", async () => {
    server.respondWith(500);
    const send = createHttpSender({ apiKey: "sk_test_123", baseUrl: server.baseUrl, timeoutMs: 1000 });

    const error = await failureOf(send([event("a")]));

    expect(error.status).toBe(500);
    expect(error.retryAfterMs).toBeUndefined();
    expect(error.message).not.toContain("sk_test_123");
  });

  it("reads Retry-After when it is a number of seconds", async () => {
    server.respondWith(429, { "retry-after": "7" });
    const send = createHttpSender({ apiKey: "sk_test_123", baseUrl: server.baseUrl, timeoutMs: 1000 });

    const error = await failureOf(send([event("a")]));

    expect(error.status).toBe(429);
    expect(error.retryAfterMs).toBe(7000);
  });

  it("ignores a Retry-After it cannot read", async () => {
    server.respondWith(429, { "retry-after": "soon" });
    const send = createHttpSender({ apiKey: "sk_test_123", baseUrl: server.baseUrl, timeoutMs: 1000 });

    const error = await failureOf(send([event("a")]));

    expect(error.status).toBe(429);
    expect(error.retryAfterMs).toBeUndefined();
  });

  it("fails without a status when the server cannot be reached", async () => {
    const send = createHttpSender({ apiKey: "sk_test_123", baseUrl: server.baseUrl, timeoutMs: 1000 });
    await server.close();

    const error = await failureOf(send([event("a")]));

    expect(error.status).toBeUndefined();
    expect(error.message).not.toContain("sk_test_123");
  });

  it("fails without a status when the answer takes too long", async () => {
    server.delayNext(300);
    const send = createHttpSender({ apiKey: "sk_test_123", baseUrl: server.baseUrl, timeoutMs: 50 });

    const error = await failureOf(send([event("a")]));

    expect(error.status).toBeUndefined();
  });
});
