import { type IncomingHttpHeaders, createServer } from "node:http";
import type { AddressInfo } from "node:net";

export type ReceivedRequest = {
  method: string;
  path: string;
  headers: IncomingHttpHeaders;
  body: unknown;
};

export type FakeFlexprice = {
  /** For example http://127.0.0.1:53124/v1 */
  baseUrl: string;
  /** Every request received so far, in order. */
  requests: ReceivedRequest[];
  /** Queues the answer for the next request. With nothing queued, the answer is 202. */
  respondWith(status: number, headers?: Record<string, string>): void;
  /** Makes the next request wait this long before it is answered. */
  delayNext(ms: number): void;
  close(): Promise<void>;
};

type Answer = { status: number; headers: Record<string, string> };

/**
 * A real HTTP server on this machine that behaves like FlexPrice's event API.
 * Tests send to it over the network, so the sender is exercised for real.
 */
export async function startFakeFlexprice(): Promise<FakeFlexprice> {
  const requests: ReceivedRequest[] = [];
  const answers: Answer[] = [];
  const timers = new Set<NodeJS.Timeout>();
  let delayMs = 0;
  let closed = false;

  const server = createServer((request, response) => {
    const chunks: Buffer[] = [];
    request.on("data", (chunk: Buffer) => chunks.push(chunk));
    request.on("end", () => {
      requests.push({
        method: request.method ?? "",
        path: request.url ?? "",
        headers: request.headers,
        body: parseJson(Buffer.concat(chunks).toString("utf8")),
      });

      const answer = answers.shift() ?? { status: 202, headers: {} };
      const wait = delayMs;
      delayMs = 0;

      const timer = setTimeout(() => {
        timers.delete(timer);
        if (response.destroyed) return;
        response.writeHead(answer.status, { "content-type": "application/json", ...answer.headers });
        response.end(
          JSON.stringify({
            message: answer.status === 202 ? "Events accepted for processing" : "error",
          }),
        );
      }, wait);
      timers.add(timer);
    });
  });

  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const { port } = server.address() as AddressInfo;

  return {
    baseUrl: `http://127.0.0.1:${port}/v1`,
    requests,
    respondWith(status, headers = {}) {
      answers.push({ status, headers });
    },
    delayNext(ms) {
      delayMs = ms;
    },
    close() {
      return new Promise<void>((resolve) => {
        if (closed) {
          resolve();
          return;
        }
        closed = true;
        for (const timer of timers) clearTimeout(timer);
        server.closeAllConnections();
        server.close(() => resolve());
      });
    },
  };
}

function parseJson(text: string): unknown {
  try {
    return JSON.parse(text);
  } catch {
    return text;
  }
}
