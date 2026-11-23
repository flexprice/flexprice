import type { Reporter } from "../errors.js";
import type { UsageEvent } from "../types.js";
import { type Failure, HOLD_DELAY_MS, backoffDelay, classifyFailure } from "./retry.js";
import type { SendEvents } from "./sender.js";

export const QUEUE_DEFAULTS = {
  /** Send what is waiting at least this often. */
  flushIntervalMs: 1000,
  /** Send at once when this many events are waiting. */
  flushAt: 100,
  /** The most events in one request. FlexPrice accepts up to 1000. */
  maxBatchSize: 1000,
  /** The most events held in memory. More than this are dropped. */
  maxQueueSize: 10_000,
} as const;

/** How long flush() waits by default before it gives up. */
export const FLUSH_TIMEOUT_MS = 10_000;

export type QueueOptions = {
  send: SendEvents;
  report: Reporter;
  flushIntervalMs?: number;
  flushAt?: number;
  maxBatchSize?: number;
  maxQueueSize?: number;
  /** Randomness for the wait before a retry. Defaults to Math.random. */
  random?: () => number;
};

export type EventQueue = {
  /** Adds an event. Never throws, never waits. */
  push(event: UsageEvent): void;
  /** Sends everything now. Resolves when nothing is left. Never rejects. */
  flush(timeoutMs?: number): Promise<void>;
  /** Events held and not yet accepted, the batch in flight included. */
  size(): number;
};

/**
 * Holds usage events in memory and sends them to FlexPrice in batches.
 *
 * At most one request is in flight at a time. Each trigger sends one batch:
 * the timer firing, enough events arriving, or a flush. After a batch it keeps
 * going while at least `flushAt` events wait, and otherwise leaves the rest to
 * the timer, so a slow trickle of events still goes out in batches.
 */
