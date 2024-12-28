package internal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/google/uuid"
	"golang.org/x/time/rate"
)

const (
	NUM_EVENTS       = 10000
	BATCH_SIZE       = 100 // Reduced batch size
	REQUESTS_PER_SEC = 50  // Rate limit: requests per second
	MAX_RETRIES      = 1   // Maximum number of retries for failed requests
	INITIAL_BACKOFF  = 100 // Initial backoff in milliseconds
	API_ENDPOINT     = "https://api.cloud.flexprice.io/v1/events"
	TIMEOUT_SECONDS  = 5
)

// MeterType represents different types of billable metrics
type MeterType struct {
	Code        string
	MinValue    int64
	MaxValue    int64
	Description string
}

var (
	// Define meter types based on Lago billable metrics
	tokenMeters = []MeterType{
		{Code: "deepseek-coder-33b-input-tokens", MinValue: 100, MaxValue: 2000, Description: "Deepseek Coder 33B input tokens"},
		{Code: "deepseek-coder-33b-output-tokens", MinValue: 200, MaxValue: 4000, Description: "Deepseek Coder 33B output tokens"},
		{Code: "llama3.3-70b-input-tokens", MinValue: 100, MaxValue: 2000, Description: "Llama 3.3 70B input tokens"},
		{Code: "llama3.3-70b-output-tokens", MinValue: 200, MaxValue: 4000, Description: "Llama 3.3 70B output tokens"},
		{Code: "phi3-4k-input-tokens", MinValue: 100, MaxValue: 2000, Description: "Phi-3 4K input tokens"},
		{Code: "phi3-4k-output-tokens", MinValue: 200, MaxValue: 4000, Description: "Phi-3 4K output tokens"},
		{Code: "llama3.1-8b-input-tokens", MinValue: 100, MaxValue: 2000, Description: "Llama 3.1 8B input tokens"},
		{Code: "llama3.1-8b-output-tokens", MinValue: 200, MaxValue: 4000, Description: "Llama 3.1 8B output tokens"},
	}

	imageMeters = []MeterType{
		{Code: "sdxl-512-steps-25", MinValue: 1, MaxValue: 10, Description: "SDXL 512x512 with 25 steps"},
		{Code: "sdxl-768-steps-25", MinValue: 1, MaxValue: 10, Description: "SDXL 768x768 with 25 steps"},
		{Code: "sdxl-1024-steps-25", MinValue: 1, MaxValue: 10, Description: "SDXL 1024x1024 with 25 steps"},
		{Code: "flux-512-steps-25", MinValue: 1, MaxValue: 10, Description: "Flux 512x512 with 25 steps"},
		{Code: "flux-768-steps-25", MinValue: 1, MaxValue: 10, Description: "Flux 768x768 with 25 steps"},
		{Code: "flux-1024-steps-25", MinValue: 1, MaxValue: 10, Description: "Flux 1024x1024 with 25 steps"},
		{Code: "sdxl-512-steps-50", MinValue: 1, MaxValue: 5, Description: "SDXL 512x512 with 50 steps"},
		{Code: "sdxl-768-steps-50", MinValue: 1, MaxValue: 5, Description: "SDXL 768x768 with 50 steps"},
		{Code: "sdxl-1024-steps-50", MinValue: 1, MaxValue: 5, Description: "SDXL 1024x1024 with 50 steps"},
		{Code: "flux-512-steps-50", MinValue: 1, MaxValue: 5, Description: "Flux 512x512 with 50 steps"},
		{Code: "flux-768-steps-50", MinValue: 1, MaxValue: 5, Description: "Flux 768x768 with 50 steps"},
		{Code: "flux-1024-steps-50", MinValue: 1, MaxValue: 5, Description: "Flux 1024x1024 with 50 steps"},
	}

	audioMeters = []MeterType{
		{Code: "whisper-large-v3", MinValue: 1, MaxValue: 10, Description: "Whisper large audio model"},
	}
)

type BatchResult struct {
	BatchNumber int
	LastEventID string
	StartTime   time.Time
	EndTime     time.Time
	EventCount  int
}

// generateEvent creates a random event with varying properties
func generateEvent(index int) dto.IngestEventRequest {
	// List of test customer IDs (you should replace these with actual customer IDs from your system)
	customerIDs := []string{
		"cus_01HKG8QWERTY123",
		"cus_02HKG8ASDFGH456",
		"cus_03HKG8ZXCVBN789",
	}

	// Randomly choose between token and image events
	var meter MeterType
	var properties map[string]interface{}

	if index%3 == 0 {
		// Token event
		meter = tokenMeters[index%len(tokenMeters)]
		tokenCount := randInt64(meter.MinValue, meter.MaxValue)
		properties = map[string]interface{}{
			"tokens": tokenCount,
			"model":  meter.Code,
		}
	} else if index%3 == 1 {
		// Image event
		meter = imageMeters[index%len(imageMeters)]
		imageCount := randInt64(meter.MinValue, meter.MaxValue)
		properties = map[string]interface{}{
			"images": imageCount,
			"model":  meter.Code,
		}
	} else if index%3 == 2 {
		// Audio event
		meter = audioMeters[index%len(audioMeters)]
		audioCount := randInt64(meter.MinValue, meter.MaxValue)
		properties = map[string]interface{}{
			"audio-minutes": audioCount,
			"model":         meter.Code,
		}
	}

	// Generate timestamp within last 72 hours
	timestamp := time.Now().Add(-time.Duration(randInt64(0, 72)) * time.Hour)

	return dto.IngestEventRequest{
		EventID:            uuid.New().String(),
		ExternalCustomerID: customerIDs[(index+rand.Intn(10))%len(customerIDs)],
		EventName:          meter.Code,
		Timestamp:          timestamp,
		Properties:         properties,
	}
}

