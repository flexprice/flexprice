package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/flexprice/go-sdk/v2"
	"github.com/flexprice/go-sdk/v2/models/types"
	"github.com/joho/godotenv"
)

func main() {
	godotenv.Load()

	apiKey := os.Getenv("FLEXPRICE_API_KEY")
	apiHost := os.Getenv("FLEXPRICE_API_HOST")
	if apiHost == "" {
		apiHost = "https://us.api.flexprice.io/v1"
	}
	if apiKey == "" {
		log.Fatal("Set FLEXPRICE_API_KEY in .env")
	}

	client := flexprice.New(
		flexprice.WithServerURL(apiHost),
		flexprice.WithSecurity(apiKey),
	)
	ctx := context.Background()

	// Sync: ingest one event
	customerID := fmt.Sprintf("sample-customer-%d", time.Now().Unix())
	req := types.DtoIngestEventRequest{
		EventName:          "Sample Event",
		ExternalCustomerID: customerID,
		Properties:         map[string]string{"source": "sample_app", "environment": "test"},
	}
	resp, err := client.Events.IngestEvent(ctx, req)
	if err != nil {
		log.Fatalf("IngestEvent: %v", err)
	}
	if resp != nil {
		if r := resp.GetHTTPMeta().Response; r != nil && r.StatusCode == 202 {
			fmt.Println("Event created (202).")
		} else {
			fmt.Printf("Event response: %+v\n", resp)
		}
	}

	// Async client (merged from api/custom/go/async.go)
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
