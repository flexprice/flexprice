package cron

import (
	"github.com/gin-gonic/gin"
)

// RegisterRoutes registers all cron job routes
func RegisterRoutes(router *gin.RouterGroup, handler *Handler) {
	cronGroup := router.Group("/cron")
	{
		// Subscription related cron jobs
		cronGroup.POST("/subscriptions/update-periods", handler.UpdateSubscriptionPeriods)
	}
}
