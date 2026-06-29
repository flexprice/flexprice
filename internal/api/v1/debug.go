package v1

import (
	"database/sql"
	"net/http"

	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/gin-gonic/gin"
)

// DebugHandler exposes debug-only endpoints. Only registered when
// FLEXPRICE_DB_ROUTING_DEBUG=true.
type DebugHandler struct {
	clients *postgres.EntClients
}

// NewDebugHandler creates a DebugHandler.
func NewDebugHandler(clients *postgres.EntClients) *DebugHandler {
	return &DebugHandler{clients: clients}
}

// LagProbeResponse is the JSON shape returned by LagProbe.
type LagProbeResponse struct {
	ReaderIsReplica bool   `json:"reader_is_replica"`
	ReaderLSN       string `json:"reader_lsn"`
	WriterLSN       string `json:"writer_lsn"`
	IsDistinct      bool   `json:"is_distinct"`
	Warning         string `json:"warning,omitempty"`
}

// LagProbe queries pg_is_in_recovery() on the reader and pg_current_wal_lsn()
// on the writer to determine whether the two endpoints are distinct instances.
//
// @Summary WAL lag probe
// @Tags Debug
// @Produce json
// @Success 200 {object} LagProbeResponse
// @Router /internal/debug/lag-probe [post]
func (h *DebugHandler) LagProbe(c *gin.Context) {
	ctx := c.Request.Context()

	var writerLSN string
	err := h.clients.WriterDB.QueryRowContext(ctx, "SELECT pg_current_wal_lsn()::text").Scan(&writerLSN)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "writer WAL query failed: " + err.Error()})
		return
	}

	var readerIsReplica bool
	var readerLSN sql.NullString
	err = h.clients.ReaderDB.QueryRowContext(ctx,
		"SELECT pg_is_in_recovery(), pg_last_wal_replay_lsn()::text",
	).Scan(&readerIsReplica, &readerLSN)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "reader WAL query failed: " + err.Error()})
		return
	}

	readerLSNStr := "N/A (primary)"
	if readerLSN.Valid {
		readerLSNStr = readerLSN.String
	}

	resp := LagProbeResponse{
		ReaderIsReplica: readerIsReplica,
		ReaderLSN:       readerLSNStr,
		WriterLSN:       writerLSN,
		IsDistinct:      readerIsReplica,
	}
	if !readerIsReplica {
		resp.Warning = "reader endpoint is the primary — reader and writer are the same instance; routing assertions will not catch under-pinning bugs"
	}

	c.JSON(http.StatusOK, resp)
}
