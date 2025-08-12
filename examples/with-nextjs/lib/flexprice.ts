// Centralized Flexprice SDK client init and small Promise wrappers
import * as FlexPrice from "@flexprice/sdk";

export function initFlexpriceClient() {
    const apiKey = process.env.FLEXPRICE_API_KEY;
    const apiHost = process.env.FLEXPRICE_API_HOST || "api.cloud.flexprice.io";

    if (!apiKey) {
        throw new Error(
            "FLEXPRICE_API_KEY is required. Add it to your .env file."
        );
    }

    const defaultClient = FlexPrice.ApiClient.instance as any;
    defaultClient.basePath = `https://${apiHost}/v1`;

    const apiKeyAuth = defaultClient.authentications["ApiKeyAuth"];
    apiKeyAuth.apiKey = apiKey;
    apiKeyAuth.in = "header";
    apiKeyAuth.name = "x-api-key";

    return {
        eventsApi: new (FlexPrice as any).EventsApi(),
        customersApi: new (FlexPrice as any).CustomersApi(),
    } as const;
}

export function wrapCustomersPost(
    api: any,
    dtoCreateCustomerRequest: {
        externalId: string;
        email?: string;
        name?: string;
        metadata?: Record<string, any>;
    }
) {
    return new Promise((resolve, reject) => {
        api.customersPost(
            { dtoCreateCustomerRequest },
            (error: any, data: any, _response: any) => {
                if (error) return reject(error);
                resolve(data);
            }
        );
    });
}

export function wrapEventsPost(api: any, eventRequest: any) {
    return new Promise((resolve, reject) => {
        api.eventsPost(
            eventRequest,
            (error: any, data: any, _response: any) => {
                if (error) return reject(error);
                resolve(data);
            }
        );
    });
}

export function wrapEventsGet(
    api: any,
    params: { external_customer_id: string }
) {
    return new Promise((resolve, reject) => {
        api.eventsGet(params, (error: any, data: any, _response: any) => {
            if (error) return reject(error);
            resolve(data);
        });
    });
}
