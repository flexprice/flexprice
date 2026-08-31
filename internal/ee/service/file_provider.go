// Package service provides file processing capabilities with support for multiple cloud storage providers.
//
// The file download system is designed to be generic and extensible, supporting various providers:
// - Direct URLs (http/https)
// - Google Drive
// - AWS S3
// - Microsoft OneDrive
// - Dropbox
// - GitHub (raw files)
//
// Example usage:
//
//	processor := NewFileProcessor(logger)
//	content, err := processor.DownloadFile(ctx, &task.Task{FileURL: "https://drive.google.com/file/d/123/view"})
//	if err != nil {
//	    // Handle error - simple error message, no complex error objects
//	    log.Printf("Download failed: %v", err)
//	}
//
// Adding a new provider:
//  1. Implement the FileProvider interface
//  2. Register it with the FileProviderRegistry
//  3. Update the GetProvider method to detect URLs for your provider
package service

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/flexprice/flexprice/internal/config"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
)

// FileProvider defines the interface for different file providers
// This allows the system to handle various cloud storage providers and file sharing services
// by converting their URLs to direct download URLs that can be fetched via HTTP.
type FileProvider interface {
	GetDownloadURL(ctx context.Context, fileURL string) (string, error)
	GetProviderName() FileProviderType
}

type FileProviderType string

const (
	FileProviderTypeDirect      FileProviderType = "direct"
	FileProviderTypeGoogleDrive FileProviderType = "google_drive"
	FileProviderTypeS3          FileProviderType = "s3"
	FileProviderTypeOneDrive    FileProviderType = "onedrive"
	FileProviderTypeDropbox     FileProviderType = "dropbox"
	FileProviderTypeGitHub      FileProviderType = "github"
	FileProviderTypeCSVBox      FileProviderType = "csvbox"
)

// csvboxPresignExpiry bounds how long the presigned GET stays valid. Kept
// short because the streaming download starts immediately after the URL is
// produced — a long lifetime just widens the window a leaked URL is usable.
const csvboxPresignExpiry = 5 * time.Minute

// DirectURLProvider handles direct file URLs
type DirectURLProvider struct{}

func (p *DirectURLProvider) GetDownloadURL(ctx context.Context, fileURL string) (string, error) {
	// Validate URL
	_, err := url.Parse(fileURL)
	if err != nil {
		return "", ierr.WithError(err).
			WithHint("Invalid URL").
			Mark(ierr.ErrValidation)
	}
	return fileURL, nil
}

func (p *DirectURLProvider) GetProviderName() FileProviderType {
	return FileProviderTypeDirect
}

// GoogleDriveProvider handles Google Drive URLs
type GoogleDriveProvider struct{}

func (p *GoogleDriveProvider) GetDownloadURL(ctx context.Context, fileURL string) (string, error) {

	// Handle different Google Drive URL formats
	patterns := []string{
		`/file/d/([^/]+)`,   // Format: /file/d/{fileId}/
		`id=([^&]+)`,        // Format: ?id={fileId}
		`/d/([^/]+)`,        // Format: /d/{fileId}/
		`/open\?id=([^&]+)`, // Format: /open?id={fileId}
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(fileURL)
		if len(matches) > 1 {
			fileID := matches[1]
			return fmt.Sprintf("https://drive.google.com/uc?export=download&id=%s", fileID), nil
		}
	}

	return "", ierr.NewErrorf("invalid Google Drive URL: %s", fileURL).
		WithHint("Invalid Google Drive URL").
		Mark(ierr.ErrValidation)
}

func (p *GoogleDriveProvider) GetProviderName() FileProviderType {
	return FileProviderTypeGoogleDrive
}

// S3Provider handles AWS S3 URLs
type S3Provider struct{}

func (p *S3Provider) GetDownloadURL(ctx context.Context, fileURL string) (string, error) {
	// S3 URLs are typically already in the correct format for direct download
	// but we can add presigned URL logic here if needed
	_, err := url.Parse(fileURL)
	if err != nil {
		return "", ierr.WithError(err).
			WithHint("Invalid S3 URL").
			Mark(ierr.ErrValidation)
	}
	return fileURL, nil
}

func (p *S3Provider) GetProviderName() FileProviderType {
	return FileProviderTypeS3
}

// OneDriveProvider handles Microsoft OneDrive URLs
type OneDriveProvider struct{}

func (p *OneDriveProvider) GetDownloadURL(ctx context.Context, fileURL string) (string, error) {
	// Extract file ID from OneDrive URL
	fileID := extractOneDriveFileID(fileURL)
	if fileID == "" {
		return "", ierr.NewErrorf("invalid OneDrive URL: %s", fileURL).
			WithHint("Invalid OneDrive URL").
			Mark(ierr.ErrValidation)
	}
	return fmt.Sprintf("https://api.onedrive.com/v1.0/shares/u!%s/root/content", fileID), nil
}

