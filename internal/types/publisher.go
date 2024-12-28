package types

// PublishDestination determines where to publish events
type PublishDestination string

const (
	PublishToKafka    PublishDestination = "kafka"
	PublishToDynamoDB PublishDestination = "dynamodb"
	PublishToAll      PublishDestination = "all"
)
