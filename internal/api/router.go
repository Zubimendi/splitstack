package api

import (
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/Zubimendi/splitstack/internal/config"
	"github.com/Zubimendi/splitstack/internal/observability"
)

func NewRouter(h *Handlers, cfg config.Config) *chi.Mux {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// Public routes
	r.Get("/metrics", observability.MetricsHandler().ServeHTTP)
	r.Post("/users", h.CreateUser)
	r.Post("/login", h.Login)

	// Protected routes
	r.Group(func(r chi.Router) {
		r.Use(JWTAuthMiddleware([]byte("dev-secret-key")))

		r.Get("/users", h.GetUsers)
		r.Get("/users/{userId}", h.GetUser)
		
		r.Post("/groups", h.CreateGroup)
		r.Get("/groups", h.GetGroups)
		r.Get("/groups/{groupId}", h.GetGroup)
		r.Post("/groups/{groupId}/expenses", h.AddExpense)
		r.Post("/groups/{groupId}/settlements", h.RecordSettlement)
		r.Get("/groups/{groupId}/balances", h.GetBalances)
		r.Get("/groups/{groupId}/balances/verified", h.GetVerifiedBalances)
		r.Get("/groups/{groupId}/settlement-plan", h.GetSettlementPlan)
	})

	return r
}
