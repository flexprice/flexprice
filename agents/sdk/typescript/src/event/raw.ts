import {
  RAW_MAX_DEPTH,
  RAW_MAX_FIELDS,
  RAW_MAX_STRING_LENGTH,
  RAW_PREFIX,
} from "./schema.js";

/**
 * Turns a provider's usage block into flat `raw_` properties.
 *
 * The block comes from the provider, so its size and shape are not ours to
 * control. Only numbers, true or false, and short strings are kept, at most
 * RAW_MAX_DEPTH levels deep and at most RAW_MAX_FIELDS fields in total.
 * Never throws.
 */
export function flattenRaw(raw: Record<string, unknown> | undefined): Record<string, string> {
  const flat: Record<string, string> = {};
  if (!isPlainObject(raw)) return flat;

  let kept = 0;

  const walk = (node: Record<string, unknown>, path: string, depth: number): void => {
    for (const [key, value] of Object.entries(node)) {
      if (kept >= RAW_MAX_FIELDS) return;

      const name = `${path}${key}`;
      const text = asText(value);

      if (text !== undefined) {
        flat[`${RAW_PREFIX}${name}`] = text;
        kept++;
      } else if (depth < RAW_MAX_DEPTH && isPlainObject(value)) {
        walk(value, `${name}_`, depth + 1);
      }
    }
  };

  try {
    walk(raw, "", 1);
  } catch {
    // An object that fails while being read: keep what was collected so far.
  }

  return flat;
}

/** The text form of a value we keep, or undefined for one we drop. */
function asText(value: unknown): string | undefined {
  if (typeof value === "number") return Number.isFinite(value) ? String(value) : undefined;
  if (typeof value === "boolean") return value ? "true" : "false";
  if (typeof value === "string") return value.length <= RAW_MAX_STRING_LENGTH ? value : undefined;
  return undefined;
}

/** True for an object written with braces. False for null, arrays and everything else. */
function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
