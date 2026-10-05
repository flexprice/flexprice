import assert from "node:assert/strict";
import { spawn, spawnSync } from "node:child_process";
import { randomUUID } from "node:crypto";
import { once } from "node:events";
import http from "node:http";
import { dirname, join } from "node:path";
import { after, before, describe, test } from "node:test";
import { fileURLToPath } from "node:url";

import { UnauthorizedError } from "@modelcontextprotocol/sdk/client/auth.js";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StreamableHTTPClientTransport } from "@modelcontextprotocol/sdk/client/streamableHttp.js";

const serverBin = join(dirname(fileURLToPath(import.meta.url)), "..", "bin", "mcp-server.js");

const now = () => Math.floor(Date.now() / 1000);

// The server never checks the signature, the API does. The jti keeps every token distinct.
function jwt(claims) {
  const part = (value) => Buffer.from(JSON.stringify(value)).toString("base64url");
  return [part({ alg: "HS256", typ: "JWT" }), part({ jti: randomUUID(), ...claims }), "signature"].join(".");
}

const userToken = (claims = {}) => jwt({ sub: "user_1", exp: now() + 3600, ...claims });

const withHeaders = (headers) => ({ requestInit: { headers } });

async function listen(handler) {
  const server = http.createServer(handler);
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  return {
    origin: `http://127.0.0.1:${server.address().port}`,
    close: () => {
      server.closeAllConnections();
      return new Promise((resolve) => server.close(resolve));
    },
  };
}

async function readBody(req) {
  let body = "";
  for await (const chunk of req) body += chunk;
  return body;
}

const sandbox = { id: "env_sandbox", name: "Sandbox", type: "development" };
const refusedEnvironment = { error: "Access denied", message: "Invalid or inaccessible environment" };

// Knows one environment and refuses any other, the way the API does.
async function startApi() {
  const requests = [];
  const paths = [];
  const api = await listen((req, res) => {
    requests.push(req.headers);
    paths.push(`${req.method} ${req.url}`);
    const json = (status, body) => {
      res.writeHead(status, { "content-type": "application/json" });
      res.end(JSON.stringify(body));
    };
    if (req.url === `/v1/environments/${sandbox.id}`) return json(200, sandbox);
    if (req.url.startsWith("/v1/environments/")) return json(403, refusedEnvironment);
    return json(200, { id: "cust_1" });
  });
  return { ...api, url: `${api.origin}/v1`, requests, paths };
}

// Same URL layout as a Supabase project: the issuer has the /auth/v1 path.
async function startLoginServer(accessToken) {
  const authorizeRequests = [];
  const login = await listen(async (req, res) => {
    const url = new URL(req.url, login.origin);
    const json = (status, body) => {
      res.writeHead(status, { "content-type": "application/json" });
      res.end(JSON.stringify(body));
    };
    switch (url.pathname) {
      case "/.well-known/oauth-authorization-server/auth/v1":
        return json(200, {
          issuer: `${login.origin}/auth/v1`,
          authorization_endpoint: `${login.origin}/auth/v1/oauth/authorize`,
          token_endpoint: `${login.origin}/auth/v1/oauth/token`,
          registration_endpoint: `${login.origin}/auth/v1/oauth/clients/register`,
          response_types_supported: ["code"],
          grant_types_supported: ["authorization_code", "refresh_token"],
          code_challenge_methods_supported: ["S256"],
          token_endpoint_auth_methods_supported: ["none"],
        });
      case "/auth/v1/oauth/clients/register":
        return json(201, { ...JSON.parse(await readBody(req)), client_id: "client_1" });
      case "/auth/v1/oauth/authorize": {
        authorizeRequests.push(url.searchParams);
        const back = new URL(url.searchParams.get("redirect_uri"));
        back.searchParams.set("code", "code_1");
        back.searchParams.set("state", url.searchParams.get("state") ?? "");
        res.writeHead(302, { location: back.href });
        return res.end();
      }
      case "/auth/v1/oauth/token":
        return json(200, {
          access_token: accessToken,
          token_type: "bearer",
          expires_in: 3600,
          refresh_token: "refresh_1",
        });
      default:
        return json(404, {});
    }
  });
  return { ...login, issuer: `${login.origin}/auth/v1`, authorizeRequests };
}

