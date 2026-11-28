import { describe, expect, it, vi } from "vitest";
import { type OnError, createReporter } from "../src/errors.js";

const wait = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

describe("createReporter", () => {
  it("passes the error and where it happened to onError", () => {
    const onError = vi.fn();
    const error = new Error("boom");

    createReporter(onError)(error, "send");

    expect(onError).toHaveBeenCalledTimes(1);
    expect(onError).toHaveBeenCalledWith(error, "send");
  });

  it("turns a problem that is not an Error into one", () => {
    const onError = vi.fn();

    createReporter(onError)("count input_tokens is not a positive whole number: NaN", "event");

    const [error, where] = onError.mock.calls[0] as [Error, string];
    expect(error).toBeInstanceOf(Error);
    expect(error.message).toBe("count input_tokens is not a positive whole number: NaN");
    expect(where).toBe("event");
  });

  it("never throws, even when onError throws", () => {
    const report = createReporter(() => {
      throw new Error("the handler is broken");
    });

    expect(() => report(new Error("boom"), "send")).not.toThrow();
  });

  it("does not leave a failing async onError unhandled", async () => {
    const unhandled: unknown[] = [];
    const listener = (reason: unknown) => unhandled.push(reason);
    process.on("unhandledRejection", listener);

    // An app can pass an async callback, although the type says it returns nothing.
    const asyncHandler = (() =>
      Promise.reject(new Error("the async handler is broken"))) as unknown as OnError;

    const report = createReporter(asyncHandler);
    report(new Error("boom"), "send");
    await wait(20);

    process.off("unhandledRejection", listener);
    expect(unhandled).toEqual([]);
  });

  it("without onError, logs the first problem of each kind and then stays quiet", () => {
    const log = vi.fn();
    const report = createReporter(undefined, log);

    report(new Error("first"), "send");
    report(new Error("second"), "send");
    report(new Error("bad key"), "auth");

    expect(log).toHaveBeenCalledTimes(2);
    expect(log.mock.calls[0]?.[0]).toMatch(/^\[flexprice-agent-sdk\].*send.*first/);
    expect(log.mock.calls[1]?.[0]).toMatch(/^\[flexprice-agent-sdk\].*auth.*bad key/);
  });
});
