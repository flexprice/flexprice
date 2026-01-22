package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	flexprice "github.com/flexprice/go-sdk"
	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()

	apiKey := os.Getenv("FLEXPRICE_API_KEY")
	customerID := os.Getenv("CUSTOMER_ID")
	apiHost := os.Getenv("FLEXPRICE_API_HOST")
	if apiHost == "" {
		apiHost = "api.cloud.flexprice.io"
	}
	if apiKey == "" || customerID == "" {
		log.Fatal("missing env vars: FLEXPRICE_API_KEY, CUSTOMER_ID")
	}

	// Initialize Flexprice Go SDK client
	cfg := flexprice.NewConfiguration()
	cfg.Scheme = "https"
	cfg.Host = apiHost
	cfg.AddDefaultHeader("x-api-key", apiKey)
	client := flexprice.NewAPIClient(cfg)
	ctx := context.Background()

	// Send two usage events using official SDK
	// 1) api.request
	event1 := flexprice.DtoIngestEventRequest{
		EventName:          "api.request",
		ExternalCustomerId: customerID,
		Properties: &map[string]string{
			"endpoint": "/v1/messages",
			"units":    "3",
			"source":   "with-go",
		},
	}
	if _, resp, err := client.EventsAPI.EventsPost(ctx).Event(event1).Execute(); err != nil {
		log.Fatalf("track usage 1: %v", err)
	} else if resp != nil && resp.StatusCode != 202 {
		log.Printf("warn: unexpected status for event1: %d", resp.StatusCode)
	}

	time.Sleep(250 * time.Millisecond)

	// 2) generation.tokens
	event2 := flexprice.DtoIngestEventRequest{
		EventName:          "generation.tokens",
		ExternalCustomerId: customerID,
		Properties: &map[string]string{
			"model":  "gpt-4o-mini",
			"units":  "1500",
			"source": "with-go",
		},
	}
	if _, resp, err := client.EventsAPI.EventsPost(ctx).Event(event2).Execute(); err != nil {
		log.Fatalf("track usage 2: %v", err)
	} else if resp != nil && resp.StatusCode != 202 {
		log.Printf("warn: unexpected status for event2: %d", resp.StatusCode)
	}

	// Fetch events and aggregate a simple last-24h units summary
	since := time.Now().Add(-24 * time.Hour)
	events, _, err := client.EventsAPI.EventsGet(ctx).ExternalCustomerId(customerID).Execute()
	if err != nil {
		log.Fatalf("events get: %v", err)
	}

	var totalUnits float64
	for _, e := range events.Events {
		// Parse timestamp
		tsStr := ""
		if e.Timestamp != nil {
			tsStr = *e.Timestamp
		}
		ts, _ := time.Parse(time.RFC3339, tsStr)
		if ts.Before(since) {
			continue
		}
		// Sum units if present
		if e.Properties != nil {
			if uStr, ok := (*e.Properties)["units"]; ok {
				if v, err := strconv.ParseFloat(uStr, 64); err == nil {
					totalUnits += v
				}
			}
		}
	}

	fmt.Printf("Customer %s last 24h totalUnits=%v\n", customerID, totalUnits)
}
