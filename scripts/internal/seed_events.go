package internal

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/repository"
	"github.com/flexprice/flexprice/internal/sentry"
	"github.com/flexprice/flexprice/internal/types"
	"golang.org/x/time/rate"
)

// EventGenerator holds the configuration for generating events
type EventGenerator struct {
	meters      []*meter.Meter
	customerIDs []string
	logger      *logger.Logger
}

// NewEventGenerator creates a new event generator with the given meters
func NewEventGenerator(meters []*meter.Meter, customers []*customer.Customer, logger *logger.Logger) *EventGenerator {
	// Sample customer IDs - replace with actual customer IDs from your system
	customerIDs := []string{
		"cus_01HKG8QWERTY123",
		"cus_02HKG8ASDFGH456",
		"cus_03HKG8ZXCVBN789",
	}

	if len(customers) > 0 {
		customerIDs = make([]string, len(customers))
		for i, c := range customers {
			customerIDs[i] = c.ExternalID
		}
	}

	return &EventGenerator{
		meters:      meters,
		customerIDs: customerIDs,
		logger:      logger,
	}
}

// generateEvent creates a random event based on the available meters
func (g *EventGenerator) generateEvent(index int) dto.IngestEventRequest {
	// Select a random meter
	selectedMeter := g.meters[index%len(g.meters)]

	// Generate properties based on meter configuration
	properties := make(map[string]interface{})

	// Handle properties based on meter aggregation and filters
	if selectedMeter.Aggregation.Type == types.AggregationSum ||
		selectedMeter.Aggregation.Type == types.AggregationAvg {
		// For sum/avg aggregation, we need to generate a value for the aggregation field
		if selectedMeter.Aggregation.Field != "" {
			// Generate a random value between 1 and 1000
			properties[selectedMeter.Aggregation.Field] = rand.Int63n(1000) + 1
		}
	}

	// Apply filter values if available
	for _, filter := range selectedMeter.Filters {
		if len(filter.Values) > 0 {
			// Select a random value from the filter values
			properties[filter.Key] = filter.Values[rand.Intn(len(filter.Values))]
		}
	}

	// Generate timestamp within last 72 hours
	timestamp := time.Now().Add(-time.Duration(randInt64(0, 72)) * time.Hour)

	return dto.IngestEventRequest{
		EventID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_EVENT),
		ExternalCustomerID: g.customerIDs[rand.Intn(len(g.customerIDs))],
		EventName:          selectedMeter.EventName,
		Timestamp:          timestamp,
		Properties:         properties,
		Source:             "script",
	}
}

// SeedEventsFromMeters seeds events data based on existing meters
func SeedEventsFromMeters() error {
	cfg, err := config.NewConfig()
	if err != nil {
		log.Fatalf("Error creating config: %v", err)
	}

	log, err := logger.NewLogger(cfg)
	if err != nil {
		log.Fatalf("Error creating logger: %v", err)
	}

	// Fetch all meters from the repository
	ctx := context.Background()
	ctx = context.WithValue(ctx, types.CtxTenantID, "<tenant_id>")

	// Initialize the other DB
	entClient, err := postgres.NewEntClient(cfg, log)
	if err != nil {
		log.Fatalf("Failed to connect to postgres: %v", err)
	}

	client := postgres.NewClient(entClient, log, sentry.NewSentryService(cfg, log))

	// Initialize repositories
	repoParams := repository.RepositoryParams{
		EntClient: client,
		Logger:    log,
	}

	meterRepo := repository.NewMeterRepository(repoParams)
	customerRepo := repository.NewCustomerRepository(repoParams)

	meters, err := meterRepo.ListAll(ctx, types.NewNoLimitMeterFilter())
	if err != nil {
		return fmt.Errorf("failed to fetch meters: %v", err)
	}

	if len(meters) == 0 {
		return fmt.Errorf("no meters found to generate events")
	}

	customers, err := customerRepo.List(ctx, types.NewCustomerFilter())
	if err != nil {
		return fmt.Errorf("failed to fetch customers: %v", err)
	}

	log.Info("Starting event seeding...")
	log.Infof("Found %d meters to generate events for", len(meters))
	log.Infof("Sending %d events in batches of %d with rate limit of %d req/s",
		NUM_EVENTS, BATCH_SIZE, REQUESTS_PER_SEC)

	// Create event generator
	generator := NewEventGenerator(meters, customers, log)

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
			event := generator.generateEvent(i + j)
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

		log.Infof("Processed batch %d: %d/%d events in %v seconds. Last Event ID: %s",
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
		log.Errorf("Error: %v", err)
	}

	// Print results
	totalTime := time.Since(start)
	avgDuration := totalDuration / time.Duration(successCount)

	log.Info("Event seeding completed!")
	log.Info("Results:")
	log.Infof("Total Time: %v", totalTime)
	log.Infof("Successful Requests: %d", successCount)
	log.Infof("Failed Requests: %d", errorCount)
	log.Infof("Average Request Duration: %v", avgDuration)
	log.Infof("Min Request Duration: %v", minDuration)
	log.Infof("Max Request Duration: %v", maxDuration)
	log.Infof("Requests per Second: %.2f", float64(successCount)/totalTime.Seconds())

	// Print batch information
	log.Info("\nBatch Details:")
	for _, batch := range batches {
		log.Infof("Batch %d: Events=%d, Duration=%v, Last Event ID=%s",
			batch.BatchNumber,
			batch.EventCount,
			batch.EndTime.Sub(batch.StartTime),
			batch.LastEventID)
	}

	return nil
}
