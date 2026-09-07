package internal

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
)

const (
	defaultEventsCSVPath = "scripts/internal/events.csv"
	defaultAPIBaseURL    = "https://api.cloud.flexprice.io/v1"
	replayTimeoutSeconds = 15
	replayMaxRetries     = 2
	replayInitialBackoff = 200 * time.Millisecond
)

// ReplayEventsFromCSV reads events from a CSV and POSTs each to POST /v1/events.
//
// Usage:
//
//	go run scripts/main.go -cmd replay-events-csv \
//	  -api-key "sk_..." \
//	  -file-path "scripts/internal/events.csv" \
//	  -api-base-url "https://api.cloud.flexprice.io/v1"
//
// Optional: -dry-run true (parse + print first few payloads, no HTTP calls)
func ReplayEventsFromCSV() error {
	apiKey := os.Getenv("FLEXPRICE_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("SCRIPT_FLEXPRICE_API_KEY")
	}
	if apiKey == "" {
		return fmt.Errorf("api key required: pass -api-key or set FLEXPRICE_API_KEY")
	}

	filePath := os.Getenv("FILE_PATH")
	if filePath == "" {
		filePath = defaultEventsCSVPath
	}

	baseURL := strings.TrimRight(os.Getenv("API_BASE_URL"), "/")
	if baseURL == "" {
		baseURL = defaultAPIBaseURL
	}
	endpoint := baseURL + "/events"

	dryRun := strings.EqualFold(os.Getenv("DRY_RUN"), "true")

	f, err := os.Open(filePath) // #nosec G703,G304 -- CLI/env file path, dev tooling
	if err != nil {
		return fmt.Errorf("open csv %s: %w", filePath, err)
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.ReuseRecord = true
	reader.FieldsPerRecord = -1

	header, err := reader.Read()
	if err != nil {
		return fmt.Errorf("read csv header: %w", err)
	}
	col := indexCSVColumns(header)
	for _, required := range []string{"id", "external_customer_id", "event_name", "timestamp", "properties"} {
		if _, ok := col[required]; !ok {
			return fmt.Errorf("csv missing required column %q", required)
		}
	}

	client := &http.Client{Timeout: time.Duration(replayTimeoutSeconds) * time.Second}

	var (
		total     int
		success   int
		failed    int
		skipped   int
		start     = time.Now()
		dryShown  int
		firstErrs []string
	)

	fmt.Printf("Replaying events from %s → %s (dry_run=%v)\n", filePath, endpoint, dryRun)

	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read csv row %d: %w", total+1, err)
		}
		total++

		event, err := csvRowToIngestEvent(row, col)
		if err != nil {
			failed++
			msg := fmt.Sprintf("row %d: parse error: %v", total, err)
			if len(firstErrs) < 10 {
				firstErrs = append(firstErrs, msg)
			}
			fmt.Println(msg)
			continue
		}

		if event.ExternalCustomerID == "" {
			skipped++
			fmt.Printf("row %d: skip event_id=%s — empty external_customer_id\n", total, event.EventID)
			continue
		}

		if dryRun {
			if dryShown < 3 {
				payload, _ := json.MarshalIndent(event, "", "  ")
				fmt.Printf("--- dry-run sample row %d ---\n%s\n", total, payload)
				dryShown++
			}
			success++
			continue
		}

		if err := postSingleEvent(client, endpoint, apiKey, event); err != nil {
			failed++
			msg := fmt.Sprintf("row %d: event_id=%s failed: %v", total, event.EventID, err)
			if len(firstErrs) < 10 {
				firstErrs = append(firstErrs, msg)
			}
			fmt.Println(msg)
			continue
		}

		success++
		if success%100 == 0 {
			fmt.Printf("progress: %d accepted / %d processed\n", success, total)
		}
	}

	fmt.Printf("\nDone in %s\n", time.Since(start).Round(time.Millisecond))
	fmt.Printf("total=%d success=%d failed=%d skipped=%d\n", total, success, failed, skipped)
	if len(firstErrs) > 0 {
		fmt.Println("first errors:")
		for _, e := range firstErrs {
			fmt.Println(" -", e)
		}
	}
	if failed > 0 && !dryRun {
		return fmt.Errorf("%d events failed to ingest", failed)
	}
	return nil
}

func indexCSVColumns(header []string) map[string]int {
	col := make(map[string]int, len(header))
	for i, h := range header {
		col[strings.TrimSpace(strings.Trim(h, `"`))] = i
	}
	return col
}

func csvCell(row []string, col map[string]int, name string) string {
	i, ok := col[name]
	if !ok || i >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[i])
}

func csvRowToIngestEvent(row []string, col map[string]int) (*dto.IngestEventRequest, error) {
	eventID := csvCell(row, col, "id")
	eventName := csvCell(row, col, "event_name")
	externalCustomerID := csvCell(row, col, "external_customer_id")
	customerID := csvCell(row, col, "customer_id")
	source := csvCell(row, col, "source")
	tsRaw := csvCell(row, col, "timestamp")
	propsRaw := csvCell(row, col, "properties")

	if eventName == "" {
		return nil, fmt.Errorf("empty event_name")
	}

	ts, err := parseEventTimestamp(tsRaw)
	if err != nil {
		return nil, fmt.Errorf("timestamp %q: %w", tsRaw, err)
	}

	properties := map[string]interface{}{}
	if propsRaw != "" && propsRaw != "{}" {
		if err := json.Unmarshal([]byte(propsRaw), &properties); err != nil {
			return nil, fmt.Errorf("properties json: %w", err)
		}
	}

	return &dto.IngestEventRequest{
		EventID:            eventID,
		EventName:          eventName,
		ExternalCustomerID: externalCustomerID,
		CustomerID:         customerID,
		Timestamp:          ts,
		Source:             source,
		Properties:         properties,
	}, nil
}

func parseEventTimestamp(raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, fmt.Errorf("empty timestamp")
	}
	layouts := []string{
		"2006-01-02 15:04:05.000",
		"2006-01-02 15:04:05.999",
		"2006-01-02 15:04:05",
		time.RFC3339Nano,
		time.RFC3339,
	}
	var lastErr error
	for _, layout := range layouts {
		ts, err := time.ParseInLocation(layout, raw, time.UTC)
		if err == nil {
			return ts, nil
		}
		lastErr = err
	}
	return time.Time{}, lastErr
}

func postSingleEvent(client *http.Client, endpoint, apiKey string, event *dto.IngestEventRequest) error {
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= replayMaxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(replayInitialBackoff * time.Duration(attempt))
		}

		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, endpoint, bytes.NewReader(body)) // #nosec G704 -- endpoint from env, replays to own API
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("x-api-key", apiKey)

		resp, err := client.Do(req) // #nosec G704 -- endpoint from env, replays to own API
		if err != nil {
			lastErr = fmt.Errorf("http: %w", err)
			continue
		}

		respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if err != nil {
			return fmt.Errorf("read response body: %w", err)
		}
		resp.Body.Close() // #nosec G104 -- seed tooling, non-prod

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}

		// Retry transient errors only.
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
			continue
		}
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return lastErr
}
