import { describe, expect, it } from "vitest";
import { createSseDecoder } from "../../src/hook/sse.js";

const bytes = (text: string) => new TextEncoder().encode(text);

describe("createSseDecoder", () => {
  it("reads events, with their names", () => {
    const decoder = createSseDecoder();

    expect(decoder.push(bytes('event: message_start\ndata: {"a":1}\n\nevent: ping\ndata: {}\n\n'))).toEqual([
      { event: "message_start", data: '{"a":1}' },
      { event: "ping", data: "{}" },
    ]);
  });

  it("reads an event split across chunks, even inside a character", () => {
    const decoder = createSseDecoder();
    const whole = bytes('event: delta\ndata: {"text":"héllo"}\n\n');
    const cut = whole.indexOf(0xc3) + 1; // between the two bytes of é

    expect(decoder.push(whole.slice(0, 9))).toEqual([]);
    expect(decoder.push(whole.slice(9, cut))).toEqual([]);
    expect(decoder.push(whole.slice(cut))).toEqual([{ event: "delta", data: '{"text":"héllo"}' }]);
  });

  it("accepts CRLF and CR line endings, also when CRLF is split across chunks", () => {
    const decoder = createSseDecoder();

    expect(decoder.push(bytes("event: a\r\ndata: 1\r"))).toEqual([]);
    expect(decoder.push(bytes("\n\r\nevent: b\rdata: 2\r\r"))).toEqual([
      { event: "a", data: "1" },
      { event: "b", data: "2" },
    ]);
  });

  it("joins several data lines and ignores comments and other fields", () => {
    const decoder = createSseDecoder();

    expect(decoder.push(bytes(": keep-alive\nid: 7\nretry: 100\ndata: a\ndata:b\n\n"))).toEqual([
      { event: undefined, data: "a\nb" },
    ]);
  });

  it("dispatches nothing for an event without data", () => {
    const decoder = createSseDecoder();

    expect(decoder.push(bytes("event: empty\n\n"))).toEqual([]);
  });
});