func (p *OneDriveProvider) GetProviderName() FileProviderType {
	return FileProviderTypeOneDrive
}

// DropboxProvider handles Dropbox URLs
type DropboxProvider struct{}

func (p *DropboxProvider) GetDownloadURL(ctx context.Context, fileURL string) (string, error) {
	// Convert Dropbox sharing URL to direct download URL
	// Format: https://www.dropbox.com/s/{file_id}/{filename}?dl=0
	// Convert to: https://www.dropbox.com/s/{file_id}/{filename}?dl=1
	if strings.Contains(fileURL, "?dl=0") {
		fileURL = strings.Replace(fileURL, "?dl=0", "?dl=1", 1)
	} else if !strings.Contains(fileURL, "?dl=") {
		fileURL += "?dl=1"
	}

	_, err := url.Parse(fileURL)
	if err != nil {
		return "", ierr.WithError(err).
			WithHint("Invalid Dropbox URL").
			Mark(ierr.ErrValidation)
	}
	return fileURL, nil
}

func (p *DropboxProvider) GetProviderName() FileProviderType {
	return FileProviderTypeDropbox
}

// GitHubProvider handles GitHub raw file URLs
type GitHubProvider struct{}

func (p *GitHubProvider) GetDownloadURL(ctx context.Context, fileURL string) (string, error) {
	// Convert GitHub file URL to raw URL
	// Format: https://github.com/user/repo/blob/branch/path/file.ext
	// Convert to: https://raw.githubusercontent.com/user/repo/branch/path/file.ext
	if strings.Contains(fileURL, "github.com") && strings.Contains(fileURL, "/blob/") {
		fileURL = strings.Replace(fileURL, "github.com", "raw.githubusercontent.com", 1)
		fileURL = strings.Replace(fileURL, "/blob/", "/", 1)
	}

	_, err := url.Parse(fileURL)
	if err != nil {
		return "", ierr.WithError(err).
			WithHint("Invalid GitHub URL").
			Mark(ierr.ErrValidation)
	}
	return fileURL, nil
}

func (p *GitHubProvider) GetProviderName() FileProviderType {
	return FileProviderTypeGitHub
}

// extractOneDriveFileID extracts file ID from OneDrive URL
func extractOneDriveFileID(url string) string {
	patterns := []string{
		`/items/([^/]+)`,       // Format: /items/{fileId}
		`/drive/items/([^/]+)`, // Format: /drive/items/{fileId}
		`id=([^&]+)`,           // Format: ?id={fileId}
		`/shares/([^/]+)`,      // Format: /shares/{fileId}
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(url)
		if len(matches) > 1 {
			return matches[1]
		}
	}
	return ""
}

// CSVBoxProvider resolves an s3://<imports-bucket>/<key> URL — produced by the
// task DTO from a caller-supplied upload_id — to a short-lived presigned GET
// URL. The credentials come from FlexpriceS3ImportsConfig, never from the
// caller, so this provider is the trust boundary that replaces the removed
// file_url path (see commit f05a1e65f).
type CSVBoxProvider struct {
	bucket    string
	region    string
	accessKey string
	secretKey string
	session   string
	logger    *logger.Logger
}

// NewCSVBoxProvider builds the provider from imports config. Returns nil when
// the imports feature is disabled or credentials are missing — callers must
// skip registration in that case.
func NewCSVBoxProvider(cfg config.FlexpriceS3ImportsConfig, log *logger.Logger) *CSVBoxProvider {
	if !cfg.Enabled || cfg.Bucket == "" || cfg.AWSAccessKeyID == "" || cfg.AWSSecretAccessKey == "" {
		return nil
	}
	return &CSVBoxProvider{
		bucket:    cfg.Bucket,
		region:    cfg.Region,
		accessKey: cfg.AWSAccessKeyID,
		secretKey: cfg.AWSSecretAccessKey,
		session:   cfg.AWSSessionToken,
		logger:    log,
	}
}

func (p *CSVBoxProvider) GetProviderName() FileProviderType {
	return FileProviderTypeCSVBox
}

// Bucket exposes the imports bucket so the registry can route only URLs that
// point at it (and not at unrelated s3:// URLs like export FileURLs).
func (p *CSVBoxProvider) Bucket() string {
	return p.bucket
}