// randInt64 generates a random int64 between min and max
func randInt64(min, max int64) int64 {
	return min + rand.Int63n(max-min+1)
}

func ingestEvent(event dto.IngestEventRequest, limiter *rate.Limiter, wg *sync.WaitGroup, results chan<- time.Duration, errors chan<- error) {
	defer wg.Done()

	// Rate limiting
	err := limiter.Wait(context.Background())
	if err != nil {
		errors <- fmt.Errorf("rate limiter error: %v", err)
		return
	}

	jsonData, err := json.Marshal(event)
	if err != nil {
		errors <- fmt.Errorf("JSON marshal error: %v", err)
		return
	}

	start := time.Now()

	// Create custom HTTP client with timeout
	client := &http.Client{
		Timeout: time.Duration(TIMEOUT_SECONDS) * time.Second,
	}

	// Retry logic
	var lastErr error
	for attempt := 0; attempt <= MAX_RETRIES; attempt++ {
		if attempt > 0 {
			// Exponential backoff
			backoff := time.Duration(INITIAL_BACKOFF*attempt) * time.Millisecond
			time.Sleep(backoff)
		}

		req, err := http.NewRequest("POST", API_ENDPOINT, bytes.NewBuffer(jsonData))
		if err != nil {
			lastErr = fmt.Errorf("error creating request: %v", err)
			continue
		}
		req.Header.Add("Content-Type", "application/json")
		req.Header.Add("Accept", "application/json")
		req.Header.Add("Authorization", "Bearer <secret>")

		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("request error: %v", err)
			continue
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			resp.Body.Close()
			results <- time.Since(start)
			return
		}

		resp.Body.Close()
		lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	errors <- lastErr
}

// SeedEventsClickhouse seeds events data into Clickhouse
func SeedEventsClickhouse() error {
	logger, err := logger.NewLogger(config.GetDefaultConfig())
	if err != nil {
		log.Fatalf("Error creating logger: %v", err)
	}

	logger.Info("Starting load test...")
	logger.Infof("Sending %d events in batches of %d with rate limit of %d req/s",
		NUM_EVENTS, BATCH_SIZE, REQUESTS_PER_SEC)

	// Create rate limiter
	limiter := rate.NewLimiter(rate.Limit(REQUESTS_PER_SEC), 1)

	var wg sync.WaitGroup
	results := make(chan time.Duration, NUM_EVENTS)
	errors := make(chan error, NUM_EVENTS)

	// Track metrics
	start := time.Now()
	successCount := 0
	errorCount := 0
	batches := make([]BatchResult, 0)

	// Process in batches
	for i := 0; i < NUM_EVENTS; i += BATCH_SIZE {
		batchStart := time.Now()
		var lastEventID string

		batchSize := BATCH_SIZE
		if i+BATCH_SIZE > NUM_EVENTS {
			batchSize = NUM_EVENTS - i
		}

		// Launch batch of requests
		for j := 0; j < batchSize; j++ {
			event := generateEvent(i + j)
			lastEventID = event.EventID
			wg.Add(1)
			go ingestEvent(event, limiter, &wg, results, errors)
		}

		// Wait for batch to complete
		wg.Wait()

		batch := BatchResult{
			BatchNumber: i/BATCH_SIZE + 1,
			LastEventID: lastEventID,
			StartTime:   batchStart,
			EndTime:     time.Now(),
			EventCount:  batchSize,
		}
		batches = append(batches, batch)

		logger.Infof("Processed batch %d: %d/%d events in %v seconds. Last Event ID: %s",
			batch.BatchNumber, i+batchSize, NUM_EVENTS, batch.EndTime.Sub(batch.StartTime).Seconds(), lastEventID)

		// Add small delay between batches
		time.Sleep(time.Second)
	}

	// Close channels
	close(results)
	close(errors)

	// Calculate metrics
	var totalDuration time.Duration
	var maxDuration time.Duration
	var minDuration = time.Hour // Start with a large value

	for duration := range results {
		successCount++
		totalDuration += duration
		if duration > maxDuration {
			maxDuration = duration
		}
		if duration < minDuration {
			minDuration = duration
		}
	}

	for err := range errors {
		errorCount++
		logger.Errorf("Error: %v", err)
	}

	// Print results
	totalTime := time.Since(start)
	avgDuration := totalDuration / time.Duration(successCount)

	logger.Info("Load test completed!")
	logger.Info("Results:")
	logger.Infof("Total Time: %v", totalTime)
	logger.Infof("Successful Requests: %d", successCount)
	logger.Infof("Failed Requests: %d", errorCount)
	logger.Infof("Average Request Duration: %v", avgDuration)
	logger.Infof("Min Request Duration: %v", minDuration)
	logger.Infof("Max Request Duration: %v", maxDuration)
	logger.Infof("Requests per Second: %.2f", float64(successCount)/totalTime.Seconds())

	// Print batch information
	logger.Info("\nBatch Details:")
	for _, batch := range batches {
		logger.Infof("Batch %d: Events=%d, Duration=%v, Last Event ID=%s",
			batch.BatchNumber,
			batch.EventCount,
			batch.EndTime.Sub(batch.StartTime),
			batch.LastEventID)
	}

	return nil
}
