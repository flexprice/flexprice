package dynamodb

import (
	"context"

	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"go.uber.org/zap"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/logger"
)

type EventPublisher struct {
	client    *Client
	tableName string
	logger    *logger.Logger
}

func NewEventPublisher(client *Client, cfg *config.Configuration, logger *logger.Logger) *EventPublisher {
	return &EventPublisher{
		client:    client,
		tableName: cfg.DynamoDB.EventTableName,
		logger:    logger,
	}
}

type DynamoEvent struct {
	PK                 string                 `dynamodbav:"pk"` // TenantID
	SK                 string                 `dynamodbav:"sk"` // EventID
	EnvironmentID      string                 `dynamodbav:"environment_id"`
	EventName          string                 `dynamodbav:"event_name"`
	Properties         map[string]interface{} `dynamodbav:"properties"`
	Timestamp          time.Time              `dynamodbav:"timestamp"`
	Source             string                 `dynamodbav:"source"`
	IngestedAt         time.Time              `dynamodbav:"ingested_at"`
	CustomerID         string                 `dynamodbav:"customer_id"`
	ExternalCustomerID string                 `dynamodbav:"external_customer_id"`
}

func (p *EventPublisher) Publish(ctx context.Context, event *events.Event) error {
	dynamoEvent := &DynamoEvent{
		PK:                 event.TenantID,
		SK:                 event.ID,
		EventName:          event.EventName,
		Properties:         event.Properties,
		Timestamp:          event.Timestamp,
		Source:             event.Source,
		EnvironmentID:      event.EnvironmentID,
		IngestedAt:         time.Now(),
		CustomerID:         event.CustomerID,
		ExternalCustomerID: event.ExternalCustomerID,
	}

	item, err := attributevalue.MarshalMap(dynamoEvent)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to marshal event").
			Mark(ierr.ErrValidation)
	}

	input := &dynamodb.PutItemInput{
		TableName: aws.String(p.tableName),
		Item:      item,
	}

	p.logger.With(
		zap.String("event_id", event.ID),
		zap.String("tenant_id", event.TenantID),
		zap.String("event_name", event.EventName),
		zap.String("environment_id", event.EnvironmentID),
	).Debug("publishing event to dynamodb")

	_, err = p.client.db.PutItem(ctx, input)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to put item in dynamodb").
			Mark(ierr.ErrValidation)
	}

	return nil
}
