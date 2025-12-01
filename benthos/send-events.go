// Kafka Producer - Send Test Events
// Build: go build -o send-events send-events.go
// Run: ./send-events 10

package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/sasl/plain"
)

type Event struct {
	EventName          string                 `json:"event_name"`
	ExternalCustomerID string                 `json:"external_customer_id"`
	CustomerID         string                 `json:"customer_id,omitempty"`
	Properties         map[string]interface{} `json:"properties"`
	Source             string                 `json:"source"`
	Timestamp          string                 `json:"timestamp"`
	EventID            string                 `json:"event_id,omitempty"`
}

func main() {
	// Get config from environment
	brokers := os.Getenv("FLEXPRICE_KAFKA_BROKERS")
	user := os.Getenv("FLEXPRICE_KAFKA_SASL_USER")
	password := os.Getenv("FLEXPRICE_KAFKA_SASL_PASSWORD")
	topic := "benthos-testing"

	if brokers == "" || user == "" || password == "" {
		log.Fatal("❌ Missing required environment variables:\n" +
			"   FLEXPRICE_KAFKA_BROKERS\n" +
			"   FLEXPRICE_KAFKA_SASL_USER\n" +
			"   FLEXPRICE_KAFKA_SASL_PASSWORD")
	}

	// Get number of events from args
	numEvents := 10
	if len(os.Args) > 1 {
		if n, err := strconv.Atoi(os.Args[1]); err == nil {
			numEvents = n
		}
	}

	fmt.Printf("🚀 Sending %d events to Kafka topic: %s\n", numEvents, topic)
	fmt.Printf("📡 Broker: %s\n\n", brokers)

	// Create SASL mechanism
	mechanism := plain.Mechanism{
		Username: user,
		Password: password,
	}

	// Create Kafka writer with TLS and SASL
	dialer := &kafka.Dialer{
		Timeout:       10 * time.Second,
		DualStack:     true,
		SASLMechanism: mechanism,
		TLS:           &tls.Config{},
	}

	writer := kafka.NewWriter(kafka.WriterConfig{
		Brokers:  []string{brokers},
		Topic:    topic,
		Balancer: &kafka.LeastBytes{},
		Dialer:   dialer,
	})
	defer writer.Close()

	// Send test events
	sent := 0
	failed := 0

	for i := 0; i < numEvents; i++ {
		event := Event{
			EventName:          []string{"api_request", "model_usage", "storage_usage", "feature_usage"}[i%4],
			ExternalCustomerID: fmt.Sprintf("test-customer-%d", i%3),
			CustomerID:         fmt.Sprintf("test-customer-%d", i%3),
			Properties: map[string]interface{}{
				"endpoint": "/api/users",
				"method":   "GET",
				"status":   "200", // String instead of number
				"test_run": "benthos-staging-test",
				"sequence": fmt.Sprintf("%d", i), // String instead of number
			},
			Source:    "manual-test-producer",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			EventID:   fmt.Sprintf("test-%d", time.Now().UnixMilli()),
		}

		// Marshal to JSON
		eventJSON, err := json.Marshal(event)
		if err != nil {
			log.Printf("❌ Failed to marshal event %d: %v", i+1, err)
			failed++
			continue
		}

		// Send to Kafka
		err = writer.WriteMessages(context.Background(), kafka.Message{
			Key:   []byte(event.ExternalCustomerID),
			Value: eventJSON,
		})

		if err != nil {
			log.Printf("❌ Failed to send event %d: %v", i+1, err)
			failed++
		} else {
			fmt.Printf("✅ [%d/%d] Sent: %s for customer %s\n",
				i+1, numEvents, event.EventName, event.ExternalCustomerID)
			sent++
		}

		time.Sleep(200 * time.Millisecond)
	}

	fmt.Printf("\n📊 Summary:\n")
	fmt.Printf("   ✅ Sent: %d\n", sent)
	fmt.Printf("   ❌ Failed: %d\n", failed)
	fmt.Printf("\n✅ Done! Benthos should now process these events.\n")
}
