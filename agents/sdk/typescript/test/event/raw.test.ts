import { describe, expect, it } from "vitest";
import { flattenRaw } from "../../src/event/raw.js";

describe("flattenRaw", () => {
  it("flattens a usage block with one nested level", () => {
    expect(
      flattenRaw({
        prompt_tokens: 51200,
        completion_tokens: 300,
        total_tokens: 51500,
        prompt_tokens_details: { cached_tokens: 50000 },
      }),
    ).toEqual({
      raw_prompt_tokens: "51200",
      raw_completion_tokens: "300",
      raw_total_tokens: "51500",
      raw_prompt_tokens_details_cached_tokens: "50000",
    });
  });

  it("keeps true, false and short strings", () => {
    expect(flattenRaw({ service_tier: "default", cached: true, flagged: false })).toEqual({
      raw_service_tier: "default",
      raw_cached: "true",
      raw_flagged: "false",
    });
  });

  it("keeps a string of exactly 64 characters", () => {
    expect(flattenRaw({ id: "x".repeat(64) })).toEqual({ raw_id: "x".repeat(64) });
  });

  it("drops what cannot be kept", () => {
    expect(
      flattenRaw({
        a: null,
        b: undefined,
        c: [1, 2],
        d: Number.NaN,
        e: Number.POSITIVE_INFINITY,
        f: "x".repeat(65),
        g: { h: { i: 1 } },
        ok: 1,
      }),
    ).toEqual({ raw_ok: "1" });
  });

  it("keeps at most 32 fields, the first ones", () => {
    const big: Record<string, number> = {};
    for (let i = 0; i < 40; i++) big[`k${i}`] = i;

    const flat = flattenRaw(big);

    expect(Object.keys(flat)).toHaveLength(32);
    expect(flat.raw_k0).toBe("0");
    expect(flat.raw_k31).toBe("31");
    expect(flat.raw_k32).toBeUndefined();
  });

  it("returns an empty object when there is nothing to flatten", () => {
    expect(flattenRaw(undefined)).toEqual({});
    expect(flattenRaw([1, 2] as unknown as Record<string, unknown>)).toEqual({});
  });
});
