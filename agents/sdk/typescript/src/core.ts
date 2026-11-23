import type { ResolvedConfig } from "./config.js";
import { type Reporter, createReporter } from "./errors.js";
import { buildEvent } from "./event/build.js";
import { resolveLabels } from "./labels.js";
import { createEventQueue } from "./pipeline/queue.js";
import type { Labels, Observation } from "./types.js";

/** What the hooks use. Not exported from the package. */
export type Core = {
  report: Reporter;
  /** The labels for a call that starts now. Undefined, and reported, when no customer is set. */
  labelsForCall: (clientLabels?: Partial<Labels>) => Labels | undefined;
  /** Builds the event for one finished call and queues it. Never throws. */
  record: (observation: Observation) => void;
  flush: (timeoutMs?: number) => Promise<void>;
  /** Sends what is waiting, then refuses later calls. */
  shutdown: (timeoutMs?: number) => Promise<void>;
};

/**
 * The part of the meter the hooks talk to.
 *
 * A hook asks for the labels when a call starts, and hands over what it saw
 * when the call ends. Everything after that happens here: building the event,
 * reporting what could not be used, and queueing it. None of it can throw into
 * the app's call.
 */
export function createCore(config: ResolvedConfig): Core {
  const report = createReporter(config.onError);
  const queue = createEventQueue({
    send: config.send,
    report,
    flushIntervalMs: config.flushIntervalMs,
    flushAt: config.flushAt,
    maxQueueSize: config.maxQueueSize,
  });
  let stopped = false;

  function labelsForCall(clientLabels?: Partial<Labels>): Labels | undefined {
    const labels = resolveLabels(clientLabels, config.defaults);
    if (!labels) {
      report(
        new Error(
          "A call had no customer id, so it was not metered. " +
            "Set customerId with withLabels(), on wrap(), or in the meter's defaults.",
        ),
        "labels",
      );
    }
    return labels;
  }

  function record(observation: Observation): void {
    if (stopped) {
      report(new Error("The meter was shut down, so a call that finished afterwards was not metered."), "event");
      return;
    }

    try {
      const { event, problems } = buildEvent(observation, { eventName: config.eventName });
      for (const problem of problems) report(problem, "event");
      if (config.debug) console.debug(`[flexprice-agent-sdk] event ${JSON.stringify(event)}`);
      queue.push(event);
    } catch (error) {
      // A malformed observation is a bug in a hook. The app's call must not pay for it.
      report(error, "event");
    }
  }

  async function shutdown(timeoutMs?: number): Promise<void> {
    // Calls that finish while this flush runs are still sent with it.
    await queue.flush(timeoutMs);
    stopped = true;
  }

  return {
    report,
    labelsForCall,
    record,
    flush: (timeoutMs) => queue.flush(timeoutMs),
    shutdown,
  };
}
