package config

// DynamoDBConfig holds configuration for DynamoDB
type DynamoDBConfig struct {
	InUse          bool   `mapstructure:"in_use" validate:"required" default:"false"`
	Region         string `mapstructure:"region"`
	EventTableName string `mapstructure:"event_table_name"`
}
