import { InvalidTokenError } from "@modelcontextprotocol/sdk/server/auth/errors.js";
import { requireBearerAuth } from "@modelcontextprotocol/sdk/server/auth/middleware/bearerAuth.js";
import type { AuthInfo } from "@modelcontextprotocol/sdk/server/auth/types.js";
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import type { Express } from "express";
import { MCPServerFlags } from "./flags.js";
import { buildSDK, formatResult } from "./tools.js";

const API_KEY_HEADER = "api-key-auth";
const ENVIRONMENT_HEADER = "x-environment-id";
const CHOSEN_ENVIRONMENTS_KEY = "mcp_environments";
const ENVIRONMENT_ID = /^[A-Za-z0-9_-]{1,128}$/;
const ENVIRONMENT_TOOL = "get-current-environment";
const METADATA_PATH = "/.well-known/oauth-protected-resource";

let enabled = false;

function urlSetting(name: string): URL | undefined {
  const value = process.env[name];
  if (!value) {
    return undefined;
  }
  const url = URL.canParse(value) ? new URL(value) : undefined;
  if (!url || !/^https?:$/.test(url.protocol)) {
    throw new Error(`${name} must be an http or https URL, got ${value}`);
  }
  return url;
}

/**
 * Browser login for the serve command, on when MCP_OAUTH_ISSUER and
 * MCP_PUBLIC_URL are set. Requests that carry an API key are left alone.
 */
export function mountBrowserLogin(app: Express): void {
  const issuer = urlSetting("MCP_OAUTH_ISSUER");
  const resource = urlSetting("MCP_PUBLIC_URL");
  if (!issuer && !resource) {
    return;
  }
  if (!issuer) {
    throw new Error("MCP_OAUTH_ISSUER is required when MCP_PUBLIC_URL is set");
  }
  if (!resource) {
    throw new Error("MCP_PUBLIC_URL is required when MCP_OAUTH_ISSUER is set");
  }
  enabled = true;

  // Clients compare this with the URL they connect to, which has no trailing slash.
  resource.pathname = resource.pathname.replace(/\/+$/, "");
  const metadataUrl = new URL(
    METADATA_PATH + (resource.pathname === "/" ? "" : resource.pathname),
    resource,
  );

  app.get([METADATA_PATH, metadataUrl.pathname], (_req, res) => {
    res.json({
      resource: resource.href,
      authorization_servers: [issuer.href],
      scopes_supported: ["email", "profile"],
      bearer_methods_supported: ["header"],
    });
  });

  const requireBearer = requireBearerAuth({
    verifier: { verifyAccessToken },
    resourceMetadataUrl: metadataUrl.href,
  });
  app.use(
    "/mcp",
    (req, res, next) =>
      req.headers[API_KEY_HEADER] ? next() : requireBearer(req, res, next),
  );
}

// The token is only read, never verified. The API verifies the signature on
// every call, so this server does not need the signing secret.
function tokenClaims(token: string): Record<string, unknown> {
  let claims: unknown;
  try {
    claims = JSON.parse(
      Buffer.from(token.split(".")[1] ?? "", "base64url").toString(),
    );
  } catch {
    claims = undefined;
  }
  if (typeof claims !== "object" || claims === null) {
    throw new InvalidTokenError("Token is not a JWT");
  }
  return claims as Record<string, unknown>;
}

async function verifyAccessToken(token: string): Promise<AuthInfo> {
  const expiresAt = tokenClaims(token)["exp"];
  return {
    token,
    clientId: "",
    scopes: [],
    ...(typeof expiresAt === "number" ? { expiresAt } : {}),
  };
}

function bearerToken(headers: Headers): string | undefined {
  const [scheme, token] = (headers.get("authorization") ?? "").split(" ");
  return scheme?.toLowerCase() === "bearer" && token ? token : undefined;
}

