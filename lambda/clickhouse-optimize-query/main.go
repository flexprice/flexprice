package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/joho/godotenv"
)

// Config holds ClickHouse connection configuration
type Config struct {
	Host     string
	Port     int
	User     string
	Password string
	Database string
	Secure   bool
	Tables   []string
}

// Response represents the Lambda response
type Response struct {
	StatusCode int    `json:"statusCode"`
	Body       string `json:"body"`
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getConfig() Config {
	port := 9000
	if p := os.Getenv("FLEXPRICE_CLICKHOUSE_PORT"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil {
			port = parsed
		}
	}

	tablesStr := getEnvOrDefault("FLEXPRICE_CLICKHOUSE_OPTIMIZE_TABLES", "flexprice.feature_usage")
	tables := strings.Split(tablesStr, ",")

	return Config{
		Host:     getEnvOrDefault("FLEXPRICE_CLICKHOUSE_HOST", ""),
		Port:     port,
		User:     getEnvOrDefault("FLEXPRICE_CLICKHOUSE_USER", "default"),
		Password: getEnvOrDefault("FLEXPRICE_CLICKHOUSE_PASSWORD", ""),
		Database: getEnvOrDefault("FLEXPRICE_CLICKHOUSE_DATABASE", "flexprice"),
		Secure:   strings.ToLower(getEnvOrDefault("FLEXPRICE_CLICKHOUSE_SECURE", "false")) == "true",
		Tables:   tables,
	}
}

func getConnection(cfg Config) (driver.Conn, error) {
	protocol := clickhouse.Native
	if cfg.Secure {
		protocol = clickhouse.Native
	}

	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)},
		Auth: clickhouse.Auth{
			Database: cfg.Database,
			Username: cfg.User,
			Password: cfg.Password,
		},
		Protocol: protocol,
		Settings: clickhouse.Settings{
			"max_execution_time": 600,
			"receive_timeout":    600, // Add this
			"send_timeout":       600, // Add this
		},
		TLS: nil, // Set to &tls.Config{} if cfg.Secure is true
	})

	return conn, err
}

func handleRequest(ctx context.Context) (Response, error) {
	cfg := getConfig()

	log.Printf("Attempting to connect to ClickHouse at %s:%d (secure=%v)", cfg.Host, cfg.Port, cfg.Secure)

	conn, err := getConnection(cfg)
	if err != nil {
		log.Printf("Failed to connect to ClickHouse: %v", err)
		log.Printf("Connection details - Host: %s, Port: %d, Secure: %v, Database: %s",
			cfg.Host, cfg.Port, cfg.Secure, cfg.Database)
		return Response{StatusCode: 500, Body: err.Error()}, err
	}
	defer conn.Close()

	// Test the connection
	var result uint8
	if err := conn.QueryRow(ctx, "SELECT 1").Scan(&result); err != nil {
		log.Printf("Connection test failed: %v", err)
		return Response{StatusCode: 500, Body: err.Error()}, err
	}
	log.Printf("Connection test result: %d", result)

	// Let ClickHouse compute the right partition based on your PARTITION BY toYYYYMM(timestamp)
	//  - Current month
	//  - Previous month
	partitionExprs := []string{
		"toYYYYMM(today())",
		"toYYYYMM(addMonths(today(), -1))",
	}

	for _, rawTable := range cfg.Tables {
		table := strings.TrimSpace(rawTable)
		if table == "" {
			continue
		}

		for _, partExpr := range partitionExprs {
			// ClickHouse requires tuple() wrapper for partition expressions
			sql := fmt.Sprintf("OPTIMIZE TABLE %s PARTITION tuple(%s) FINAL", table, partExpr)

			// optimize_skip_merged_partitions avoids redoing work if the partition
			// is already in a single merged part.
			if err := conn.Exec(ctx, sql); err != nil {
				log.Printf("Failed to optimize %s, partition %s: %v", table, partExpr, err)
				return Response{StatusCode: 500, Body: err.Error()}, err
			}

			log.Printf("OPTIMIZE result for %s, partition %s: success", table, partExpr)
		}
	}

	return Response{StatusCode: 200, Body: "OK"}, nil
}

func main() {
	// Load .env file if running locally
	if os.Getenv("AWS_LAMBDA_FUNCTION_NAME") == "" {
		// Try to load .env file (ignore error if not found)
		if err := godotenv.Load(); err != nil {
			log.Println("No .env file found, using system environment variables")
		} else {
			log.Println("Loaded .env file")
		}

		log.Println("Running locally...")
		response, err := handleRequest(context.Background())
		if err != nil {
			log.Fatalf("Error: %v", err)
		}
		log.Printf("Result: %+v", response)
		return
	}

	// Running in Lambda
	lambda.Start(handleRequest)
}
