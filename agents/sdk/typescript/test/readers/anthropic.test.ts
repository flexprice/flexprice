import { describe, expect, it } from "vitest";
import {
  createAnthropicStreamReader,
  outcomeFromStopReason,
  readAnthropicMessage,
  usageFromAnthropic,
} from "../../src/readers/anthropic.js";
import type { Usage } from "../../src/types.js";

/** Every token in our counts, added up. Each token sits in exactly one count. */
const total = (usage: Usage) =>
  (usage.inputTokens ?? 0) +
  (usage.cacheReadTokens ?? 0) +
  (usage.cacheWriteTokens ?? 0) +
  (usage.cacheWrite1hTokens ?? 0) +
  (usage.outputTokens ?? 0);

describe("usageFromAnthropic", () => {
  it("maps input and output", () => {
    expect(usageFromAnthropic({ input_tokens: 1200, output_tokens: 300 })).toStrictEqual({
      inputTokens: 1200,
      cacheReadTokens: undefined,
      cacheWriteTokens: undefined,
      cacheWrite1hTokens: undefined,
      outputTokens: 300,
    });
  });

  it("maps cache reads and writes, which Anthropic counts beside the input", () => {
    const block = {
      input_tokens: 9,
      cache_read_input_tokens: 3962,
      cache_creation_input_tokens: 100,
      output_tokens: 20,
    };
    const usage = usageFromAnthropic(block);

    expect(usage).toMatchObject({ inputTokens: 9, cacheReadTokens: 3962, cacheWriteTokens: 100, outputTokens: 20 });
    expect(total(usage)).toBe(9 + 3962 + 100 + 20);
  });

  it("splits cache writes into 5 minute and 1 hour, without counting them twice", () => {
    const block = {
      input_tokens: 10,
      cache_read_input_tokens: 0,
      cache_creation_input_tokens: 300,
      cache_creation: { ephemeral_5m_input_tokens: 100, ephemeral_1h_input_tokens: 200 },
      output_tokens: 5,
    };
    const usage = usageFromAnthropic(block);

    expect(usage).toMatchObject({ cacheWriteTokens: 100, cacheWrite1hTokens: 200 });
    expect(total(usage)).toBe(10 + 0 + 300 + 5);
  });

  it("never drops a value it cannot read", () => {
    const usage = usageFromAnthropic({ input_tokens: "12", output_tokens: null });

    expect(usage.inputTokens).toBeNaN();
    expect(usage.outputTokens).toBeUndefined();
  });
});

describe("outcomeFromStopReason", () => {
  it("maps every stop reason", () => {
    const reasons = [
      "end_turn",
      "tool_use",
      "stop_sequence",
      "pause_turn",
      "max_tokens",
      "model_context_window_exceeded",
      "refusal",
      "something_new",
    ];

    expect(reasons.map((reason) => outcomeFromStopReason(reason))).toEqual([
      "completed",
      "completed",
      "completed",
      "completed",
      "max_tokens",
      "max_tokens",
      "refused",
      "completed",
    ]);
  });
});

describe("readAnthropicMessage", () => {
  it("reads the usage, the model, the id and the outcome", () => {
    const usage = { input_tokens: 1200, output_tokens: 300, service_tier: "standard" };

    expect(
      readAnthropicMessage({ id: "msg_01", model: "claude-sonnet-5-5", stop_reason: "end_turn", usage }),
    ).toStrictEqual({
      usage: usageFromAnthropic(usage),
      raw: usage,
      model: "claude-sonnet-5-5",
      responseId: "msg_01",
      outcome: "completed",
    });
  });

  it("finds nothing in a message without usage", () => {
    expect(readAnthropicMessage({ id: "msg_01" })).toBeUndefined();
    expect(readAnthropicMessage("not a message")).toBeUndefined();
  });
});

describe("createAnthropicStreamReader", () => {
  const start = {
    type: "message_start",
    message: {
      id: "msg_01",
      model: "claude-sonnet-5-5",
      usage: { input_tokens: 1200, cache_read_input_tokens: 50000, output_tokens: 1 },
    },
  };
  const delta = { type: "message_delta", delta: { stop_reason: "end_turn" }, usage: { output_tokens: 300 } };

  it("takes the input side from the start and the output from the end", () => {
    const reader = createAnthropicStreamReader();
    for (const event of [start, { type: "content_block_delta" }, delta, { type: "message_stop" }]) {
      reader.onEvent(event);
    }

    expect(reader.finish(false)).toMatchObject({
      usage: { inputTokens: 1200, cacheReadTokens: 50000, outputTokens: 300 },
      model: "claude-sonnet-5-5",
      responseId: "msg_01",
      outcome: "completed",
    });
  });

  it("does not add up counts that a later event restates", () => {
    const reader = createAnthropicStreamReader();
    reader.onEvent(start);
    reader.onEvent({ ...delta, usage: { input_tokens: 1200, cache_read_input_tokens: 50000, output_tokens: 300 } });

    expect(reader.finish(false)?.usage).toMatchObject({ inputTokens: 1200, cacheReadTokens: 50000 });
  });

  it("marks a stream that ended early as cut short, with what had arrived", () => {
    const reader = createAnthropicStreamReader();
    reader.onEvent(start);

    expect(reader.finish(false)).toMatchObject({
      usage: { inputTokens: 1200, cacheReadTokens: 50000, outputTokens: 1 },
      outcome: "cut_short",
    });
  });

  it("marks a stream that failed as an error", () => {
    const reader = createAnthropicStreamReader();
    reader.onEvent(start);

    expect(reader.finish(true)?.outcome).toBe("error");
  });

  it("finds nothing when the stream never started", () => {
    const reader = createAnthropicStreamReader();
    reader.onEvent({ type: "ping" });

    expect(reader.finish(true)).toBeUndefined();
  });

  it("ignores events it does not understand", () => {
    const reader = createAnthropicStreamReader();

    expect(() => reader.onEvent(null)).not.toThrow();
    expect(() => reader.onEvent({ type: "message_start", message: null })).not.toThrow();
  });
});