// The environment the user picked on the authorization page for the app this
// token was issued to. The dashboard saves it on the user's own login record as
// user_metadata.mcp_environments[<client_id>], and the login server copies that
// record into the token. The user can write that record, so this is only a
// request: the API checks on every call that the environment is in the tenant.
function chosenEnvironment(claims: Record<string, unknown>): string | undefined {
  const clientId = claims["client_id"];
  const metadata = claims["user_metadata"];
  if (typeof clientId !== "string" || typeof metadata !== "object" || metadata === null) {
    return undefined;
  }
  const choices = (metadata as Record<string, unknown>)[CHOSEN_ENVIRONMENTS_KEY];
  if (typeof choices !== "object" || choices === null) {
    return undefined;
  }
  const chosen = (choices as Record<string, unknown>)[clientId];
  return typeof chosen === "string" && ENVIRONMENT_ID.test(chosen) ? chosen : undefined;
}

// What a logged-in request acts with: the user's token and the environment it
// works in. A token that names its own environment needs no environment sent.
function loginFor(headers: Headers) {
  const token = bearerToken(headers);
  if (!token) {
    throw new Error("Missing bearer token");
  }
  const claims = tokenClaims(token);
  const environmentId = chosenEnvironment(claims) ?? headers.get(ENVIRONMENT_HEADER);
  const tokenEnvironmentId = claims["environment_id"];
  if (!environmentId && !tokenEnvironmentId) {
    throw new Error(
      "No environment is selected for this connection. Remove this server and connect again to choose one.",
    );
  }
  return { token, environmentId, tokenEnvironmentId };
}

export function sdkForRequest(
  headers: Headers,
  cliFlags: MCPServerFlags,
  disableStaticAuth: boolean,
  logger: { level: string },
) {
  if (!enabled || headers.get(API_KEY_HEADER)) {
    return buildSDK(headers, cliFlags, disableStaticAuth, logger);
  }

  const { token, environmentId } = loginFor(headers);

  // A logged-in call must never fall back to an API key the server was started with.
  const sdk = buildSDK(headers, cliFlags, true, logger);
  const hooks = sdk._options.hooks;
  if (!hooks) {
    throw new Error("SDK hooks are unavailable");
  }
  hooks.registerBeforeRequestHook({
    beforeRequest: (_ctx, request) => {
      request.headers.set("Authorization", `Bearer ${token}`);
      if (environmentId) {
        request.headers.set("X-Environment-ID", environmentId);
      }
      return request;
    },
  });
  return sdk;
}

/**
 * Lets a logged-in app ask which environment it works in. The environment is
 * added to every API call out of the app's sight, so without this tool the app
 * cannot tell the user. An API key connection does not get it: only the API
 * knows which environment a key belongs to.
 */
export function registerEnvironmentTool(
  server: McpServer,
  headers: Headers,
  cliFlags: MCPServerFlags,
  logger: { level: string },
): void {
  if (!enabled || headers.get(API_KEY_HEADER)) {
    return;
  }
  if (cliFlags.tool && !cliFlags.tool.includes(ENVIRONMENT_TOOL)) {
    return;
  }

  server.registerTool(
    ENVIRONMENT_TOOL,
    {
      description:
        "Get the environment this connection works in: its id, name and type (for example production or development). "
        + "Every other tool reads and changes data in this environment only. "
        + "It was chosen when the user connected this server, and no tool can change it.",
      annotations: {
        title: "Get current environment",
        readOnlyHint: true,
        destructiveHint: false,
        idempotentHint: true,
        openWorldHint: false,
      },
    },
    async () => {
      const { token, environmentId, tokenEnvironmentId } = loginFor(headers);
      const id = encodeURIComponent(String(environmentId ?? tokenEnvironmentId));
      const url = new URL(buildSDK(headers, cliFlags, true, logger)._baseURL ?? "");
      url.pathname = `${url.pathname.replace(/\/+$/, "")}/environments/${id}`;

      const response = await fetch(url, {
        headers: {
          Authorization: `Bearer ${token}`,
          ...(environmentId ? { "X-Environment-ID": environmentId } : {}),
        },
      });
      return formatResult(response);
    },
  );
}
