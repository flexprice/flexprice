package s3backend

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/cockroachdb/errors"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/storage"
)

const defaultPresignExpiry = 30 * time.Minute

// Config holds everything needed to construct an S3-backed storage.Storage.
type Config struct {
	Bucket             string
	Region             string
	KeyPrefix          string
	CompressionGzip    bool
	ServerSideEncrypt  string // "", "AES256", "aws:kms", "aws:kms:dsse"
	EndpointURL        string // custom endpoint (MinIO etc.)
	UsePathStyle       bool
	AWSAccessKeyID     string
	AWSSecretAccessKey string
	AWSSessionToken    string
	// FederationRoleARN, if set, enables GCP-token-to-AWS-role federation via
	// STS AssumeRoleWithWebIdentity. Inert until the AWS-side OIDC provider/role
	// exists (see infrastructure repo's s3-federation module, Plan 2).
	FederationRoleARN     string
	FederationTokenSource stscreds.IdentityTokenRetriever // nil unless federation enabled
}

type client struct {
	s3     *s3.Client
	cfg    *Config
	logger *logger.Logger
}

// New constructs an S3-backed storage.Storage. Credential resolution order:
// 1. explicit static keys (AWSAccessKeyID/AWSSecretAccessKey), if set
// 2. OIDC federation (FederationRoleARN + FederationTokenSource), if set
// 3. ambient AWS default credential chain
func New(cfg *Config, log *logger.Logger) (storage.Storage, error) {
	ctx := context.Background()
	opts := []func(*awsConfig.LoadOptions) error{
		awsConfig.WithRegion(cfg.Region),
	}

	switch {
	case cfg.AWSAccessKeyID != "" && cfg.AWSSecretAccessKey != "":
		log.Info(ctx, "s3backend: using static credentials")
		opts = append(opts, awsConfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AWSAccessKeyID, cfg.AWSSecretAccessKey, cfg.AWSSessionToken),
		))
	case cfg.FederationRoleARN != "" && cfg.FederationTokenSource != nil:
		log.Info(ctx, "s3backend: using OIDC federation credentials", "role_arn", cfg.FederationRoleARN)
		baseCfg, err := awsConfig.LoadDefaultConfig(ctx, awsConfig.WithRegion(cfg.Region))
		if err != nil {
			return nil, ierr.WithError(err).WithHint("failed to load base aws config for federation").Mark(ierr.ErrHTTPClient)
		}
		stsClient := sts.NewFromConfig(baseCfg)
		provider := stscreds.NewWebIdentityRoleProvider(stsClient, cfg.FederationRoleARN, cfg.FederationTokenSource)
		opts = append(opts, awsConfig.WithCredentialsProvider(aws.NewCredentialsCache(provider)))
	case cfg.FederationRoleARN != "" && cfg.FederationTokenSource == nil:
		// FederationRoleARN is set but no token source was wired (e.g. federation_enabled
		// is true in config but the real GKE token retriever, added in the separate
		// GKE-AWS-OIDC-federation plan, isn't wired yet). This is NOT an error — it's
		// expected during the gap between this plan and that one — but it must be loud,
		// since silently falling through to the ambient chain looks identical to a
		// successful federation setup right up until the ambient chain also fails to
		// resolve credentials (e.g. on GKE, where there is no ambient AWS chain at all).
		log.Warn(ctx, "s3backend: federation_role_arn is set but no FederationTokenSource is configured — falling back to ambient AWS credential chain, which will not work on non-AWS compute", "role_arn", cfg.FederationRoleARN)
	default:
		log.Info(ctx, "s3backend: no explicit credentials configured, using ambient AWS default credential chain")
	}

	awsCfg, err := awsConfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, ierr.WithError(err).WithHint("failed to load aws config").Mark(ierr.ErrHTTPClient)
	}

	s3Client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.EndpointURL != "" {
			o.BaseEndpoint = aws.String(cfg.EndpointURL)
		}
		if cfg.UsePathStyle {
			o.UsePathStyle = true
		}
	})

	return &client{s3: s3Client, cfg: cfg, logger: log}, nil
}

func (c *client) Provider() storage.Provider { return storage.ProviderS3 }

