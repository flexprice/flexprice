package storage

import (
	"context"
	"net/http"
	"time"
)

const (
	defaultGCPMetadataURL = "http://metadata.google.internal/computeMetadata/v1/"
	defaultAWSMetadataURL = "http://169.254.169.254/latest/meta-data/"
	defaultProbeTimeout   = 500 * time.Millisecond
)

// CloudDetector probes cloud metadata endpoints once to determine which
// cloud the process is running on. Result should be cached by the caller —
// this type does not cache internally so tests can construct fresh instances.
type CloudDetector struct {
	gcpMetadataURL string
	awsMetadataURL string
	timeout        time.Duration
	client         *http.Client
}

func NewCloudDetector(gcpMetadataURL, awsMetadataURL string, timeout time.Duration) *CloudDetector {
	return &CloudDetector{
		gcpMetadataURL: gcpMetadataURL,
		awsMetadataURL: awsMetadataURL,
		timeout:        timeout,
		client:         &http.Client{Timeout: timeout},
	}
}

// NewDefaultCloudDetector uses the real GCP/AWS metadata endpoints.
func NewDefaultCloudDetector() *CloudDetector {
	return NewCloudDetector(defaultGCPMetadataURL, defaultAWSMetadataURL, defaultProbeTimeout)
}

// Detect returns ProviderGCS or ProviderS3 based on which metadata endpoint
// responds, or "" if neither is reachable (e.g. local dev, bare metal).
func (d *CloudDetector) Detect(ctx context.Context) Provider {
	if d.probeGCP(ctx) {
		return ProviderGCS
	}
	if d.probeAWS(ctx) {
		return ProviderS3
	}
	return ""
}

func (d *CloudDetector) probeGCP(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.gcpMetadataURL, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Metadata-Flavor", "Google")
	resp, err := d.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.Header.Get("Metadata-Flavor") == "Google" && resp.StatusCode == http.StatusOK
}

func (d *CloudDetector) probeAWS(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, d.awsMetadataURL, nil)
	if err != nil {
		return false
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusMethodNotAllowed
}
