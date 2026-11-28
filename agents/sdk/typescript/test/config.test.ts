import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { resolveConfig } from "../src/config.js";
import type { OnError } from "../src/errors.js";
import type { SendEvents } from "../src/pipeline/sender.js";
import { type FakeFlexprice, startFakeFlexprice } from "./helpers/fake-flexprice.js";
import { makeEvent } from "./helpers/scripted-send.js";

const BASE_URL = "https://us.api.flexprice.io/v1";
const sendEvents: SendEvents = () => Promise.resolve();

describe("resolveConfig", () => {
  it("fills in the defaults", () => {
    const config = resolveConfig({ apiKey: "sk_test_123", baseUrl: BASE_URL });

    expect(config).toMatchObject({
      flushIntervalMs: 1000,
      flushAt: 100,
      maxQueueSize: 10_000,
      eventName: "model.usage",
      enabled: true,
      debug: false,
    });
    expect(typeof config.send).toBe("function");
  });

  it("keeps the API key out of the resolved config", () => {
    const config = resolveConfig({ apiKey: "sk_secret_123", baseUrl: BASE_URL });

    expect(JSON.stringify(config)).not.toContain("sk_secret_123");
  });

  it("uses sendEvents in place of the built-in sender, and needs no key then", () => {
    expect(resolveConfig({ sendEvents }).send).toBe(sendEvents);
  });

  it("refuses a missing or empty API key", () => {
    for (const apiKey of [undefined, "", "   "]) {
      expect(() => resolveConfig({ apiKey, baseUrl: BASE_URL })).toThrow(/apiKey/);
    }
  });

  it("refuses a base URL that is not an http URL ending in /v1", () => {
    const bad = [
      undefined,
      "us.api.flexprice.io/v1",
      "ftp://us.api.flexprice.io/v1",
      "https://us.api.flexprice.io",
      "https://us.api.flexprice.io/v2",
    ];
    for (const baseUrl of bad) {
      expect(() => resolveConfig({ apiKey: "sk_test_123", baseUrl })).toThrow(/baseUrl/);
    }
  });

  it("accepts a base URL with or without a trailing slash, and a local one", () => {
    for (const baseUrl of [BASE_URL, `${BASE_URL}/`, "http://localhost:8080/v1"]) {
      expect(() => resolveConfig({ apiKey: "sk_test_123", baseUrl })).not.toThrow();
    }
  });

  it("never puts the API key in an error message", () => {
    let message = "";
    try {
      resolveConfig({ apiKey: "sk_secret_123", baseUrl: "not a url" });
    } catch (error) {
      message = (error as Error).message;
    }

    expect(message).toMatch(/^\[flexprice-agent-sdk\].*baseUrl/);
    expect(message).not.toContain("sk_secret_123");
  });

  it.each(["flushIntervalMs", "flushAt", "maxQueueSize", "requestTimeoutMs"] as const)(
    "refuses a %s that is not a positive whole number",
    (option) => {
      for (const value of [0, -1, 1.5, Number.NaN]) {
        expect(() => resolveConfig({ sendEvents, [option]: value })).toThrow(option);
      }
    },
  );

  it("refuses an empty event name", () => {
    expect(() => resolveConfig({ sendEvents, eventName: " " })).toThrow(/eventName/);
  });

  it("refuses a sendEvents or onError that is not a function", () => {
    expect(() => resolveConfig({ sendEvents: "nope" as unknown as SendEvents })).toThrow(/sendEvents/);
    expect(() => resolveConfig({ sendEvents, onError: 42 as unknown as OnError })).toThrow(/onError/);
  });
});

describe("the built-in sender", () => {
  let server: FakeFlexprice;

  beforeEach(async () => {
    server = await startFakeFlexprice();
  });

  afterEach(async () => {
    await server.close();
  });

  it("accepts a key copied with spaces or a newline around it", async () => {
    const config = resolveConfig({ apiKey: "  sk_test_123\n", baseUrl: server.baseUrl });

    await config.send([makeEvent("a")]);

    expect(server.requests[0]?.headers["x-api-key"]).toBe("sk_test_123");
  });
});
