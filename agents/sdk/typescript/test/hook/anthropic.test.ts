import Anthropic from "@anthropic-ai/sdk";
import { betaTool } from "@anthropic-ai/sdk/helpers/beta/json-schema";
import { describe, expect, it, vi } from "vitest";
import { createAgentMeter } from "../../src/meter.js";
import { type AnthropicStep, anthropicMessage, fakeAnthropic } from "../helpers/fake-anthropic.js";
import { rawProperties, scriptedSend } from "../helpers/scripted-send.js";

const params = {
  model: "claude-sonnet-5-5",
  max_tokens: 100,
  messages: [{ role: "user" as const, content: "Hi" }],
};

function setup(
  steps: AnthropicStep[],
  options: { maxRetries?: number; baseURL?: string; enabled?: boolean } = {},
) {
  const fake = fakeAnthropic(steps);
  const scripted = scriptedSend();
  const onError = vi.fn();
  const meter = createAgentMeter({ sendEvents: scripted.send, onError, enabled: options.enabled });
  const client = new Anthropic({
    apiKey: "sk-ant-test",
    fetch: fake.fetch,
    maxRetries: options.maxRetries ?? 0,
    ...(options.baseURL ? { baseURL: options.baseURL } : {}),
  });
  return { meter, client, onError, sent: scripted.sent, fake };
}

