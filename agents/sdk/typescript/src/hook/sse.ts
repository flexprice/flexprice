/** One event from a server-sent event stream. */
export type SseEvent = {
  /** The event's name, from its `event:` line. */
  event: string | undefined;
  /** Its `data:` lines, joined by newlines. */
  data: string;
};

export type SseDecoder = {
  /** Takes the next chunk of bytes, and returns the events it completed. */
  push: (chunk: Uint8Array) => SseEvent[];
};

/**
 * Decodes the text/event-stream format, one chunk at a time.
 *
 * Chunks can end anywhere: in the middle of a line, or even in the middle of a
 * character, so unfinished text is kept for the next chunk. Lines may end with
 * CRLF, LF or CR, and a CRLF may be split across two chunks. An empty line ends
 * an event. Comments (lines starting with a colon) and fields other than
 * `event` and `data` are ignored.
 */
export function createSseDecoder(): SseDecoder {
  const decoder = new TextDecoder();
  let pending = "";
  let event: string | undefined;
  let data: string[] = [];
  /** The last chunk ended with a CR, so a LF at the start of the next one belongs to it. */
  let skipLeadingLf = false;

  function takeLine(line: string, completed: SseEvent[]): void {
    if (line === "") {
      if (data.length > 0) completed.push({ event, data: data.join("\n") });
      event = undefined;
      data = [];
      return;
    }
    if (line.startsWith(":")) return;

    const colon = line.indexOf(":");
    const field = colon === -1 ? line : line.slice(0, colon);
    let value = colon === -1 ? "" : line.slice(colon + 1);
    if (value.startsWith(" ")) value = value.slice(1);

    if (field === "event") event = value;
    else if (field === "data") data.push(value);
  }

  return {
    push(chunk) {
      let text = decoder.decode(chunk, { stream: true });
      if (skipLeadingLf && text !== "") {
        if (text.startsWith("\n")) text = text.slice(1);
        skipLeadingLf = false;
      }
      pending += text;
      const completed: SseEvent[] = [];

      for (;;) {
        const end = /\r\n|\r|\n/.exec(pending);
        if (!end) break;

        takeLine(pending.slice(0, end.index), completed);
        pending = pending.slice(end.index + end[0].length);
        // A CR that ends the chunk ends its line now. If the next chunk starts
        // with a LF, the two were one CRLF, and that LF is skipped.
        if (end[0] === "\r" && pending === "") skipLeadingLf = true;
      }

      return completed;
    },
  };
}
