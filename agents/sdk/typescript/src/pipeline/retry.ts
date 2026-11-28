/** The first wait after a failed send. Each failure in a row doubles it. */
export const BACKOFF_START_MS = 1000;
/** The longest wait between two attempts. */
export const BACKOFF_MAX_MS = 60_000;
/** How long to wait after FlexPrice refused the request, for example the API key. */
export const HOLD_DELAY_MS = 60_000;

/**
 * What to do with a batch that was not accepted.
 *
 * - `retry`: nothing went wrong with the events. Send the same batch again later.
 * - `reject`: FlexPrice cannot accept something in the batch. Find the bad events.
 * - `hold`: FlexPrice refused the request itself, usually the key or the URL.
 *   Keep the events and try again slowly, until the setup is fixed.
 */
export type FailureKind = "retry" | "reject" | "hold";

export type Failure = {
  kind: FailureKind;
  /** The HTTP status, or undefined when there was no answer. */
  status: number | undefined;
  retryAfterMs: number | undefined;
};

/**
 * Sorts a failed send into one of the three kinds.
 *
 * It reads a numeric `status` and `retryAfterMs` from whatever was thrown, so
 * it works for the SDK's own sender and for a send function the app supplies.
 * Anything without a status counts as no answer, which is always safe to
 * retry: the events keep their ids, so FlexPrice counts a resent event once.
 */
export function classifyFailure(error: unknown): Failure {
  const status = numberProperty(error, "status");
  const retryAfterMs = numberProperty(error, "retryAfterMs");
  return { kind: kindOf(status), status, retryAfterMs };
}

function kindOf(status: number | undefined): FailureKind {
  if (status === undefined) return "retry";
  if (status === 408 || status === 429 || status >= 500) return "retry";
  if (status === 400 || status === 413 || status === 422) return "reject";
  return "hold";
}

/**
 * How long to wait, in whole milliseconds, before attempt number failures + 1.
 *
 * Starts at 1 second and doubles up to 60. A random fifth is added on top, so
 * many apps that failed at the same moment do not all retry at the same moment.
 * It never waits less than FlexPrice asked for with Retry-After.
 */
export function backoffDelay(
  failures: number,
  retryAfterMs: number | undefined,
  random: () => number,
): number {
  const base = Math.min(BACKOFF_START_MS * 2 ** (failures - 1), BACKOFF_MAX_MS);
  const withJitter = Math.min(BACKOFF_MAX_MS, Math.round(base * (1 + 0.2 * random())));
  return Math.max(withJitter, retryAfterMs ?? 0);
}

/** A finite, non-negative number stored under `key` on an object, if there is one. */
function numberProperty(value: unknown, key: string): number | undefined {
  if (typeof value !== "object" || value === null) return undefined;
  const found = (value as Record<string, unknown>)[key];
  return typeof found === "number" && Number.isFinite(found) && found >= 0 ? found : undefined;
}
