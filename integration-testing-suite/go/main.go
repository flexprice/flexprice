package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	flexprice "github.com/flexprice/go-sdk/v2"
)

const defaultAPIHost = "api.cloud.flexprice.io/v1"

// ts returns a unique-ish timestamp suffix for entity names.
func ts() int64 {
	return time.Now().UnixNano() / int64(time.Millisecond)
}

func main() {
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║              FLEXPRICE ORCHESTRATED SANITY TEST              ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Println()

	// ── Read environment variables ──────────────────────────────────────

	apiKey := os.Getenv("FLEXPRICE_API_KEY")
	apiHost := os.Getenv("FLEXPRICE_API_HOST")

	if apiKey == "" {
		log.Fatal("FLEXPRICE_API_KEY environment variable is required")
	}
	if apiHost == "" {
		apiHost = defaultAPIHost
	}

	// Strip scheme if user accidentally included it (e.g. "https://api.cloud.flexprice.io/v1").
	apiHost = strings.TrimPrefix(apiHost, "https://")
	apiHost = strings.TrimPrefix(apiHost, "http://")

	// Display masked credentials.
	maskedKey := apiKey
	if len(apiKey) > 12 {
		maskedKey = apiKey[:8] + "..." + apiKey[len(apiKey)-4:]
	}
	fmt.Printf("API Host: %s\n", apiHost)
	fmt.Printf("API Key:  %s\n", maskedKey)
	fmt.Printf("Started:  %s\n", time.Now().Format(time.RFC3339))

	// ── Build server URL ────────────────────────────────────────────────

	// Support both localhost (http) and remote (https).
	scheme := "https://"
	if strings.HasPrefix(apiHost, "localhost") || strings.HasPrefix(apiHost, "127.0.0.1") {
		scheme = "http://"
	}
	serverURL := scheme + apiHost

	// ── Initialize SDK client ───────────────────────────────────────────

	client := flexprice.New(
		flexprice.WithServerURL(serverURL),
		flexprice.WithSecurity(apiKey),
	)

	// Also keep a raw HTTP client as fallback for any edge cases.
	raw := NewRawClient(serverURL, apiKey)

	// ── Run orchestrated sanity test ────────────────────────────────────

	runner := &SanityRunner{client: client, raw: raw}
	ctx := contextWithTimeout()
	start := time.Now()

	// Phases 1-7: Full billing lifecycle.
	runner.runCatalogSteps(ctx)
	runner.runBillingSteps(ctx)
	runner.runSubscriptionSteps(ctx)
	runner.runWalletSteps(ctx)
	runner.runUsageSteps(ctx)
	runner.runInvoiceSteps(ctx)
	runner.runCleanupSteps(ctx)

	totalDuration := time.Since(start)

	// ── Print final report ──────────────────────────────────────────────

	runner.printReport(totalDuration)

	// Exit with non-zero if any non-cleanup step failed.
	for _, r := range runner.results {
		if !r.Passed && !r.Skipped && !strings.HasPrefix(r.Phase, "PHASE 7") {
			os.Exit(1)
		}
	}
}

func contextWithTimeout() context.Context {
	return context.Background()
}
