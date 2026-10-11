package api

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"webhooklens/internal/api/handlers"
	"webhooklens/internal/api/middleware"
	"webhooklens/internal/cache"
	"webhooklens/internal/config"
	"webhooklens/internal/mailer"
	"webhooklens/internal/store"
	"webhooklens/internal/worker"
)

type RouterDependencies struct {
	Store      store.Store
	Cache      cache.Cache
	WorkerPool *worker.Pool
	Config     *config.Config
	Mailer     mailer.Mailer
}

func NewRouter(deps RouterDependencies) http.Handler {
	r := chi.NewRouter()

	// Base middlewares
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.Timeout(60 * time.Second))

	// CORS configuration for headless API consumers
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link", "Content-Length"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Health check endpoint
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		dbErr := deps.Store.Ping(ctx)
		redisErr := deps.Cache.Ping(ctx)

		if dbErr != nil || redisErr != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"degraded","postgres":"` + errStatus(dbErr) + `","redis":"` + errStatus(redisErr) + `"}`))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"healthy","postgres":"connected","redis":"connected"}`))
	})

	// Handlers
	authHandler := handlers.NewAuthHandler(deps.Store, deps.Config, deps.Mailer)
	endpointHandler := handlers.NewEndpointHandler(deps.Store, deps.Cache, deps.Config)
	ingestionHandler := handlers.NewIngestionHandler(deps.Store, deps.Cache, deps.WorkerPool, deps.Config)
	streamHandler := handlers.NewStreamHandler(deps.Store, deps.Cache, deps.Config)

	// Webhook Ingestion Route - handles any HTTP method (POST, GET, PUT, etc.)
	r.HandleFunc("/w/{endpoint_id}", ingestionHandler.Ingest)

	// Real-time SSE Stream Route
	r.Get("/api/endpoints/{endpoint_id}/stream", streamHandler.Stream)

	// API Routes
	r.Route("/api", func(api chi.Router) {
		// Public Auth Routes
		api.Route("/auth", func(auth chi.Router) {
			auth.Post("/register", authHandler.Register)
			auth.Get("/verify-email", authHandler.VerifyEmail)
			auth.Post("/resend-verification", authHandler.ResendVerification)
			auth.Post("/login", authHandler.Login)
		})

		// Protected Management Routes
		api.Group(func(protected chi.Router) {
			protected.Use(middleware.AuthMiddleware(deps.Config.JWTSecret))

			protected.Get("/auth/me", authHandler.Me)

			protected.Route("/endpoints", func(ep chi.Router) {
				ep.Post("/", endpointHandler.Create)
				ep.Get("/", endpointHandler.List)

				ep.Route("/{endpoint_id}", func(item chi.Router) {
					item.Get("/", endpointHandler.Get)
					item.Patch("/", endpointHandler.Update)
					item.Delete("/", endpointHandler.Delete)

					item.Post("/toggle-auth", endpointHandler.ToggleAuth)
					item.Post("/toggle-active", endpointHandler.ToggleActive)
					item.Post("/regenerate-token", endpointHandler.RegenerateToken)

					item.Get("/logs", endpointHandler.ListLogs)
					item.Delete("/logs", endpointHandler.ClearLogs)
					item.Get("/logs/{log_id}", endpointHandler.GetLog)
				})
			})
		})
	})

	return r
}

func errStatus(err error) string {
	if err != nil {
		return "disconnected"
	}
	return "connected"
}
