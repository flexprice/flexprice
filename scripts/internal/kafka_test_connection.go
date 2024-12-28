package internal

import (
	"fmt"
	"time"

	"github.com/Shopify/sarama"
)

func TestKafkaConnection(brokers []string, username, password string) error {
	config := sarama.NewConfig()
	config.Net.TLS.Enable = true
	config.Net.SASL.Enable = true
	config.Net.SASL.Mechanism = sarama.SASLTypePlaintext
	config.Net.SASL.User = username
	config.Net.SASL.Password = password
	config.Version = sarama.V2_8_0_0

	// Add timeouts
	config.Net.DialTimeout = 10 * time.Second
	config.Net.ReadTimeout = 10 * time.Second
	config.Net.WriteTimeout = 10 * time.Second

	// Create client
	client, err := sarama.NewClient(brokers, config)
	if err != nil {
		return fmt.Errorf("error creating client: %v", err)
	}
	defer client.Close()

	// List topics to test connection
	topics, err := client.Topics()
	if err != nil {
		return fmt.Errorf("error listing topics: %v", err)
	}

	fmt.Printf("Successfully connected! Available topics: %v\n", topics)
	return nil
}
