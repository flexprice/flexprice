package output

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	flexprice "github.com/flexprice/go-sdk"
	"github.com/warpstreamlabs/bento/public/service"
)

// flexpriceConfig holds the configuration for the Flexprice output plugin
type flexpriceConfig struct {
	APIHost string
	APIKey  string
	Scheme  string
}

// flexpriceConfigSpec returns the configuration spec for the Flexprice output
func flexpriceConfigSpec() *service.ConfigSpec {
	return service.NewConfigSpec().
		Summary("Send events to Flexprice using the official Go SDK").
		Description("This output plugin uses the Flexprice Go SDK to send events to the Flexprice API. " +
			"It supports both single and bulk event ingestion with automatic batching, retries and error handling.").
		Field(service.NewStringField("api_host").
			Description("The host of the Flexprice API (e.g., api.cloud.flexprice.io)").
			Default("api.cloud.flexprice.io")).
		Field(service.NewStringField("api_key").
			Description("Your Flexprice API key for authentication").
			Secret().
			Default("")).
		Field(service.NewStringField("scheme").
			Description("The scheme for the API (http or https)").
			Default("https")).
		Field(service.NewBatchPolicyField("batching")).
		Field(service.NewOutputMaxInFlightField().Default(10))
}

// init registers the Flexprice output plugin with Bento as a batch output
func init() {
	err := service.RegisterBatchOutput(
		"flexprice",
		flexpriceConfigSpec(),
		func(conf *service.ParsedConfig, mgr *service.Resources) (
			output service.BatchOutput,
			batchPolicy service.BatchPolicy,
			maxInFlight int,
			err error,
		) {
			// Parse batch policy
			if batchPolicy, err = conf.FieldBatchPolicy("batching"); err != nil {
				return
			}

			// Parse max in flight
			if maxInFlight, err = conf.FieldInt("max_in_flight"); err != nil {
				return
			}

			// Parse configuration
			apiHost, err := conf.FieldString("api_host")
			if err != nil {
				return nil, batchPolicy, 0, err
			}
			apiKey, err := conf.FieldString("api_key")
			if err != nil {
				return nil, batchPolicy, 0, err
			}
			scheme, err := conf.FieldString("scheme")
			if err != nil {
				return nil, batchPolicy, 0, err
			}

			if apiKey == "" {
				return nil, batchPolicy, 0, fmt.Errorf("api_key is required")
			}

			// Initialize Flexprice SDK client
			config := flexprice.NewConfiguration()
			config.Scheme = scheme
			config.Host = apiHost
			config.AddDefaultHeader("x-api-key", apiKey)

			client := flexprice.NewAPIClient(config)

			output = &flexpriceOutput{
				client: client,
				logger: mgr.Logger(),
			}

			return output, batchPolicy, maxInFlight, nil
		},
	)
	if err != nil {
		panic(err)
	}
}

// flexpriceOutput implements the service.BatchOutput interface using the Flexprice Go SDK
type flexpriceOutput struct {
	client *flexprice.APIClient
	logger *service.Logger
}

// Connect implements service.BatchOutput
func (f *flexpriceOutput) Connect(ctx context.Context) error {
	f.logger.Info("Flexprice output connected and ready")
	return nil
}

// WriteBatch implements service.BatchOutput - sends events to Flexprice using the Go SDK
// This method handles both single events and bulk batches efficiently
func (f *flexpriceOutput) WriteBatch(ctx context.Context, batch service.MessageBatch) error {
	// Parse all messages into event requests
	events := make([]flexprice.DtoIngestEventRequest, 0, len(batch))

	err := batch.WalkWithBatchedErrors(func(_ int, msg *service.Message) error {
		msgBytes, err := msg.AsBytes()
		if err != nil {
			return fmt.Errorf("failed to read message: %w", err)
		}

		var eventRequest flexprice.DtoIngestEventRequest
		if err := json.Unmarshal(msgBytes, &eventRequest); err != nil {
			f.logger.Errorf("Failed to parse message as DtoIngestEventRequest: %v", err)
			return fmt.Errorf("failed to parse message: %w", err)
		}

		// Validate required fields
		if eventRequest.EventName == "" {
			return fmt.Errorf("event_name is required")
		}
		if eventRequest.ExternalCustomerId == "" {
			return fmt.Errorf("external_customer_id is required")
		}

		// Set default timestamp if not provided
		if eventRequest.Timestamp == nil || *eventRequest.Timestamp == "" {
			now := time.Now().Format(time.RFC3339)
			eventRequest.Timestamp = &now
		}

		events = append(events, eventRequest)
		return nil
	})

	if err != nil {
		// Return batch errors - Bento will handle retrying failed messages
		return err
	}

	// No events to send (all failed validation)
	if len(events) == 0 {
		return fmt.Errorf("no valid events in batch")
	}

	// Use bulk endpoint if we have multiple events, single endpoint for one event
	if len(events) == 1 {
		return f.sendSingleEvent(ctx, events[0])
	}

	return f.sendBulkEvents(ctx, events)
}

