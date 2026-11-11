package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

// fakeEnv turns a map into a getenv function, so tests never touch the real
// environment.
func fakeEnv(vars map[string]string) func(string) string {
	return func(key string) string { return vars[key] }
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(fakeEnv(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := Config{
		Port:            DefaultPort,
		LogLevel:        DefaultLogLevel,
		DrainDelay:      DefaultDrainDelay,
		ShutdownTimeout: DefaultShutdownTimeout,
	}
	if cfg != want {
		t.Errorf("got %+v, want %+v", cfg, want)
	}
}

func TestLoadValidValues(t *testing.T) {
	cfg, err := Load(fakeEnv(map[string]string{
		"PORT":             "9090",
		"LOG_LEVEL":        "debug",
		"DRAIN_DELAY":      "0s",
		"SHUTDOWN_TIMEOUT": "1500ms",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != 9090 || cfg.LogLevel != slog.LevelDebug ||
		cfg.DrainDelay != 0 || cfg.ShutdownTimeout != 1500*time.Millisecond {
		t.Errorf("unexpected config: %+v", cfg)
	}
}

// A table-driven test: each row is one invalid input and the text the error
// must contain. Adding a case is one line.
func TestLoadInvalidValues(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{"port not a number", map[string]string{"PORT": "abc"}, "PORT"},
		{"port zero", map[string]string{"PORT": "0"}, "PORT"},
		{"port too large", map[string]string{"PORT": "70000"}, "PORT"},
		{"unknown log level", map[string]string{"LOG_LEVEL": "loud"}, "LOG_LEVEL"},
		{"drain delay not a duration", map[string]string{"DRAIN_DELAY": "5"}, "DRAIN_DELAY"},
		{"negative shutdown timeout", map[string]string{"SHUTDOWN_TIMEOUT": "-1s"}, "SHUTDOWN_TIMEOUT"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(fakeEnv(tt.env))
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not mention %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoadReportsAllErrorsAtOnce(t *testing.T) {
	_, err := Load(fakeEnv(map[string]string{"PORT": "abc", "LOG_LEVEL": "loud"}))
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	for _, want := range []string{"PORT", "LOG_LEVEL"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}
