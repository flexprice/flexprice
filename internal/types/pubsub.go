package types

// PubSubType represents the type of pubsub system
type PubSubType string

const (
	MemoryPubSub PubSubType = "memory"
	KafkaPubSub  PubSubType = "kafka"
)
