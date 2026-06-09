package s3

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/flexprice/flexprice/internal/domain/connection"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/security"
	"github.com/flexprice/flexprice/internal/types"
)

// Client represents an S3 integration client
type Client struct {
	connectionRepo    connection.Repository
	encryptionService security.EncryptionService
	logger            *logger.Logger
}

// NewClient creates a new S3 client
func NewClient(
	connectionRepo connection.Repository,
	encryptionService security.EncryptionService,
	logger *logger.Logger,
) *Client {
	return &Client{
		connectionRepo:    connectionRepo,
		encryptionService: encryptionService,
		logger:            logger,
	}
}

// S3Config holds decrypted S3 configuration
type S3Config struct {
	AWSAccessKeyID     string
	AWSSecretAccessKey string
	AWSSessionToken    string // Optional: for temporary credentials
	Bucket             string
	Region             string
	KeyPrefix          string
	Compression        types.S3CompressionType
	Encryption         types.S3EncryptionType
	EndpointURL        string
	VirtualHostStyle   bool
	MaxFileSizeMB      int
}

// GetS3Client returns a configured S3 client with the provided job config and connection ID
func (c *Client) GetS3Client(ctx context.Context, jobConfig *types.S3JobConfig, connectionID string) (*s3Client, *S3Config, error) {
	var conn *connection.Connection
	var err error

	// Connection ID is mandatory — S3 supports multiple connections per environment
	if connectionID == "" {
		return nil, nil, ierr.NewError("connection ID is required for S3").
			WithHint("Provide a connection_id when using S3; multiple S3 connections are supported per environment").
			Mark(ierr.ErrValidation)
	}

	conn, err = c.connectionRepo.Get(ctx, connectionID)
	if err != nil {
		return nil, nil, ierr.NewError("failed to get S3 connection by ID").
			WithHintf("Connection ID '%s' not found", connectionID).
			Mark(ierr.ErrNotFound)
	}

	s3Config, err := c.GetDecryptedS3Config(conn, jobConfig)
	if err != nil {
		return nil, nil, ierr.NewError("failed to get S3 configuration").
			WithHint("Invalid S3 configuration").
			Mark(ierr.ErrValidation)
	}

	// Create AWS config with explicit credentials
	// Supports both:
	// 1. Temporary credentials (ASIA keys) with session token
	// 2. Permanent credentials (AKIA keys) with access key + secret key only
	var credProvider aws.CredentialsProvider
	if s3Config.AWSSessionToken != "" {
		// Use temporary credentials with session token
		c.logger.Info(ctx, "using temporary AWS credentials with session token")
		credProvider = credentials.NewStaticCredentialsProvider(
			s3Config.AWSAccessKeyID,
			s3Config.AWSSecretAccessKey,
			s3Config.AWSSessionToken,
		)
	} else {
		// Use permanent credentials (access key + secret key only)
		c.logger.Info(ctx, "using permanent AWS credentials (access key + secret key)")
		credProvider = credentials.NewStaticCredentialsProvider(
			s3Config.AWSAccessKeyID,
			s3Config.AWSSecretAccessKey,
			"", // empty session token for permanent credentials
		)
	}

	awsCfg, err := awsConfig.LoadDefaultConfig(ctx,
		awsConfig.WithRegion(s3Config.Region),
		awsConfig.WithCredentialsProvider(credProvider),
	)
	if err != nil {
		return nil, nil, ierr.WithError(err).
			WithHint("failed to load AWS config").
			Mark(ierr.ErrHTTPClient)
	}

	// Create S3 client options
	s3Options := []func(*s3.Options){
		func(o *s3.Options) {
			o.Region = s3Config.Region
			// Configure custom endpoint if provided (for MinIO or other S3-compatible services)
			if s3Config.EndpointURL != "" {
				o.BaseEndpoint = aws.String(s3Config.EndpointURL)
				c.logger.Info(ctx, "configuring custom S3 endpoint", "endpoint_url", s3Config.EndpointURL)
			}
			// Use path-style addressing if VirtualHostStyle is false (required for MinIO)
			// Path-style: http://endpoint/bucket/key
			// Virtual-hosted-style: http://bucket.endpoint/key
			if !s3Config.VirtualHostStyle {
				o.UsePathStyle = true
				c.logger.Info(ctx, "using path-style S3 addressing")
			}
		},
	}

	// Create S3 client
	awsS3Client := s3.NewFromConfig(awsCfg, s3Options...)

	c.logger.Info(ctx, "S3 client created successfully",
		"bucket", s3Config.Bucket,
		"region", s3Config.Region,
		"key_prefix", s3Config.KeyPrefix,
		"endpoint_url", s3Config.EndpointURL,
		"use_path_style", !s3Config.VirtualHostStyle,
		"credential_type", map[bool]string{true: "temporary", false: "permanent"}[s3Config.AWSSessionToken != ""],
	)

	return &s3Client{
		s3Client: awsS3Client,
		config:   s3Config,
		logger:   c.logger,
	}, s3Config, nil
}

