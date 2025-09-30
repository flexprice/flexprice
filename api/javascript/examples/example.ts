// FlexPrice TypeScript SDK Example
// This example demonstrates how to use the FlexPrice TypeScript SDK
// to interact with the FlexPrice API with full type safety.

import * as FlexPrice from '@flexprice/sdk';
import * as dotenv from 'dotenv';

// Load environment variables
dotenv.config();

/**
 * Main example function demonstrating FlexPrice TypeScript SDK usage
 */
async function runExample(): Promise<void> {
    console.log('Starting FlexPrice TypeScript SDK example...');

    try {
        // Configure the API client with your API key
        // API keys should start with 'sk_' followed by a unique identifier
        const apiKey = process.env.FLEXPRICE_API_KEY || 'YOUR_API_KEY';
        const apiHost = process.env.FLEXPRICE_API_HOST || 'api.cloud.flexprice.io';

        if (apiKey === 'YOUR_API_KEY') {
            console.error('ERROR: You must provide a valid FlexPrice API key. Set the FLEXPRICE_API_KEY environment variable.');
            console.error('Example: FLEXPRICE_API_KEY=sk_your_api_key_here npm start');
            process.exit(1);
        }

        if (!apiKey.startsWith('sk_')) {
            console.warn('WARNING: API key format may be incorrect. FlexPrice API keys typically start with "sk_"');
        }

        console.log(`Using API key: ${apiKey.substring(0, 5)}${'*'.repeat(apiKey.length - 5)}`);

        // Initialize the API client with your API key
        const defaultClient = FlexPrice.ApiClient.instance;

        // Set the base path directly to the API endpoint including /v1
        defaultClient.basePath = `https://${apiHost}/v1`;

        // Configure API key authorization
        const apiKeyAuth = defaultClient.authentications['ApiKeyAuth'];
        apiKeyAuth.apiKey = apiKey;
        apiKeyAuth.in = 'header';
        apiKeyAuth.name = 'x-api-key';

        // Generate a unique customer ID for this example
        const customerId = `sample-customer-${Date.now()}`;

        // Create API instances for different endpoints
        const eventsApi = new FlexPrice.EventsApi();
        const customersApi = new FlexPrice.CustomersApi();

        // Step 1: Create a customer
        console.log(`Creating customer with ID: ${customerId}...`);

        const customerRequest: FlexPrice.DtoCreateCustomerRequest = {
            externalId: customerId,
            email: `example-${customerId}@example.com`,
            name: 'Example Customer',
            metadata: {
                source: 'typescript-sdk-example',
                createdAt: new Date().toISOString()
            }
        };

        try {
            const customerResult = await customersApi.customersPost({
                customer: customerRequest
            });

            console.log('Customer created successfully!');
            console.log(`Customer ID: ${customerResult.id}`);
        } catch (error) {
            console.error('Error creating customer:', error);
            return;
        }

        // Step 2: Create an event
        console.log('Creating event...');

        // Type-safe event request with proper TypeScript types
        const eventRequest: FlexPrice.DtoIngestEventRequest = {
            eventName: 'Sample Event',
            externalCustomerId: customerId,
            properties: {
                source: 'typescript_sample_app',
                environment: 'test',
                timestamp: new Date().toISOString()
            },
            source: 'typescript_sample_app',
            timestamp: new Date().toISOString()
        };

        try {
            const eventResult = await eventsApi.eventsPost({ event: eventRequest });
            console.log(`Event created successfully! ID: ${eventResult.eventId}`);

            // Step 3: Retrieve events for this customer
            console.log(`Retrieving events for customer ${customerId}...`);

            const events = await eventsApi.eventsQueryPost({
                request: {
                    externalCustomerId: customerId
                }
            });

            console.log(`Found ${events.events.length} events:`);

            events.events.forEach((event: any, index: number) => {
                console.log(`Event ${index + 1}: ${event.id} - ${event.eventName}`);
                console.log(`Properties: ${JSON.stringify(event.properties)}`);
            });

            console.log('TypeScript example completed successfully!');
        } catch (error) {
            handleApiError('creating event', error);
        }

    } catch (error) {
        console.error('Error running example:');
        console.error('Error message:', (error as Error).message);
        console.error('Error stack:', (error as Error).stack);
    }
}

/**
 * Handle API errors with helpful messages
 */
function handleApiError(action: string, error: any): void {
    console.error(`Error ${action}:`);

    if (error.status === 403) {
        console.error('Authentication failed (403 Forbidden). This is likely due to:');
        console.error('1. Invalid API key');
        console.error('2. Expired API key');
        console.error('3. API key doesn\'t have required permissions');
        console.error('\nPlease check your API key or generate a new one from your FlexPrice dashboard.');
    } else if (error.status === 401) {
        console.error('Authentication failed (401 Unauthorized). Please check your API key.');
    } else if (error.status === 404) {
        console.error('Resource not found (404). Please check the API endpoint URL.');
        console.error(`Current API endpoint: https://${process.env.FLEXPRICE_API_HOST || 'api.cloud.flexprice.io'}/v1`);
    } else if (error.status === 400) {
        console.error('Bad request (400). This is likely due to invalid parameters or payload format.');
        console.error('Make sure you are using snake_case for all property names (e.g., event_name, external_customer_id).');
        if (error.response && error.response.body) {
            console.error('Error details:', JSON.stringify(error.response.body));
        }
    } else {
        if (error.response) {
            console.error(`Status: ${error.response.status}`);
            console.error(`Headers: ${JSON.stringify(error.response.headers)}`);
            console.error(`Data: ${JSON.stringify(error.response.body)}`);
        } else if (error.request) {
            console.error('No response received:', error.request);
        } else {
            console.error('Error message:', error.message);
        }
    }
}

// Execute the example
runExample().catch(console.error);
