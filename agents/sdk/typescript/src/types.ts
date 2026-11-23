/**
 * The shapes that every part of the SDK shares.
 * This file holds types only. It produces no code when built.
 */

/** Who a call is for. Read when the call starts. */
export type Labels = {
  /** Becomes `external_customer_id` on the event. */
  customerId: string;
  agentId?: string;
  /** Extra properties on the event. */
  tags?: Record<string, string>;
};

/** Our fixed counts. A field is absent when the provider did not report it. */
export type Usage = {
  inputTokens?: number;
  cacheReadTokens?: number;
  cacheWriteTokens?: number;
  cacheWrite1hTokens?: number;
  outputTokens?: number;
  /** What a gateway says the call cost, exactly as the gateway wrote it. */
  costUsd?: string;
};

/** The format of a provider's request and response. */
export type ApiShape = "openai.chat" | "openai.responses" | "anthropic.messages";

/** What we know about the call besides its counts. */
export type CallInfo = {
  provider: string;
  gateway?: string;
  api: ApiShape;
  type: "chat";
  /** The model that answered. */
  model?: string;
  /** The model the app asked for. */
  requestedModel?: string;
  /**
   * The provider's id for the response. Set only when the host is one the SDK
   * recognizes, because other hosts can repeat ids.
   */
  responseId?: string;
  streamed: boolean;
};

/** How a call ended. */
export type Outcome = "completed" | "max_tokens" | "refused" | "cut_short" | "error";

/** What a hook hands to the pipeline. */
export type Observation = {
  labels: Labels;
  info: CallInfo;
  /** Running totals so far, never a difference. */
  usage: Usage;
  /** The provider's own usage block. */
  raw?: Record<string, unknown>;
  /** 0 in the first version. */
  sequence: number;
  /** true in the first version. */
  final: boolean;
  outcome?: Outcome;
  /** Set once, when the call ended. */
  endedAt: Date;
};

/** One event, in the shape FlexPrice's API accepts. */
export type UsageEvent = {
  event_name: string;
  event_id: string;
  external_customer_id: string;
  timestamp: string;
  source: string;
  properties: Record<string, string>;
};
