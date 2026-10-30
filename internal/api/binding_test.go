package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin/binding"
	"github.com/stretchr/testify/require"
)

func TestMicrosecondBinding_TruncatesBoundTimes(t *testing.T) {
	type entry struct {
		At time.Time `json:"at"`
	}
	type payload struct {
		StartDate time.Time  `json:"start_date"`
		EndDate   *time.Time `json:"end_date"`
		Entries   []entry    `json:"entries"`
	}

	body := []byte(`{
		"start_date": "2026-01-10T12:00:00.123456789Z",
		"end_date":   "2026-01-10T13:00:00.999999999Z",
		"entries":    [{"at": "2026-01-10T14:00:00.000000500Z"}]
	}`)

	var p payload
	b := microsecondBinding{inner: binding.JSON}
	require.NoError(t, b.BindBody(body, &p))

	require.Equal(t, time.Date(2026, 1, 10, 12, 0, 0, 123456000, time.UTC), p.StartDate)
	require.Equal(t, time.Date(2026, 1, 10, 13, 0, 0, 999999000, time.UTC), *p.EndDate)
	require.Equal(t, time.Date(2026, 1, 10, 14, 0, 0, 0, time.UTC), p.Entries[0].At)
}

func TestMicrosecondValueBinding_TruncatesQueryTimes(t *testing.T) {
	type filter struct {
		StartTime time.Time `form:"start_time"`
	}

	req := httptest.NewRequest(http.MethodGet, "/x?start_time=2026-01-10T12:00:00.123456789Z", nil)
	var f filter
	b := microsecondValueBinding{inner: binding.Query}
	require.NoError(t, b.Bind(req, &f))

	require.Equal(t, time.Date(2026, 1, 10, 12, 0, 0, 123456000, time.UTC), f.StartTime)
}
