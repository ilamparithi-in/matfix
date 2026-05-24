package api

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// metricsHandler returns a handler for GET /metrics that serves the default
// Prometheus registry in text exposition format.
func metricsHandler() http.HandlerFunc {
	h := promhttp.Handler()
	return func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r)
	}
}
