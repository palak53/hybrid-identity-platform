// Package server defines auth-service's HTTP routes and probe behaviour.
package server

import (
	"log/slog"
	"net/http"
	"sync/atomic"

	"github.com/palak53/hybrid-identity-platform/services/auth-service/internal/middleware"
)

// Server holds state shared by the handlers.
type Server struct {
	logger *slog.Logger
	// ready is read by many request goroutines and written by main during
	// startup and shutdown. atomic.Bool makes that safe without a mutex.
	ready atomic.Bool
}

// New returns a Server that starts out NOT ready. main marks it ready once
// startup has finished.
func New(logger *slog.Logger) *Server {
	return &Server{logger: logger}
}

// SetReady flips the readiness probe result.
func (s *Server) SetReady(ready bool) { s.ready.Store(ready) }

// Handler returns the full HTTP handler: routes wrapped in middleware.
//
// Middleware order, outermost first:
//
//	RequestID → Logging → Recover → routes
//
// RequestID runs first so every log line has an ID. Recover sits inside
// Logging so a recovered panic is logged with its 500 status.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /readyz", s.readyz)

	var h http.Handler = mux
	h = middleware.Recover(s.logger)(h)
	h = middleware.Logging(s.logger)(h)
	h = middleware.RequestID(h)
	return h
}

// healthz is the LIVENESS probe: "is the process alive and able to serve
// HTTP?". It must never check dependencies such as Postgres or Redis. If it
// did, a database blip would fail liveness on every pod at once, and
// Kubernetes would restart them all, turning a small problem into an outage.
func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	writePlain(w, http.StatusOK, "ok")
}

// readyz is the READINESS probe: "should this pod receive traffic right
// now?". It returns 503 before startup completes and during shutdown, so
// Kubernetes and load balancers stop routing to the pod. Dependency checks
// (Postgres, Redis) will be added here later.
func (s *Server) readyz(w http.ResponseWriter, _ *http.Request) {
	if !s.ready.Load() {
		writePlain(w, http.StatusServiceUnavailable, "not ready")
		return
	}
	writePlain(w, http.StatusOK, "ready")
}

// writePlain sends a short plain-text response that must never be cached.
func writePlain(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body + "\n"))
}
