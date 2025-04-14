/*
This file is meant to be copied into the FlexPrice Go SDK.
It contains types and functions that depend on other SDK types:

- APIClient: The main client for the FlexPrice API
- DtoIngestEventRequest: The request type for creating events

DO NOT attempt to compile this file directly. It is designed to be copied
to the SDK directory by the add_go_async.sh script, where it will have
access to all required type definitions.

The linter will show errors when editing this file standalone, which is expected
and can be ignored. The file will compile correctly when copied to the SDK.
*/

// nolint
package flexprice

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Note: This file is meant to be included in the FlexPrice Go SDK
// The following types are defined in the SDK:
// - APIClient: The main client for the FlexPrice API
// - DtoIngestEventRequest: The DTO for creating events

// AsyncConfig provides configuration options for the enhanced FlexPrice client
type AsyncConfig struct {
	// BatchSize defines maximum number of events to batch before sending
	BatchSize int

	// FlushInterval defines how often the queue is flushed even if the batch size hasn't been reached
	FlushInterval time.Duration

	// MaxQueueSize is the maximum size of the queue before blocking
	MaxQueueSize int

	// MaxConcurrentRequests is the maximum number of concurrent API requests
	MaxConcurrentRequests int

	// DefaultSource is the default source to apply to events if not specified
	DefaultSource string

	// Debug enables debug logging
	Debug bool
}

// DefaultAsyncConfig returns a default configuration for the async client
func DefaultAsyncConfig() AsyncConfig {
	return AsyncConfig{
		BatchSize:             10,
		FlushInterval:         time.Millisecond * 100,
		MaxQueueSize:          1000,
		MaxConcurrentRequests: 10,
		DefaultSource:         "go-sdk",
		Debug:                 false,
	}
}

// AsyncClient provides an enhanced asynchronous client for the FlexPrice API
// It builds on top of the FlexPrice Go SDK to provide batching and asynchronous
// event sending capabilities.
type AsyncClient struct {
	apiClient     *APIClient
	config        AsyncConfig
	msgs          chan EventOptions
	quit          chan struct{}
	shutdown      chan struct{}
	wg            sync.WaitGroup
	ctx           context.Context
	debugf        func(format string, args ...interface{})
	errorCallback func(error)
}

// EventOptions provides all available options for adding an event
type EventOptions struct {
	// EventName is the name of the event (required)
	EventName string

	// ExternalCustomerID is the external customer ID (required)
	ExternalCustomerID string

	// CustomerID is the internal FlexPrice customer ID
	CustomerID string

	// EventID is a custom event ID
	EventID string

	// Properties contains event properties
	Properties map[string]interface{}

	// Source identifies the source of the event
	Source string

	// Timestamp is the event timestamp in RFC3339 format
	Timestamp string
}

// NewAsyncClient creates a new asynchronous FlexPrice client with default configuration
func (c *APIClient) NewAsyncClient() *AsyncClient {
	return c.NewAsyncClientWithConfig(DefaultAsyncConfig())
}

// NewAsyncClientWithConfig creates a new asynchronous FlexPrice client with the provided config
func (c *APIClient) NewAsyncClientWithConfig(config AsyncConfig) *AsyncClient {
	// Apply defaults for unset values
	if config.BatchSize <= 0 {
		config.BatchSize = DefaultAsyncConfig().BatchSize
	}
	if config.FlushInterval <= 0 {
		config.FlushInterval = DefaultAsyncConfig().FlushInterval
	}
	if config.MaxQueueSize <= 0 {
		config.MaxQueueSize = DefaultAsyncConfig().MaxQueueSize
	}
	if config.MaxConcurrentRequests <= 0 {
		config.MaxConcurrentRequests = DefaultAsyncConfig().MaxConcurrentRequests
	}
	if config.DefaultSource == "" {
		config.DefaultSource = DefaultAsyncConfig().DefaultSource
	}

	ac := &AsyncClient{
		apiClient: c,
		config:    config,
		msgs:      make(chan EventOptions, config.MaxQueueSize),
		quit:      make(chan struct{}),
		shutdown:  make(chan struct{}),
		ctx:       context.Background(),
		debugf: func(format string, args ...interface{}) {
			if config.Debug {
				fmt.Printf("[FlexPrice Debug] "+format+"\n", args...)
			}
		},
		errorCallback: func(err error) {
			if config.Debug {
				fmt.Printf("[FlexPrice Error] %v\n", err)
			}
		},
	}

	// Start processing loop
	go ac.loop()

	return ac
}

// Enqueue adds an event to the processing queue
func (c *AsyncClient) Enqueue(eventName, externalCustomerID string, properties map[string]interface{}) error {
	return c.EnqueueWithOptions(EventOptions{
		EventName:          eventName,
		ExternalCustomerID: externalCustomerID,
		Properties:         properties,
	})
}

