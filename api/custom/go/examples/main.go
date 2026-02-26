package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/flexprice/flexprice-go"
	"github.com/flexprice/flexprice-go/models/components"
	"github.com/joho/godotenv"
)

// This sample demonstrates the FlexPrice Go SDK (Speakeasy-generated) and the custom async client.
// To run: from api/go/examples, ensure parent api/go is built, then: go run main.go

func main() {
	godotenv.Load()

	apiKey := os.Getenv("FLEXPRICE_API_KEY")
	apiHost := os.Getenv("FLEXPRICE_API_HOST")
	if apiHost == "" {
		apiHost = "https://us.api.flexprice.io"
	}
	if apiKey == "" {
		log.Fatal("Set FLEXPRICE_API_KEY in .env")
	}

	// Initialize SDK (flexprice.New + WithSecurity)
	client := flexprice.New(apiHost, flexprice.WithSecurity(apiKey))
	ctx := context.Background()

	// Sync: ingest one event
	customerID := fmt.Sprintf("sample-customer-%d", time.Now().Unix())
	req := components.DtoIngestEventRequest{
		EventName:          "Sample Event",
		ExternalCustomerID: customerID,
		Properties:         map[string]string{"source": "sample_app", "environment": "test"},
	}
	resp, err := client.Events.IngestEvent(ctx, req)
	if err != nil {
		log.Fatalf("IngestEvent: %v", err)
	}
	if resp != nil && resp.RawResponse != nil && resp.RawResponse.StatusCode == 202 {
		fmt.Println("Event created (202).")
	} else {
		fmt.Printf("Event response: %+v\n", resp)
	}

	// Async client (custom)
	asyncConfig := flexprice.DefaultAsyncConfig()
	asyncConfig.Debug = true
	asyncClient := client.NewAsyncClientWithConfig(asyncConfig)
	defer asyncClient.Close()

	_ = asyncClient.Enqueue("api_request", "customer-123", map[string]interface{}{
		"path": "/api/resource", "method": "GET", "status": "200",
	})
	fmt.Println("Enqueued async event. Waiting 2s...")
	time.Sleep(2 * time.Second)
	fmt.Println("Done.")
}
