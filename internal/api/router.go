package api

import (
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewRouter builds the HTTP handler tree for the service.
func NewRouter(pool *pgxpool.Pool, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthzHandler(pool, logger))

	return recoverPanic(logRequests(mux, logger), logger)
}
