/** What the fake answers one request with. */
export type AnthropicStep =
  /** 200, with this message as JSON. */
  | { message: Record<string, unknown> }
  /** An error answer, with optional headers such as retry-after-ms. */
  | { status: number; headers?: Record<string, string> }
  /**
   * 200, as a stream of these events. With holdAfter, it sends that many and
   * then keeps the stream open until the request is aborted, the way a real
   * server does while the model is still writing. With failAfter, it sends
   * that many and then the connection breaks.
   */
  | { events: Record<string, unknown>[]; holdAfter?: number; failAfter?: number };

export type FakeAnthropic = {
  /** Pass this as the Anthropic client's fetch option. */
  fetch: typeof fetch;
  /** The JSON body of each request, in order. Retries count as requests. */
  requests: unknown[];
  /** How many streamed answers the client cancelled before they ended. */
  readonly cancelledStreams: number;
};

/**
 * Stands in for Anthropic's API. The real client runs against it, so its own
 * code (promises, streams, helpers, retries) runs for real.
 */
export function fakeAnthropic(steps: AnthropicStep[]): FakeAnthropic {
  const remaining = [...steps];
  const requests: unknown[] = [];
  let cancelled = 0;

  const answer = (step: AnthropicStep | undefined, signal: AbortSignal | null | undefined): Response => {
    if (step === undefined) return errorResponse(500, {});
    if ("message" in step) return jsonResponse(200, step.message, {});
    if ("events" in step) {
      if (step.failAfter !== undefined) return brokenStreamResponse(step.events, step.failAfter);
      return streamResponse(step.events, step.holdAfter, signal, () => {
        cancelled++;
      });
    }
    return errorResponse(step.status, step.headers ?? {});
  };

  const fakeFetch = (input: string | URL | Request, init?: RequestInit): Promise<Response> => {
    requests.push(parseBody(init?.body));
    const url = input instanceof Request ? input.url : String(input);
    return Promise.resolve(withUrl(answer(remaining.shift(), init?.signal), url));
  };

  return {
    fetch: fakeFetch,
    requests,
    get cancelledStreams() {
      return cancelled;
    },
  };
}

/**
 * A complete message, as the Messages API returns it. The usage block has the
 * same fields, in the same order, as a real response.
 */
export function anthropicMessage(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    id: "msg_01",
    type: "message",
    role: "assistant",
    model: "claude-sonnet-5-5",
    content: [{ type: "text", text: "Hello" }],
    stop_reason: "end_turn",
    stop_sequence: null,
    usage: {
      input_tokens: 1200,
      cache_creation_input_tokens: 0,
      cache_read_input_tokens: 0,
      cache_creation: { ephemeral_5m_input_tokens: 0, ephemeral_1h_input_tokens: 0 },
      output_tokens: 300,
      output_tokens_details: { thinking_tokens: 0 },
      service_tier: "standard",
      inference_geo: "global",
      speed: "standard",
    },
    ...overrides,
  };
}

/**
 * The events of a complete stream, as a real one sends them: start, one text
 * block with a ping inside it, delta, stop.
 */
export function anthropicStreamEvents(): Record<string, unknown>[] {
  return [
    {
      type: "message_start",
      message: {
        id: "msg_01",
        type: "message",
        role: "assistant",
        model: "claude-sonnet-5-5",
        content: [],
        stop_reason: null,
        stop_sequence: null,
        usage: {
          input_tokens: 1200,
          cache_creation_input_tokens: 0,
          cache_read_input_tokens: 50000,
          cache_creation: { ephemeral_5m_input_tokens: 0, ephemeral_1h_input_tokens: 0 },
          output_tokens: 1,
          service_tier: "standard",
          inference_geo: "global",
          speed: "standard",
        },
      },
    },
    { type: "content_block_start", index: 0, content_block: { type: "text", text: "" } },
    { type: "ping" },
    { type: "content_block_delta", index: 0, delta: { type: "text_delta", text: "Hello" } },
    { type: "content_block_stop", index: 0 },
    {
      type: "message_delta",
      delta: { stop_reason: "end_turn", stop_sequence: null },
      // Running totals: the input side is stated again, and output_tokens_details arrives only here.
      usage: {
        input_tokens: 1200,
        cache_creation_input_tokens: 0,
        cache_read_input_tokens: 50000,
        output_tokens: 300,
        output_tokens_details: { thinking_tokens: 0 },
      },
    },
    { type: "message_stop" },
  ];
}

function jsonResponse(status: number, body: unknown, headers: Record<string, string>): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json", "request-id": "req_test", ...headers },
  });
}

function errorResponse(status: number, headers: Record<string, string>): Response {
  const type = status === 401 ? "authentication_error" : status === 529 ? "overloaded_error" : "api_error";
  return jsonResponse(status, { type: "error", error: { type, message: `fake ${type}` } }, headers);
}

/** A response from fetch carries the URL it came from. One made with new Response() does not. */
function withUrl(response: Response, url: string): Response {
  Object.defineProperty(response, "url", { value: url });
  return response;
}

/** Each event as the bytes of one server-sent event. */
function sseChunks(events: Record<string, unknown>[]): Uint8Array[] {
  const encoder = new TextEncoder();
  return events.map((event) => encoder.encode(`event: ${String(event.type)}\ndata: ${JSON.stringify(event)}\n\n`));
}

function streamResponse(
  events: Record<string, unknown>[],
  holdAfter: number | undefined,
  signal: AbortSignal | null | undefined,
  onCancel: () => void,
): Response {
  const chunks = sseChunks(events);

  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const chunk of holdAfter === undefined ? chunks : chunks.slice(0, holdAfter)) {
        controller.enqueue(chunk);
      }
      if (holdAfter === undefined) {
        controller.close();
        return;
      }

      const abort = () => controller.error(signal?.reason ?? new DOMException("aborted", "AbortError"));
      if (signal?.aborted) abort();
      else signal?.addEventListener("abort", abort, { once: true });
    },
    cancel() {
      onCancel();
    },
  });

  return new Response(body, { status: 200, headers: { "content-type": "text/event-stream" } });
}

/**
 * Sends the first `count` events, then fails the way fetch does when the
 * server closes the connection partway through a body.
 *
 * Each event is handed over only when it is read. Failing a stream throws
 * away whatever was queued and not yet read, so queueing them all at once
 * would lose them.
 */
function brokenStreamResponse(events: Record<string, unknown>[], count: number): Response {
  const chunks = sseChunks(events).slice(0, count);

  const body = new ReadableStream<Uint8Array>(
    {
      pull(controller) {
        const chunk = chunks.shift();
        if (chunk !== undefined) controller.enqueue(chunk);
        else controller.error(new TypeError("terminated"));
      },
    },
    { highWaterMark: 0 },
  );

  return new Response(body, { status: 200, headers: { "content-type": "text/event-stream" } });
}

function parseBody(body: unknown): unknown {
  if (typeof body !== "string") return undefined;
  try {
    return JSON.parse(body);
  } catch {
    return body;
  }
}
