import { describe, expect, it } from "vitest";
import { createAgentMeter } from "../src/meter.js";
import type { SendEvents } from "../src/pipeline/sender.js";

const sendEvents: SendEvents = () => Promise.resolve();

describe("createAgentMeter", () => {
  it("refuses a bad config when it is created", () => {
    expect(() => createAgentMeter({})).toThrow(/apiKey/);
  });

  it("runs a function with labels and returns what it returns", () => {
    const meter = createAgentMeter({ sendEvents });

    expect(meter.withLabels({ customerId: "cust_42" }, () => 42)).toBe(42);
  });

  it("flushes and shuts down", async () => {
    const meter = createAgentMeter({ sendEvents });

    await expect(meter.flush()).resolves.toBeUndefined();
    await expect(meter.shutdown()).resolves.toBeUndefined();
  });
});

describe("the package", () => {
  it("exports only the public names", async () => {
    const index = await import("../src/index.js");

    expect(Object.keys(index).sort()).toEqual(["SDK_VERSION", "createAgentMeter"]);
  });
});
