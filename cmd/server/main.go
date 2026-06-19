package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"privacy-relay/internal/cache"
	"privacy-relay/internal/config"
	"privacy-relay/internal/handler"
	"privacy-relay/internal/repository"
	"privacy-relay/internal/service"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	db, err := repository.NewDatabase(&cfg.MySQL)
	if err != nil {
		log.Fatalf("failed to init database: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("warning: failed to close database: %v", err)
		}
	}()

	redisClient, err := cache.NewRedisClient(&cfg.Redis)
	if err != nil {
		log.Fatalf("failed to init redis: %v", err)
	}
	defer func() {
		if err := redisClient.Close(); err != nil {
			log.Printf("warning: failed to close redis: %v", err)
		}
	}()

	relayRepo := repository.NewRelayRepository(db)
	stateRepo := repository.NewStateTransitionRepository(db)
	replayRepo := repository.NewReplayRecordRepository(db)

	idempotentSvc := service.NewIdempotentService(redisClient, &cfg.Relay)
	replaySvc := service.NewReplayProtectionService(redisClient, &cfg.Relay)
	relaySvc := service.NewRelayService(cfg, relayRepo, stateRepo, replayRepo, idempotentSvc)

	relayHandler := handler.NewRelayHandler(relaySvc)
	healthHandler := handler.NewHealthHandler()

	router := handler.NewRouter(cfg, relayHandler, healthHandler, replaySvc)
	engine := router.SetupEngine()

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      engine,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}

	go func() {
		log.Printf("server starting on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("failed to start server: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("server forced to shutdown: %v", err)
	}

	log.Println("server exited gracefully")
}
