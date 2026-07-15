package storage_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/storage"
	"github.com/stretchr/testify/assert"
)

func TestCloudDetector_DetectsGCP(t *testing.T) {
	gcpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Metadata-Flavor") == "Google" {
			w.Header().Set("Metadata-Flavor", "Google")
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer gcpServer.Close()

	unreachableAWS := "http://127.0.0.1:1" // deliberately unreachable, simulates no AWS metadata

	d := storage.NewCloudDetector(gcpServer.URL, unreachableAWS, 200*time.Millisecond)
	provider := d.Detect(context.Background())
	assert.Equal(t, storage.ProviderGCS, provider)
}

// TestCloudDetector_DetectsAWS verifies probeAWS's IMDSv2 contract: a PUT to
// the token endpoint with the required X-aws-ec2-metadata-token-ttl-seconds
// header, treating a 200 response as AWS detected. A PUT to
// /latest/meta-data/ (the old, wrong target) returns 403 on real AWS
// infrastructure and would never satisfy this contract.
func TestCloudDetector_DetectsAWS(t *testing.T) {
	var gotMethod, gotTTLHeader string
	awsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotTTLHeader = r.Header.Get("X-aws-ec2-metadata-token-ttl-seconds")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("fake-imdsv2-token"))
	}))
	defer awsServer.Close()

	unreachableGCP := "http://127.0.0.1:1" // deliberately unreachable, simulates no GCP metadata

	d := storage.NewCloudDetector(unreachableGCP, awsServer.URL, 200*time.Millisecond)
	provider := d.Detect(context.Background())

	assert.Equal(t, storage.ProviderS3, provider)
	assert.Equal(t, http.MethodPut, gotMethod, "IMDSv2 token endpoint must be called with PUT")
	assert.Equal(t, "21600", gotTTLHeader, "IMDSv2 token request must set the TTL header")
}

func TestCloudDetector_NeitherReachable_ReturnsUnknown(t *testing.T) {
	d := storage.NewCloudDetector("http://127.0.0.1:1", "http://127.0.0.1:2", 200*time.Millisecond)
	provider := d.Detect(context.Background())
	assert.Equal(t, storage.Provider(""), provider)
}

func TestCloudDetector_SlowResponder_BoundedByTimeout(t *testing.T) {
	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second) // longer than the detector's timeout
		w.WriteHeader(http.StatusOK)
	}))
	defer slowServer.Close()

	d := storage.NewCloudDetector(slowServer.URL, "http://127.0.0.1:1", 200*time.Millisecond)

	start := time.Now()
	provider := d.Detect(context.Background())
	elapsed := time.Since(start)

	assert.Equal(t, storage.Provider(""), provider)
	assert.Less(t, elapsed, 1*time.Second, "Detect should be bounded by the configured timeout, not block on a slow responder")
}
