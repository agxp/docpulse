package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/arman/docpulse/internal/api/middleware"
	"github.com/arman/docpulse/internal/database"
)

func NewRouter(h *Handlers, tenants *database.TenantStore) http.Handler {
	r := chi.NewRouter()

	r.Use(chimiddleware.Recoverer)
	r.Use(middleware.Logging)

	// Public
	r.Get("/health", h.HandleHealth)

	// Authenticated
	r.Group(func(r chi.Router) {
		r.Use(middleware.Auth(tenants))

		r.Post("/v1/extract", h.HandleExtract)
		r.Get("/v1/jobs/{id}", h.HandleGetJob)
		r.Get("/v1/jobs", h.HandleListJobs)
	})

	return r
}
