import type { BeforeRequestContext, BeforeRequestHook } from "./types.js";

type Credential = string | (() => string | undefined | Promise<string | undefined>) | undefined;

// One environment per API call. Every attempt of a call gets the same context, so retries reuse it.
const environmentByCall = new WeakMap<BeforeRequestContext, string | undefined>();

async function resolveCredential(value: Credential): Promise<string | undefined> {
  const resolved = typeof value === "function" ? await value() : value;
  return resolved || undefined;
}

async function environmentFor(
  hookCtx: BeforeRequestContext,
  environmentId: Credential,
): Promise<string | undefined> {
  if (!environmentByCall.has(hookCtx)) {
    environmentByCall.set(hookCtx, await resolveCredential(environmentId));
  }
  return environmentByCall.get(hookCtx);
}

/** Sends bearerAuth and environmentId as headers. Headers passed on a call take precedence. */
export const authHeadersHook: BeforeRequestHook = {
  async beforeRequest(hookCtx, request) {
    const { apiKeyAuth, bearerAuth, environmentId } = hookCtx.options;
    if (apiKeyAuth && bearerAuth) {
      throw new Error("set either apiKeyAuth or bearerAuth, not both.");
    }

    if (!request.headers.has("Authorization")) {
      const token = await resolveCredential(bearerAuth);
      if (token) {
        request.headers.set("Authorization", `Bearer ${token}`);
      }
    }

    if (!request.headers.has("X-Environment-ID")) {
      const envId = await environmentFor(hookCtx, environmentId);
      if (envId) {
        request.headers.set("X-Environment-ID", envId);
      }
    }

    return request;
  },
};
