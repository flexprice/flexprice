import { randomUUID } from "node:crypto";

import type { Observation, Usage, UsageEvent } from "../types.js";
import { SDK_VERSION } from "../version.js";
import { flattenRaw } from "./raw.js";
import {
  CALL_PROPERTIES,
  COST_PROPERTY,
  COUNT_PROPERTIES,
  EVENT_NAME,
  EVENT_SOURCE,
  MARKER_PROPERTIES,
  SCHEMA_VERSION,
  SDK_LANGUAGE,
  isReservedProperty,
} from "./schema.js";

export type BuildOptions = {
  /** The event name to send. Defaults to EVENT_NAME. */
  eventName?: string;
  /** Makes an event id when the call has no response id. Defaults to a random id. */
  newId?: () => string;
};

export type BuildResult = {
  event: UsageEvent;
  /** One line for each value that was left out because it could not be used. */
  problems: string[];
};

/**
 * Turns one observed call into the event FlexPrice receives.
 *
 * It only reads its arguments and returns a result: no network, no clock, no
 * shared state. A count, cost or tag that cannot be used is left out of the
 * event and described in `problems`, so a wrong number never reaches a billing
 * meter. An observation that is itself malformed is a bug in the caller, and
 * the caller guards against it.
 */
export function buildEvent(observation: Observation, options: BuildOptions = {}): BuildResult {
  const { labels, info, usage } = observation;
  const problems: string[] = [];
  const properties: Record<string, string> = {};

  // About the call. Optional values are added only when they are set.
  properties[CALL_PROPERTIES.provider] = info.provider;
  if (info.gateway) properties[CALL_PROPERTIES.gateway] = info.gateway;
  properties[CALL_PROPERTIES.api] = info.api;
  properties[CALL_PROPERTIES.type] = info.type;
  if (info.model) properties[CALL_PROPERTIES.model] = info.model;
  if (info.requestedModel && info.requestedModel !== info.model) {
    properties[CALL_PROPERTIES.requestedModel] = info.requestedModel;
  }
  properties[CALL_PROPERTIES.streamed] = String(info.streamed);
  if (observation.outcome) properties[CALL_PROPERTIES.outcome] = observation.outcome;
  if (labels.agentId) properties[CALL_PROPERTIES.agentId] = labels.agentId;

  // What gets added up and priced.
  addCounts(properties, usage, problems);
  addCost(properties, usage.costUsd, problems);

  properties[CALL_PROPERTIES.sequence] = String(observation.sequence);
  properties[CALL_PROPERTIES.isFinal] = String(observation.final);

  // The marker that says this SDK sent the event.
  properties[MARKER_PROPERTIES.flag] = "true";
  properties[MARKER_PROPERTIES.language] = SDK_LANGUAGE;
  properties[MARKER_PROPERTIES.version] = SDK_VERSION;
  properties[MARKER_PROPERTIES.schemaVersion] = SCHEMA_VERSION;

  // The provider's own numbers.
  Object.assign(properties, flattenRaw(observation.raw));

  // Custom tags go last, and can never replace anything added above.
  addTags(properties, labels.tags, problems);

  const makeId = options.newId ?? randomUUID;

  return {
    event: {
      event_name: options.eventName ?? EVENT_NAME,
      event_id: info.responseId || makeId(),
      external_customer_id: labels.customerId,
      timestamp: toTimestamp(observation.endedAt),
      source: EVENT_SOURCE,
      properties,
    },
    problems,
  };
}

/** Adds each token count that is a positive whole number. */
function addCounts(properties: Record<string, string>, usage: Usage, problems: string[]): void {
  const counts = Object.entries(COUNT_PROPERTIES) as [keyof typeof COUNT_PROPERTIES, string][];

  for (const [field, property] of counts) {
    const value: unknown = usage[field];

    // Not reported, or reported as nothing: leave it out. It is never sent as 0.
    if (value === undefined || value === null || value === 0) continue;

    if (typeof value === "number" && Number.isSafeInteger(value) && value > 0) {
      properties[property] = String(value);
    } else {
      problems.push(`count ${property} is not a positive whole number: ${describeValue(value)}`);
    }
  }
}

/** A cost is kept only as plain decimal text, such as "0.0123". */
const PLAIN_DECIMAL = /^\d+(\.\d+)?$/;

/**
 * Adds the gateway's cost, exactly as written, when it is a plain positive decimal.
 * Anything else is left out: a negative or unreadable cost must not reach a meter
 * that adds up money.
 */
function addCost(properties: Record<string, string>, cost: unknown, problems: string[]): void {
  if (cost === undefined || cost === null || cost === "") return;

  if (typeof cost === "string" && PLAIN_DECIMAL.test(cost)) {
    // A cost of zero is nothing to bill, so it is left out like a count of zero.
    if (Number(cost) > 0) properties[COST_PROPERTY] = cost;
    return;
  }

  problems.push(`cost ${COST_PROPERTY} is not a plain decimal number in text: ${describeValue(cost)}`);
}

/** A bad value, for a problem message: a number or short text as it is, otherwise its kind. */
function describeValue(value: unknown): string {
  if (typeof value === "number") return String(value);
  if (typeof value === "string") return JSON.stringify(value.slice(0, 20));
  return typeof value;
}

/** Adds the app's own tags, skipping any that use a name the SDK owns. */
function addTags(
  properties: Record<string, string>,
  tags: Record<string, unknown> | undefined,
  problems: string[],
): void {
  if (typeof tags !== "object" || tags === null) return;

  for (const [name, value] of Object.entries(tags)) {
    if (isReservedProperty(name)) {
      problems.push(`tag "${name}" uses a reserved property name`);
    } else if (typeof value === "string") {
      properties[name] = value;
    } else if (typeof value === "boolean" || (typeof value === "number" && Number.isFinite(value))) {
      properties[name] = String(value);
    } else {
      problems.push(`tag "${name}" has a value that is not text, a number, or true or false`);
    }
  }
}

/** The time in UTC, cut to whole seconds: 2026-11-23T09:15:02Z. */
function toTimestamp(date: Date): string {
  return `${date.toISOString().slice(0, 19)}Z`;
}
