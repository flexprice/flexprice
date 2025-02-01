package dynamodb

import (
	"context"
	"fmt"

	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"

	"github.com/flexprice/flexprice/internal/config"
)

type Client struct {
	db *dynamodb.Client
}

func NewClient(cfg *config.Configuration) (*Client, error) {
	if !cfg.DynamoDB.InUse {
		return nil, nil
	}

	awsCfg, err := awsConfig.LoadDefaultConfig(context.Background(),
		awsConfig.WithRegion(cfg.DynamoDB.Region),
	)
	if err != nil {
		return nil, fmt.Errorf("unable to load AWS SDK config: %w", err)
	}

	return &Client{
		db: dynamodb.NewFromConfig(awsCfg),
	}, nil
}

func (c *Client) DB() *dynamodb.Client {
	return c.db
}
