import type { Core } from "../core.js";
import { attachToAnthropic, isAnthropicClient } from "./anthropic.js";

export type WrapOptions = {
  /** Labels for every call through this client. A withLabels() block wins over them. */
  customerId?: string;
  agentId?: string;
  tags?: Record<string, string>;
  /** Names the provider, when the client's URL does not show it. */
  provider?: string;
  /** Names the gateway the client goes through. */
  gateway?: string;
};

/**
 * Hooks the meter into a client and returns the same client.
 *
 * A client the SDK cannot meter is returned untouched, with a "wrap" problem
 * reported. Wrapping never throws into the app.
 */
export function wrapClient<T>(client: T, core: Core, enabled: boolean, options: WrapOptions = {}): T {
  if (!enabled) return client;

  try {
    if (isAnthropicClient(client)) {
      attachToAnthropic(client, core, {
        clientLabels: { customerId: options.customerId, agentId: options.agentId, tags: options.tags },
        provider: options.provider,
        gateway: options.gateway,
      });
    } else {
      core.report(new Error("This client is not one the SDK can meter yet, so its calls are not metered."), "wrap");
    }
  } catch (error) {
    core.report(error, "wrap");
  }

  return client;
}
