package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestIDGeneratedWhenMissing(t *testing.T) {
	var seen string
	h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = RequestIDFrom(r.Context())
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))

	got := rr.Header().Get(RequestIDHeader)
	if len(got) != 32 {
		t.Errorf("expected a 32-char generated ID, got %q", got)
	}
	if seen != got {
		t.Errorf("context ID %q differs from header ID %q", seen, got)
	}
}

func TestRequestIDReusedWhenValid(t *testing.T) {
	h := RequestID(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(RequestIDHeader, "abc-123_x.y")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if got := rr.Header().Get(RequestIDHeader); got != "abc-123_x.y" {
		t.Errorf("expected incoming ID to be reused, got %q", got)
	}
}

func TestRequestIDReplacedWhenInvalid(t *testing.T) {
	for _, bad := range []string{"has space", "new\nline", strings.Repeat("a", 129)} {
		h := RequestID(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set(RequestIDHeader, bad)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)

		if got := rr.Header().Get(RequestIDHeader); got == bad || len(got) != 32 {
			t.Errorf("invalid ID %q should be replaced, got %q", bad, got)
		}
	}
}

func TestLoggingRecordsStatusAndRequestID(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	h := RequestID(Logging(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = io.WriteString(w, "hi")
	})))
	req := httptest.NewRequest(http.MethodGet, "/token", nil)
	req.Header.Set(RequestIDHeader, "req-1")
	h.ServeHTTP(httptest.NewRecorder(), req)

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("log line is not JSON: %v: %s", err, buf.String())
	}
	if line["status"] != float64(http.StatusTeapot) || line["request_id"] != "req-1" ||
		line["path"] != "/token" || line["bytes"] != float64(2) {
		t.Errorf("unexpected log line: %v", line)
	}
}

func TestLoggingProbesAtDebugLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	h := Logging(logger)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if buf.Len() != 0 {
		t.Errorf("probe request should not be logged at info level, got: %s", buf.String())
	}
}

func TestRecoverReturns500(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	h := Recover(logger)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr.Code)
	}
}
