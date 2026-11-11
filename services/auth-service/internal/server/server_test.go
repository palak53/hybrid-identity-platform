package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestServer() *Server {
	return New(slog.New(slog.NewJSONHandler(io.Discard, nil)))
}

func TestRoutes(t *testing.T) {
	tests := []struct {
		name       string
		ready      bool
		method     string
		path       string
		wantStatus int
		wantBody   string
	}{
		{"liveness ok when ready", true, http.MethodGet, "/healthz", http.StatusOK, "ok"},
		{"liveness ok even when not ready", false, http.MethodGet, "/healthz", http.StatusOK, "ok"},
		{"not ready before startup", false, http.MethodGet, "/readyz", http.StatusServiceUnavailable, "not ready"},
		{"ready after startup", true, http.MethodGet, "/readyz", http.StatusOK, "ready"},
		{"unknown path", true, http.MethodGet, "/nope", http.StatusNotFound, ""},
		{"wrong method", true, http.MethodPost, "/healthz", http.StatusMethodNotAllowed, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer()
			s.SetReady(tt.ready)
			rr := httptest.NewRecorder()
			s.Handler().ServeHTTP(rr, httptest.NewRequest(tt.method, tt.path, nil))

			if rr.Code != tt.wantStatus {
				t.Errorf("status: got %d, want %d", rr.Code, tt.wantStatus)
			}
			if tt.wantBody != "" && strings.TrimSpace(rr.Body.String()) != tt.wantBody {
				t.Errorf("body: got %q, want %q", rr.Body.String(), tt.wantBody)
			}
			if rr.Header().Get("X-Request-ID") == "" {
				t.Error("every response should carry an X-Request-ID")
			}
		})
	}
}

func TestReadinessFlipsDuringShutdown(t *testing.T) {
	s := newTestServer()
	s.SetReady(true)
	s.SetReady(false) // what main does when SIGTERM arrives

	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 after shutdown starts, got %d", rr.Code)
	}
}
