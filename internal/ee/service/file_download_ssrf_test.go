package service

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/task"
	"github.com/flexprice/flexprice/internal/httpclient"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A task file_url is caller-supplied and is fetched long after it was validated,
// so the download clients must refuse a non-public destination at dial time
// rather than trusting the earlier check (VAPT F16 / CWE-918).
func TestFileDownloadRefusesLoopbackDestination(t *testing.T) {
	log, err := logger.NewLogger(&config.Configuration{
		Logging: config.LoggingConfig{Level: types.LogLevelDebug},
	})
	require.NoError(t, err)

	var serverHit atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverHit.Store(true)
		w.Header().Set("Content-Type", "text/csv")
		_, _ = w.Write([]byte("name,type,lookup_key,unit_singular,unit_plural\nx,boolean,x,,\n"))
	}))
	defer server.Close()

	loopbackTask := &task.Task{FileURL: server.URL + "/features.csv"}

	t.Run("streaming processor", func(t *testing.T) {
		serverHit.Store(false)
		sp := NewStreamingProcessor(log)

		start := time.Now()
		body, err := sp.downloadFileStream(context.Background(), loopbackTask)
		if err == nil {
			require.NoError(t, body.Close())
		}

		require.Error(t, err, "loopback download should be refused")
		assert.True(t, strings.Contains(err.Error(), httpclient.ErrBlockedAddress.Error()),
			"expected blocked-address error, got: %v", err)
		assert.False(t, serverHit.Load(), "internal server must not be reached")
		// A blocked address is permanent, so it must not burn the retry budget.
		assert.Less(t, time.Since(start), retryClientMinBackoff,
			"blocked address should fail fast rather than retry")
	})

	t.Run("file processor", func(t *testing.T) {
		serverHit.Store(false)
		fp := NewFileProcessor(log)

		_, err := fp.DownloadFile(context.Background(), loopbackTask)

		require.Error(t, err, "loopback download should be refused")
		assert.True(t, strings.Contains(err.Error(), httpclient.ErrBlockedAddress.Error()),
			"expected blocked-address error, got: %v", err)
		assert.False(t, serverHit.Load(), "internal server must not be reached")
	})
}

// The guard must reject only non-public destinations, so an ordinary public
// import host is still allowed through the address check.
func TestPublicImportHostIsNotBlocked(t *testing.T) {
	assert.True(t, validator.IsPublicIP(net.ParseIP("93.184.216.34")),
		"a public import host must not be classified as blocked")
}
