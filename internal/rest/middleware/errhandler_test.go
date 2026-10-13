package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestErrorHandler_RecordsErrorCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name     string
		tenantID string
		err      error
		wantCode string
	}{
		{name: "marked error", tenantID: "ten_errh_1", err: ierr.NewError("missing").Mark(ierr.ErrNotFound), wantCode: "not_found"},
		{name: "unmarked error", tenantID: "ten_errh_2", err: errors.New("boom"), wantCode: "internal_error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := tracetest.NewSpanRecorder()
			tracer := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)).Tracer("test")

			router := gin.New()
			router.Use(func(c *gin.Context) {
				ctx, span := tracer.Start(context.WithValue(c.Request.Context(), types.CtxTenantID, tt.tenantID), "request")
				defer span.End()
				c.Request = c.Request.WithContext(ctx)
				c.Next()
			})
			router.Use(ErrorHandler())
			router.GET("/fail", func(c *gin.Context) { _ = c.Error(tt.err) })

			router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/fail", nil))

			spans := recorder.Ended()
			require.Len(t, spans, 1)
			require.Contains(t, spans[0].Attributes(), attribute.String("app.error_code", tt.wantCode))
		})
	}
}
