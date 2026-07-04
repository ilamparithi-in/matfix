package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// buildRouter constructs the chi router and attaches all routes.
func buildRouter(cfg Config, rl *rateLimiter) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(debugLogger)

	// Health probes - unauthenticated.
	r.Get("/health/live", liveHandler())
	r.Get("/health/ready", readyHandler(cfg.Accounts))

	// Prometheus metrics - unauthenticated.
	r.Get("/metrics", metricsHandler())

	// Authenticated, rate-limited API routes.
	r.Group(func(r chi.Router) {
		r.Use(APIKeyMiddleware(cfg.APIKeyStore))
		r.Use(rl.middleware())

		r.With(RequireRoute("send")).Post("/v1/send", sendHandler(cfg.Submission))
		r.With(RequireRoute("receive")).Post("/v1/receive", receiveHandler(cfg.Correlation))
		r.With(RequireRoute("ask")).Post("/v1/ask", askHandler(cfg.Submission, cfg.Correlation))
		r.Get("/v1/jobs/{job_id}", jobStatusHandler(cfg.QueueStore, cfg.CorrStore))

		r.Route("/v1/admin", func(r chi.Router) {
			r.Use(RequireRoute("admin"))
			r.Get("/queue", adminQueueHandler(cfg.Accounts, cfg.QueueStore))
			r.Get("/accounts", adminAccountsHandler(cfg.Accounts))
			r.Get("/subscriptions", adminSubscriptionsHandler(cfg.CorrStore))
		})
	})

	return r
}

// debugLogger is middleware that emits a slog.Debug line for every request,
// including method, path, status code, and elapsed time.
// It is a no-op when the effective log level is above Debug.
func debugLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		slog.Debug("api: request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"bytes", ww.BytesWritten(),
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", middleware.GetReqID(r.Context()),
			"remote_addr", r.RemoteAddr,
		)
	})
}
