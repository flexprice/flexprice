import type { SDKOptions } from "../lib/config.js";
import { HTTPClient } from "../lib/http.js";
import { Flexprice } from "./sdk.js";

export type FlexPriceSecurity = {
  bearerAuth: string;
  environmentId?: string | undefined;
};

export type FlexPriceOptions = SDKOptions & {
  security?:
    | FlexPriceSecurity
    | (() => Promise<FlexPriceSecurity>)
    | undefined;
};

export class FlexPrice extends Flexprice {
  constructor(options: FlexPriceOptions = {}) {
    const { security, ...sdkOptions } = options;

    if (security) {
      const httpClient = sdkOptions.httpClient?.clone() ?? new HTTPClient();
      httpClient.addHook("beforeRequest", async (request) => {
        const credentials = typeof security === "function"
          ? await security()
          : security;
        const headers = new Headers(request.headers);

        headers.set("Authorization", `Bearer ${credentials.bearerAuth}`);
        if (credentials.environmentId) {
          headers.set("X-Environment-ID", credentials.environmentId);
        } else {
          headers.delete("X-Environment-ID");
        }

        return new Request(request, { headers });
      });
      sdkOptions.httpClient = httpClient;
    }

    super(sdkOptions);
  }
}
