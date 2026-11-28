import type { Core } from "../core.js";
import { providerFromBaseUrl } from "../detect/hosts.js";
import { createAnthropicStreamReader, readAnthropicMessage } from "../readers/anthropic.js";
import type { CallInfo, Labels, ReadResult } from "../types.js";
import { createSseDecoder } from "./sse.js";

/*
 * The hook for the @anthropic-ai/sdk client, through the client's own middleware.
 *
 * The client calls each function in its `middleware` list for every HTTP
 * request it makes, and reads the list on every request, so adding to it works
 * on a client that already exists. The client is described here by its shape:
 * this file never loads @anthropic-ai/sdk.
 */

/** The parts of the client's request options the hook reads. */
type RequestOptions = { method?: unknown; path?: unknown; body?: unknown; stream?: unknown };

/** What the client hands each middleware next to the request. */
type MiddlewareContext = {
  options?: RequestOptions;
  /** Parses a copy of the response. The response itself stays readable. */
  parse: (response: Response) => Promise<unknown>;
};

type Middleware = (
  request: unknown,
  next: (request: unknown) => Promise<Response>,
  ctx: MiddlewareContext,
) => Promise<Response>;

/** An @anthropic-ai/sdk client, as far as the hook needs to know it. */
export type AnthropicLikeClient = {
  baseURL?: unknown;
  messages: { create: unknown };
  middleware?: unknown;
};

/** How this client's calls are labelled and described. */
export type AnthropicHookSettings = {
  clientLabels: Partial<Labels>;
  /** Named by the app, when the client's URL does not show it. */
  provider: string | undefined;
  gateway: string | undefined;
};

/** What every event from this client says about where the call went. */
type Destination = {
  provider: string;
  gateway: string | undefined;
  /** The response id is used as the event id only when the provider is known. */
  trustIds: boolean;
};

/**
 * Marks a middleware function with the meter that added it. Shared through the
 * global symbol registry, so two copies of this package loaded side by side
 * still recognize each other's middleware.
 */
const METER = Symbol.for("flexprice.agent-sdk.meter");

export function isAnthropicClient(client: unknown): client is AnthropicLikeClient {
  if (typeof client !== "object" || client === null) return false;
  const messages = (client as { messages?: unknown }).messages;
  return (
    typeof messages === "object" &&
    messages !== null &&
    typeof (messages as { create?: unknown }).create === "function"
  );
}

/** Adds the meter's middleware to the client, unless it is already there. Reports why not, when it cannot. */
export function attachToAnthropic(client: AnthropicLikeClient, core: Core, settings: AnthropicHookSettings): void {
  const list: unknown = client.middleware;
  if (!Array.isArray(list)) {
    core.report(
      new Error(
        "This Anthropic client has no middleware, so its calls are not metered. " +
          "Upgrade @anthropic-ai/sdk to 0.101.0 or newer.",
      ),
      "wrap",
    );
    return;
  }

  // A copy made with withOptions() inherits the list, so the marker is looked for there.
  const owners = list.map(ownerOf);
  if (owners.includes(core)) return;
  if (owners.some((owner) => owner !== undefined)) {
    core.report(new Error("This client is already metered by another meter, so it was not wrapped again."), "wrap");
    return;
  }

  const detected = providerFromBaseUrl(client.baseURL);
  const destination: Destination = {
    provider: settings.provider ?? detected ?? "unknown",
    gateway: settings.gateway,
    trustIds: settings.provider !== undefined || detected !== undefined,
  };

  client.middleware = [...(list as unknown[]), createMiddleware(core, destination, settings.clientLabels)];
}

function createMiddleware(core: Core, destination: Destination, clientLabels: Partial<Labels>): Middleware {
  const middleware: Middleware = async (request, next, ctx) => {
    // The client's own call. What it throws belongs to the app, so it is not caught here.
    const response = await next(request);
    if (!response.ok || !isCreateMessage(ctx?.options)) return response;

    try {
      return await observe(response, ctx, core, destination, clientLabels);
    } catch (error) {
      // Nothing above has touched the response's body, so it is handed back as it was.
      core.report(error, "reader");
      return response;
    }
  };

  Object.defineProperty(middleware, METER, { value: core });
  return middleware;
}

/**
 * Meters one successful call. Returns the response the client should read: the
 * same one for a call that was not streamed, and a pass-through for a stream.
 */