export function createEventQueue(options: QueueOptions): EventQueue {
  const { send, report } = options;
  const flushIntervalMs = options.flushIntervalMs ?? QUEUE_DEFAULTS.flushIntervalMs;
  const flushAt = options.flushAt ?? QUEUE_DEFAULTS.flushAt;
  const maxBatchSize = options.maxBatchSize ?? QUEUE_DEFAULTS.maxBatchSize;
  const maxQueueSize = options.maxQueueSize ?? QUEUE_DEFAULTS.maxQueueSize;
  const random = options.random ?? Math.random;

  /** Events waiting to be sent, oldest first. */
  const waiting: UsageEvent[] = [];
  /** The batch being sent right now. */
  let inFlight: UsageEvent[] = [];
  /** True while a send is running. */
  let sending = false;
  /** True while the queue waits before sending a failed batch again. */
  let pausedForRetry = false;
  /** The queue's one timer: the flush interval, or the wait before a retry. */
  let timer: NodeJS.Timeout | undefined;
  /** flush() calls that are waiting for the queue to empty. */
  const flushWaiters = new Set<() => void>();
  /** Sends that got no answer or a server error, in a row. Sets the wait before the next try. */
  let failures = 0;
  /** Events dropped since the queue became full. */
  let dropped = 0;

  const size = () => waiting.length + inFlight.length;

  function push(event: UsageEvent): void {
    if (size() >= maxQueueSize) {
      // Reported once when dropping starts, not once per event: during a long
      // outage that could be thousands of reports a second.
      dropped++;
      if (dropped === 1) {
        report(
          new Error(`The queue is full (${maxQueueSize} events), so new events are dropped until it has room.`),
          "overflow",
        );
      }
      return;
    }

    if (dropped > 0) {
      report(new Error(`${countOf(dropped, "event")} dropped while the queue was full.`), "overflow");
      dropped = 0;
    }

    waiting.push(event);
    if (sending || pausedForRetry) return;

    if (waiting.length >= flushAt) startSending(false);
    else if (!timer) startTimer(flushIntervalMs);
  }

  /** Sets the queue's one timer. It never keeps the process alive. */
  function startTimer(ms: number): void {
    if (timer) clearTimeout(timer);
    timer = setTimeout(() => {
      timer = undefined;
      pausedForRetry = false;
      startSending(true);
    }, ms);
    timer.unref();
  }

  function startSending(sendAnyway: boolean): void {
    void sendWaiting(sendAnyway);
  }

  /** Sends batches while there is a reason to. Never rejects. */
  async function sendWaiting(sendAnyway: boolean): Promise<void> {
    if (sending) return;
    sending = true;
    if (timer) {
      clearTimeout(timer);
      timer = undefined;
    }

    try {
      while (waiting.length > 0 && (sendAnyway || waiting.length >= flushAt)) {
        sendAnyway = false;
        inFlight = waiting.splice(0, maxBatchSize);
        const kept = await deliver(inFlight);
        inFlight = [];

        if (kept.length > 0) {
          // Back to the front, so they go before anything that arrived later.
          // The wait before the next try is already set.
          waiting.unshift(...kept);
          break;
        }
      }
    } catch (error) {
      report(error, "send");
    } finally {
      sending = false;
      if (!pausedForRetry && waiting.length > 0 && !timer) startTimer(flushIntervalMs);
      if (size() === 0) {
        for (const done of [...flushWaiters]) done();
      }
    }
  }

  /** Sends one batch. Returns the events to keep for another attempt. */
  async function deliver(batch: UsageEvent[]): Promise<UsageEvent[]> {
    try {
      await send(batch);
      failures = 0;
      return [];
    } catch (error) {
      const failure = classifyFailure(error);
      if (failure.kind === "reject") return deliverOneByOne(batch, failure);
      return keepForLater(batch, failure, error);
    }
  }

  /**
   * FlexPrice rejected the batch, so something in it is bad. Sends each event on
   * its own and drops only the ones FlexPrice still rejects, so one bad event
   * cannot hold up every event behind it.
   */
  async function deliverOneByOne(batch: UsageEvent[], failure: Failure): Promise<UsageEvent[]> {
    const [only] = batch;
    if (batch.length === 1 && only) {
      // Sending a single event again on its own would get the same answer.
      drop(only, failure);
      return [];
    }

    for (const [index, event] of batch.entries()) {
      try {
        await send([event]);
      } catch (error) {
        const oneFailure = classifyFailure(error);
        if (oneFailure.kind === "reject") {
          drop(event, oneFailure);
          continue;
        }
        // FlexPrice stopped answering, or refused the request: keep the rest for later.
        return keepForLater(batch.slice(index), oneFailure, error);
      }
    }

    failures = 0;
    return [];
  }

  /** Keeps events that were not accepted, says why, and sets the wait before the next try. */
  function keepForLater(events: UsageEvent[], failure: Failure, error: unknown): UsageEvent[] {
    let waitMs: number;

    if (failure.kind === "retry") {
      failures++;
      waitMs = backoffDelay(failures, failure.retryAfterMs, random);
      report(
        new Error(
          `FlexPrice did not accept ${countOf(events.length, "event")} (${reasonOf(error)}). ` +
            `They will be sent again in ${secondsOf(waitMs)}.`,
          { cause: error },
        ),
        "send",
      );
    } else {
      waitMs = Math.max(HOLD_DELAY_MS, failure.retryAfterMs ?? 0);
      const keyRefused = failure.status === 401 || failure.status === 403;
      const problem = keyRefused
        ? `FlexPrice refused the API key (status ${failure.status}).`
        : `FlexPrice refused the request (status ${failure.status}). Check the base URL.`;
      report(
        new Error(
          `${problem} ${countOf(events.length, "event")} kept, to be sent again in ${secondsOf(waitMs)}.`,
          { cause: error },
        ),
        keyRefused ? "auth" : "send",
      );
    }

    pausedForRetry = true;
    startTimer(waitMs);
    return events;
  }

  /** Gives up on one event FlexPrice will not accept, and says which one. */
  function drop(event: UsageEvent, failure: Failure): void {
    report(
      new Error(`FlexPrice rejected event ${event.event_id} (status ${failure.status}), so it was dropped.`),
      "send",
    );
  }

  function flush(): Promise<void> {
    if (size() === 0) return Promise.resolve();

    return new Promise<void>((resolve) => {
      const done = () => {
        flushWaiters.delete(done);
        resolve();
      };
      flushWaiters.add(done);
      if (!sending && !pausedForRetry) startSending(true);
    });
  }

  return { push, flush, size };
}

/** "1 event was" or "3 events were". */
function countOf(count: number, noun: string): string {
  return count === 1 ? `1 ${noun} was` : `${count} ${noun}s were`;
}

function secondsOf(ms: number): string {
  return `${Math.round(ms / 1000)} s`;
}

function reasonOf(error: unknown): string {
  return error instanceof Error ? error.message : `a ${typeof error} was thrown`;
}
