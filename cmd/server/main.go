package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"webhooklens/internal/api"
	"webhooklens/internal/cache"
	"webhooklens/internal/config"
	"webhooklens/internal/store/postgres"
	"webhooklens/internal/worker"
)

func main() {
	cfg := config.Load()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Printf("[server] initializing WebHookLens API on port %s", cfg.Port)

	// Initialize PostgreSQL store
	pgStore, err := postgres.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("[server] fatal: failed to initialize database: %v", err)
	}
	defer pgStore.Close()
	log.Println("[server] connected to PostgreSQL")

	// Initialize Redis cache and pub/sub
	redisCache, err := cache.NewRedisCache(ctx, cfg.RedisURL)
	if err != nil {
		log.Fatalf("[server] fatal: failed to initialize redis: %v", err)
	}
	defer redisCache.Close()
	log.Println("[server] connected to Redis")

	// Initialize and start the ingestion worker pool
	workerPool := worker.NewPool(pgStore, redisCache, cfg.WorkerPoolSize, cfg.WorkerBufferSize)
	workerPool.Start()

	// Initialize HTTP router
	router := api.NewRouter(api.RouterDependencies{
		Store:      pgStore,
		Cache:      redisCache,
		WorkerPool: workerPool,
		Config:     cfg,
	})

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start HTTP server in a separate goroutine
	serverErrCh := make(chan error, 1)
	go func() {
		log.Printf("[server] WebHookLens API listening at http://localhost:%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrCh <- err
		}
	}()

	// Wait for termination signal or server error
	select {
	case <-ctx.Done():
		log.Println("[server] shutdown signal received, initiating graceful termination...")
	case err := <-serverErrCh:
		log.Fatalf("[server] HTTP server crashed: %v", err)
	}

	// Graceful shutdown sequence
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("[server] error shutting down HTTP server: %v", err)
	} else {
		log.Println("[server] HTTP server shut down successfully")
	}

	if err := workerPool.Stop(shutdownCtx); err != nil {
		log.Printf("[server] error draining worker pool: %v", err)
	}

	log.Println("[server] WebHookLens API terminated cleanly")
}
