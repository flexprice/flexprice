package kafka

import (
	"context"
	"fmt"

	"github.com/Shopify/sarama"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
)

// ConsumerLag represents the lag for a consumer group on a topic
type ConsumerLag struct {
	Topic         string
	ConsumerGroup string
	TotalLag      int64
	PartitionLags map[int32]int64
}

// MonitoringService provides Kafka monitoring capabilities
type MonitoringService struct {
	config *config.Configuration
	logger *logger.Logger
}

// NewMonitoringService creates a new Kafka monitoring service
func NewMonitoringService(cfg *config.Configuration, log *logger.Logger) *MonitoringService {
	return &MonitoringService{
		config: cfg,
		logger: log,
	}
}

// GetConsumerLag calculates the consumer lag for a given topic and consumer group
func (m *MonitoringService) GetConsumerLag(ctx context.Context, topic string, consumerGroup string) (*ConsumerLag, error) {
	// Create Sarama config
	saramaConfig := GetSaramaConfig(m.config)
	saramaConfig.Consumer.Return.Errors = true

	// Create cluster admin client
	admin, err := sarama.NewClusterAdmin(m.config.Kafka.Brokers, saramaConfig)
	if err != nil {
		m.logger.Errorw("failed to create Kafka admin client",
			"error", err,
			"brokers", m.config.Kafka.Brokers)
		return nil, fmt.Errorf("failed to create Kafka admin client: %w", err)
	}
	defer admin.Close()

	// Create client to get partition offsets
	client, err := sarama.NewClient(m.config.Kafka.Brokers, saramaConfig)
	if err != nil {
		m.logger.Errorw("failed to create Kafka client",
			"error", err,
			"brokers", m.config.Kafka.Brokers)
		return nil, fmt.Errorf("failed to create Kafka client: %w", err)
	}
	defer client.Close()

	// Get partitions for the topic
	partitions, err := client.Partitions(topic)
	if err != nil {
		m.logger.Errorw("failed to get partitions for topic",
			"error", err,
			"topic", topic)
		return nil, fmt.Errorf("failed to get partitions for topic %s: %w", topic, err)
	}

	m.logger.Infow("fetching consumer group offsets",
		"topic", topic,
		"consumer_group", consumerGroup,
		"partitions", partitions)

	// Use ClusterAdmin to fetch consumer group offsets - THIS IS THE CORRECT WAY
	offsetFetchResponse, err := admin.ListConsumerGroupOffsets(consumerGroup, map[string][]int32{
		topic: partitions,
	})
	if err != nil {
		m.logger.Errorw("failed to list consumer group offsets",
			"error", err,
			"consumer_group", consumerGroup,
			"topic", topic)
		return nil, fmt.Errorf("failed to list consumer group offsets: %w", err)
	}

	// Calculate lag for each partition
	consumerLag := &ConsumerLag{
		Topic:         topic,
		ConsumerGroup: consumerGroup,
		TotalLag:      0,
		PartitionLags: make(map[int32]int64),
	}

	for _, partition := range partitions {
		// Get the latest offset (high water mark) for this partition
		latestOffset, err := client.GetOffset(topic, partition, sarama.OffsetNewest)
		if err != nil {
			m.logger.Warnw("failed to get latest offset for partition",
				"error", err,
				"topic", topic,
				"partition", partition)
			continue
		}

		// Get consumer's current offset for this partition from the response
		block := offsetFetchResponse.GetBlock(topic, partition)
		if block == nil {
			m.logger.Warnw("no offset block found for partition",
				"topic", topic,
				"partition", partition,
				"consumer_group", consumerGroup)
			// If no block found, assume consumer hasn't started - lag = all messages
			partitionLag := latestOffset
			if partitionLag < 0 {
				partitionLag = 0
			}
			consumerLag.PartitionLags[partition] = partitionLag
			consumerLag.TotalLag += partitionLag
			continue
		}

		consumerOffset := block.Offset

		m.logger.Debugw("partition offset details",
			"topic", topic,
			"partition", partition,
			"consumer_group", consumerGroup,
			"latest_offset", latestOffset,
			"consumer_offset", consumerOffset,
			"offset_metadata", block.Metadata)

		// If consumer offset is -1, it means no offset has been committed yet
		// In this case, the lag is the total number of messages in the partition
		if consumerOffset == -1 {
			partitionLag := latestOffset
			if partitionLag < 0 {
				partitionLag = 0
			}
			consumerLag.PartitionLags[partition] = partitionLag
			consumerLag.TotalLag += partitionLag

			m.logger.Infow("partition has no committed offset",
				"topic", topic,
				"partition", partition,
				"consumer_group", consumerGroup,
				"lag", partitionLag)
			continue
		}

		// Calculate lag for this partition
		// Lag = Latest Offset - Consumer's Committed Offset
		partitionLag := latestOffset - consumerOffset
		if partitionLag < 0 {
			partitionLag = 0
		}

		consumerLag.PartitionLags[partition] = partitionLag
		consumerLag.TotalLag += partitionLag

		m.logger.Infow("partition lag calculated",
			"topic", topic,
			"partition", partition,
			"consumer_group", consumerGroup,
			"latest_offset", latestOffset,
			"consumer_offset", consumerOffset,
			"lag", partitionLag)
	}

	m.logger.Infow("total consumer lag calculated",
		"topic", topic,
		"consumer_group", consumerGroup,
		"total_lag", consumerLag.TotalLag,
		"partitions", len(partitions),
		"partition_details", consumerLag.PartitionLags)

	return consumerLag, nil
}

// GetMultipleConsumerLags fetches lag for multiple topic/consumer-group pairs
func (m *MonitoringService) GetMultipleConsumerLags(ctx context.Context, configs []struct {
	Topic         string
	ConsumerGroup string
}) (map[string]*ConsumerLag, error) {
	results := make(map[string]*ConsumerLag)

	for _, cfg := range configs {
		key := fmt.Sprintf("%s:%s", cfg.Topic, cfg.ConsumerGroup)
		lag, err := m.GetConsumerLag(ctx, cfg.Topic, cfg.ConsumerGroup)
		if err != nil {
			m.logger.Warnw("failed to get consumer lag",
				"error", err,
				"topic", cfg.Topic,
				"consumer_group", cfg.ConsumerGroup)
			// Store zero lag on error to avoid blocking the entire response
			results[key] = &ConsumerLag{
				Topic:         cfg.Topic,
				ConsumerGroup: cfg.ConsumerGroup,
				TotalLag:      0,
				PartitionLags: make(map[int32]int64),
			}
			continue
		}
		results[key] = lag
	}

	return results, nil
}