func (p *CSVBoxProvider) GetDownloadURL(ctx context.Context, fileURL string) (string, error) {
	bucket, key, err := parseS3URL(fileURL)
	if err != nil {
		return "", err
	}
	if bucket != p.bucket {
		return "", ierr.NewErrorf("bucket %q is not the Flexprice imports bucket", bucket).
			WithHint("The CSVBox provider only downloads from the configured imports bucket").
			Mark(ierr.ErrValidation)
	}

	awsCfg, err := awsConfig.LoadDefaultConfig(ctx,
		awsConfig.WithRegion(p.region),
		awsConfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			p.accessKey, p.secretKey, p.session,
		)),
	)
	if err != nil {
		return "", ierr.WithError(err).
			WithHint("Failed to load AWS config for imports").
			Mark(ierr.ErrInternal)
	}

	presigner := s3.NewPresignClient(s3.NewFromConfig(awsCfg))
	res, err := presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(csvboxPresignExpiry))
	if err != nil {
		return "", ierr.WithError(err).
			WithHint("Failed to presign imports object").
			WithReportableDetails(map[string]interface{}{
				"bucket": bucket,
				"key":    key,
			}).
			Mark(ierr.ErrInternal)
	}
	return res.URL, nil
}

// parseS3URL splits an s3://bucket/key URL. The key may contain slashes; the
// bucket may not.
func parseS3URL(s3URL string) (bucket, key string, err error) {
	if !strings.HasPrefix(s3URL, "s3://") {
		return "", "", ierr.NewErrorf("not an s3 URL: %s", s3URL).
			WithHint("Expected s3://bucket/key").
			Mark(ierr.ErrValidation)
	}
	rest := strings.TrimPrefix(s3URL, "s3://")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", ierr.NewErrorf("invalid s3 URL: %s", s3URL).
			WithHint("Expected s3://bucket/key").
			Mark(ierr.ErrValidation)
	}
	return parts[0], parts[1], nil
}

// FileProviderRegistry manages different file providers
type FileProviderRegistry struct {
	providers    map[FileProviderType]FileProvider
	csvboxBucket string
}

// NewFileProviderRegistry creates a registry with the default (public-URL)
// providers only. Callers that want CSVBox routing must additionally invoke
// RegisterCSVBoxProvider.
func NewFileProviderRegistry() *FileProviderRegistry {
	registry := &FileProviderRegistry{
		providers: make(map[FileProviderType]FileProvider),
	}

	// Register default providers
	registry.RegisterProvider(&DirectURLProvider{})
	registry.RegisterProvider(&GoogleDriveProvider{})
	registry.RegisterProvider(&S3Provider{})
	registry.RegisterProvider(&OneDriveProvider{})
	registry.RegisterProvider(&DropboxProvider{})
	registry.RegisterProvider(&GitHubProvider{})

	return registry
}

// RegisterCSVBoxProvider registers the CSVBox provider and remembers its
// bucket so GetProvider can route only matching s3:// URLs to it. A nil
// provider is a no-op, letting callers pass NewCSVBoxProvider's result
// unconditionally.
func (r *FileProviderRegistry) RegisterCSVBoxProvider(p *CSVBoxProvider) {
	if p == nil {
		return
	}
	r.providers[p.GetProviderName()] = p
	r.csvboxBucket = p.Bucket()
}

// RegisterProvider registers a file provider
func (r *FileProviderRegistry) RegisterProvider(provider FileProvider) {
	r.providers[provider.GetProviderName()] = provider
}

// GetProvider returns the appropriate provider for a given URL
func (r *FileProviderRegistry) GetProvider(fileURL string) FileProvider {
	// s3:// URLs come from the /tasks import path (upload_id resolved to
	// s3://<imports-bucket>/<key>) — route to CSVBox only when the bucket
	// matches the configured imports bucket, so unrelated s3:// URLs (e.g.
	// export FileURLs) do not accidentally get presigned with imports creds.
	if strings.HasPrefix(fileURL, "s3://") && r.csvboxBucket != "" {
		if bucket, _, err := parseS3URL(fileURL); err == nil && bucket == r.csvboxBucket {
			return r.providers[FileProviderTypeCSVBox]
		}
	}

	// Check for specific providers based on URL patterns
	if strings.Contains(fileURL, "drive.google.com") {
		return r.providers[FileProviderTypeGoogleDrive]
	}
	if strings.Contains(fileURL, "amazonaws.com") || strings.Contains(fileURL, "s3.") {
		return r.providers[FileProviderTypeS3]
	}
	if strings.Contains(fileURL, "onedrive.live.com") || strings.Contains(fileURL, "1drv.ms") {
		return r.providers[FileProviderTypeOneDrive]
	}
	if strings.Contains(fileURL, "dropbox.com") {
		return r.providers[FileProviderTypeDropbox]
	}
	if strings.Contains(fileURL, "github.com") {
		return r.providers[FileProviderTypeGitHub]
	}

	// Default to direct URL provider
	return r.providers[FileProviderTypeDirect]
}
