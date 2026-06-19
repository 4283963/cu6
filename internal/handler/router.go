package handler

import (
	"github.com/gin-gonic/gin"

	"privacy-relay/internal/config"
	"privacy-relay/internal/middleware"
	"privacy-relay/internal/service"
)

type Router struct {
	cfg           *config.Config
	relayHandler  *RelayHandler
	healthHandler *HealthHandler
	replaySvc     service.ReplayProtectionService
}

func NewRouter(
	cfg *config.Config,
	relayHandler *RelayHandler,
	healthHandler *HealthHandler,
	replaySvc service.ReplayProtectionService,
) *Router {
	return &Router{
		cfg:           cfg,
		relayHandler:  relayHandler,
		healthHandler: healthHandler,
		replaySvc:     replaySvc,
	}
}

func (r *Router) SetupEngine() *gin.Engine {
	gin.SetMode(r.cfg.Server.Mode)
	engine := gin.New()

	engine.Use(middleware.PanicRecovery())
	engine.Use(gin.Recovery())
	engine.Use(middleware.RequestLogger())
	engine.Use(middleware.ValidateRequest())
	engine.Use(middleware.ErrorHandler())
	engine.Use(middleware.ResponseFormatter())

	apiV1 := engine.Group("/api/v1")
	{
		health := apiV1.Group("")
		r.healthHandler.RegisterRoutes(health)

		relayGroup := apiV1.Group("")
		relayGroup.Use(middleware.OptionalReplayProtection(r.replaySvc, &r.cfg.Security))
		r.relayHandler.RegisterRoutes(relayGroup)
	}

	engine.NoRoute(func(c *gin.Context) {
		middleware.SetError(c, gin.Error{Err: nil})
	})

	return engine
}
