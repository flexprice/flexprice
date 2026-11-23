import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createEventQueue } from "../../src/pipeline/queue.js";
import { makeEvent, scriptedSend } from "../helpers/scripted-send.js";

const quiet = () => undefined;

describe("the queue, sending in batches", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("waits for the flush interval before sending a few events", async () => {
    const { send, batches } = scriptedSend();
    const queue = createEventQueue({ send, report: quiet, flushIntervalMs: 1000, flushAt: 100 });

    queue.push(makeEvent("a"));
    queue.push(makeEvent("b"));
    queue.push(makeEvent("c"));

    await vi.advanceTimersByTimeAsync(999);
    expect(batches).toEqual([]);

    await vi.advanceTimersByTimeAsync(1);
    expect(batches).toEqual([["a", "b", "c"]]);
    expect(queue.size()).toBe(0);
  });

  it("sends at once when flushAt events are waiting", async () => {
    const { send, batches } = scriptedSend();
    const queue = createEventQueue({ send, report: quiet, flushIntervalMs: 1000, flushAt: 3 });

    queue.push(makeEvent("a"));
    queue.push(makeEvent("b"));
    queue.push(makeEvent("c"));
    await vi.advanceTimersByTimeAsync(0);

    expect(batches).toEqual([["a", "b", "c"]]);
  });

  it("never sends more than maxBatchSize events in one request", async () => {
    const { send, batches } = scriptedSend();
    const queue = createEventQueue({
      send,
      report: quiet,
      flushIntervalMs: 1000,
      flushAt: 3,
      maxBatchSize: 2,
    });

    for (const id of ["a", "b", "c", "d", "e"]) queue.push(makeEvent(id));
    await vi.advanceTimersByTimeAsync(0);
    expect(batches).toEqual([
      ["a", "b"],
      ["c", "d"],
    ]);

    await vi.advanceTimersByTimeAsync(1000);
    expect(batches).toEqual([["a", "b"], ["c", "d"], ["e"]]);
  });

  it("never has more than one request in flight", async () => {
    const { send, batches } = scriptedSend([{ delayMs: 500 }, { delayMs: 500 }]);
    const queue = createEventQueue({ send, report: quiet, flushIntervalMs: 1000, flushAt: 1 });

    queue.push(makeEvent("a"));
    queue.push(makeEvent("b"));
    await vi.advanceTimersByTimeAsync(0);
    expect(batches).toEqual([["a"]]);

    await vi.advanceTimersByTimeAsync(500);
    expect(batches).toEqual([["a"], ["b"]]);
  });

  it("keeps events that arrive while a batch is being sent", async () => {
    const { send, batches } = scriptedSend([{ delayMs: 500 }]);
    const queue = createEventQueue({ send, report: quiet, flushIntervalMs: 1000, flushAt: 100 });

    queue.push(makeEvent("a"));
    await vi.advanceTimersByTimeAsync(1000);
    queue.push(makeEvent("b"));
    queue.push(makeEvent("c"));
    await vi.advanceTimersByTimeAsync(500);
    expect(batches).toEqual([["a"]]);

    await vi.advanceTimersByTimeAsync(1000);
    expect(batches).toEqual([["a"], ["b", "c"]]);
    expect(queue.size()).toBe(0);
  });
});

describe("the queue and the process", () => {
  it("does not keep the process alive while it waits", async () => {
    const timers = () =>
      process.getActiveResourcesInfo().filter((type) => type === "Timeout").length;
    const { send } = scriptedSend();
    const queue = createEventQueue({ send, report: quiet, flushIntervalMs: 60_000 });

    const before = timers();
    queue.push(makeEvent("a"));
    expect(timers()).toBe(before);

    await queue.flush();
  });
});
