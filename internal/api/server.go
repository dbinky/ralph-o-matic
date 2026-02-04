package api

import (
	"context"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/ryan/ralph-o-matic/internal/auth"
	"github.com/ryan/ralph-o-matic/internal/dashboard"
	"github.com/ryan/ralph-o-matic/internal/db"
	"github.com/ryan/ralph-o-matic/internal/queue"
	"github.com/ryan/ralph-o-matic/web"
)

// Server is the HTTP API server
type Server struct {
	db           *db.DB
	queue        *queue.Queue
	dashboard    *dashboard.Dashboard
	addr         string
	router       chi.Router
	server       *http.Server
	authProvider *auth.EntraProvider
	sessions     *auth.SessionStore
	secure       bool
}

// ServerOptions holds optional configuration for the server.
// When nil is passed to NewServer, auth is disabled.
type ServerOptions struct {
	AuthProvider *auth.EntraProvider
	Sessions     *auth.SessionStore
	Secure       bool
}

// NewServer creates a new API server. Pass nil for opts to disable authentication.
func NewServer(database *db.DB, q *queue.Queue, addr string, opts *ServerOptions) *Server {
	templatesFS, err := fs.Sub(web.Templates, "templates")
	if err != nil {
		log.Fatalf("failed to load templates: %v", err)
	}

	s := &Server{
		db:        database,
		queue:     q,
		dashboard: dashboard.New(database, q, templatesFS),
		addr:      addr,
	}

	if opts != nil {
		s.authProvider = opts.AuthProvider
		s.sessions = opts.Sessions
		s.secure = opts.Secure
	}

	s.setupRoutes()
	return s
}

func (s *Server) setupRoutes() {
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(corsMiddleware)

	// Health check — always accessible, no auth required
	r.Get("/health", s.handleHealth)

	// Auth routes — accessible without auth middleware
	r.Mount("/auth", auth.NewAuthRoutes(s.authProvider, s.sessions, s.secure))

	// Protected routes — wrapped in auth middleware
	r.Group(func(r chi.Router) {
		r.Use(auth.Middleware(s.authProvider, s.sessions))

		// Dashboard
		r.Get("/", s.dashboard.HandleIndex)
		r.Get("/config", s.dashboard.HandleConfig)
		r.Get("/jobs/{jobID}", func(w http.ResponseWriter, r *http.Request) {
			idStr := chi.URLParam(r, "jobID")
			id, err := strconv.ParseInt(idStr, 10, 64)
			if err != nil {
				http.Error(w, "Invalid job ID", http.StatusBadRequest)
				return
			}
			s.dashboard.HandleJob(w, r, id)
		})

		// API routes
		r.Route("/api", func(r chi.Router) {
			r.Route("/jobs", func(r chi.Router) {
				r.Post("/", s.handleCreateJob)
				r.Get("/", s.handleListJobs)
				r.Put("/order", auth.RequireRole("Admin", s.handleReorderJobs))

				r.Route("/{jobID}", func(r chi.Router) {
					r.Get("/", s.handleGetJob)
					r.Delete("/", s.handleCancelJob)
					r.Patch("/", s.handleUpdateJob)
					r.Get("/logs", s.handleGetJobLogs)
					r.Post("/pause", s.handlePauseJob)
					r.Post("/resume", s.handleResumeJob)
				})
			})

			r.Route("/config", func(r chi.Router) {
				r.Get("/", s.handleGetConfig)
				r.Patch("/", auth.RequireRole("Admin", s.handleUpdateConfig))
				r.Post("/test-notify", auth.RequireRole("Admin", s.handleTestNotify))
			})
		})
	})

	s.router = r
}

// Router returns the chi router for testing
func (s *Server) Router() chi.Router {
	return s.router
}

// Start begins listening for HTTP requests
func (s *Server) Start() error {
	s.server = &http.Server{
		Addr:              s.addr,
		Handler:           s.router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("API server starting on %s", s.addr)
	return s.server.ListenAndServe()
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	if s.server == nil {
		return nil
	}
	return s.server.Shutdown(ctx)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Response helpers
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// CORS middleware
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}
