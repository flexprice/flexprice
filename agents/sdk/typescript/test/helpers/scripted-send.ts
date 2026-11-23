import { SendError, type SendEvents } from "../../src/pipeline/sender.js";
import type { UsageEvent } from "../../src/types.js";

/** What one call to the send function does. */
export type Step =
  /** Accepted. */
  | "ok"
  /** No answer at all. */
  | "no-answer"
  /** A bug in the send function: it throws instead of returning a failed promise. */
  | "throw"
  /** FlexPrice answers with this status. */
  | number
  /** FlexPrice answers with this status and a Retry-After. */
  | { status: number; retryAfterMs: number }
  /** Accepted, after this many milliseconds. */
  | { delayMs: number };

export type ScriptedSend = {
  send: SendEvents;
  /** The event ids of each batch, in the order the batches were sent. */
  batches: string[][];
};

/**
 * A send function that follows the steps in order, one step per call, then
 * accepts everything. It records every batch it is given.
 */
export function scriptedSend(steps: Step[] = []): ScriptedSend {
  const remaining = [...steps];
  const batches: string[][] = [];

  const send: SendEvents = (events) => {
    batches.push(events.map((event) => event.event_id));
    const step = remaining.shift() ?? "ok";

    if (step === "ok") return Promise.resolve();
    if (step === "no-answer") return Promise.reject(new SendError("no answer from FlexPrice"));
    if (step === "throw") throw new Error("the send function has a bug");
    if (typeof step === "number") {
      return Promise.reject(new SendError(`FlexPrice answered ${step}`, { status: step }));
    }
    if ("delayMs" in step) {
      return new Promise<void>((resolve) => setTimeout(resolve, step.delayMs));
    }
    return Promise.reject(new SendError(`FlexPrice answered ${step.status}`, step));
  };

  return { send, batches };
}

/** A usage event with the given id. */
export function makeEvent(id: string): UsageEvent {
  return {
    event_name: "model.usage",
    event_id: id,
    external_customer_id: "cust_42",
    timestamp: "2026-11-23T09:15:02Z",
    source: "flexprice-agent-sdk",
    properties: { input_tokens: "1200" },
  };
}
