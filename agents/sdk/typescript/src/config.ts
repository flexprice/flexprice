import type { OnError } from "./errors.js";
import { EVENT_NAME } from "./event/schema.js";
import { QUEUE_DEFAULTS } from "./pipeline/queue.js";
import { type SendEvents, createHttpSender } from "./pipeline/sender.js";
import type { Labels } from "./types.js";

/** How long one request to FlexPrice may take, by default. */
export const REQUEST_TIMEOUT_MS = 10_000;

const BASE_URL_EXAMPLE = "https://us.api.flexprice.io/v1";

export type AgentMeterConfig = {
  /** Your FlexPrice API key. Required unless sendEvents is given. */
  apiKey?: string;
  /** Your region's API URL, ending in /v1. Required unless sendEvents is given. */
  baseUrl?: string;
  /** Sends a batch of events, in place of the built-in sender. */
  sendEvents?: SendEvents;
  /** Labels for every call. */
  defaults?: Partial<Labels>;
  /** Called for every metering problem. Without it, the first problem of each kind is logged. */
  onError?: OnError;
  /** Send what is waiting at least this often, in milliseconds. Default 1000. */
  flushIntervalMs?: number;
  /** Send at once when this many events wait. Default 100. */
  flushAt?: number;
  /** The most events held in memory. Default 10,000. */
  maxQueueSize?: number;
  /** How long one request to FlexPrice may take, in milliseconds. Default 10,000. */
  requestTimeoutMs?: number;
  /** The event name. Change it only if the app already sends its own events under model.usage. */
  eventName?: string;
  /** false turns metering off: wrap() returns the client untouched. Default true. */
  enabled?: boolean;
  /** Logs each event as it is built. Default false. */
  debug?: boolean;
};

/**
 * The options after they were checked and filled in.
 *
 * It holds no API key: the key lives only inside `send`, so logging or
 * printing this object can never leak it.
 */
export type ResolvedConfig = {
  send: SendEvents;
  defaults: Partial<Labels> | undefined;
  onError: OnError | undefined;
  flushIntervalMs: number;
  flushAt: number;
  maxQueueSize: number;
  eventName: string;
  enabled: boolean;
  debug: boolean;
};

/**
 * Checks the app's options and fills in the defaults.
 *
 * It runs once, when the meter is created, and throws a TypeError for a bad
 * option. A setup mistake is then found when the app starts, never during a
 * model call. It makes no network request.
 */
export function resolveConfig(config: AgentMeterConfig): ResolvedConfig {
  if (typeof config !== "object" || config === null) fail("the config must be an object.");

  const requestTimeoutMs = positiveWholeNumber(config.requestTimeoutMs, "requestTimeoutMs", REQUEST_TIMEOUT_MS);

  return {
    send: resolveSender(config, requestTimeoutMs),
    defaults: config.defaults,
    onError: optionalFunction(config.onError, "onError"),
    flushIntervalMs: positiveWholeNumber(config.flushIntervalMs, "flushIntervalMs", QUEUE_DEFAULTS.flushIntervalMs),
    flushAt: positiveWholeNumber(config.flushAt, "flushAt", QUEUE_DEFAULTS.flushAt),
    maxQueueSize: positiveWholeNumber(config.maxQueueSize, "maxQueueSize", QUEUE_DEFAULTS.maxQueueSize),
    eventName: config.eventName === undefined ? EVENT_NAME : nonEmptyText(config.eventName, "eventName"),
    enabled: config.enabled !== false,
    debug: config.debug === true,
  };
}

/** The app's own send function, or the built-in sender made from the key and URL. */
function resolveSender(config: AgentMeterConfig, timeoutMs: number): SendEvents {
  if (config.sendEvents !== undefined) {
    if (typeof config.sendEvents !== "function") fail("sendEvents must be a function.");
    return config.sendEvents;
  }

  // Trimmed, so a key made only of spaces or a newline counts as missing and
  // fails at startup.
  const apiKey = typeof config.apiKey === "string" ? config.apiKey.trim() : "";
  if (apiKey === "") fail("apiKey is required. Pass your FlexPrice API key, or a sendEvents function.");

  return createHttpSender({ apiKey, baseUrl: checkBaseUrl(config.baseUrl), timeoutMs });
}

/**
 * Every FlexPrice API path starts with /v1, so a URL that does not end in it
 * would get a 404 for every batch. The bad value is never repeated in the
 * message, in case the key was passed in the wrong place.
 */
function checkBaseUrl(value: unknown): string {
  if (typeof value !== "string") fail(`baseUrl is required, for example ${BASE_URL_EXAMPLE}.`);

  let url: URL;
  try {
    url = new URL(value);
  } catch {
    fail(`baseUrl is not a URL. Use your region's API URL, for example ${BASE_URL_EXAMPLE}.`);
  }

  if (url.protocol !== "https:" && url.protocol !== "http:") {
    fail(`baseUrl must start with https://, for example ${BASE_URL_EXAMPLE}.`);
  }
  if (!url.pathname.replace(/\/+$/, "").endsWith("/v1")) {
    fail(`baseUrl must end with /v1, for example ${BASE_URL_EXAMPLE}.`);
  }

  return value;
}

function positiveWholeNumber(value: unknown, name: string, fallback: number): number {
  if (value === undefined) return fallback;
  if (typeof value === "number" && Number.isSafeInteger(value) && value > 0) return value;
  fail(`${name} must be a positive whole number.`);
}

function nonEmptyText(value: unknown, name: string): string {
  if (typeof value === "string" && value.trim() !== "") return value.trim();
  fail(`${name} must be a non-empty string.`);
}

function optionalFunction<T>(value: T | undefined, name: string): T | undefined {
  if (value === undefined || typeof value === "function") return value;
  fail(`${name} must be a function.`);
}

function fail(message: string): never {
  throw new TypeError(`[flexprice-agent-sdk] ${message}`);
}