// GetDecryptedS3Config decrypts credentials and combines with job configuration
func (c *Client) GetDecryptedS3Config(conn *connection.Connection, jobConfig *types.S3JobConfig) (*S3Config, error) {
	// Check if we have S3 credentials
	if conn.EncryptedSecretData.S3 == nil {
		return nil, ierr.NewError("no S3 credentials found").
			WithHint("S3 credentials not configured").
			Mark(ierr.ErrValidation)
	}

	// Decrypt credentials
	c.logger.Info(context.Background(), "Decrypting S3 credentials", "connection_id", conn.ID)

	accessKey, err := c.encryptionService.Decrypt(conn.EncryptedSecretData.S3.AWSAccessKeyID)
	if err != nil {
		c.logger.Error(context.Background(), "failed to decrypt AWS access key", "connection_id", conn.ID, "error", err)
		return nil, ierr.NewError("failed to decrypt AWS access key").Mark(ierr.ErrInternal)
	}

	secretKey, err := c.encryptionService.Decrypt(conn.EncryptedSecretData.S3.AWSSecretAccessKey)
	if err != nil {
		c.logger.Error(context.Background(), "failed to decrypt AWS secret key", "connection_id", conn.ID, "error", err)
		return nil, ierr.NewError("failed to decrypt AWS secret key").Mark(ierr.ErrInternal)
	}

	// Decrypt session token if present (for temporary credentials)
	var sessionToken string
	if conn.EncryptedSecretData.S3.AWSSessionToken != "" {
		sessionToken, err = c.encryptionService.Decrypt(conn.EncryptedSecretData.S3.AWSSessionToken)
		if err != nil {
			c.logger.Error(context.Background(), "failed to decrypt AWS session token", "connection_id", conn.ID, "error", err)
			return nil, ierr.NewError("failed to decrypt AWS session token").Mark(ierr.ErrInternal)
		}
	}

	c.logger.Info(context.Background(), "Decrypted S3 credentials",
		"connection_id", conn.ID,
		"has_session_token", sessionToken != "",
	)

	// Validate job config is provided
	if jobConfig == nil {
		return nil, ierr.NewError("no job configuration provided").
			WithHint("S3 job configuration is required").
			Mark(ierr.ErrValidation)
	}

	// Combine decrypted credentials with job config
	s3Config := &S3Config{
		AWSAccessKeyID:     accessKey,
		AWSSecretAccessKey: secretKey,
		AWSSessionToken:    sessionToken,
		Bucket:             jobConfig.Bucket,
		Region:             jobConfig.Region,
		KeyPrefix:          jobConfig.KeyPrefix,
		Compression:        jobConfig.Compression,
		Encryption:         jobConfig.Encryption,
		EndpointURL:        jobConfig.EndpointURL,
		VirtualHostStyle:   !jobConfig.UsePathStyle, // VirtualHostStyle is opposite of UsePathStyle
	}

	c.logger.Info(context.Background(), "successfully created S3 configuration",
		"connection_id", conn.ID,
		"bucket", s3Config.Bucket,
		"region", s3Config.Region,
		"endpoint_url", s3Config.EndpointURL,
		"use_path_style", jobConfig.UsePathStyle,
	)

	return s3Config, nil
}

// s3Client is the actual S3 client with AWS SDK
type s3Client struct {
	s3Client *s3.Client
	config   *S3Config
	logger   *logger.Logger
}

// GetAWSS3Client returns the underlying AWS S3 client
func (c *s3Client) GetAWSS3Client() *s3.Client {
	return c.s3Client
}

// ValidateConnection validates the S3 connection by checking bucket access
func (c *s3Client) ValidateConnection(ctx context.Context) error {
	// Try to head bucket to validate connection
	_, err := c.s3Client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(c.config.Bucket),
	})
	if err != nil {
		return ierr.WithError(err).
			WithHint("failed to validate S3 connection - check credentials and bucket name").
			WithMessagef("bucket: %s, region: %s", c.config.Bucket, c.config.Region).
			Mark(ierr.ErrHTTPClient)
	}

	c.logger.Info(ctx, "S3 connection validated successfully",
		"bucket", c.config.Bucket,
		"region", c.config.Region,
	)

	return nil
}
