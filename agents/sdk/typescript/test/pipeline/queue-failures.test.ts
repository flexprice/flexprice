import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createEventQueue } from "../../src/pipeline/queue.js";
import { makeEvent, scriptedSend } from "../helpers/scripted-send.js";

const noJitter = () => 0;

describe("the queue, when sending fails", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("sends the same events again after no answer, waiting 1, 2 and 4 seconds", async () => {
    const report = vi.fn();
    const { send, batches } = scriptedSend(["no-answer", "no-answer", "no-answer", "ok"]);
    const queue = createEventQueue({ send, report, flushIntervalMs: 1000, random: noJitter });

    queue.push(makeEvent("a"));
    await vi.advanceTimersByTimeAsync(1000);
    expect(batches).toHaveLength(1);

    await vi.advanceTimersByTimeAsync(999);
    expect(batches).toHaveLength(1);
    await vi.advanceTimersByTimeAsync(1);
    expect(batches).toHaveLength(2);

    await vi.advanceTimersByTimeAsync(1999);
    expect(batches).toHaveLength(2);
    await vi.advanceTimersByTimeAsync(1);
    expect(batches).toHaveLength(3);

    await vi.advanceTimersByTimeAsync(4000);
    expect(batches).toEqual([["a"], ["a"], ["a"], ["a"]]);
    expect(queue.size()).toBe(0);
    expect(report).toHaveBeenCalledTimes(3);
    expect(report).toHaveBeenCalledWith(expect.any(Error), "send");
  });

  it("waits at least as long as Retry-After asks", async () => {
    const { send, batches } = scriptedSend([{ status: 429, retryAfterMs: 7000 }, "ok"]);
    const queue = createEventQueue({ send, report: vi.fn(), flushIntervalMs: 1000, random: noJitter });

    queue.push(makeEvent("a"));
    await vi.advanceTimersByTimeAsync(1000);
    await vi.advanceTimersByTimeAsync(6999);
    expect(batches).toHaveLength(1);

    await vi.advanceTimersByTimeAsync(1);
    expect(batches).toHaveLength(2);
  });

  it("starts again from 1 second after a batch gets through", async () => {
    const { send, batches } = scriptedSend(["no-answer", "ok", "no-answer", "ok"]);
    const queue = createEventQueue({ send, report: vi.fn(), flushIntervalMs: 1000, random: noJitter });

    queue.push(makeEvent("a"));
    await vi.advanceTimersByTimeAsync(1000);
    await vi.advanceTimersByTimeAsync(1000);
    expect(batches).toEqual([["a"], ["a"]]);

    queue.push(makeEvent("b"));
    await vi.advanceTimersByTimeAsync(1000);
    await vi.advanceTimersByTimeAsync(999);
    expect(batches).toHaveLength(3);

    await vi.advanceTimersByTimeAsync(1);
    expect(batches).toEqual([["a"], ["a"], ["b"], ["b"]]);
  });

  it.each([401, 403])(
    "keeps the events when the API key is refused (%i), reports auth, and tries again a minute later",
    async (status) => {
      const report = vi.fn();
      const { send, batches } = scriptedSend([status, "ok"]);
      const queue = createEventQueue({ send, report, flushIntervalMs: 1000 });

      queue.push(makeEvent("a"));
      await vi.advanceTimersByTimeAsync(1000);
      expect(report).toHaveBeenCalledWith(expect.any(Error), "auth");
      expect(queue.size()).toBe(1);

      await vi.advanceTimersByTimeAsync(59_999);
      expect(batches).toHaveLength(1);

      await vi.advanceTimersByTimeAsync(1);
      expect(batches).toEqual([["a"], ["a"]]);
      expect(queue.size()).toBe(0);
    },
  );

  it("keeps the events after any other refusal, such as a wrong URL, and reports it as send", async () => {
    const report = vi.fn();
    const { send, batches } = scriptedSend([404, "ok"]);
    const queue = createEventQueue({ send, report, flushIntervalMs: 1000 });

    queue.push(makeEvent("a"));
    await vi.advanceTimersByTimeAsync(1000);
    expect(report).toHaveBeenCalledWith(expect.any(Error), "send");

    await vi.advanceTimersByTimeAsync(59_999);
    expect(batches).toHaveLength(1);

    await vi.advanceTimersByTimeAsync(1);
    expect(batches).toEqual([["a"], ["a"]]);
  });

  it("sends a rejected batch one event at a time and drops only the bad one", async () => {
    const report = vi.fn();
    const { send, batches } = scriptedSend([400, "ok", 400, "ok"]);
    const queue = createEventQueue({ send, report, flushIntervalMs: 1000, flushAt: 3 });

    queue.push(makeEvent("evt-good-1"));
    queue.push(makeEvent("evt-bad"));
    queue.push(makeEvent("evt-good-2"));
    await vi.advanceTimersByTimeAsync(0);

    expect(batches).toEqual([
      ["evt-good-1", "evt-bad", "evt-good-2"],
      ["evt-good-1"],
      ["evt-bad"],
      ["evt-good-2"],
    ]);
    expect(queue.size()).toBe(0);
    expect(report).toHaveBeenCalledTimes(1);
    const [error, where] = report.mock.calls[0] as [Error, string];
    expect(error.message).toContain("evt-bad");
    expect(where).toBe("send");
  });

  it("drops a single rejected event without sending it again", async () => {
    const report = vi.fn();
    const { send, batches } = scriptedSend([422]);
    const queue = createEventQueue({ send, report, flushIntervalMs: 1000 });

    queue.push(makeEvent("evt-bad"));
    await vi.advanceTimersByTimeAsync(1000);

    expect(batches).toEqual([["evt-bad"]]);
    expect(queue.size()).toBe(0);
    expect(report).toHaveBeenCalledTimes(1);
  });

  it("stops sending one at a time when FlexPrice stops answering, and keeps the rest", async () => {
    const { send, batches } = scriptedSend([400, "ok", "no-answer", "ok"]);
    const queue = createEventQueue({
      send,
      report: vi.fn(),
      flushIntervalMs: 1000,
      flushAt: 3,
      random: noJitter,
    });

    queue.push(makeEvent("e1"));
    queue.push(makeEvent("e2"));
    queue.push(makeEvent("e3"));
    await vi.advanceTimersByTimeAsync(0);
    expect(batches).toEqual([["e1", "e2", "e3"], ["e1"], ["e2"]]);
    expect(queue.size()).toBe(2);

    await vi.advanceTimersByTimeAsync(1000);
    expect(batches).toEqual([["e1", "e2", "e3"], ["e1"], ["e2"], ["e2", "e3"]]);
    expect(queue.size()).toBe(0);
  });

  it("treats a send function that throws as no answer", async () => {
    const report = vi.fn();
    const { send, batches } = scriptedSend(["throw", "ok"]);
    const queue = createEventQueue({ send, report, flushIntervalMs: 1000, random: noJitter });

    queue.push(makeEvent("a"));
    await vi.advanceTimersByTimeAsync(1000);
    expect(report).toHaveBeenCalledWith(expect.any(Error), "send");

    await vi.advanceTimersByTimeAsync(1000);
    expect(batches).toEqual([["a"], ["a"]]);
    expect(queue.size()).toBe(0);
  });

  it("drops new events when the queue is full, and reports it once", () => {
    const report = vi.fn();
    const { send } = scriptedSend();
    const queue = createEventQueue({ send, report, flushIntervalMs: 1000, maxQueueSize: 3 });

    for (const id of ["a", "b", "c", "d", "e"]) queue.push(makeEvent(id));

    expect(queue.size()).toBe(3);
    expect(report).toHaveBeenCalledTimes(1);
    expect(report).toHaveBeenCalledWith(expect.any(Error), "overflow");
  });

  it("reports how many were dropped once the queue has room again", async () => {
    const report = vi.fn();
    const { send, batches } = scriptedSend();
    const queue = createEventQueue({ send, report, flushIntervalMs: 1000, maxQueueSize: 3 });

    for (const id of ["a", "b", "c", "d", "e"]) queue.push(makeEvent(id));
    await vi.advanceTimersByTimeAsync(1000);
    expect(batches).toEqual([["a", "b", "c"]]);

    queue.push(makeEvent("f"));

    expect(report).toHaveBeenCalledTimes(2);
    const [error, where] = report.mock.calls[1] as [Error, string];
    expect(error.message).toContain("2 events");
    expect(where).toBe("overflow");
    expect(queue.size()).toBe(1);
  });
});