describe("wrapping the Anthropic client", () => {
  it("meters a call and gives the app the same message", async () => {
    const { meter, client, sent } = setup([{ message: anthropicMessage() }, { message: anthropicMessage() }]);
    const unwrapped = await client.messages.create(params);

    const claude = meter.wrap(client);
    const message = await meter.withLabels({ customerId: "cust_42" }, () => claude.messages.create(params));
    await meter.flush();

    expect(message).toEqual(unwrapped);
    expect(sent.flat()).toHaveLength(1);
    expect(sent[0]?.[0]).toMatchObject({
      event_id: "msg_01",
      external_customer_id: "cust_42",
      properties: {
        provider: "anthropic",
        api: "anthropic.messages",
        model: "claude-sonnet-5-5",
        streamed: "false",
        outcome: "completed",
        input_tokens: "1200",
        output_tokens: "300",
      },
    });
  });

  it("keeps Anthropic's whole usage block in raw_ properties", async () => {
    const { meter, client, sent } = setup([{ message: anthropicMessage() }]);
    const claude = meter.wrap(client, { customerId: "cust_42" });

    await claude.messages.create(params);
    await meter.flush();

    expect(rawProperties(sent[0]?.[0])).toStrictEqual({
      raw_input_tokens: "1200",
      raw_cache_creation_input_tokens: "0",
      raw_cache_read_input_tokens: "0",
      raw_cache_creation_ephemeral_5m_input_tokens: "0",
      raw_cache_creation_ephemeral_1h_input_tokens: "0",
      raw_output_tokens: "300",
      raw_output_tokens_details_thinking_tokens: "0",
      raw_service_tier: "standard",
      raw_inference_geo: "global",
      raw_speed: "standard",
    });
  });

  it("returns the same client object", () => {
    const { meter, client } = setup([]);

    expect(meter.wrap(client)).toBe(client);
  });

  it("keeps withResponse() and asResponse() working", async () => {
    const { meter, client, sent } = setup([{ message: anthropicMessage() }, { message: anthropicMessage() }]);
    const claude = meter.wrap(client, { customerId: "cust_42" });

    const { data } = await claude.messages.create(params).withResponse();
    const raw = await claude.messages.create(params).asResponse();
    const body = (await raw.json()) as { id: string };
    await meter.flush();

    expect(data.id).toBe("msg_01");
    expect(body.id).toBe("msg_01");
    expect(sent.flat()).toHaveLength(2);
  });

  it("bills the customer from the block first, then the client's labels", async () => {
    const { meter, client, sent } = setup([{ message: anthropicMessage() }, { message: anthropicMessage() }]);
    const claude = meter.wrap(client, { customerId: "cust_client" });

    await claude.messages.create(params);
    await meter.withLabels({ customerId: "cust_block" }, () => claude.messages.create(params));
    await meter.flush();

    expect(sent.flat().map((event) => event.external_customer_id)).toEqual(["cust_client", "cust_block"]);
  });

  it("does not meter a call with no customer, and reports it", async () => {
    const { meter, client, onError, sent } = setup([{ message: anthropicMessage() }]);
    const claude = meter.wrap(client);

    await claude.messages.create(params);
    await meter.flush();

    expect(sent).toEqual([]);
    expect(onError).toHaveBeenCalledWith(expect.any(Error), "labels");
  });

  it("passes the client's own errors through and meters nothing", async () => {
    const { meter, client, onError, sent } = setup([{ status: 401 }]);
    const claude = meter.wrap(client, { customerId: "cust_42" });

    await expect(claude.messages.create(params)).rejects.toBeInstanceOf(Anthropic.AuthenticationError);
    await meter.flush();

    expect(sent).toEqual([]);
    expect(onError).not.toHaveBeenCalled();
  });

  it("counts a retried call once", async () => {
    const { meter, client, sent, fake } = setup(
      [{ status: 529, headers: { "retry-after-ms": "1" } }, { message: anthropicMessage() }],
      { maxRetries: 1 },
    );
    const claude = meter.wrap(client, { customerId: "cust_42" });

    await claude.messages.create(params);
    await meter.flush();

    expect(fake.requests).toHaveLength(2);
    expect(sent.flat()).toHaveLength(1);
  });

  it("leaves calls that do not create a message alone", async () => {
    const { meter, client, onError, sent } = setup([{ message: { input_tokens: 12 } }]);
    const claude = meter.wrap(client, { customerId: "cust_42" });

    await claude.messages.countTokens(params);
    await meter.flush();

    expect(sent).toEqual([]);
    expect(onError).not.toHaveBeenCalled();
  });

  it("meters beta.messages.create too", async () => {
    const { meter, client, sent } = setup([{ message: anthropicMessage() }]);
    const claude = meter.wrap(client, { customerId: "cust_42" });

    await claude.beta.messages.create(params);
    await meter.flush();

    expect(sent.flat()).toHaveLength(1);
  });

  it("meters every call the tool runner makes, and bills them all to the block's customer", async () => {
    const askForTool = anthropicMessage({
      id: "msg_01",
      stop_reason: "tool_use",
      content: [{ type: "tool_use", id: "toolu_01", name: "get_weather", input: { city: "Paris" } }],
    });
    const { meter, client, sent, fake } = setup([
      { message: askForTool },
      { message: anthropicMessage({ id: "msg_02" }) },
    ]);
    const claude = meter.wrap(client);
    const getWeather = betaTool({
      name: "get_weather",
      description: "Gets the weather in a city.",
      inputSchema: { type: "object", properties: { city: { type: "string" } }, required: ["city"] },
      run: ({ city }) => `Sunny in ${city}`,
    });

    const final = await meter.withLabels({ customerId: "cust_block" }, () =>
      claude.beta.messages.toolRunner({ ...params, tools: [getWeather] }).runUntilDone(),
    );
    await meter.flush();

    expect(final.id).toBe("msg_02");
    expect(JSON.stringify(fake.requests[1])).toContain("Sunny in Paris");
    expect(sent.flat().map((event) => [event.event_id, event.external_customer_id])).toEqual([
      ["msg_01", "cust_block"],
      ["msg_02", "cust_block"],
    ]);
  });

  it("adds the middleware once, also to a copy made with withOptions", async () => {
    const { meter, client, sent } = setup([{ message: anthropicMessage() }, { message: anthropicMessage() }]);
    const claude = meter.wrap(meter.wrap(client, { customerId: "cust_42" }));
    const copy = meter.wrap(claude.withOptions({ timeout: 30_000 }));

    await claude.messages.create(params);
    await copy.messages.create(params);
    await meter.flush();

    expect(sent.flat()).toHaveLength(2);
  });

  it("does nothing when metering is off", async () => {
    const { meter, client, sent } = setup([{ message: anthropicMessage() }], { enabled: false });
    const before = client.middleware;

    meter.wrap(client, { customerId: "cust_42" });
    await client.messages.create(params);
    await meter.flush();

    expect(client.middleware).toBe(before);
    expect(sent).toEqual([]);
  });

  it("trusts the response id only from Anthropic's own host, or when the provider is named", async () => {
    const steps: AnthropicStep[] = [{ message: anthropicMessage() }];

    const proxied = setup(steps, { baseURL: "https://llm-proxy.example.com" });
    const unknown = proxied.meter.wrap(proxied.client, { customerId: "cust_42" });
    await unknown.messages.create(params);
    await proxied.meter.flush();

    const named = setup(steps, { baseURL: "https://llm-proxy.example.com" });
    const known = named.meter.wrap(named.client, { customerId: "cust_42", provider: "anthropic" });
    await known.messages.create(params);
    await named.meter.flush();

    expect(proxied.sent[0]?.[0]?.properties.provider).toBe("unknown");
    expect(proxied.sent[0]?.[0]?.event_id).not.toBe("msg_01");
    expect(named.sent[0]?.[0]?.properties.provider).toBe("anthropic");
    expect(named.sent[0]?.[0]?.event_id).toBe("msg_01");
  });

  it("reports a client too old to have middleware, and leaves it alone", () => {
    const { meter, onError } = setup([]);
    const old = { baseURL: "https://api.anthropic.com", messages: { create: () => undefined } };

    expect(meter.wrap(old)).toBe(old);
    expect(onError).toHaveBeenCalledWith(expect.any(Error), "wrap");
    const [error] = onError.mock.calls[0] as [Error];
    expect(error.message).toContain("0.101.0");
  });

  it("reports a client it cannot meter yet", () => {
    const { meter, onError } = setup([]);
    const other = { chat: {} };

    expect(meter.wrap(other)).toBe(other);
    expect(onError).toHaveBeenCalledWith(expect.any(Error), "wrap");
  });
});
