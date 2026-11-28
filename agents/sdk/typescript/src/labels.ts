import { AsyncLocalStorage } from "node:async_hooks";

import type { Labels } from "./types.js";

/**
 * The labels of the block the current code is running in, if any.
 *
 * A plain variable would not work here. An app handles many requests at the
 * same time, and each one pauses at every `await`, so one shared variable would
 * hold whichever request set it last. AsyncLocalStorage keeps a separate value
 * for each chain of async work and carries it across every `await`.
 */
const current = new AsyncLocalStorage<Partial<Labels>>();

/** Runs fn. Every call that starts inside it gets these labels. */
export function withLabels<T>(labels: Partial<Labels>, fn: () => T): T {
  return current.run(merge(current.getStore(), labels), fn);
}

/**
 * The labels for a call that starts right now, or undefined when no customer is set.
 * Priority, key by key: the current block (innermost first), then the client's
 * labels, then the defaults.
 */
export function resolveLabels(
  clientLabels?: Partial<Labels>,
  defaults?: Partial<Labels>,
): Labels | undefined {
  const merged = merge(merge(defaults, clientLabels), current.getStore());
  if (merged.customerId === undefined) return undefined;

  const labels: Labels = { customerId: merged.customerId };
  if (merged.agentId !== undefined) labels.agentId = merged.agentId;
  if (merged.tags && Object.keys(merged.tags).length > 0) labels.tags = merged.tags;
  return labels;
}

/** `over` wins over `base`, key by key. Tags are merged key by key too. */
function merge(
  base: Partial<Labels> | undefined,
  over: Partial<Labels> | undefined,
): Partial<Labels> {
  const merged: Partial<Labels> = { ...base };
  if (typeof over !== "object" || over === null) return merged;

  const customerId = asId(over.customerId);
  if (customerId !== undefined) merged.customerId = customerId;

  const agentId = asId(over.agentId);
  if (agentId !== undefined) merged.agentId = agentId;

  if (typeof over.tags === "object" && over.tags !== null) {
    merged.tags = { ...base?.tags, ...over.tags };
  }

  return merged;
}

/**
 * An id as text. Apps often have numeric ids, and FlexPrice accepts only text,
 * so a number is converted. An empty string, or any other kind of value, counts
 * as not set.
 */
function asId(value: unknown): string | undefined {
  if (typeof value === "string" && value !== "") return value;
  if (typeof value === "number" && Number.isFinite(value)) return String(value);
  return undefined;
}