func (c *client) FileURL(key string) string {
	return storage.FileURL(storage.ProviderS3, c.cfg.Bucket, key)
}

func (c *client) Upload(ctx context.Context, req *storage.UploadRequest) (*storage.UploadResponse, error) {
	data := req.Data
	originalSize := int64(len(data))
	compressedSize := originalSize

	if req.Compress && c.cfg.CompressionGzip {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		if _, err := gz.Write(data); err != nil {
			return nil, ierr.WithError(err).WithHint("failed to compress data").Mark(ierr.ErrSystem)
		}
		if err := gz.Close(); err != nil {
			return nil, ierr.WithError(err).WithHint("failed to close gzip writer").Mark(ierr.ErrSystem)
		}
		data = buf.Bytes()
		compressedSize = int64(len(data))
	}

	contentType := req.ContentType
	if contentType == "" {
		contentType = contentTypeFor(req.Format, req.Compress && c.cfg.CompressionGzip)
	}

	input := &s3.PutObjectInput{
		Bucket:      aws.String(c.cfg.Bucket),
		Key:         aws.String(req.Key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(contentType),
	}
	switch c.cfg.ServerSideEncrypt {
	case "AES256":
		input.ServerSideEncryption = types.ServerSideEncryptionAes256
	case "aws:kms":
		input.ServerSideEncryption = types.ServerSideEncryptionAwsKms
	case "aws:kms:dsse":
		input.ServerSideEncryption = types.ServerSideEncryptionAwsKmsDsse
	}

	if _, err := c.s3.PutObject(ctx, input); err != nil {
		return nil, ierr.WithError(err).
			WithHint("failed to upload object to S3").
			WithMessagef("bucket:%s, key:%s", c.cfg.Bucket, req.Key).
			Mark(ierr.ErrHTTPClient)
	}

	return &storage.UploadResponse{
		FileURL:        c.FileURL(req.Key),
		Bucket:         c.cfg.Bucket,
		Key:            req.Key,
		FileSizeBytes:  originalSize,
		CompressedSize: compressedSize,
		UploadedAt:     time.Now(),
	}, nil
}

func (c *client) Download(ctx context.Context, key string) ([]byte, error) {
	result, err := c.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.cfg.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("failed to download object from S3").
			WithMessagef("bucket:%s, key:%s", c.cfg.Bucket, key).
			Mark(ierr.ErrHTTPClient)
	}
	defer result.Body.Close()
	return io.ReadAll(result.Body)
}

func (c *client) Exists(ctx context.Context, key string) (bool, error) {
	_, err := c.s3.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(c.cfg.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		var nsk *types.NoSuchKey
		var nf *types.NotFound
		if errors.As(err, &nsk) || errors.As(err, &nf) {
			return false, nil
		}
		return false, ierr.WithError(err).WithHint("failed to check object existence").Mark(ierr.ErrHTTPClient)
	}
	return true, nil
}

func (c *client) PresignGet(ctx context.Context, key string, duration time.Duration) (string, error) {
	if duration == 0 {
		duration = defaultPresignExpiry
	}
	presigner := s3.NewPresignClient(c.s3)
	result, err := presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.cfg.Bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(duration))
	if err != nil {
		return "", ierr.WithError(err).
			WithHint("failed to generate presigned url").
			WithMessagef("bucket:%s, key:%s", c.cfg.Bucket, key).
			Mark(ierr.ErrHTTPClient)
	}
	return result.URL, nil
}

func (c *client) ValidateConnection(ctx context.Context) error {
	if _, err := c.s3.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(c.cfg.Bucket)}); err != nil {
		return ierr.WithError(err).
			WithHint("failed to validate S3 connection - check credentials and bucket name").
			WithMessagef("bucket:%s, region:%s", c.cfg.Bucket, c.cfg.Region).
			Mark(ierr.ErrHTTPClient)
	}
	return nil
}

func contentTypeFor(format storage.UploadFormat, compressed bool) string {
	if compressed {
		return "application/gzip"
	}
	switch format {
	case storage.UploadFormatCSV:
		return "text/csv"
	case storage.UploadFormatJSON:
		return "application/json"
	case storage.UploadFormatPDF:
		return "application/pdf"
	default:
		return "application/octet-stream"
	}
}
