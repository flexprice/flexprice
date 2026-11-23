import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createEventQueue } from "../../src/pipeline/queue.js";
import { createHttpSender } from "../../src/pipeline/sender.js";
import { startFakeFlexprice } from "../helpers/fake-flexprice.js";
import { makeEvent, scriptedSend } from "../helpers/scripted-send.js";

const quiet = () => undefined;

describe("flush", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("sends what is waiting right away", async () => {
    const { send, batches } = scriptedSend();
    const queue = createEventQueue({ send, report: quiet, flushIntervalMs: 1000 });

    queue.push(makeEvent("a"));
    queue.push(makeEvent("b"));
    const flushed = queue.flush();
    await vi.advanceTimersByTimeAsync(0);
    await flushed;

    expect(batches).toEqual([["a", "b"]]);
    expect(queue.size()).toBe(0);
  });

  it("sends everything, in batches no larger than maxBatchSize", async () => {
    const { send, batches } = scriptedSend();
    const queue = createEventQueue({ send, report: quiet, flushIntervalMs: 1000, maxBatchSize: 2 });

    for (const id of ["a", "b", "c", "d", "e"]) queue.push(makeEvent(id));
    const flushed = queue.flush();
    await vi.advanceTimersByTimeAsync(0);
    await flushed;

    expect(batches).toEqual([["a", "b"], ["c", "d"], ["e"]]);
  });

  it("resolves at once when there is nothing to send", async () => {
    const { send, batches } = scriptedSend();
    const queue = createEventQueue({ send, report: quiet });

    await queue.flush();

    expect(batches).toEqual([]);
  });

  it("waits for a batch that is already being sent", async () => {
    const { send } = scriptedSend([{ delayMs: 500 }]);
    const queue = createEventQueue({ send, report: quiet, flushIntervalMs: 1000 });

    queue.push(makeEvent("a"));
    await vi.advanceTimersByTimeAsync(1000);

    let done = false;
    void queue.flush().then(() => {
      done = true;
    });
    await vi.advanceTimersByTimeAsync(499);
    expect(done).toBe(false);

    await vi.advanceTimersByTimeAsync(1);
    expect(done).toBe(true);
    expect(queue.size()).toBe(0);
  });

  it("gives up when its timeout passes, and keeps the events", async () => {
    const { send } = scriptedSend(Array.from({ length: 10 }, () => "no-answer" as const));
    const queue = createEventQueue({ send, report: quiet, flushIntervalMs: 1000, random: () => 0 });

    queue.push(makeEvent("a"));
    let done = false;
    void queue.flush(5000).then(() => {
      done = true;
    });

    await vi.advanceTimersByTimeAsync(4999);
    expect(done).toBe(false);

    await vi.advanceTimersByTimeAsync(1);
    expect(done).toBe(true);
    expect(queue.size()).toBe(1);
  });
});

describe("flush over HTTP", () => {
  it("delivers events to a FlexPrice server", async () => {
    const server = await startFakeFlexprice();
    try {
      const send = createHttpSender({ apiKey: "sk_test_123", baseUrl: server.baseUrl, timeoutMs: 1000 });
      const queue = createEventQueue({ send, report: quiet });

      queue.push(makeEvent("a"));
      queue.push(makeEvent("b"));
      await queue.flush();

      expect(server.requests).toHaveLength(1);
      expect(server.requests[0]?.body).toStrictEqual({ events: [makeEvent("a"), makeEvent("b")] });
    } finally {
      await server.close();
    }
  });
});
