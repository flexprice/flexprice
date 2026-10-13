package v1

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMe(t *testing.T) {
	router := setupAdminRouter(http.MethodGet, "/v1/me", NewMeHandler().Me)

	w := doAdminRequest(router, http.MethodGet, "/v1/me", "")

	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"ok": true}`, w.Body.String())
}
