import { describe, expect, it } from "vitest";
import { resolveLabels, withLabels } from "../src/labels.js";
import type { Labels } from "../src/types.js";

const wait = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

describe("labels", () => {
  it("resolves to nothing when no customer is set anywhere", () => {
    expect(resolveLabels()).toBeUndefined();
    expect(resolveLabels({ agentId: "a" }, { tags: { team: "x" } })).toBeUndefined();
    expect(resolveLabels({ customerId: "" })).toBeUndefined();
  });

  it("turns a numeric customer or agent id into text", () => {
    const numeric = { customerId: 42, agentId: 7 } as unknown as Partial<Labels>;
    const broken = { customerId: { id: 1 } } as unknown as Partial<Labels>;

    expect(resolveLabels(numeric)).toStrictEqual({ customerId: "42", agentId: "7" });
    expect(resolveLabels(broken)).toBeUndefined();
  });

  it("uses the defaults when nothing else is set", () => {
    expect(resolveLabels(undefined, { customerId: "cust_default" })).toStrictEqual({
      customerId: "cust_default",
    });
  });

  it("lets the client's labels win over the defaults, key by key", () => {
    expect(
      resolveLabels(
        { customerId: "cust_client", tags: { team: "support" } },
        { customerId: "cust_default", agentId: "default-agent", tags: { team: "x", region: "eu" } },
      ),
    ).toStrictEqual({
      customerId: "cust_client",
      agentId: "default-agent",
      tags: { team: "support", region: "eu" },
    });
  });

  it("lets a block win over the client's labels", () => {
    const labels = withLabels({ customerId: "cust_block" }, () =>
      resolveLabels({ customerId: "cust_client", agentId: "client-agent" }),
    );

    expect(labels).toStrictEqual({ customerId: "cust_block", agentId: "client-agent" });
  });

  it("lets an inner block win over an outer one, and restores the outer one afterwards", () => {
    withLabels({ customerId: "outer", agentId: "outer-agent" }, () => {
      withLabels({ customerId: "inner" }, () => {
        expect(resolveLabels()).toStrictEqual({ customerId: "inner", agentId: "outer-agent" });
      });
      expect(resolveLabels()).toStrictEqual({ customerId: "outer", agentId: "outer-agent" });
    });

    expect(resolveLabels()).toBeUndefined();
  });

  it("keeps the labels across an await", async () => {
    const labels = await withLabels({ customerId: "cust_42" }, async () => {
      await wait(5);
      return resolveLabels();
    });

    expect(labels).toStrictEqual({ customerId: "cust_42" });
  });

  it("keeps two blocks that run at the same time apart", async () => {
    const run = (customerId: string, ms: number) =>
      withLabels({ customerId }, async () => {
        await wait(ms);
        return resolveLabels()?.customerId;
      });

    const [slow, fast] = await Promise.all([run("cust_slow", 15), run("cust_fast", 5)]);

    expect(slow).toBe("cust_slow");
    expect(fast).toBe("cust_fast");
  });

  it("returns what the function returns, and lets its error through", () => {
    expect(withLabels({ customerId: "c" }, () => 42)).toBe(42);
    expect(() =>
      withLabels({ customerId: "c" }, () => {
        throw new Error("app error");
      }),
    ).toThrow("app error");
  });
});
