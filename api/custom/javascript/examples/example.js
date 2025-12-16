// FlexPrice JavaScript SDK Example
// This example demonstrates how to use the FlexPrice JavaScript SDK
// to interact with the FlexPrice API.

// Import the FlexPrice SDK
const FlexPrice = require('@flexprice/sdk');
// Or using ES modules:
// import * as FlexPrice from '@flexprice/sdk';

// Load environment variables (in a real application, use dotenv)
require('dotenv').config();

/**
 * Main example function demonstrating FlexPrice SDK usage
 */
function runExample() {
  console.log('Starting FlexPrice JavaScript SDK example...');

  try {
    // Configure the API client with your API key
    // API keys should start with 'sk_' followed by a unique identifier
    const apiKey = process.env.FLEXPRICE_API_KEY || 'YOUR_API_KEY';
    const apiHost = process.env.FLEXPRICE_API_HOST || 'api.cloud.flexprice.io';
    
    if (apiKey === 'YOUR_API_KEY') {
      console.error('ERROR: You must provide a valid FlexPrice API key. Set the FLEXPRICE_API_KEY environment variable.');
      console.error('Example: FLEXPRICE_API_KEY=sk_your_api_key_here node example.js');
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

    console.log(`Creating event for customer: ${customerId}...`);

    // Step 1: Create an event
    // IMPORTANT: Use snake_case for all property names to match the API
    const eventRequest = {
      event_name: 'Sample Event',
      external_customer_id: customerId,
      properties: {
        source: 'javascript_sample_app',
        environment: 'test',
        timestamp: new Date().toISOString()
      },
      source: 'javascript_sample_app',
      timestamp: new Date().toISOString()
    };

    // IMPORTANT: Pass the event directly without wrapping it
    eventsApi.eventsPost(eventRequest, function(error, eventResult, response) {
      if (error) {
        handleApiError('creating event', error);
        return;
      }
      
      console.log(`Event created successfully! ID: ${eventResult.event_id}`);

      // Step 2: Retrieve events for this customer
      console.log(`Retrieving events for customer ${customerId}...`);
      
      // IMPORTANT: Use snake_case for parameter names
      eventsApi.eventsGet({
        external_customer_id: customerId
      }, function(error, events, response) {
        if (error) {
          handleApiError('retrieving events', error);
          return;
        }
        
        console.log(`Found ${events.events.length} events:`);
        
        events.events.forEach((event, index) => {
          console.log(`Event ${index + 1}: ${event.id} - ${event.event_name}`);
          console.log(`Properties: ${JSON.stringify(event.properties)}`);
        });

        console.log('Example completed successfully!');
      });
    });

  } catch (error) {
    console.error('Error running example:');
    console.error('Error message:', error.message);
    console.error('Error stack:', error.stack);
  }
}

/**
 * Handle API errors with helpful messages
 */
function handleApiError(action, error) {
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
runExample(); 