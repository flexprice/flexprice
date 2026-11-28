import type { Outcome, ReadResult, Usage } from "../types.js";

/*
 * Reads what Anthropic's Messages API reports about a call.
 *
 * Anthropic counts cache reads and cache writes beside the input, not inside
 * it, so no count has to be taken out of another: each token already sits in
 * exactly one of them. Thinking tokens are already part of output_tokens.
 */

/** Turns Anthropic's usage block into our counts. */
export function usageFromAnthropic(block: unknown): Usage {
  const usage = isObject(block) ? block : {};
  const split = isObject(usage.cache_creation) ? usage.cache_creation : undefined;

  return {
    inputTokens: count(usage.input_tokens),
    cacheReadTokens: count(usage.cache_read_input_tokens),
    // When Anthropic splits cache writes by how long the cache is kept, the two
    // parts add up to cache_creation_input_tokens. The 1 hour part costs more.
    cacheWriteTokens: split ? count(split.ephemeral_5m_input_tokens) : count(usage.cache_creation_input_tokens),
    cacheWrite1hTokens: split ? count(split.ephemeral_1h_input_tokens) : undefined,
    outputTokens: count(usage.output_tokens),
  };
}

/** How a call ended, from Anthropic's stop reason. */
export function outcomeFromStopReason(stopReason: unknown): Outcome {
  switch (stopReason) {
    case "max_tokens":
    case "model_context_window_exceeded":
      return "max_tokens";
    case "refusal":
      return "refused";
    default:
      // end_turn, tool_use, stop_sequence, pause_turn, and any reason added later:
      // a stop reason means the model finished.
      return "completed";
  }
}

/** Reads a complete message. Undefined when it carries no usage block. */
export function readAnthropicMessage(message: unknown): ReadResult | undefined {
  if (!isObject(message) || !isObject(message.usage)) return undefined;

  return {
    usage: usageFromAnthropic(message.usage),
    raw: { ...message.usage },
    model: text(message.model),
    responseId: text(message.id),
    outcome: outcomeFromStopReason(message.stop_reason),
  };
}

export type AnthropicStreamReader = {
  /** Looks at one stream event. Never throws. */
  onEvent: (event: unknown) => void;
  /** The result once the stream is over. Undefined when no usage arrived. */
  finish: (failed: boolean) => ReadResult | undefined;
};

/**
 * Reads the events of one stream, in order.
 *
 * Usage arrives twice: the input side in message_start, and the output in
 * message_delta. Anthropic states running totals, and a later event may state
 * a count again, so each stated count replaces the earlier one. Adding them up
 * would count the input twice.
 */
export function createAnthropicStreamReader(): AnthropicStreamReader {
  // No prototype, so a key such as __proto__ from the wire is stored as plain data.
  const usage = Object.create(null) as Record<string, unknown>;
  let started = false;
  let model: string | undefined;
  let responseId: string | undefined;
  let stopReason: unknown;

  return {
    onEvent(event) {
      if (!isObject(event)) return;

      if (event.type === "message_start" && isObject(event.message)) {
        started = true;
        model = text(event.message.model) ?? model;
        responseId = text(event.message.id) ?? responseId;
        restate(usage, event.message.usage);
      } else if (event.type === "message_delta") {
        restate(usage, event.usage);
        if (isObject(event.delta) && event.delta.stop_reason != null) stopReason = event.delta.stop_reason;
      }
    },

    finish(failed) {
      if (!started) return undefined;

      let outcome: Outcome;
      if (stopReason !== undefined) outcome = outcomeFromStopReason(stopReason);
      else outcome = failed ? "error" : "cut_short";

      return { usage: usageFromAnthropic(usage), raw: { ...usage }, model, responseId, outcome };
    },
  };
}

/** Copies what an event states over the running block. A missing or null value keeps the earlier one. */
function restate(into: Record<string, unknown>, stated: unknown): void {
  if (!isObject(stated)) return;
  for (const [key, value] of Object.entries(stated)) {
    if (value !== undefined && value !== null) into[key] = value;
  }
}

/**
 * A count as the provider sent it. A missing one is undefined. Anything that is
 * not a number becomes NaN, so the event builder reports it instead of it being
 * dropped in silence.
 */
function count(value: unknown): number | undefined {
  if (value === undefined || value === null) return undefined;
  return typeof value === "number" ? value : Number.NaN;
}

function text(value: unknown): string | undefined {
  return typeof value === "string" && value !== "" ? value : undefined;
}

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
