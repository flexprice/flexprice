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

	cfg := flexprice.NewConfiguration()
	cfg.Scheme = "https"
	cfg.Host = apiHost
	cfg.AddDefaultHeader("x-api-key", apiKey)
	client := flexprice.NewAPIClient(cfg)
	ctx := context.Background()

	// 1) Grant monthly credits
	grant := flexprice.DtoIngestEventRequest{
		EventName:          "credits.grant",
		ExternalCustomerId: customerID,
		Properties: &map[string]string{
			"units":        "10000", // grant 10k credits
			"period_start": time.Now().UTC().AddDate(0, 0, -1).Format(time.RFC3339),
			"period_end":   time.Now().UTC().AddDate(0, 1, -1).Format(time.RFC3339),
			"source":       "with-go-credits",
		},
		Source:    ptr("with-go-credits"),
		Timestamp: ptr(time.Now().UTC().Format(time.RFC3339)),
	}
	if _, resp, err := client.EventsAPI.EventsPost(ctx).Event(grant).Execute(); err != nil {
		log.Fatalf("credits grant: %v", err)
	} else if resp != nil && resp.StatusCode != 202 {
		log.Printf("warn: unexpected status for grant: %d", resp.StatusCode)
	}

	time.Sleep(200 * time.Millisecond)

	// 2) Consume some credits
	consume := flexprice.DtoIngestEventRequest{
		EventName:          "credits.consume",
		ExternalCustomerId: customerID,
		Properties: &map[string]string{
			"units":  "2500", // consume 2.5k credits
			"reason": "generation",
			"source": "with-go-credits",
		},
		Source:    ptr("with-go-credits"),
		Timestamp: ptr(time.Now().UTC().Format(time.RFC3339)),
	}
	if _, resp, err := client.EventsAPI.EventsPost(ctx).Event(consume).Execute(); err != nil {
		log.Fatalf("credits consume: %v", err)
	} else if resp != nil && resp.StatusCode != 202 {
		log.Printf("warn: unexpected status for consume: %d", resp.StatusCode)
	}

	// 3) Aggregate remaining credits in current period
	data, _, err := client.EventsAPI.EventsGet(ctx).ExternalCustomerId(customerID).Execute()
	if err != nil {
		log.Fatalf("events get: %v", err)
	}

	var granted, used float64
	since := time.Now().AddDate(0, 0, -30)
	for _, e := range data.Events {
		// Timestamp filter
		tsStr := ""
		if e.Timestamp != nil {
			tsStr = *e.Timestamp
		}
		ts, _ := time.Parse(time.RFC3339, tsStr)
		if ts.Before(since) {
			continue
		}
		if e.Properties == nil {
			continue
		}
		unitsStr := (*e.Properties)["units"]
		if unitsStr == "" {
			continue
		}
		u, err := strconv.ParseFloat(unitsStr, 64)
		if err != nil {
			continue
		}
		if e.EventName == "credits.grant" {
			granted += u
		}
		if e.EventName == "credits.consume" {
			used += u
		}
	}

	fmt.Printf("Credits summary for %s (last 30d) -> granted=%.0f, used=%.0f, remaining=%.0f\n", customerID, granted, used, granted-used)
}

func ptr[T any](v T) *T { return &v }