// EnqueueWithOptions adds an event to the processing queue with additional options
func (c *AsyncClient) EnqueueWithOptions(opts EventOptions) error {
	if opts.EventName == "" {
		return fmt.Errorf("event name is required")
	}
	if opts.ExternalCustomerID == "" {
		return fmt.Errorf("external customer ID is required")
	}

	// Apply defaults
	if opts.Source == "" {
		opts.Source = c.config.DefaultSource
	}
	if opts.Timestamp == "" {
		opts.Timestamp = time.Now().Format(time.RFC3339)
	}

	select {
	case c.msgs <- opts:
		return nil
	case <-c.quit:
		return fmt.Errorf("client is closed")
	}
}

// Flush forces all queued events to be sent immediately
func (c *AsyncClient) Flush() error {
	c.debugf("manually flushing events")
	// Create a channel to wait for flush completion
	done := make(chan struct{})

	// Wait for processing loop to handle the flush
	go func() {
		c.wg.Wait() // Wait for all current batch processing to complete
		close(done)
	}()

	// Signal flush and wait for completion
	select {
	case c.msgs <- EventOptions{EventName: "@flush", ExternalCustomerID: "@flush"}:
		// Wait for the flush to complete with a timeout
		select {
		case <-done:
			c.debugf("flush completed")
			return nil
		case <-time.After(time.Second * 5):
			c.debugf("flush timed out after 5 seconds")
			return fmt.Errorf("flush timed out")
		}
	case <-c.quit:
		return fmt.Errorf("client is closed")
	}
}

// Close shuts down the client
func (c *AsyncClient) Close() error {
	c.debugf("closing client")
	close(c.quit)
	c.debugf("waiting for shutdown to complete")
	<-c.shutdown
	c.debugf("client shutdown complete")
	return nil
}

// Main processing loop
func (c *AsyncClient) loop() {
	defer close(c.shutdown)

	ticker := time.NewTicker(c.config.FlushInterval)
	defer ticker.Stop()

	batch := make([]EventOptions, 0, c.config.BatchSize)

	for {
		select {
		case msg := <-c.msgs:
			// Handle special flush message
			if msg.EventName == "@flush" && msg.ExternalCustomerID == "@flush" {
				c.sendBatch(batch)
				batch = batch[:0]
				continue
			}

			// Add message to batch
			batch = append(batch, msg)

			// Send batch if it reaches max size
			if len(batch) >= c.config.BatchSize {
				c.debugf("batch size reached (%d), flushing", len(batch))
				c.sendBatch(batch)
				batch = batch[:0]
			}

		case <-ticker.C:
			// Send batch on ticker interval if not empty
			if len(batch) > 0 {
				c.debugf("interval triggered, flushing %d events", len(batch))
				c.sendBatch(batch)
				batch = batch[:0]
			}

		case <-c.quit:
			c.debugf("shutdown requested, flushing remaining %d events", len(batch))
			// Flush remaining events on shutdown
			if len(batch) > 0 {
				c.sendBatch(batch)
			}
			return
		}
	}
}

// Send a batch of events to the API
func (c *AsyncClient) sendBatch(batch []EventOptions) {
	if len(batch) == 0 {
		return
	}

	c.debugf("Sending batch of %d events", len(batch))
	c.wg.Add(1)
	go func(events []EventOptions) {
		defer c.wg.Done()

		for _, event := range events {
			// Create the event request
			req := DtoIngestEventRequest{
				EventName:          event.EventName,
				ExternalCustomerId: event.ExternalCustomerID,
			}

			if event.CustomerID != "" {
				req.SetCustomerId(event.CustomerID)
			}

			if event.EventID != "" {
				req.SetEventId(event.EventID)
			}

			if event.Source != "" {
				req.SetSource(event.Source)
			}

			if event.Timestamp != "" {
				req.SetTimestamp(event.Timestamp)
			}

			if event.Properties != nil {
				// Convert map[string]interface{} to map[string]string for the API call
				strProperties := make(map[string]string)
				for k, v := range event.Properties {
					strProperties[k] = fmt.Sprintf("%v", v)
				}
				req.SetProperties(strProperties)
			}

			// Debug the request
			c.debugf("Sending event: %s %s", event.EventName, event.EventID)

			// Send to API
			_, resp, err := c.apiClient.EventsAPI.EventsPost(c.ctx).Event(req).Execute()

			if err != nil {
				c.errorCallback(fmt.Errorf("error sending event: %v", err))
				continue
			}

			// Check response status code
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				c.errorCallback(fmt.Errorf("unexpected status code: %d", resp.StatusCode))
				continue
			}

			c.debugf("event sent successfully: event_name=%s, id=%s", event.EventName, event.EventID)
		}
		c.debugf("Batch processing complete")
	}(append([]EventOptions{}, batch...)) // Create a copy of the batch to avoid race conditions
}
