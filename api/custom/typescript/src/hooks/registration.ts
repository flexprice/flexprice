import { authHeadersHook } from "./auth-headers.js";
import type { Hooks } from "./types.js";

/**
 * Registers FlexPrice's custom hooks. The generated SDK calls initHooks(this) from hooks/hooks.ts.
 */
export function initHooks(hooks: Hooks): void {
  hooks.registerBeforeRequestHook(authHeadersHook);
}
