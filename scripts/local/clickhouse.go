package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
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
	NUM_EVENTS       = 500000
	BATCH_SIZE       = 500 // Reduced batch size
	REQUESTS_PER_SEC = 50  // Rate limit: requests per second
	MAX_RETRIES      = 1    // Maximum number of retries for failed requests
	INITIAL_BACKOFF  = 100  // Initial backoff in milliseconds
	API_ENDPOINT     = "https://api.cloud.flexprice.io/v1/events"
	// API_ENDPOINT    = "http://localhost:8080/v1/events/ingest"
	TIMEOUT_SECONDS = 5
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
	sources := []string{"web", "mobile", "api", "backend"}
	clouds := []string{"aws", "gcp", "azure"}
	// eventTypes := []string{"api_call", "page_view", "button_click", "form_submit"}
	eventTypes := []string{"tokens", "audio_length", "images_processed"}
	// image_sizes := []string{"512x512", "768x768", "1024x1024"}
	image_sizes := []string{"512x512", "768x768", "1024x1024"}
	audio_models := []string{"whisper"}
	text_models := []string{"llama3_1_8b", "llama3_1_70b"}

	return dto.IngestEventRequest{
		EventID:            uuid.New().String(),
		EventName:          eventTypes[index%len(eventTypes)],
		ExternalCustomerID: fmt.Sprintf("cus_loadtest_%d", index%2), // 100 different customers
		// ExternalCustomerID: "cust-00000007",
		Source:    sources[index%len(sources)],
		Timestamp: time.Now().Add(-time.Duration((index%10)*2) * time.Minute),
		Properties: map[string]interface{}{
			"bytes_transferred": 100 + (index % 1000),
			"duration_ms":       50 + (index % 200),
			"status_code":       200 + (index%3)*100, // 200, 300, 400
			"input_tokens":      100 + (index % 1000),
			"output_tokens":     100 + (index % 1000),
			"image_count":       10 + (index % 1000),
			"image_size":        image_sizes[index%len(image_sizes)],
			"audio_length_secs": 1 + (index % 1000),
			"audio_model":       audio_models[index%len(audio_models)],
			"text_model":        text_models[index%len(text_models)],
			"cloud":             clouds[index%len(clouds)],
			"test_group":        fmt.Sprintf("group_%d", index%10),
		},
	}
}

func ingestEvent(event dto.IngestEventRequest, limiter *rate.Limiter, wg *sync.WaitGroup, results chan<- time.Duration, errors chan<- error) {
	defer wg.Done()

	// Wait for rate limiter
	err := limiter.Wait(context.Background())
	if err != nil {
		errors <- fmt.Errorf("rate limiter error: %v", err)
		return
	}

	start := time.Now()
	retryCount := 0
	backoff := INITIAL_BACKOFF

	for retryCount <= MAX_RETRIES {
		jsonData, err := json.Marshal(event)
		if err != nil {
			errors <- fmt.Errorf("marshal error: %v", err)
			return
		}

		client := &http.Client{
			Timeout: time.Second * TIMEOUT_SECONDS,
		}

		req, err := http.NewRequest("POST", API_ENDPOINT, bytes.NewBuffer(jsonData))

		if err != nil {
			fmt.Println(err)
			return
		}
		req.Header.Add("Content-Type", "application/json")
		req.Header.Add("Accept", "application/json")
		req.Header.Add("x-api-key", "a1bb7da0c3bf6f34b18b73d421e39d805283725bb7d994e18d1e88ce12f2ba61")

		
		// resp, err := client.Post(API_ENDPOINT, "application/json", bytes.NewBuffer(jsonData))
		resp, err := client.Do(req)
		if err != nil {
			if retryCount == MAX_RETRIES {
				errors <- fmt.Errorf("request error after %d retries: %v", MAX_RETRIES, err)
				return
			}
			retryCount++
			time.Sleep(time.Duration(backoff) * time.Millisecond)
			backoff *= 2 // Exponential backoff
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusAccepted {
			duration := time.Since(start)
			results <- duration
			return
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusInternalServerError {
			if retryCount == MAX_RETRIES {
				errors <- fmt.Errorf("unexpected status after %d retries: %d", MAX_RETRIES, resp.StatusCode)
				return
			}
			retryCount++
			time.Sleep(time.Duration(backoff) * time.Millisecond)
			backoff *= 2 // Exponential backoff
			continue
		}

		errors <- fmt.Errorf("unexpected status: %d", resp.StatusCode)
		return
	}
}

func SeedEventsClickhouse() {
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
}