// sendSingleEvent sends a single event using the single event API
func (f *flexpriceOutput) sendSingleEvent(ctx context.Context, event flexprice.DtoIngestEventRequest) error {
	f.logger.Infof("📤 Sending event: %s for customer: %s", event.EventName, event.ExternalCustomerId)

	result, httpResp, err := f.client.EventsAPI.EventsPost(ctx).
		Event(event).
		Execute()

	if err != nil {
		return f.handleAPIError(err, httpResp, "single event")
	}

	// Check for successful response
	if httpResp.StatusCode == 202 {
		if result != nil {
			if eventID, ok := result["event_id"]; ok {
				f.logger.Infof("✅ Event accepted successfully, ID: %v", eventID)
			} else {
				f.logger.Info("✅ Event accepted successfully")
			}
		}
		return nil
	}

	// Unexpected status code
	f.logger.Warnf("Unexpected status code: %d", httpResp.StatusCode)
	return fmt.Errorf("unexpected status code: %d", httpResp.StatusCode)
}

// sendBulkEvents sends multiple events using the bulk API
func (f *flexpriceOutput) sendBulkEvents(ctx context.Context, events []flexprice.DtoIngestEventRequest) error {
	f.logger.Infof("📦 Sending bulk batch: %d events", len(events))

	// Create bulk request
	bulkRequest := flexprice.NewDtoBulkIngestEventRequest(events)

	result, httpResp, err := f.client.EventsAPI.EventsBulkPost(ctx).
		Event(*bulkRequest).
		Execute()

	if err != nil {
		return f.handleAPIError(err, httpResp, fmt.Sprintf("bulk batch (%d events)", len(events)))
	}

	// Check for successful response
	if httpResp.StatusCode == 202 {
		if result != nil {
			f.logger.Infof("✅ Bulk batch accepted successfully: %d events processed", len(events))
			if msg, ok := result["message"]; ok {
				f.logger.Debugf("Response: %v", msg)
			}
		}
		return nil
	}

	// Unexpected status code
	f.logger.Warnf("Unexpected status code for bulk batch: %d", httpResp.StatusCode)
	return fmt.Errorf("unexpected status code: %d", httpResp.StatusCode)
}

// handleAPIError processes API errors and determines retry behavior
func (f *flexpriceOutput) handleAPIError(err error, httpResp *http.Response, operation string) error {
	f.logger.Errorf("Failed to send %s: %v", operation, err)

	// Check HTTP status code for retry logic
	if httpResp != nil {
		switch httpResp.StatusCode {
		case 400:
			// Bad request - don't retry
			f.logger.Errorf("Bad request (400) for %s - check event format", operation)
			return fmt.Errorf("bad request (400): %w", err)
		case 401, 403:
			// Authentication error - fail fast
			f.logger.Errorf("Authentication failed (%d) for %s", httpResp.StatusCode, operation)
			return fmt.Errorf("authentication failed (%d): %w", httpResp.StatusCode, err)
		case 429:
			// Rate limited - retry
			f.logger.Warnf("Rate limited (429) for %s, will retry", operation)
			return fmt.Errorf("rate limited (429): %w", err)
		case 500, 502, 503, 504:
			// Server error - retry
			f.logger.Errorf("Server error (%d) for %s, will retry", httpResp.StatusCode, operation)
			return fmt.Errorf("server error (%d): %w", httpResp.StatusCode, err)
		}
	}

	// Unknown error - retry
	return fmt.Errorf("failed to send %s: %w", operation, err)
}

// Close implements service.BatchOutput
func (f *flexpriceOutput) Close(ctx context.Context) error {
	f.logger.Info("Closing Flexprice output")
	return nil
}
