import { describe, expect, it, vi } from "vitest";
import { resolveConfig } from "../src/config.js";
import { createCore } from "../src/core.js";
import { withLabels } from "../src/labels.js";
import type { Labels, Observation, UsageEvent } from "../src/types.js";
import { startFakeFlexprice } from "./helpers/fake-flexprice.js";
import { scriptedSend } from "./helpers/scripted-send.js";

/** A call to Anthropic that finished normally. */
function finishedCall(overrides: Partial<Observation> = {}): Observation {
  return {
    labels: { customerId: "cust_42" },
    info: {
      provider: "anthropic",
      api: "anthropic.messages",
      type: "chat",
      model: "claude-sonnet-5-5",
      responseId: "msg_1",
      streamed: false,
    },
    usage: { inputTokens: 10, outputTokens: 20 },
    sequence: 0,
    final: true,
    outcome: "completed",
    endedAt: new Date("2026-11-23T09:15:02.000Z"),
    ...overrides,
  };
}

function coreWith(options: { eventName?: string; debug?: boolean; defaults?: Partial<Labels> } = {}) {
  const scripted = scriptedSend();
  const onError = vi.fn();
  const core = createCore(resolveConfig({ sendEvents: scripted.send, onError, ...options }));
  return { core, onError, batches: scripted.batches, sent: scripted.sent };
}

describe("the core", () => {
  it("queues the event for a finished call and sends it", async () => {
    const { core, sent } = coreWith();

    core.record(finishedCall());
    await core.flush();

    expect(sent).toHaveLength(1);
    expect(sent[0]?.[0]).toMatchObject({
      event_name: "model.usage",
      event_id: "msg_1",
      external_customer_id: "cust_42",
      properties: { input_tokens: "10", output_tokens: "20" },
    });
  });

  it("uses the configured event name", async () => {
    const { core, sent } = coreWith({ eventName: "acme.model.usage" });

    core.record(finishedCall());
    await core.flush();

    expect(sent[0]?.[0]?.event_name).toBe("acme.model.usage");
  });

  it("reports what could not be used, and still sends the rest", async () => {
    const { core, onError, sent } = coreWith();

    core.record(finishedCall({ usage: { inputTokens: Number.NaN, outputTokens: 20 } }));
    await core.flush();

    expect(onError).toHaveBeenCalledWith(expect.any(Error), "event");
    const [error] = onError.mock.calls[0] as [Error];
    expect(error.message).toContain("input_tokens");
    expect(sent[0]?.[0]?.properties).toMatchObject({ output_tokens: "20" });
    expect(sent[0]?.[0]?.properties.input_tokens).toBeUndefined();
  });

  it("never throws for a malformed observation, and reports it", async () => {
    const { core, onError, sent } = coreWith();

    expect(() => core.record(finishedCall({ endedAt: new Date("not a date") }))).not.toThrow();
    await core.flush();

    expect(onError).toHaveBeenCalledWith(expect.any(Error), "event");
    expect(sent).toEqual([]);
  });

  it("gives the labels for a call from the block first, then the client, then the defaults", () => {
    const { core } = coreWith({ defaults: { customerId: "cust_default", agentId: "default-agent" } });

    expect(core.labelsForCall()).toStrictEqual({ customerId: "cust_default", agentId: "default-agent" });
    expect(core.labelsForCall({ customerId: "cust_client" })).toStrictEqual({
      customerId: "cust_client",
      agentId: "default-agent",
    });
    withLabels({ customerId: "cust_block" }, () => {
      expect(core.labelsForCall({ customerId: "cust_client" })?.customerId).toBe("cust_block");
    });
  });

  it("reports a call with no customer, and gives it no labels", () => {
    const { core, onError } = coreWith();

    expect(core.labelsForCall()).toBeUndefined();
    expect(onError).toHaveBeenCalledWith(expect.any(Error), "labels");
  });

  it("logs each event when debug is on", () => {
    const debug = vi.spyOn(console, "debug").mockImplementation(() => undefined);
    try {
      const { core } = coreWith({ debug: true });

      core.record(finishedCall());

      expect(debug).toHaveBeenCalledTimes(1);
      expect(debug.mock.calls[0]?.[0]).toMatch(/^\[flexprice-agent-sdk\] event .*msg_1/);
    } finally {
      debug.mockRestore();
    }
  });

  it("sends what was waiting when it shuts down, and refuses calls after that", async () => {
    const { core, onError, batches } = coreWith();

    core.record(finishedCall());
    await core.shutdown();
    expect(batches).toEqual([["msg_1"]]);

    core.record(finishedCall({ info: { ...finishedCall().info, responseId: "msg_2" } }));
    await core.flush();

    expect(batches).toEqual([["msg_1"]]);
    expect(onError).toHaveBeenCalledWith(expect.any(Error), "event");
  });

  it("delivers an event to FlexPrice over HTTP", async () => {
    const server = await startFakeFlexprice();
    try {
      const core = createCore(
        resolveConfig({ apiKey: "sk_test_123", baseUrl: server.baseUrl, onError: vi.fn() }),
      );

      core.record(finishedCall());
      await core.flush();

      expect(server.requests).toHaveLength(1);
      expect(server.requests[0]?.headers["x-api-key"]).toBe("sk_test_123");
      const body = server.requests[0]?.body as { events: UsageEvent[] };
      expect(body.events[0]?.event_id).toBe("msg_1");
    } finally {
      await server.close();
    }
  });
});
