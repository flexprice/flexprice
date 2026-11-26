/** The hosts the SDK recognizes, and the provider each one belongs to. */
const PROVIDER_HOSTS: ReadonlyArray<readonly [host: string, provider: string]> = [
  ["api.anthropic.com", "anthropic"],
];

/**
 * The provider a client's base URL belongs to, when the SDK knows the host.
 *
 * It compares the exact host, or a subdomain of it, and never checks whether
 * the URL merely contains the name: api.anthropic.com.evil.net contains it and
 * is somebody else's server.
 */
export function providerFromBaseUrl(baseUrl: unknown): string | undefined {
  if (typeof baseUrl !== "string") return undefined;

  let host: string;
  try {
    host = new URL(baseUrl).hostname.toLowerCase();
  } catch {
    return undefined;
  }

  for (const [known, provider] of PROVIDER_HOSTS) {
    if (host === known || host.endsWith(`.${known}`)) return provider;
  }
  return undefined;
}
