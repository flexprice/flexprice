package v1

import (
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
	WriterReachable bool   `json:"writer_reachable"`
	IsDistinct      bool   `json:"is_distinct"`
	Warning         string `json:"warning,omitempty"`
}

// LagProbe checks whether the reader endpoint is a real Aurora replica.
// Uses only pg_is_in_recovery() — no replication privileges required.
//
// @Summary Lag probe — confirms reader/writer are distinct instances
// @Tags Debug
// @Produce json
// @Success 200 {object} LagProbeResponse
// @Router /internal/debug/lag-probe [post]
func (h *DebugHandler) LagProbe(c *gin.Context) {
	ctx := c.Request.Context()

	// Confirm writer is reachable with an unprivileged query.
	var writerOne int
	writerErr := h.clients.WriterDB.QueryRowContext(ctx, "SELECT 1").Scan(&writerOne)

	// Check whether the reader is in recovery (i.e. a replica).
	var readerIsReplica bool
	err := h.clients.ReaderDB.QueryRowContext(ctx, "SELECT pg_is_in_recovery()").Scan(&readerIsReplica)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "reader query failed: " + err.Error()})
		return
	}

	resp := LagProbeResponse{
		ReaderIsReplica: readerIsReplica,
		WriterReachable: writerErr == nil,
		IsDistinct:      readerIsReplica,
	}
	if !readerIsReplica {
		resp.Warning = "reader endpoint is the primary — reader and writer are the same instance; routing assertions will not catch under-pinning bugs"
	}

	c.JSON(http.StatusOK, resp)
}
