import Anthropic from "@anthropic-ai/sdk";
import { describe, expect, it, vi } from "vitest";
import { createAgentMeter } from "../../src/meter.js";
import { type AnthropicStep, anthropicStreamEvents, fakeAnthropic } from "../helpers/fake-anthropic.js";
import { rawProperties, scriptedSend } from "../helpers/scripted-send.js";

const params = {
  model: "claude-sonnet-5-5",
  max_tokens: 100,
  messages: [{ role: "user" as const, content: "Hi" }],
};

function setup(steps: AnthropicStep[]) {
  const fake = fakeAnthropic(steps);
  const scripted = scriptedSend();
  const meter = createAgentMeter({ sendEvents: scripted.send, onError: vi.fn() });
  const client = new Anthropic({ apiKey: "sk-ant-test", fetch: fake.fetch, maxRetries: 0 });
  const claude = meter.wrap(client, { customerId: "cust_42" });
  return { meter, claude, sent: scripted.sent, fake };
}

/** Reads a stream to the end, the way an app's for await loop does. */
async function readAll<T>(stream: AsyncIterable<T>): Promise<T[]> {
  const events: T[] = [];
  for await (const event of stream) events.push(event);
  return events;
}

describe("streamed calls on the Anthropic client", () => {
  it("meters a streamed call, and gives the app the same events as without the meter", async () => {
    const fake = fakeAnthropic([{ events: anthropicStreamEvents() }]);
    const unwrapped = new Anthropic({ apiKey: "sk-ant-test", fetch: fake.fetch, maxRetries: 0 });
    const expected = await readAll(await unwrapped.messages.create({ ...params, stream: true }));
    const { meter, claude, sent } = setup([{ events: anthropicStreamEvents() }]);

    const seen = await readAll(await claude.messages.create({ ...params, stream: true }));
    await meter.flush();

    expect(seen).toEqual(expected);
    expect(sent.flat()).toHaveLength(1);
    expect(sent[0]?.[0]).toMatchObject({
      event_id: "msg_01",
      properties: {
        streamed: "true",
        outcome: "completed",
        input_tokens: "1200",
        cache_read_tokens: "50000",
        output_tokens: "300",
      },
    });
  });

  it("keeps the usage from both ends of the stream in raw_ properties", async () => {
    const { meter, claude, sent } = setup([{ events: anthropicStreamEvents() }]);

    await readAll(await claude.messages.create({ ...params, stream: true }));
    await meter.flush();

    // inference_geo and speed arrive in message_start, output_tokens_details only in message_delta.
    expect(rawProperties(sent[0]?.[0])).toStrictEqual({
      raw_input_tokens: "1200",
      raw_cache_creation_input_tokens: "0",
      raw_cache_read_input_tokens: "50000",
      raw_cache_creation_ephemeral_5m_input_tokens: "0",
      raw_cache_creation_ephemeral_1h_input_tokens: "0",
      raw_output_tokens: "300",
      raw_service_tier: "standard",
      raw_inference_geo: "global",
      raw_speed: "standard",
      raw_output_tokens_details_thinking_tokens: "0",
    });
  });

  it("meters the messages.stream() helper once", async () => {
    const { meter, claude, sent } = setup([{ events: anthropicStreamEvents() }]);

    const message = await claude.messages.stream(params).finalMessage();
    await meter.flush();

    expect(message.usage.output_tokens).toBe(300);
    expect(sent.flat()).toHaveLength(1);
  });

  it("cancels the request at once when the app stops reading", async () => {
    const { meter, claude, fake } = setup([{ events: anthropicStreamEvents(), holdAfter: 1 }]);

    const stream = await claude.messages.create({ ...params, stream: true });
    for await (const event of stream) {
      if (event.type === "message_start") break;
    }

    expect(fake.cancelledStreams).toBe(1);
    await meter.flush();
  });

  it("marks a stream the app stopped early as cut short, with what had arrived", async () => {
    const { meter, claude, sent } = setup([{ events: anthropicStreamEvents(), holdAfter: 1 }]);

    const stream = await claude.messages.create({ ...params, stream: true });
    for await (const event of stream) {
      if (event.type === "message_start") break;
    }
    await meter.flush();

    expect(sent[0]?.[0]?.properties).toMatchObject({
      outcome: "cut_short",
      input_tokens: "1200",
      cache_read_tokens: "50000",
      output_tokens: "1",
    });
  });

  it("marks a stream that Anthropic ended with an error", async () => {
    const [start] = anthropicStreamEvents();
    const failing = [start ?? {}, { type: "error", error: { type: "overloaded_error", message: "Overloaded" } }];
    const { meter, claude, sent } = setup([{ events: failing }]);

    const stream = await claude.messages.create({ ...params, stream: true });
    await expect(
      (async () => {
        for await (const event of stream) void event;
      })(),
    ).rejects.toThrow();
    await meter.flush();

    expect(sent[0]?.[0]?.properties).toMatchObject({ outcome: "error", input_tokens: "1200" });
  });

  it("marks a stream whose connection broke as an error, with what had arrived", async () => {
    const { meter, claude, sent } = setup([{ events: anthropicStreamEvents(), failAfter: 3 }]);

    const stream = await claude.messages.create({ ...params, stream: true });
    await expect(readAll(stream)).rejects.toThrow("terminated");
    await meter.flush();

    expect(sent[0]?.[0]?.properties).toMatchObject({ outcome: "error", input_tokens: "1200", output_tokens: "1" });
  });

  it("meters a stream the app reads itself, through asResponse()", async () => {
    const { meter, claude, sent } = setup([{ events: anthropicStreamEvents() }]);

    const response = await claude.messages.create({ ...params, stream: true }).asResponse();
    const body = await response.text();
    await meter.flush();

    expect(body).toContain("event: message_stop");
    expect(sent[0]?.[0]?.properties).toMatchObject({ streamed: "true", outcome: "completed", output_tokens: "300" });
  });

  it("gives the app a streamed response with the same status, headers and URL", async () => {
    const fake = fakeAnthropic([{ events: anthropicStreamEvents() }]);
    const unwrapped = new Anthropic({ apiKey: "sk-ant-test", fetch: fake.fetch, maxRetries: 0 });
    const expected = await unwrapped.messages.create({ ...params, stream: true }).asResponse();
    const { claude } = setup([{ events: anthropicStreamEvents() }]);

    const response = await claude.messages.create({ ...params, stream: true }).asResponse();

    expect(expected.url).toBe("https://api.anthropic.com/v1/messages");
    expect(response.url).toBe(expected.url);
    expect(response.status).toBe(expected.status);
    expect([...response.headers]).toEqual([...expected.headers]);
    await Promise.all([expected.body?.cancel(), response.body?.cancel()]);
  });

  it("bills a stream to the customer of the block it started in", async () => {
    const { meter, claude, sent } = setup([{ events: anthropicStreamEvents() }]);

    const stream = await meter.withLabels({ customerId: "cust_block" }, () =>
      claude.messages.create({ ...params, stream: true }),
    );
    for await (const event of stream) void event;
    await meter.flush();

    expect(sent[0]?.[0]?.external_customer_id).toBe("cust_block");
  });
});
