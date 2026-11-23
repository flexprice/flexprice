/** Where in the SDK a problem happened. */
export type ErrorWhere = "wrap" | "labels" | "reader" | "event" | "overflow" | "send" | "auth";

/** The app's callback for metering problems. */
export type OnError = (error: Error, where: ErrorWhere) => void;

/** What the SDK's own code calls to report a problem. It never throws. */
export type Reporter = (problem: unknown, where: ErrorWhere) => void;

const LOG_PREFIX = "[flexprice-agent-sdk]";

/**
 * Makes the one function every part of the SDK reports problems through.
 *
 * With an `onError` callback, each problem goes to it. Without one, the first
 * problem of each kind is logged and later ones of that kind are not, so a
 * broken setup is visible without flooding the app's logs.
 *
 * Reporting a problem can never cause one: whatever the callback does, the
 * returned function does not throw.
 */
export function createReporter(
  onError?: OnError,
  log: (message: string) => void = console.warn,
): Reporter {
  const alreadyLogged = new Set<ErrorWhere>();

  return (problem, where) => {
    try {
      const error = toError(problem);

      if (onError) {
        // The callback may be async. A promise it returns must not fail unhandled,
        // because an unhandled failure ends a Node process.
        const result: unknown = onError(error, where);
        if (isPromiseLike(result)) result.then(undefined, () => undefined);
        return;
      }

      if (alreadyLogged.has(where)) return;
      alreadyLogged.add(where);
      log(`${LOG_PREFIX} ${where}: ${error.message}`);
    } catch {
      // The app's callback or logger failed. Metering must not take the app down.
    }
  };
}

/** True for a promise, or anything else that has a `then` method. */
function isPromiseLike(value: unknown): value is PromiseLike<unknown> {
  return (
    typeof value === "object" &&
    value !== null &&
    typeof (value as { then?: unknown }).then === "function"
  );
}

/** Whatever was reported, as an Error. */
function toError(problem: unknown): Error {
  if (problem instanceof Error) return problem;
  if (typeof problem === "string") return new Error(problem);
  return new Error(`a problem of type ${typeof problem} was reported`);
}
