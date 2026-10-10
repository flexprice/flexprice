package v1

import (
	"bytes"
	"net/http/httptest"

	"github.com/flexprice/flexprice/internal/rest/middleware"
	"github.com/gin-gonic/gin"
)

// setupAdminRouter registers one handler behind ErrorHandler. This mirrors router.go: handlers
// call c.Error(err) and rely on the middleware to turn it into a status code and JSON body.
func setupAdminRouter(method, path string, handler gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.ErrorHandler())
	router.Handle(method, path, handler)
	return router
}

func doAdminRequest(router *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}
