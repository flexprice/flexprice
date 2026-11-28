import type { UsageEvent } from "../types.js";

/** Sends one batch. Resolves when FlexPrice accepted it, and rejects otherwise. */
export type SendEvents = (events: UsageEvent[]) => Promise<void>;

/**
 * Why a batch was not accepted.
 *
 * `status` is the difference that matters to whoever decides what to do next.
 * With a status, FlexPrice answered and said no. Without one, there was no
 * answer at all, so the batch may or may not have arrived.
 */
export class SendError extends Error {
  /** The HTTP status of the answer, or undefined when there was no answer. */
  readonly status: number | undefined;
  /** How long FlexPrice asked us to wait before trying again, when it said. */
  readonly retryAfterMs: number | undefined;

  constructor(
    message: string,
    details: { status?: number; retryAfterMs?: number; cause?: unknown } = {},
  ) {
    super(message, { cause: details.cause });
    this.name = "SendError";
    this.status = details.status;
    this.retryAfterMs = details.retryAfterMs;
  }
}

/**
 * Makes the function that posts a batch of events to FlexPrice.
 *
 * It makes exactly one attempt and reports what happened. Deciding whether to
 * try again belongs to the queue.
 */
export function createHttpSender(options: {
  apiKey: string;
  baseUrl: string;
  timeoutMs: number;
}): SendEvents {
  const url = `${options.baseUrl.replace(/\/$/, "")}/events/bulk`;

  return async (events) => {
    if (events.length === 0) return;

    let response: Response;
    try {
      response = await fetch(url, {
        method: "POST",
        headers: {
          "content-type": "application/json",
          "x-api-key": options.apiKey,
        },
        body: JSON.stringify({ events }),
        // Give up after the timeout, so a slow FlexPrice can never hold a send open forever.
        signal: AbortSignal.timeout(options.timeoutMs),
      });
    } catch (cause) {
      // Connection refused, DNS failure, or the timeout above.
      throw new SendError("no answer from FlexPrice", { cause });
    }

    // The body of the answer is not needed. Dropping it frees the connection.
    await response.body?.cancel().catch(() => undefined);

    if (response.ok) return;

    throw new SendError(`FlexPrice answered ${response.status}`, {
      status: response.status,
      retryAfterMs: readRetryAfter(response.headers.get("retry-after")),
    });
  };
}

/** A Retry-After header given in seconds, as milliseconds. Anything else is ignored. */
function readRetryAfter(value: string | null): number | undefined {
  if (value === null || value.trim() === "") return undefined;

  const seconds = Number(value);
  return Number.isFinite(seconds) && seconds >= 0 ? seconds * 1000 : undefined;
}
