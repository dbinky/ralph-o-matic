package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/ryan/ralph-o-matic/internal/auth"
	"github.com/ryan/ralph-o-matic/internal/broadcast"
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
	version      string
	router       chi.Router
	server       *http.Server
	authProvider *auth.EntraProvider
	sessions     *auth.SessionStore
	secure       bool
	broadcaster  *broadcast.Broadcaster
	apiKey       string
}

// ServerOptions holds optional configuration for the server.
// When nil is passed to NewServer, auth is disabled.
type ServerOptions struct {
	AuthProvider *auth.EntraProvider
	Sessions     *auth.SessionStore
	Secure       bool
	Broadcaster  *broadcast.Broadcaster
	Version      string
	APIKey       string // static Bearer token for AuthModeAPIKey
}

// NewServer creates a new API server. Pass nil for opts to disable authentication.
func NewServer(database *db.DB, q *queue.Queue, addr string, opts *ServerOptions) *Server {
	templatesFS, err := fs.Sub(web.Templates, "templates")
	if err != nil {
		log.Fatalf("failed to load templates: %v", err)
	}

	var ver string
	if opts != nil {
		ver = opts.Version
	}

	s := &Server{
		db:        database,
		queue:     q,
		dashboard: dashboard.New(database, q, templatesFS, ver),
		addr:      addr,
		version:   ver,
	}

	if opts != nil {
		s.authProvider = opts.AuthProvider
		s.sessions = opts.Sessions
		s.secure = opts.Secure
		s.broadcaster = opts.Broadcaster
		s.apiKey = opts.APIKey
	}

	s.setupRoutes()
	return s
}

func (s *Server) setupRoutes() {
	r := chi.NewRouter()

	// Middleware (applied to all routes)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware)

	// Health and readiness — always accessible, no auth required.
	// /readiness is intentionally unauthenticated so load balancers and
	// monitoring probes can reach it without credentials. Bind to localhost
	// or restrict via reverse proxy to prevent information leakage.
	r.Get("/health", s.handleHealth)
	r.Get("/readiness", s.handleReadiness)

	// Auth routes — accessible without auth middleware
	r.Mount("/auth", auth.NewAuthRoutes(s.authProvider, s.sessions, s.secure))

	// Protected routes — wrapped in auth middleware
	r.Group(func(r chi.Router) {
		r.Use(auth.Middleware(s.authProvider, s.sessions, s.apiKey))

		// SSE routes — no timeout (long-lived connections)
		// Global SSE is open to all authenticated users (no admin guard) for
		// small-team transparency. Auth middleware still gates access. Revisit
		// if the tool is deployed in a multi-tenant or untrusted-user context.
		r.Get("/api/events", s.handleSSEGlobal)
		r.Get("/api/jobs/{jobID}/events", s.handleSSEJob)

		// All other routes — with timeout
		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(60 * time.Second))

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

			// Dashboard state (for SSE reconnect reconciliation)
			r.Get("/api/dashboard-state", s.handleDashboardState)

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
					r.Post("/notify", s.handleSendNotify)
				})
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
	resp := map[string]string{"status": "ok"}
	if s.version != "" {
		resp["version"] = s.version
	}
	writeJSON(w, http.StatusOK, resp)
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

func (s *Server) handleSSEGlobal(w http.ResponseWriter, r *http.Request) {
	s.serveSSE(w, r, "global")
}

func (s *Server) handleSSEJob(w http.ResponseWriter, r *http.Request) {
	if s.broadcaster == nil {
		writeError(w, http.StatusServiceUnavailable, "SSE not configured")
		return
	}

	// Check job access before subscribing
	jobIDStr := chi.URLParam(r, "jobID")
	jobID, err := strconv.ParseInt(jobIDStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid job ID")
		return
	}

	if _, ok := s.authorizedJob(w, r, jobID); !ok {
		return
	}

	s.serveSSE(w, r, "job:"+jobIDStr)
}

// serveSSE subscribes to a broadcaster topic and streams events to the client
// until the request context is cancelled.
func (s *Server) serveSSE(w http.ResponseWriter, r *http.Request, topic string) {
	if s.broadcaster == nil {
		writeError(w, http.StatusServiceUnavailable, "SSE not configured")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Subscribe before flushing headers so the client is registered
	// by the time the caller receives the response.
	clientID, ch := s.broadcaster.Subscribe(topic)
	defer s.broadcaster.Unsubscribe(topic, clientID)

	// Flush headers to unblock the HTTP client.
	flusher.Flush()

	for {
		select {
		case msg := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
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
