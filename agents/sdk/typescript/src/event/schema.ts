/**
 * Every name and limit of the usage event, in one place.
 *
 * Meters in FlexPrice are built on these names. Changing one is a change to
 * the public contract: it needs a new SCHEMA_VERSION, and a test pins the list
 * so it cannot change by accident.
 */

/** The name every usage event is sent under, unless the app configures another. */
export const EVENT_NAME = "model.usage";

/** The value of the event's built-in `source` field. */
export const EVENT_SOURCE = "flexprice-agent-sdk";

export const SCHEMA_VERSION = "1";
export const SDK_LANGUAGE = "typescript";

/** The provider's own usage numbers are kept under this prefix. */
export const RAW_PREFIX = "raw_";
export const RAW_MAX_FIELDS = 32;
export const RAW_MAX_DEPTH = 2;
export const RAW_MAX_STRING_LENGTH = 64;

/** Properties that describe the call. */
export const CALL_PROPERTIES = {
  provider: "provider",
  gateway: "gateway",
  api: "api",
  type: "type",
  model: "model",
  requestedModel: "requested_model",
  streamed: "streamed",
  outcome: "outcome",
  agentId: "agent_id",
  sequence: "sequence",
  isFinal: "is_final",
} as const;

/** The property each token count is sent as. Keys are the fields of Usage. */
export const COUNT_PROPERTIES = {
  inputTokens: "input_tokens",
  cacheReadTokens: "cache_read_tokens",
  cacheWriteTokens: "cache_write_tokens",
  cacheWrite1hTokens: "cache_write_1h_tokens",
  outputTokens: "output_tokens",
} as const;

/** What a gateway says the call cost. */
export const COST_PROPERTY = "cost_usd";

/** Counts reserved now so nothing else uses the names. Filled in a later phase. */
export const RESERVED_COUNT_PROPERTIES = [
  "audio_input_tokens",
  "audio_output_tokens",
  "image_input_tokens",
  "image_output_tokens",
  "input_characters",
  "input_audio_seconds",
  "output_images",
] as const;

/** The properties that mark an event as sent by this SDK. */
export const MARKER_PROPERTIES = {
  flag: "flexprice_agent_sdk",
  language: "sdk_language",
  version: "sdk_version",
  schemaVersion: "schema_version",
} as const;

/** Every fixed property name in this schema version, the reserved ones included. */
export const FIXED_PROPERTIES: readonly string[] = [
  ...Object.values(CALL_PROPERTIES),
  ...Object.values(COUNT_PROPERTIES),
  COST_PROPERTY,
  ...RESERVED_COUNT_PROPERTIES,
  ...Object.values(MARKER_PROPERTIES),
];

const FIXED_PROPERTY_SET: ReadonlySet<string> = new Set(FIXED_PROPERTIES);

/**
 * True for a name the SDK owns: a fixed property, or anything under `raw_`.
 * A custom tag may not use one.
 */
export function isReservedProperty(name: string): boolean {
  return FIXED_PROPERTY_SET.has(name) || name.startsWith(RAW_PREFIX);
}
