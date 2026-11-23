import { type AgentMeterConfig, resolveConfig } from "./config.js";
import { createCore } from "./core.js";
import { withLabels } from "./labels.js";
import type { Labels } from "./types.js";

/**
 * What an app holds after creating a meter.
 *
 * Written as properties rather than methods: none of them uses `this`, so an
 * app can safely take one out, as in `const { flush } = meter`.
 */
export type AgentMeter = {
  /** Runs fn. Every call that starts inside it is billed to these labels. */
  withLabels: <T>(labels: Partial<Labels>, fn: () => T) => T;
  /** Sends everything now. For serverless functions, before they return. */
  flush: (timeoutMs?: number) => Promise<void>;
  /** Sends what is waiting, then stops metering. */
  shutdown: (timeoutMs?: number) => Promise<void>;
};

/**
 * Creates a meter. One per app is enough.
 *
 * Throws a TypeError for a bad option, such as a missing API key, so a setup
 * mistake is found when the app starts. Nothing it returns throws afterwards.
 */
export function createAgentMeter(config: AgentMeterConfig): AgentMeter {
  const core = createCore(resolveConfig(config));

  return {
    withLabels,
    flush: (timeoutMs) => core.flush(timeoutMs),
    shutdown: (timeoutMs) => core.shutdown(timeoutMs),
  };
}
