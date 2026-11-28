package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestTracingMiddleware_SkipsHealth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := tracetest.NewSpanRecorder()
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)))
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	cfg := &config.Configuration{}
	cfg.Otel.Enabled = true
	cfg.Otel.Traces.Enabled = true
	cfg.Otel.ServiceName = "test"

	r := gin.New()
	r.Use(TracingMiddleware(cfg)...)
	r.GET("/health", func(c *gin.Context) { c.String(http.StatusOK, "health-ok") })
	r.GET("/v1/ping", func(c *gin.Context) { c.String(http.StatusOK, "ping-ok") })

	for p, body := range map[string]string{"/health": "health-ok", "/v1/ping": "ping-ok"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, p, nil))
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, body, w.Body.String())
	}

	ended := rec.Ended()
	assert.Len(t, ended, 1)
	assert.Contains(t, ended[0].Name(), "/v1/ping")
}
