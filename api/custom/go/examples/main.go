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

// Default API base URL: https://us.api.flexprice.io/v1 (include /v1; no trailing slash).
// Override with FLEXPRICE_API_HOST (full URL, e.g. https://api.cloud.flexprice.io/v1).
// Run from api/go/examples after merge-custom: go mod tidy && go run .

func main() {
	godotenv.Load()

	apiKey := os.Getenv("FLEXPRICE_API_KEY")
	serverURL := os.Getenv("FLEXPRICE_API_HOST")
	if serverURL == "" {
		serverURL = "https://us.api.flexprice.io/v1"
	}
	if apiKey == "" {
		log.Fatal("Set FLEXPRICE_API_KEY in .env or environment")
	}

	client := flexprice.New(
		flexprice.WithServerURL(serverURL),
		flexprice.WithSecurity(apiKey),
	)
	ctx := context.Background()

	externalID := fmt.Sprintf("sample-customer-%d", time.Now().Unix())
	name := fmt.Sprintf("Sample Customer %d", time.Now().Unix())

	createResp, err := client.Customers.CreateCustomer(ctx, types.CreateCustomerRequest{
		ExternalID: externalID,
		Name:       flexprice.String(name),
		Email:      flexprice.String(fmt.Sprintf("sample-%d@example.com", time.Now().Unix())),
		Metadata: map[string]string{
			"source": "go_sdk_example", "environment": "example",
		},
	})
	if err != nil {
		log.Fatalf("CreateCustomer: %v", err)
	}
	if createResp != nil {
		if c := createResp.GetCustomerResponse(); c != nil && c.ID != nil {
			fmt.Printf("Customer created: %s (external_id=%s)\n", *c.ID, externalID)
		} else {
			fmt.Println("Customer created.")
		}
	}

	eventReq := types.IngestEventRequest{
		EventName:          "Sample Event",
		ExternalCustomerID: externalID,
		Properties:         map[string]string{"source": "sample_app", "environment": "test"},
	}
	ingestResp, err := client.Events.IngestEvent(ctx, eventReq)
	if err != nil {
		log.Fatalf("IngestEvent: %v", err)
	}
	if ingestResp != nil {
		meta := ingestResp.GetHTTPMeta()
		if hr := meta.GetResponse(); hr != nil && hr.StatusCode == 202 {
			fmt.Println("Event ingested (202).")
		} else {
			fmt.Printf("Event response: %+v\n", ingestResp.GetObject())
		}
	}

	// Custom async client (see api/custom/go/async.go)
	asyncConfig := flexprice.DefaultAsyncConfig()
	asyncConfig.Debug = true
	asyncClient := client.NewAsyncClientWithConfig(asyncConfig)
	defer asyncClient.Close()

	_ = asyncClient.Enqueue("api_request", externalID, map[string]interface{}{
		"path": "/api/resource", "method": "GET", "status": "200",
	})
	fmt.Println("Enqueued async event. Waiting 2s...")
	time.Sleep(2 * time.Second)
	fmt.Println("Done.")
}
