package handler

import (
	"github.com/gin-gonic/gin"

	"privacy-relay/internal/middleware"
	"privacy-relay/internal/model"
	"privacy-relay/internal/service"
	appErr "privacy-relay/pkg/errors"
)

type MetricsHandler struct {
	metricsSvc service.MetricsService
}

func NewMetricsHandler(metricsSvc service.MetricsService) *MetricsHandler {
	return &MetricsHandler{
		metricsSvc: metricsSvc,
	}
}

func (h *MetricsHandler) RegisterRoutes(r *gin.RouterGroup) {
	metrics := r.Group("/metrics")
	{
		metrics.GET("", h.GetMetrics)
		metrics.DELETE("", h.ResetCounters)
	}
}

func (h *MetricsHandler) GetMetrics(c *gin.Context) {
	var req model.MetricsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		middleware.SetError(c, appErr.InvalidParams("invalid query params: "+err.Error()))
		return
	}

	resp, err := h.metricsSvc.GetMetrics(c.Request.Context(), &req)
	if err != nil {
		middleware.SetError(c, err)
		return
	}

	middleware.SetOK(c, resp)
}

func (h *MetricsHandler) ResetCounters(c *gin.Context) {
	if err := h.metricsSvc.ResetRealtimeCounters(c.Request.Context()); err != nil {
		middleware.SetError(c, err)
		return
	}
	middleware.SetOK(c, gin.H{
		"message": "realtime counters reset successfully",
	})
}