async function freePort() {
  const probe = await listen(() => {});
  await probe.close();
  return Number(new URL(probe.origin).port);
}

function serverEnv(settings) {
  const env = { ...process.env, MCP_OAUTH_ISSUER: undefined, MCP_PUBLIC_URL: undefined, ...settings };
  for (const [name, value] of Object.entries(env)) {
    if (value === undefined) delete env[name];
  }
  return env;
}

async function startMcp({
  apiUrl,
  issuer,
  publicUrl = (origin) => `${origin}/mcp`,
  auth = ["--disable-static-auth"],
}) {
  const port = await freePort();
  const origin = `http://127.0.0.1:${port}`;
  const child = spawn(
    process.execPath,
    [serverBin, "serve", ...auth, "--port", String(port), "--server-url", apiUrl],
    {
      env: serverEnv(issuer ? { MCP_OAUTH_ISSUER: issuer, MCP_PUBLIC_URL: publicUrl(origin) } : {}),
      stdio: ["ignore", "ignore", "pipe"],
    },
  );
  let stderr = "";
  child.stderr.on("data", (chunk) => (stderr += chunk));
  const stop = async () => {
    if (child.exitCode !== null) return;
    child.kill();
    await once(child, "exit");
  };

  const deadline = Date.now() + 15_000;
  while (!(await fetch(origin).then((res) => res.ok, () => false))) {
    if (child.exitCode !== null || Date.now() > deadline) {
      await stop();
      throw new Error(`server did not start:\n${stderr}`);
    }
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  return { origin, stop };
}

function post(origin, body, headers = {}) {
  return fetch(`${origin}/mcp`, {
    method: "POST",
    headers: {
      "content-type": "application/json",
      accept: "application/json, text/event-stream",
      ...headers,
    },
    body,
  });
}

function initialize(origin, headers) {
  const request = {
    jsonrpc: "2.0",
    id: 1,
    method: "initialize",
    params: {
      protocolVersion: "2025-06-18",
      capabilities: {},
      clientInfo: { name: "browser-login-test", version: "0" },
    },
  };
  return post(origin, JSON.stringify(request), headers);
}

async function withClient(origin, transportOptions, use) {
  const client = new Client({ name: "browser-login-test", version: "0" });
  await client.connect(new StreamableHTTPClientTransport(new URL(`${origin}/mcp`), transportOptions));
  try {
    return await use(client);
  } finally {
    await client.close();
  }
}

const callTool = (origin, transportOptions) =>
  withClient(origin, transportOptions, (client) =>
    client.callTool({ name: "get-customer", arguments: { request: { id: "cust_1" } } }),
  );

const ENVIRONMENT_TOOL = "get-current-environment";

const askEnvironment = (origin, transportOptions) =>
  withClient(origin, transportOptions, (client) => client.callTool({ name: ENVIRONMENT_TOOL, arguments: {} }));

const toolNames = (origin, transportOptions) =>
  withClient(origin, transportOptions, async (client) => (await client.listTools()).tools.map((tool) => tool.name));

// Makes one tool call and returns the one request it caused at the API.
async function apiRequestFrom(api, origin, transportOptions) {
  const seen = api.requests.length;
  const result = await callTool(origin, transportOptions);
  assert.notEqual(result.isError, true, JSON.stringify(result.content));
  assert.equal(api.requests.length, seen + 1);
  return api.requests.at(-1);
}

describe("with login configured", () => {
  const issuedToken = userToken();
  let api, login, mcp;

  before(async () => {
    api = await startApi();
    login = await startLoginServer(issuedToken);
    mcp = await startMcp({ apiUrl: api.url, issuer: login.issuer });
  });

  after(async () => {
    await mcp?.stop();
    await login?.close();
    await api?.close();
  });

  for (const path of ["/.well-known/oauth-protected-resource/mcp", "/.well-known/oauth-protected-resource"]) {
    test(`${path} names this server and its login server`, async () => {
      const res = await fetch(`${mcp.origin}${path}`);
      assert.equal(res.status, 200);
      const metadata = await res.json();
      assert.equal(metadata.resource, `${mcp.origin}/mcp`);
      assert.deepEqual(metadata.authorization_servers, [login.issuer]);
    });
  }

  test("a public URL with a trailing slash still matches the URL clients use", async () => {
    const server = await startMcp({
      apiUrl: api.url,
      issuer: login.issuer,
      publicUrl: (origin) => `${origin}/mcp/`,
    });
    try {
      const res = await fetch(`${server.origin}/.well-known/oauth-protected-resource/mcp`);
      assert.equal((await res.json()).resource, `${server.origin}/mcp`);
    } finally {
      await server.stop();
    }
  });

  test("a request without credentials is told where to log in", async () => {
    const res = await initialize(mcp.origin);
    assert.equal(res.status, 401);
    assert.match(
      res.headers.get("www-authenticate") ?? "",
      new RegExp(`resource_metadata="${mcp.origin}/.well-known/oauth-protected-resource/mcp"`),
    );
  });

  test("a request without credentials is turned away before its body is read", async () => {
    const res = await post(mcp.origin, "{");
    assert.equal(res.status, 401);
  });

  const rejected = {
    "an expired token": () => userToken({ exp: now() - 60 }),
    "a token without an expiry": () => jwt({ sub: "user_1" }),
    "a token that is not a JWT": () => "not-a-jwt",
  };
  for (const [name, token] of Object.entries(rejected)) {
    test(`${name} is rejected so the client logs in again`, async () => {
      const res = await initialize(mcp.origin, { authorization: `Bearer ${token()}` });
      assert.equal(res.status, 401);
      assert.match(res.headers.get("www-authenticate") ?? "", /error="invalid_token"/);
    });
  }

  test("a browser may send the Authorization header", async () => {
    const res = await fetch(`${mcp.origin}/mcp`, {
      method: "OPTIONS",
      headers: {
        origin: "https://client.example",
        "access-control-request-method": "POST",
        "access-control-request-headers": "authorization",
      },
    });
    assert.match(res.headers.get("access-control-allow-headers") ?? "", /authorization/i);
  });

  test("a browser can read where to log in", async () => {
    const res = await initialize(mcp.origin);
    assert.match(res.headers.get("access-control-expose-headers") ?? "", /www-authenticate/i);
  });

  test("a logged-in call reaches the API with the user's token and environment", async () => {
    const token = userToken();
    const sent = await apiRequestFrom(
      api,
      mcp.origin,
      withHeaders({ authorization: `Bearer ${token}`, "x-environment-id": "env_1" }),
    );
    assert.equal(sent.authorization, `Bearer ${token}`);
    assert.equal(sent["x-environment-id"], "env_1");
    assert.equal(sent["x-api-key"], undefined);
  });

  test("a logged-in call without an environment says to connect again and choose one", async () => {
    const seen = api.requests.length;
    const result = await callTool(mcp.origin, withHeaders({ authorization: `Bearer ${userToken()}` }));
    assert.equal(result.isError, true);
    assert.match(JSON.stringify(result.content), /connect again/i);
    assert.equal(api.requests.length, seen);
  });

  // The authorization page saves the user's choice on their own record, keyed by
  // the app it was made for, and the login server copies that record into the token.
  const tokenWithChoices = (clientId, choices, claims = {}) =>
    userToken({ client_id: clientId, user_metadata: { mcp_environments: choices }, ...claims });

  test("a logged-in call uses the environment chosen for this app on the authorization page", async () => {
    const token = tokenWithChoices("client_1", { client_1: "env_chosen", client_2: "env_other_app" });
    const sent = await apiRequestFrom(api, mcp.origin, withHeaders({ authorization: `Bearer ${token}` }));
    assert.equal(sent["x-environment-id"], "env_chosen");
    assert.equal(sent.authorization, `Bearer ${token}`);
  });

  test("the chosen environment wins over a header", async () => {
    const token = tokenWithChoices("client_1", { client_1: "env_chosen" });
    const sent = await apiRequestFrom(
      api,
      mcp.origin,
      withHeaders({ authorization: `Bearer ${token}`, "x-environment-id": "env_from_header" }),
    );
    assert.equal(sent["x-environment-id"], "env_chosen");
  });

  test("an environment chosen for another app is not used", async () => {
    const seen = api.requests.length;
    const token = tokenWithChoices("client_2", { client_1: "env_chosen" });
    const result = await callTool(mcp.origin, withHeaders({ authorization: `Bearer ${token}` }));
    assert.equal(result.isError, true);
    assert.equal(api.requests.length, seen);
  });

  for (const [name, choice] of [
    ["a chosen environment with a line break", "env_1\r\nx-injected: 1"],
    ["a chosen environment with a space", "env 1"],
    ["a chosen environment that is not text", { id: "env_1" }],
    ["an empty chosen environment", ""],
  ]) {
    test(`${name} is ignored`, async () => {
      const seen = api.requests.length;
      const token = tokenWithChoices("client_1", { client_1: choice });
      const result = await callTool(mcp.origin, withHeaders({ authorization: `Bearer ${token}` }));
      assert.equal(result.isError, true);
      assert.equal(api.requests.length, seen);
    });
  }

  test("a token that names its environment needs no header", async () => {
    const token = userToken({ environment_id: "env_1" });
    const sent = await apiRequestFrom(api, mcp.origin, withHeaders({ authorization: `Bearer ${token}` }));
    assert.equal(sent.authorization, `Bearer ${token}`);
    assert.equal(sent["x-environment-id"], undefined);
  });

  // The environment is added to every call out of the app's sight, so the app
  // needs a tool to find out which one it is working in.
  describe("asking which environment the connection works in", () => {
    const chose = (environmentId) =>
      withHeaders({ authorization: `Bearer ${tokenWithChoices("client_1", { client_1: environmentId })}` });

    test("a logged-in app is offered the tool", async () => {
      const names = await toolNames(mcp.origin, chose(sandbox.id));
      assert.ok(names.includes(ENVIRONMENT_TOOL), names.join(", "));
    });

    test("answers with the id, name and type of the chosen environment", async () => {
      const result = await askEnvironment(mcp.origin, chose(sandbox.id));
      assert.notEqual(result.isError, true, JSON.stringify(result.content));
      assert.deepEqual(JSON.parse(result.content[0].text), sandbox);
    });

    test("looks the environment up at the API as the user", async () => {
      const token = tokenWithChoices("client_1", { client_1: sandbox.id });
      const seen = api.requests.length;
      await askEnvironment(mcp.origin, withHeaders({ authorization: `Bearer ${token}` }));
      assert.equal(api.requests.length, seen + 1);
      assert.equal(api.paths.at(-1), `GET /v1/environments/${sandbox.id}`);
      assert.equal(api.requests.at(-1).authorization, `Bearer ${token}`);
      assert.equal(api.requests.at(-1)["x-environment-id"], sandbox.id);
      assert.equal(api.requests.at(-1)["x-api-key"], undefined);
    });

    test("passes on the API's refusal of an environment that is gone", async () => {
      const result = await askEnvironment(mcp.origin, chose("env_deleted"));
      assert.equal(result.isError, true);
      assert.match(JSON.stringify(result.content), /Invalid or inaccessible environment/);
    });

    test("says to connect again when no environment was chosen", async () => {
      const seen = api.requests.length;
      const result = await askEnvironment(mcp.origin, withHeaders({ authorization: `Bearer ${userToken()}` }));
      assert.equal(result.isError, true);
      assert.match(JSON.stringify(result.content), /connect again/i);
      assert.equal(api.requests.length, seen);
    });

    test("an environment named by the token itself is looked up too", async () => {
      const token = userToken({ environment_id: sandbox.id });
      const result = await askEnvironment(mcp.origin, withHeaders({ authorization: `Bearer ${token}` }));
      assert.deepEqual(JSON.parse(result.content[0].text), sandbox);
      assert.equal(api.requests.at(-1)["x-environment-id"], undefined);
    });

    test("an environment from a header cannot point the lookup at another address", async () => {
      await askEnvironment(
        mcp.origin,
        withHeaders({ authorization: `Bearer ${userToken()}`, "x-environment-id": "env_1/../../users/me" }),
      );
      assert.equal(api.paths.at(-1), "GET /v1/environments/env_1%2F..%2F..%2Fusers%2Fme");
    });

    test("an API key connection is not offered the tool", async () => {
      // Only the API knows which environment a key belongs to.
      const names = await toolNames(mcp.origin, withHeaders({ "api-key-auth": "sk_test" }));
      assert.ok(names.includes("get-customer"));
      assert.ok(!names.includes(ENVIRONMENT_TOOL));
    });

    for (const [name, tools, offered] of [
      ["a server limited to other tools does not add the tool", ["get-customer"], false],
      ["a server limited to tools that include it adds the tool", ["get-customer", ENVIRONMENT_TOOL], true],
    ]) {
      test(name, async () => {
        const server = await startMcp({
          apiUrl: api.url,
          issuer: login.issuer,
          auth: ["--disable-static-auth", ...tools.flatMap((tool) => ["--tool", tool])],
        });
        try {
          const names = await toolNames(server.origin, chose(sandbox.id));
          assert.deepEqual(names.sort(), offered ? [...tools].sort() : ["get-customer"]);
        } finally {
          await server.stop();
        }
      });
    }
  });

  test("a logged-in call never borrows an API key the server was started with", async () => {
    const server = await startMcp({
      apiUrl: api.url,
      issuer: login.issuer,
      auth: ["--api-key-auth", "sk_static"],
    });
    try {
      const token = userToken();
      const sent = await apiRequestFrom(
        api,
        server.origin,
        withHeaders({ authorization: `Bearer ${token}`, "x-environment-id": "env_1" }),
      );
      assert.equal(sent["x-api-key"], undefined);
      assert.equal(sent.authorization, `Bearer ${token}`);
    } finally {
      await server.stop();
    }
  });

  test("an API key call skips login and reaches the API as an API key", async () => {
    const sent = await apiRequestFrom(api, mcp.origin, withHeaders({ "api-key-auth": "sk_test" }));
    assert.equal(sent["x-api-key"], "sk_test");
    assert.equal(sent.authorization, undefined);
  });

  test("an API key wins when a token is sent with it", async () => {
    const sent = await apiRequestFrom(
      api,
      mcp.origin,
      withHeaders({ "api-key-auth": "sk_test", authorization: `Bearer ${userToken()}` }),
    );
    assert.equal(sent["x-api-key"], "sk_test");
    assert.equal(sent.authorization, undefined);
  });

  test("an MCP client can log in through the login server and call a tool", async () => {
    const saved = {};
    const authProvider = {
      redirectUrl: "http://127.0.0.1:9/callback",
      clientMetadata: {
        client_name: "browser-login-test",
        redirect_uris: ["http://127.0.0.1:9/callback"],
        grant_types: ["authorization_code", "refresh_token"],
        response_types: ["code"],
        token_endpoint_auth_method: "none",
      },
      clientInformation: () => saved.client,
      saveClientInformation: (client) => void (saved.client = client),
      tokens: () => saved.tokens,
      saveTokens: (tokens) => void (saved.tokens = tokens),
      redirectToAuthorization: (url) => void (saved.authorizationUrl = url),
      saveCodeVerifier: (verifier) => void (saved.verifier = verifier),
      codeVerifier: () => saved.verifier,
    };
    const options = { authProvider, ...withHeaders({ "x-environment-id": "env_1" }) };

    const transport = new StreamableHTTPClientTransport(new URL(`${mcp.origin}/mcp`), options);
    await assert.rejects(
      new Client({ name: "browser-login-test", version: "0" }).connect(transport),
      UnauthorizedError,
    );

    const approved = await fetch(saved.authorizationUrl, { redirect: "manual" });
    const code = new URL(approved.headers.get("location")).searchParams.get("code");
    await transport.finishAuth(code);

    const sent = await apiRequestFrom(api, mcp.origin, options);

    assert.equal(login.authorizeRequests.at(-1).get("scope"), "email profile");
    assert.equal(sent.authorization, `Bearer ${issuedToken}`);
  });
});

describe("without login configured", () => {
  let api, mcp;

  before(async () => {
    api = await startApi();
    mcp = await startMcp({ apiUrl: api.url });
  });

  after(async () => {
    await mcp?.stop();
    await api?.close();
  });

  test("a request without credentials is served as before", async () => {
    const res = await initialize(mcp.origin);
    assert.equal(res.status, 200);
  });

  test("there is no login metadata", async () => {
    const res = await fetch(`${mcp.origin}/.well-known/oauth-protected-resource/mcp`);
    assert.equal(res.status, 404);
  });

  test("the environment tool is not offered", async () => {
    const names = await toolNames(mcp.origin, withHeaders({ authorization: `Bearer ${userToken()}` }));
    assert.ok(names.includes("get-customer"));
    assert.ok(!names.includes(ENVIRONMENT_TOOL));
  });

  test("an API key call reaches the API as an API key", async () => {
    const sent = await apiRequestFrom(api, mcp.origin, withHeaders({ "api-key-auth": "sk_test" }));
    assert.equal(sent["x-api-key"], "sk_test");
  });

  test("a bearer token is not passed on to the API", async () => {
    const sent = await apiRequestFrom(
      api,
      mcp.origin,
      withHeaders({ authorization: `Bearer ${userToken()}`, "x-environment-id": "env_1" }),
    );
    assert.equal(sent.authorization, undefined);
    assert.equal(sent["x-environment-id"], undefined);
  });

  test("an API key the server was started with is still used", async () => {
    const server = await startMcp({ apiUrl: api.url, auth: ["--api-key-auth", "sk_static"] });
    try {
      const sent = await apiRequestFrom(api, server.origin, withHeaders({}));
      assert.equal(sent["x-api-key"], "sk_static");
    } finally {
      await server.stop();
    }
  });
});

describe("the server refuses to start", () => {
  const issuer = "http://127.0.0.1:9/auth/v1";
  const publicUrl = "http://127.0.0.1:9/mcp";
  const cases = {
    "with only the login server set": [{ MCP_OAUTH_ISSUER: issuer }, /MCP_PUBLIC_URL is required/],
    "with only the public URL set": [{ MCP_PUBLIC_URL: publicUrl }, /MCP_OAUTH_ISSUER is required/],
    "when the login server is not a URL": [
      { MCP_OAUTH_ISSUER: "example.supabase.co/auth/v1", MCP_PUBLIC_URL: publicUrl },
      /MCP_OAUTH_ISSUER must be/,
    ],
    "when the public URL is not http": [
      { MCP_OAUTH_ISSUER: issuer, MCP_PUBLIC_URL: "localhost:2718/mcp" },
      /MCP_PUBLIC_URL must be/,
    ],
  };
  for (const [name, [settings, message]] of Object.entries(cases)) {
    test(name, () => {
      const run = spawnSync(
        process.execPath,
        [serverBin, "serve", "--disable-static-auth", "--port", "0", "--server-url", "http://127.0.0.1:9/v1"],
        { env: serverEnv(settings), encoding: "utf8", timeout: 10_000 },
      );
      assert.equal(run.status, 1);
      assert.match(run.stderr, message);
    });
  }
});
