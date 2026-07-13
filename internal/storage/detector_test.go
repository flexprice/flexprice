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
