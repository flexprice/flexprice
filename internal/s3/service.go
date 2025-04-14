package s3

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/cockroachdb/errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/flexprice/flexprice/internal/config"
	ierr "github.com/flexprice/flexprice/internal/errors"
)

const (
	defaultPresignExpiryDuration = 30 * time.Minute
)

var (
	validDocumentTypes = []DocumentType{DocumentTypeInvoice}
)

type Service interface {
	UploadDocument(ctx context.Context, document *Document) error
	GetPresignedUrl(ctx context.Context, id string, docType DocumentType) (string, error)
	GetDocument(ctx context.Context, id string, docType DocumentType) ([]byte, error)
	Exists(ctx context.Context, id string, docType DocumentType) (bool, error)
}

type s3ServiceImpl struct {
	client *s3.Client
	config *config.S3Config
}

func NewService(config *config.Configuration) (Service, error) {
	if !config.S3.Enabled {
		return nil, nil
	}

	awsCfg, err := awsConfig.LoadDefaultConfig(context.Background(),
		awsConfig.WithRegion(config.S3.Region),
	)
	if err != nil {
		return nil, ierr.WithError(err).WithHint("failed to load aws config").
			Mark(ierr.ErrHTTPClient)
	}

	return &s3ServiceImpl{
		config: &config.S3,
		client: s3.NewFromConfig(awsCfg),
	}, nil
}

func (s *s3ServiceImpl) getObjectKey(id string, docType DocumentType) (string, error) {
	switch docType {
	case DocumentTypeInvoice:
		if s.config.InvoiceBucketConfig.KeyPrefix != "" {
			return fmt.Sprintf("%s/%s.pdf", s.config.InvoiceBucketConfig.KeyPrefix, id), nil
		}
		return fmt.Sprintf("%s.pdf", id), nil
	default:
		return "", ierr.NewErrorf("invalid doc type: %s", docType).
			WithHintf("valid doc types are: %v", validDocumentTypes).
			Mark(ierr.ErrSystem)
	}
}

func (s *s3ServiceImpl) getBucket(docType DocumentType) string {
	switch docType {
	case DocumentTypeInvoice:
		return s.config.InvoiceBucketConfig.Bucket
	default:
		return ""
	}
}

func (s *s3ServiceImpl) getContentType(docKind DocumentKind) string {
	switch docKind {
	case DocumentKindPdf:
		return "application/pdf"
	default:
		return "application/octet-stream"
	}
}

// Exists implements S3Service.
func (s *s3ServiceImpl) Exists(ctx context.Context, id string, docType DocumentType) (bool, error) {
	key, err := s.getObjectKey(id, docType)
	if err != nil {
		return false, err
	}

	_, err = s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.getBucket(docType)),
		Key:    aws.String(key),
	})

	if err != nil {
		var nsk *types.NoSuchKey
		var nske *types.NotFound
		if errors.As(err, &nsk) || errors.As(err, &nske) {
			return false, nil
		}
		return false, ierr.NewErrorf("failed to check if document exists: %w", err).
			Mark(ierr.ErrHTTPClient)
	}

	return true, nil
}

// GetPresignedUrl implements S3Service.
func (s *s3ServiceImpl) GetPresignedUrl(ctx context.Context, id string, docType DocumentType) (string, error) {
	key, err := s.getObjectKey(id, docType)
	if err != nil {
		return "", err
	}

	duration, err := time.ParseDuration(s.config.InvoiceBucketConfig.PresignExpiryDuration)
	if err != nil {
		duration = defaultPresignExpiryDuration
	}

	presigner := s3.NewPresignClient(s.client)
	result, err := presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.getBucket(docType)),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(duration))
	if err != nil {
		return "", ierr.WithError(err).WithHint("failed to get presigned url").
			WithMessagef("bucket:%s, key:%s", s.getBucket(docType), key).
			Mark(ierr.ErrHTTPClient)
	}

	return result.URL, nil
}

// UploadDocument implements S3Service.
func (s *s3ServiceImpl) UploadDocument(ctx context.Context, document *Document) error {
	key, err := s.getObjectKey(document.ID, document.Type)
	if err != nil {
		return err
	}

	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.getBucket(document.Type)),
		Key:         aws.String(key),
		Body:        bytes.NewReader(document.Data),
		ContentType: aws.String(s.getContentType(document.Kind)),
	})
	if err != nil {
		return ierr.WithError(err).WithHint("failed to upload document").
			WithMessagef("bucket:%s, key:%s", s.getBucket(document.Type), key).
			Mark(ierr.ErrHTTPClient)
	}

	return nil
}

// GetDocument implements S3Service.
func (s *s3ServiceImpl) GetDocument(ctx context.Context, id string, docType DocumentType) ([]byte, error) {
	key, err := s.getObjectKey(id, docType)
	if err != nil {
		return nil, err
	}

	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.getBucket(docType)),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, ierr.WithError(err).WithHint("failed to get document").
			WithMessagef("bucket:%s, key:%s", s.getBucket(docType), key).
			Mark(ierr.ErrHTTPClient)
	}

	defer result.Body.Close()

	return io.ReadAll(result.Body)
}
