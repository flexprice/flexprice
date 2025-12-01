package output

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/benthosdev/benthos/v4/public/service"
	flexprice "github.com/flexprice/go-sdk"
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
			"It supports single event ingestion with automatic retries and error handling.").
		Field(service.NewStringField("api_host").
			Description("The host of the Flexprice API (e.g., api.cloud.flexprice.io)").
			Default("api.cloud.flexprice.io")).
		Field(service.NewStringField("api_key").
			Description("Your Flexprice API key for authentication").
			Secret().
			Default("")).
		Field(service.NewStringField("scheme").
			Description("The scheme for the API (http or https)").
			Default("https"))
}

// init registers the Flexprice output plugin with Benthos
func init() {
	err := service.RegisterOutput(
		"flexprice",
		flexpriceConfigSpec(),
		func(conf *service.ParsedConfig, mgr *service.Resources) (service.Output, int, error) {
			// Parse configuration
			apiHost, err := conf.FieldString("api_host")
			if err != nil {
				return nil, 0, err
			}
			apiKey, err := conf.FieldString("api_key")
			if err != nil {
				return nil, 0, err
			}
			scheme, err := conf.FieldString("scheme")
			if err != nil {
				return nil, 0, err
			}

			if apiKey == "" {
				return nil, 0, fmt.Errorf("api_key is required")
			}

			// Initialize Flexprice SDK client
			config := flexprice.NewConfiguration()
			config.Scheme = scheme
			config.Host = apiHost
			config.AddDefaultHeader("x-api-key", apiKey)

			client := flexprice.NewAPIClient(config)

			output := &flexpriceOutput{
				client: client,
				logger: mgr.Logger(),
			}

			// Return output with maxInFlight of 10 for better throughput
			return output, 10, nil
		},
	)
	if err != nil {
		panic(err)
	}
}

// flexpriceOutput implements the service.Output interface using the Flexprice Go SDK
type flexpriceOutput struct {
	client *flexprice.APIClient
	logger *service.Logger
}

// Connect implements service.Output
func (f *flexpriceOutput) Connect(ctx context.Context) error {
	f.logger.Info("Flexprice output connected and ready")
	return nil
}

// Write implements service.Output - sends events to Flexprice using the Go SDK
func (f *flexpriceOutput) Write(ctx context.Context, msg *service.Message) error {
	// Parse message into Flexprice DtoIngestEventRequest
	var eventRequest flexprice.DtoIngestEventRequest

	// Convert structured data to event request
	// This handles the JSON unmarshaling internally
	msgBytes, err := msg.AsBytes()
	if err != nil {
		return fmt.Errorf("failed to read message: %w", err)
	}

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

	// Send event using Flexprice Go SDK
	f.logger.Infof("📤 Sending event: %s for customer: %s", eventRequest.EventName, eventRequest.ExternalCustomerId)

	result, httpResp, err := f.client.EventsAPI.EventsPost(ctx).
		Event(eventRequest).
		Execute()

	if err != nil {
		f.logger.Errorf("Failed to send event: %v", err)

		// Check HTTP status code for retry logic
		if httpResp != nil {
			switch httpResp.StatusCode {
			case 400:
				// Bad request - don't retry
				return fmt.Errorf("bad request (400): %w", err)
			case 401, 403:
				// Authentication error - fail fast
				return fmt.Errorf("authentication failed (%d): %w", httpResp.StatusCode, err)
			case 429:
				// Rate limited - retry
				f.logger.Warnf("Rate limited (429), will retry")
				return fmt.Errorf("rate limited (429): %w", err)
			case 500, 502, 503, 504:
				// Server error - retry
				f.logger.Errorf("Server error (%d), will retry", httpResp.StatusCode)
				return fmt.Errorf("server error (%d): %w", httpResp.StatusCode, err)
			}
		}

		// Unknown error - retry
		return fmt.Errorf("failed to send event: %w", err)
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

// Close implements service.Output
func (f *flexpriceOutput) Close(ctx context.Context) error {
	f.logger.Info("Closing Flexprice output")
	return nil
}
