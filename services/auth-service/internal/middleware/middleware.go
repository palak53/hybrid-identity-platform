// Package middleware holds HTTP middleware shared by every route.
//
// A middleware is a function that takes a handler and returns a new handler
// that does something before and/or after calling the original. It is the Go
// equivalent of Django middleware.
package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"
)

// RequestIDHeader is the header used to carry a request ID between services.
const RequestIDHeader = "X-Request-ID"

// ctxKey is a private type so no other package can collide with our context key.
type ctxKey struct{}

// RequestID makes sure every request has an ID. It reuses a valid incoming
// X-Request-ID (so one ID follows a request across services), otherwise it
// generates one. The ID is returned in the response header and stored in the
// request context for logging.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(RequestIDHeader)
		if !validRequestID(id) {
			id = newRequestID()
		}
		w.Header().Set(RequestIDHeader, id)
		ctx := context.WithValue(r.Context(), ctxKey{}, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequestIDFrom returns the request ID stored by RequestID, or "" if none.
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(ctxKey{}).(string)
	return id
}

// validRequestID rejects empty, oversized, or unusual IDs. The ID ends up in
// logs and headers, so accepting arbitrary client input would allow log
// injection and log bloat.
func validRequestID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, c := range id {
		isAlnum := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
		if !isAlnum && c != '-' && c != '_' && c != '.' {
			return false
		}
	}
	return true
}

// newRequestID returns 16 random bytes as 32 hex characters.
func newRequestID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // crypto/rand.Read never returns an error on supported platforms
	return hex.EncodeToString(b)
}

// statusRecorder wraps http.ResponseWriter to remember the status code and
// body size, which the standard writer does not expose.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK // Write without WriteHeader means 200
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// Unwrap lets http.ResponseController reach the underlying writer (for
// flushing, deadlines, etc.).
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// quietPaths are logged at debug level. Kubernetes probes hit them every few
// seconds per pod; logging them at info would bury real traffic and cost money
// in the log pipeline.
var quietPaths = map[string]bool{"/healthz": true, "/readyz": true}

// Logging writes one structured JSON line per request.
func Logging(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)

			if rec.status == 0 {
				rec.status = http.StatusOK // handler wrote nothing at all
			}
			level := slog.LevelInfo
			if quietPaths[r.URL.Path] {
				level = slog.LevelDebug
			}
			logger.LogAttrs(r.Context(), level, "request",
				slog.String("request_id", RequestIDFrom(r.Context())),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rec.status),
				slog.Int("bytes", rec.bytes),
				slog.Float64("duration_ms", float64(time.Since(start).Microseconds())/1000),
				slog.String("remote_addr", r.RemoteAddr),
			)
		})
	}
}

// Recover turns a panic inside a handler into a logged 500 response, instead
// of a dropped connection with no trace of what happened.
func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if v := recover(); v != nil {
					if v == http.ErrAbortHandler {
						panic(v) // deliberate abort; let net/http handle it
					}
					logger.ErrorContext(r.Context(), "panic in handler",
						slog.String("request_id", RequestIDFrom(r.Context())),
						slog.Any("panic", v),
						slog.String("stack", string(debug.Stack())),
					)
					http.Error(w, "internal server error", http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
