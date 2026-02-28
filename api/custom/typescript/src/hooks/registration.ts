import type { Hooks } from "./types.js";

/**
 * No-op hook registration. The generated SDK calls initHooks(this) from hooks/hooks.ts.
 * Customize here only if you need to register debug or instrumentation hooks.
 */
export function initHooks(_hooks: Hooks): void {
  // No hooks registered.
}
