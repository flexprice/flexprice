package cron

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/gin-gonic/gin"
)

// KafkaLagMonitoringHandler handles periodic Kafka consumer lag monitoring for cron jobs.
// It monitors lag metrics across event consumption and post-processing pipelines.
type KafkaLagMonitoringHandler struct {
	logger       *logger.Logger
	eventService service.EventService
}

// NewKafkaLagMonitoringHandler creates a new handler for Kafka lag monitoring cron jobs.
func NewKafkaLagMonitoringHandler(log *logger.Logger, eventService service.EventService) *KafkaLagMonitoringHandler {
	return &KafkaLagMonitoringHandler{
		logger:       log,
		eventService: eventService,
	}
}

// HandleKafkaLagMonitoring is the HTTP handler for the Kafka lag monitoring cron endpoint.
// It triggers lag monitoring across all configured Kafka consumer groups and reports metrics to Sentry.
func (h *KafkaLagMonitoringHandler) HandleKafkaLagMonitoring(c *gin.Context) {
	ctx := c.Request.Context()

	h.logger.Infow("kafka lag monitoring job started")

	if err := h.eventService.MonitorKafkaLag(ctx); err != nil {
		h.logger.Errorw("kafka lag monitoring job failed", "error", err)
		c.Error(err)
		return
	}

	h.logger.Infow("kafka lag monitoring job completed successfully")
	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "kafka lag monitoring completed",
	})
}
