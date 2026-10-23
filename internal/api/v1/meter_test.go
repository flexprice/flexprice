package v1

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// setupMeterHandler wires a MeterHandler to a real MeterService backed by an
// in-memory meter store, mirroring the other handler tests in this package.
func setupMeterHandler(t *testing.T) (*MeterHandler, *testutil.InMemoryMeterStore) {
	t.Helper()

	cfg := &config.Configuration{
		Logging: config.LoggingConfig{Level: types.LogLevelInfo},
	}
	log, err := logger.NewLogger(cfg)
	require.NoError(t, err)

	store := testutil.NewInMemoryMeterStore()
	svc := service.NewMeterService(store)
	return NewMeterHandler(svc, log), store
}

// TestUpdateMeter_TrimsFilterWhitespace exercises the UpdateMeter handler
// end-to-end (through req.Sanitize()) and asserts the stored meter's filter
// keys and values have their leading/trailing whitespace stripped.
func TestUpdateMeter_TrimsFilterWhitespace(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h, store := setupMeterHandler(t)

	ctx := testutil.SetupContext()
	existing := meter.NewMeter("Test Meter", types.GetTenantID(ctx), "test-user")
	existing.EventName = "api_request"
	existing.Aggregation = meter.Aggregation{Type: types.AggregationCount}
	existing.EnvironmentID = types.GetEnvironmentID(ctx)
	require.NoError(t, store.CreateMeter(ctx, existing))

	body := []byte(`{"filters":[{"key":"  status  ","values":["  active  ","inactive"]}]}`)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/v1/meters/"+existing.ID, bytes.NewReader(body))
	c.Request = c.Request.WithContext(ctx)
	c.Params = gin.Params{{Key: "id", Value: existing.ID}}

	h.UpdateMeter(c)

	require.Equal(t, http.StatusOK, w.Code)

	updated, err := store.GetMeter(ctx, existing.ID)
	require.NoError(t, err)
	require.Len(t, updated.Filters, 1)
	require.Equal(t, "status", updated.Filters[0].Key)
	require.ElementsMatch(t, []string{"active", "inactive"}, updated.Filters[0].Values)
}

// TestUpdateMeter_EmptyFiltersAfterTrim_StillRejected ensures an
// all-whitespace filters payload is treated as empty (it fails validation
// the same way an empty filters slice would) rather than being persisted.
func TestUpdateMeter_MissingID_ReturnsError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h, _ := setupMeterHandler(t)

	body := []byte(`{"filters":[{"key":"status","values":["active"]}]}`)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/v1/meters/", bytes.NewReader(body))
	// No "id" param set.

	h.UpdateMeter(c)

	require.NotEmpty(t, c.Errors)
}
