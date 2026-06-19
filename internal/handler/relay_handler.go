package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"privacy-relay/internal/middleware"
	"privacy-relay/internal/model"
	"privacy-relay/internal/service"
	appErr "privacy-relay/pkg/errors"
)

type RelayHandler struct {
	relaySvc service.RelayService
}

func NewRelayHandler(relaySvc service.RelayService) *RelayHandler {
	return &RelayHandler{
		relaySvc: relaySvc,
	}
}

func (h *RelayHandler) RegisterRoutes(r *gin.RouterGroup) {
	relay := r.Group("/relays")
	{
		relay.POST("", h.RegisterRelay)
		relay.GET("", h.ListRelays)
		relay.GET("/:relay_id", h.GetRelay)
		relay.POST("/dispatch", h.DispatchDecrypt)
		relay.POST("/status", h.UpdateDecryptStatus)
	}
}

func (h *RelayHandler) RegisterRelay(c *gin.Context) {
	var req model.RegisterRelayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.SetError(c, appErr.InvalidParams("invalid request body: "+err.Error()))
		return
	}

	resp, err := h.relaySvc.RegisterRelay(c.Request.Context(), &req)
	if err != nil {
		if ae, ok := err.(*appErr.AppError); ok && ae.Code == appErr.CodeConflict {
			middleware.SetOK(c, resp)
			return
		}
		middleware.SetError(c, err)
		return
	}

	middleware.SetCreated(c, resp)
}

func (h *RelayHandler) GetRelay(c *gin.Context) {
	relayID := c.Param("relay_id")
	if relayID == "" {
		middleware.SetError(c, appErr.InvalidParams("relay_id is required"))
		return
	}

	resp, err := h.relaySvc.GetRelay(c.Request.Context(), relayID)
	if err != nil {
		middleware.SetError(c, err)
		return
	}

	middleware.SetOK(c, resp)
}

func (h *RelayHandler) ListRelays(c *gin.Context) {
	var req model.ListRelaysRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		middleware.SetError(c, appErr.InvalidParams("invalid query params: "+err.Error()))
		return
	}

	clientIDFromCtx, exists := c.Get("client_id")
	if exists && req.ClientID == "" {
		req.ClientID = clientIDFromCtx.(string)
	}

	resp, err := h.relaySvc.ListRelays(c.Request.Context(), &req)
	if err != nil {
		middleware.SetError(c, err)
		return
	}

	middleware.SetOK(c, resp)
}

func (h *RelayHandler) DispatchDecrypt(c *gin.Context) {
	var req model.DispatchDecryptRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.SetError(c, appErr.InvalidParams("invalid request body: "+err.Error()))
		return
	}

	resp, err := h.relaySvc.DispatchDecrypt(c.Request.Context(), &req)
	if err != nil {
		middleware.SetError(c, err)
		return
	}

	middleware.SetOK(c, resp)
}

func (h *RelayHandler) UpdateDecryptStatus(c *gin.Context) {
	var req model.UpdateDecryptStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.SetError(c, appErr.InvalidParams("invalid request body: "+err.Error()))
		return
	}

	resp, err := h.relaySvc.UpdateDecryptStatus(c.Request.Context(), &req)
	if err != nil {
		if ae, ok := err.(*appErr.AppError); ok {
			if ae.Code == appErr.CodeConflict {
				middleware.SetOK(c, resp)
				return
			}
			if ae.Code == appErr.CodeMaxRetryExceeded {
				c.Set("response_data", resp)
				c.Set("response_status", http.StatusServiceUnavailable)
				c.Abort()
				return
			}
		}
		middleware.SetError(c, err)
		return
	}

	middleware.SetOK(c, resp)
}

type HealthHandler struct{}

func NewHealthHandler() *HealthHandler {
	return &HealthHandler{}
}

func (h *HealthHandler) RegisterRoutes(r *gin.RouterGroup) {
	r.GET("/health", h.HealthCheck)
	r.GET("/ready", h.ReadyCheck)
}

func (h *HealthHandler) HealthCheck(c *gin.Context) {
	middleware.SetOK(c, gin.H{
		"status": "ok",
	})
}

func (h *HealthHandler) ReadyCheck(c *gin.Context) {
	middleware.SetOK(c, gin.H{
		"status": "ready",
	})
}
