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
//	processor := NewFileProcessor(httpClient, logger)
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

	ierr "github.com/flexprice/flexprice/internal/errors"
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
)

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

// FileProviderRegistry manages different file providers
type FileProviderRegistry struct {
	providers map[FileProviderType]FileProvider
}

// NewFileProviderRegistry creates a new file provider registry
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

// RegisterProvider registers a file provider
func (r *FileProviderRegistry) RegisterProvider(provider FileProvider) {
	r.providers[provider.GetProviderName()] = provider
}

// GetProvider returns the appropriate provider for a given URL
func (r *FileProviderRegistry) GetProvider(fileURL string) FileProvider {
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
