# FlexPrice Go SDK

This is the Go client library for the FlexPrice API.

## Installation

```bash
go get github.com/flexprice/go-sdk
```

## Usage

### Basic API Usage

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	flexprice "github.com/flexprice/go-sdk"
	"github.com/joho/godotenv"
	"github.com/samber/lo"
)

// This sample application demonstrates how to use the FlexPrice Go SDK
// to create and retrieve events, showing the basic patterns for API interaction.
// To run this example:
// 1. Copy this file to your project
// 2. Create a .env file with FLEXPRICE_API_KEY and FLEXPRICE_API_HOST
// 3. Run with: go run main.go

// Sample .env file:
// FLEXPRICE_API_KEY=your_api_key_here
// FLEXPRICE_API_HOST=api.flexprice.io

func RunSample() {
	// Load .env file if present
	godotenv.Load()

	// Get API credentials from environment
	apiKey := os.Getenv("FLEXPRICE_API_KEY")
	apiHost := os.Getenv("FLEXPRICE_API_HOST")

	if apiKey == "" || apiHost == "" {
		log.Fatal("Missing required environment variables: FLEXPRICE_API_KEY and FLEXPRICE_API_HOST")
	}

	// Initialize API client
	config := flexprice.NewConfiguration()
	config.Scheme = "https"
	config.Host = apiHost
	config.AddDefaultHeader("x-api-key", apiKey)

	client := flexprice.NewAPIClient(config)
	ctx := context.Background()

	// Generate a unique customer ID for this sample
	customerId := fmt.Sprintf("sample-customer-%d", time.Now().Unix())

	// Step 1: Create an event
	fmt.Println("Creating event...")
	eventRequest := flexprice.DtoIngestEventRequest{
		EventName:          "Sample Event",
		ExternalCustomerId: customerId,
		Properties: &map[string]string{
			"source":      "sample_app",
			"environment": "test",
			"timestamp":   time.Now().String(),
		},
		Source:    lo.ToPtr("sample_app"),
		Timestamp: lo.ToPtr(time.Now().Format(time.RFC3339)),
	}

	// Send the event creation request
	result, response, err := client.EventsAPI.EventsPost(ctx).
		Event(eventRequest).
		Execute()

	if err != nil {
		log.Fatalf("Error creating event: %v", err)
	}

	if response.StatusCode != 202 {
		log.Fatalf("Expected status code 202, got %d", response.StatusCode)
	}

	// The result is a map, so we need to use map access
	eventId := result["event_id"]
	fmt.Printf("Event created successfully! ID: %v\n\n", eventId)

	// Step 2: Retrieve events for this customer
	fmt.Println("Retrieving events for customer...")
	events, response, err := client.EventsAPI.EventsGet(ctx).
		ExternalCustomerId(customerId).
		Execute()

	if err != nil {
		log.Fatalf("Error retrieving events: %v", err)
	}

	if response.StatusCode != 200 {
		log.Fatalf("Expected status code 200, got %d", response.StatusCode)
	}

	// Process the events (the response is a map)
	fmt.Printf("Raw response: %+v\n\n", response)

	for i, event := range events.Events {
		fmt.Printf("Event %d: %v - %v\n", i+1, event.Id, event.EventName)
		fmt.Printf("Event properties: %v\n", event.Properties)
	}

	fmt.Println("Sample application completed successfully!")
}

func main() {
	RunSample()
}
```

### Async Client Usage

The FlexPrice Go SDK includes an asynchronous client for more efficient event tracking, especially for high-volume applications:

```go
func RunAsyncSample(client *flexprice.APIClient) {
	// Create an AsyncClient with debug enabled
	asyncConfig := flexprice.DefaultAsyncConfig()
	asyncConfig.Debug = true
	asyncClient := client.NewAsyncClientWithConfig(asyncConfig)

	// Example 1: Simple event
	err := asyncClient.Enqueue(
		"api_request",
		"customer-123",
		map[string]interface{}{
			"path":             "/api/resource",
			"method":           "GET",
			"status":           "200",
			"response_time_ms": 150,
		},
	)
	if err != nil {
		log.Fatalf("Failed to enqueue event: %v", err)
	}
	fmt.Println("Enqueued simple event")

	// Example 2: Event with additional options
	err = asyncClient.EnqueueWithOptions(flexprice.EventOptions{
		EventName:          "file_upload",
		ExternalCustomerID: "customer-123",
		CustomerID:         "cust_456",  // Optional internal FlexPrice ID
		EventID:            "event_789", // Custom event ID
		Properties: map[string]interface{}{
			"file_size_bytes": 1048576,
			"file_type":       "image/jpeg",
			"storage_bucket":  "user_uploads",
		},
		Source:    "upload_service",
		Timestamp: time.Now().Format(time.RFC3339),
	})
	if err != nil {
		log.Fatalf("Failed to enqueue event: %v", err)
	}
	fmt.Println("Enqueued event with custom options")

	// Example 3: Batch multiple events
	for i := 0; i < 10; i++ {
		err = asyncClient.Enqueue(
			"batch_example",
			fmt.Sprintf("customer-%d", i),
			map[string]interface{}{
				"index": i,
				"batch": "demo",
			},
		)
		if err != nil {
			log.Fatalf("Failed to enqueue batch event: %v", err)
		}
	}
	fmt.Println("Enqueued 10 batch events")

	// Wait for a moment to let the API requests complete
	fmt.Println("Waiting for events to be processed...")
	time.Sleep(time.Second * 5)
	
	// Explicitly close the client - this will flush any remaining events
	fmt.Println("Closing client...")
	asyncClient.Close()
	
	fmt.Println("Example completed successfully!")
}
```

## Async Client Benefits

The async client provides several advantages:

1. **Efficient Batching**: Events are automatically batched for more efficient API usage
2. **Background Processing**: Events are sent asynchronously, not blocking your application
3. **Auto-Generated IDs**: EventIDs are automatically generated if not provided
4. **Rich Property Types**: Properties support various types (numbers, booleans, etc.)
5. **Detailed Logging**: Enable debug mode for comprehensive logging

## Features

- Complete API coverage
- Type-safe client
- Detailed documentation
- Error handling
- Batch processing for high-volume applications

## Documentation

For detailed API documentation, refer to the code comments and the official FlexPrice API documentation. 