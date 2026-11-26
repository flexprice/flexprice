import { describe, expect, it } from "vitest";
import { providerFromBaseUrl } from "../../src/detect/hosts.js";

describe("providerFromBaseUrl", () => {
  it("knows Anthropic's own host", () => {
    expect(providerFromBaseUrl("https://api.anthropic.com")).toBe("anthropic");
    expect(providerFromBaseUrl("https://api.anthropic.com/")).toBe("anthropic");
  });

  it("does not match a look-alike or a non-URL", () => {
    expect(providerFromBaseUrl("https://api.anthropic.com.evil.net")).toBeUndefined();
    expect(providerFromBaseUrl("https://notanthropic.com")).toBeUndefined();
    expect(providerFromBaseUrl("not a url")).toBeUndefined();
    expect(providerFromBaseUrl(undefined)).toBeUndefined();
  });
});
