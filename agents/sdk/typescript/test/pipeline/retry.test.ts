import { describe, expect, it } from "vitest";
import { backoffDelay, classifyFailure } from "../../src/pipeline/retry.js";
import { SendError } from "../../src/pipeline/sender.js";

const answered = (status: number) => new SendError(`FlexPrice answered ${status}`, { status });

describe("classifyFailure", () => {
  it("retries when there was no answer", () => {
    expect(classifyFailure(new SendError("no answer from FlexPrice")).kind).toBe("retry");
    expect(classifyFailure(new Error("socket hang up")).kind).toBe("retry");
    expect(classifyFailure("something odd").kind).toBe("retry");
  });

  it("retries a timeout, a rate limit and a server error", () => {
    for (const status of [408, 429, 500, 502, 503, 504]) {
      expect(classifyFailure(answered(status)).kind).toBe("retry");
    }
  });

  it("rejects a batch FlexPrice cannot accept", () => {
    for (const status of [400, 413, 422]) {
      expect(classifyFailure(answered(status)).kind).toBe("reject");
    }
  });

  it("holds on a refused key and on any other refusal", () => {
    for (const status of [401, 403, 404, 405]) {
      expect(classifyFailure(answered(status)).kind).toBe("hold");
    }
  });

  it("passes on the status and Retry-After", () => {
    expect(classifyFailure(new SendError("x", { status: 429, retryAfterMs: 7000 }))).toStrictEqual({
      kind: "retry",
      status: 429,
      retryAfterMs: 7000,
    });
    expect(classifyFailure(new SendError("no answer"))).toStrictEqual({
      kind: "retry",
      status: undefined,
      retryAfterMs: undefined,
    });
  });
});

describe("backoffDelay", () => {
  const noJitter = () => 0;

  it("starts at 1 second and doubles up to 60 seconds", () => {
    expect([1, 2, 3, 4, 5, 6, 7, 8, 50].map((n) => backoffDelay(n, undefined, noJitter))).toEqual([
      1000, 2000, 4000, 8000, 16000, 32000, 60000, 60000, 60000,
    ]);
  });

  it("adds up to a fifth more at random, without going past 60 seconds", () => {
    expect(backoffDelay(1, undefined, () => 0.5)).toBe(1100);
    expect(backoffDelay(6, undefined, () => 1)).toBe(38400);
    expect(backoffDelay(7, undefined, () => 1)).toBe(60000);
  });

  it("never waits less than Retry-After asks", () => {
    expect(backoffDelay(1, 7000, noJitter)).toBe(7000);
    expect(backoffDelay(3, 1000, noJitter)).toBe(4000);
  });
});
