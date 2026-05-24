package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// buildRouter constructs the chi router and attaches all routes.
func buildRouter(cfg Config, rl *rateLimiter) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)

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

		r.Route("/v1/admin", func(r chi.Router) {
			r.Use(RequireRoute("admin"))
			r.Get("/queue", adminQueueHandler(cfg.Accounts, cfg.QueueStore))
			r.Get("/accounts", adminAccountsHandler(cfg.Accounts))
			r.Get("/subscriptions", adminSubscriptionsHandler(cfg.CorrStore))
		})
	})

	return r
}