async function observe(
  response: Response,
  ctx: MiddlewareContext,
  core: Core,
  destination: Destination,
  clientLabels: Partial<Labels>,
): Promise<Response> {
  // Read here, inside the call, so the labels of the app's withLabels() block apply.
  const labels = core.labelsForCall(clientLabels);
  if (!labels) return response;

  const options = ctx.options;
  const recordRead = (read: ReadResult, streamed: boolean) =>
    core.record({
      labels,
      info: callInfo(destination, read, options, streamed),
      usage: read.usage,
      raw: read.raw,
      sequence: 0,
      final: true,
      outcome: read.outcome,
      endedAt: new Date(),
    });

  if (options?.stream === true) {
    return watchStream(response, core, (read) => recordRead(read, true));
  }

  // Waiting for the parsed copy means the event is queued before the app's await returns.
  const read = readAnthropicMessage(await ctx.parse(response));
  if (read) recordRead(read, false);
  else {
    core.report(
      new Error(
        "A message from Anthropic had no usage, so the call was not metered. Its response format may have changed.",
      ),
      "reader",
    );
  }
  return response;
}

/**
 * Gives the client a stream that passes every chunk on unchanged, and reads
 * the usage from the chunks as they go by.
 *
 * It does not copy the stream. Under the streams standard, cancelling one half
 * of a copied stream only finishes once the other half is cancelled too, or the
 * answer ends. So with a copy, an app that stopped reading early would wait for
 * the model to finish, and the request would not be cancelled. Passing chunks
 * on only as the app asks for them keeps cancelling exactly as it was.
 */
function watchStream(response: Response, core: Core, onRead: (read: ReadResult) => void): Response {
  const source = response.body as ReadableStream<Uint8Array> | null;
  if (source === null) return response;

  const decoder = createSseDecoder();
  const reader = createAnthropicStreamReader();
  let sawError = false;
  let reportedUnreadable = false;
  let finished = false;

  /** Records the call once, however the stream ends. */
  const finish = (failed: boolean) => {
    if (finished) return;
    finished = true;
    const read = reader.finish(failed || sawError);
    if (read) onRead(read);
  };

  /** Looks at one chunk on its way to the app. Never throws. */
  const look = (chunk: Uint8Array) => {
    try {
      for (const { event, data } of decoder.push(chunk)) {
        if (event === "error") {
          sawError = true;
          continue;
        }

        let parsed: unknown;
        try {
          parsed = JSON.parse(data);
        } catch {
          if (!reportedUnreadable) {
            reportedUnreadable = true;
            core.report(new Error("An event in a stream from Anthropic could not be read."), "reader");
          }
          continue;
        }

        reader.onEvent(parsed);
        // The message is complete, so it is recorded now, whatever the app does next.
        if (isObject(parsed) && parsed.type === "message_stop") finish(false);
      }
    } catch (error) {
      core.report(error, "reader");
    }
  };

  let upstream: ReadableStreamDefaultReader<Uint8Array>;

  const passThrough = new ReadableStream<Uint8Array>(
    {
      start() {
        upstream = source.getReader();
      },
      async pull(controller) {
        const result = await upstream.read().catch((error: unknown) => {
          // The connection broke, or the app aborted the request.
          finish(!isAbortError(error));
          controller.error(error);
          return undefined;
        });
        if (result === undefined) return;

        if (result.done) {
          finish(false);
          controller.close();
          return;
        }

        look(result.value);
        controller.enqueue(result.value);
      },
      cancel(reason) {
        // The app stopped reading. What arrived is recorded, and the request is cancelled as usual.
        finish(false);
        return upstream.cancel(reason);
      },
    },
    // Pull nothing ahead of the app.
    { highWaterMark: 0 },
  );

  const watched = new Response(passThrough, {
    status: response.status,
    statusText: response.statusText,
    headers: response.headers,
  });
  // A Response made with new Response() has no URL. The app and the client's
  // logs read it, so it keeps the original's.
  Object.defineProperty(watched, "url", { value: response.url });
  return watched;
}

function callInfo(
  destination: Destination,
  read: ReadResult,
  options: RequestOptions | undefined,
  streamed: boolean,
): CallInfo {
  return {
    provider: destination.provider,
    gateway: destination.gateway,
    api: "anthropic.messages",
    type: "chat",
    model: read.model,
    requestedModel: requestedModelOf(options?.body),
    responseId: destination.trustIds ? read.responseId : undefined,
    streamed,
  };
}

/**
 * Both messages.create and beta.messages.create post to /v1/messages. Older
 * clients put ?beta=true in the path of the beta one, so the query is ignored.
 */
function isCreateMessage(options: RequestOptions | undefined): boolean {
  if (typeof options?.method !== "string" || typeof options.path !== "string") return false;
  return options.method.toLowerCase() === "post" && options.path.split("?")[0] === "/v1/messages";
}

function requestedModelOf(body: unknown): string | undefined {
  if (!isObject(body)) return undefined;
  return typeof body.model === "string" ? body.model : undefined;
}

function isAbortError(error: unknown): boolean {
  return isObject(error) && error.name === "AbortError";
}

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function ownerOf(middleware: unknown): unknown {
  return typeof middleware === "function" ? (middleware as { [METER]?: unknown })[METER] : undefined;
}
