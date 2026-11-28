import { describe, expect, it } from "vitest";
import {
  EVENT_NAME,
  EVENT_SOURCE,
  FIXED_PROPERTIES,
  SCHEMA_VERSION,
  isReservedProperty,
} from "../../src/event/schema.js";

// If this test fails because a name was added, renamed or removed:
// that is a change to the public contract. Update the list here AND
// change SCHEMA_VERSION, on purpose.
const VERSION_1_PROPERTIES = [
  // about the call
  "provider", "gateway", "api", "type", "model", "requested_model", "streamed", "outcome",
  // labels
  "agent_id",
  // counts filled in version 1
  "input_tokens", "cache_read_tokens", "cache_write_tokens", "cache_write_1h_tokens",
  "output_tokens", "cost_usd",
  // counts reserved for later phases
  "audio_input_tokens", "audio_output_tokens", "image_input_tokens", "image_output_tokens",
  "input_characters", "input_audio_seconds", "output_images",
  // stream fields
  "sequence", "is_final",
  // the marker
  "flexprice_agent_sdk", "sdk_language", "sdk_version", "schema_version",
];

describe("the event schema", () => {
  it("names the event and its source", () => {
    expect(EVENT_NAME).toBe("model.usage");
    expect(EVENT_SOURCE).toBe("flexprice-agent-sdk");
  });

  it("pins the property names of schema version 1", () => {
    expect(SCHEMA_VERSION).toBe("1");
    expect(FIXED_PROPERTIES).toHaveLength(28);
    expect([...FIXED_PROPERTIES].sort()).toEqual([...VERSION_1_PROPERTIES].sort());
  });

  it("treats fixed names and raw_ names as reserved", () => {
    expect(isReservedProperty("model")).toBe(true);
    expect(isReservedProperty("output_images")).toBe(true);
    expect(isReservedProperty("raw_anything")).toBe(true);
    expect(isReservedProperty("team")).toBe(false);
  });
});
