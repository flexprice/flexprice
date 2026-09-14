import assert from "node:assert/strict";
import { createServer } from "node:http";

import { FlexPrice, Flexprice } from "@flexprice/sdk";

async function main(): Promise<void> {
  const requests: Array<{
    apiKey: string | undefined;
    authorization: string | undefined;
    environmentId: string | undefined;
  }> = [];
  const server = createServer((request, response) => {
    requests.push({
      apiKey: request.headers["x-api-key"] as string | undefined,
      authorization: request.headers.authorization,
      environmentId: request.headers["x-environment-id"] as string | undefined,
    });
    response.writeHead(401, { "content-type": "application/json" });
    response.end('{"error":"test response"}');
  });

  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));

  try {
    const address = server.address();
    assert(address && typeof address === "object");
    const serverURL = `http://127.0.0.1:${address.port}`;

    const apiKeyClient = new Flexprice({
      serverURL,
      apiKeyAuth: "existing-api-key",
    });
    await apiKeyClient.users.getUserInfo().catch(() => undefined);

    let token = "first-token";
    let environmentId: string | undefined = "env-one";
    const client = new FlexPrice({
      serverURL,
      security: async () => ({ bearerAuth: token, environmentId }),
    });

    await client.users.getUserInfo().catch(() => undefined);

    token = "second-token";
    environmentId = undefined;
    await client.users.getUserInfo().catch(() => undefined);

    assert.deepEqual(requests, [
      {
        apiKey: "existing-api-key",
        authorization: undefined,
        environmentId: undefined,
      },
      {
        apiKey: undefined,
        authorization: "Bearer first-token",
        environmentId: "env-one",
      },
      {
        apiKey: undefined,
        authorization: "Bearer second-token",
        environmentId: undefined,
      },
    ]);
  } finally {
    await new Promise<void>((resolve, reject) => {
      server.close((error) => error ? reject(error) : resolve());
    });
  }
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
