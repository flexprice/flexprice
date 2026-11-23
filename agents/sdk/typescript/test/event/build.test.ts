import { describe, expect, it } from "vitest";
import { buildEvent } from "../../src/event/build.js";
import type { Observation } from "../../src/types.js";
import { SDK_VERSION } from "../../src/version.js";

/** A call to OpenAI that was not streamed and read 50,000 tokens from the cache. */
function openAiCall(): Observation {
  return {
    labels: { customerId: "cust_42", agentId: "researcher" },
    info: {
      provider: "openai",
      api: "openai.chat",
      type: "chat",
      model: "gpt-4o-2024-08-06",
      requestedModel: "gpt-4o",
      responseId: "chatcmpl-AbC123",
      streamed: false,
    },
    usage: { inputTokens: 1200, cacheReadTokens: 50000, outputTokens: 300 },
    raw: {
      prompt_tokens: 51200,
      completion_tokens: 300,
      total_tokens: 51500,
      prompt_tokens_details: { cached_tokens: 50000 },
    },
    sequence: 0,
    final: true,
    outcome: "completed",
    endedAt: new Date("2026-11-23T09:15:02.000Z"),
  };
}

describe("buildEvent", () => {
  it("builds the complete event for an OpenAI call", () => {
    const { event, problems } = buildEvent(openAiCall());

    expect(event).toEqual({
      event_name: "model.usage",
      event_id: "chatcmpl-AbC123",
      external_customer_id: "cust_42",
      timestamp: "2026-11-23T09:15:02Z",
      source: "flexprice-agent-sdk",
      properties: {
        provider: "openai",
        api: "openai.chat",
        type: "chat",
        model: "gpt-4o-2024-08-06",
        requested_model: "gpt-4o",
        streamed: "false",
        outcome: "completed",
        agent_id: "researcher",
        input_tokens: "1200",
        cache_read_tokens: "50000",
        output_tokens: "300",
        sequence: "0",
        is_final: "true",
        flexprice_agent_sdk: "true",
        sdk_language: "typescript",
        sdk_version: SDK_VERSION,
        schema_version: "1",
        raw_prompt_tokens: "51200",
        raw_completion_tokens: "300",
        raw_total_tokens: "51500",
        raw_prompt_tokens_details_cached_tokens: "50000",
      },
    });
    expect(problems).toEqual([]);
  });

  it("adds the gateway and its cost for a call through a gateway", () => {
    const call = openAiCall();
    call.info.gateway = "openrouter";
    call.info.model = "openai/gpt-4o";
    call.usage.costUsd = "0.0123";

    const { event } = buildEvent(call);

    expect(event.properties.gateway).toBe("openrouter");
    expect(event.properties.model).toBe("openai/gpt-4o");
    expect(event.properties.cost_usd).toBe("0.0123");
  });

  it("makes an event id only when the call has no response id", () => {
    let made = 0;
    const newId = () => {
      made++;
      return "generated-id";
    };

    expect(buildEvent(openAiCall(), { newId }).event.event_id).toBe("chatcmpl-AbC123");
    expect(made).toBe(0);

    const call = openAiCall();
    delete call.info.responseId;
    expect(buildEvent(call, { newId }).event.event_id).toBe("generated-id");
    expect(made).toBe(1);
  });

  it("cuts the timestamp to whole seconds in UTC", () => {
    const call = openAiCall();
    call.endedAt = new Date("2026-11-23T14:45:02.987+05:30");

    expect(buildEvent(call).event.timestamp).toBe("2026-11-23T09:15:02Z");
  });

  it("leaves out everything that was not reported", () => {
    const { event } = buildEvent(
      {
        labels: { customerId: "cust_42" },
        info: { provider: "unknown", api: "openai.chat", type: "chat", streamed: true },
        usage: {},
        sequence: 0,
        final: true,
        endedAt: new Date("2026-11-23T09:15:02.000Z"),
      },
      { newId: () => "id-1" },
    );

    expect(Object.keys(event.properties).sort()).toEqual([
      "api",
      "flexprice_agent_sdk",
      "is_final",
      "provider",
      "schema_version",
      "sdk_language",
      "sdk_version",
      "sequence",
      "streamed",
      "type",
    ]);
    expect(event.properties.streamed).toBe("true");
    expect(Object.values(event.properties).every((value) => typeof value === "string")).toBe(true);
  });

  it("leaves out requested_model when it is the same as model", () => {
    const call = openAiCall();
    call.info.requestedModel = call.info.model;

    expect(buildEvent(call).event.properties.requested_model).toBeUndefined();
  });

  it("leaves out a count of zero without a problem", () => {
    const call = openAiCall();
    call.usage.cacheWriteTokens = 0;

    const { event, problems } = buildEvent(call);

    expect(event.properties.cache_write_tokens).toBeUndefined();
    expect(problems).toEqual([]);
  });

  it("drops a count that is not a positive whole number and reports it", () => {
    const call = openAiCall();
    call.usage = { inputTokens: Number.NaN, cacheReadTokens: -5, outputTokens: 1.5 };

    const { event, problems } = buildEvent(call);

    expect(event.properties.input_tokens).toBeUndefined();
    expect(event.properties.cache_read_tokens).toBeUndefined();
    expect(event.properties.output_tokens).toBeUndefined();
    expect(problems).toHaveLength(3);
    expect(problems).toEqual([
      expect.stringContaining("input_tokens"),
      expect.stringContaining("cache_read_tokens"),
      expect.stringContaining("output_tokens"),
    ]);
  });

  it("drops a tag that uses a reserved name and reports it", () => {
    const call = openAiCall();
    call.labels.tags = { team: "support", model: "hacked", raw_x: "1" };

    const { event, problems } = buildEvent(call);

    expect(event.properties.team).toBe("support");
    expect(event.properties.model).toBe("gpt-4o-2024-08-06");
    expect(event.properties.raw_x).toBeUndefined();
    expect(problems).toEqual([
      expect.stringContaining("model"),
      expect.stringContaining("raw_x"),
    ]);
  });

  it("turns number and boolean tag values into text", () => {
    const call = openAiCall();
    call.labels.tags = { attempt: 2, vip: true, nested: { a: 1 } } as unknown as Record<string, string>;

    const { event, problems } = buildEvent(call);

    expect(event.properties.attempt).toBe("2");
    expect(event.properties.vip).toBe("true");
    expect(event.properties.nested).toBeUndefined();
    expect(problems).toEqual([expect.stringContaining("nested")]);
  });

  it("uses a custom event name when one is given", () => {
    expect(buildEvent(openAiCall(), { eventName: "acme.model.usage" }).event.event_name).toBe(
      "acme.model.usage",
    );
  });
});
