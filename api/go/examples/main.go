package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	gosdk "github.com/flexprice/go-sdk"
	"github.com/flexprice/go-sdk/models/components"
	"github.com/flexprice/go-sdk/models/operations"
	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()
	apiKey := os.Getenv("FLEXPRICE_API_KEY")
	apiHost := os.Getenv("FLEXPRICE_API_HOST")
	if apiKey == "" {
		log.Fatal("FLEXPRICE_API_KEY environment variable is required")
	}
	if apiHost == "" {
		apiHost = "https://api.cloud.flexprice.io"
	}

	fmt.Println("FlexPrice Go SDK Example")
	fmt.Println("========================")
	fmt.Printf("API Host: %s\n\n", apiHost)

	client := gosdk.New(apiHost, gosdk.WithSecurity(apiKey))
	ctx := context.Background()

	// Example 1: Send event
	fmt.Println("1. Sending event...")
	event := components.DtoIngestEventRequest{
		ExternalCustomerID: "customer_123",
		EventName:          "api_call",
		Properties: map[string]string{
			"method": "GET",
		},
		Timestamp: gosdk.String(time.Now().Format(time.RFC3339)),
	}
	res, err := client.Events.Ingest(ctx, event)
	if err != nil {
		log.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("✓ Event sent: %+v\n", res.Object)
	}

	// Example 2: List invoices
	fmt.Println("\n2. Listing invoices...")
	limit := int64(5)
	invoicesReq := operations.GetInvoicesRequest{Limit: &limit}
	invoicesRes, err := client.Invoices.List(ctx, invoicesReq)
	if err != nil {
		log.Printf("Error: %v\n", err)
	} else if invoicesRes.DtoListInvoicesResponse != nil {
		fmt.Printf("✓ Found %d invoices\n", len(invoicesRes.DtoListInvoicesResponse.Items))
	}

	fmt.Println("\nExample completed!")
}
