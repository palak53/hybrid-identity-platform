// Package config loads auth-service settings from environment variables.
//
// Why environment variables: Kubernetes injects ConfigMaps and Secrets as env
// vars, systemd on the on-prem VMs does the same with EnvironmentFile, and so
// does an EC2 launch template. One mechanism works on every runtime we target.
//
// Why fail fast: an invalid value stops the process at startup with a clear
// message. A pod that crashes on deploy is caught by the rollout; a bad value
// discovered at 3 a.m. under load is an incident.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"
)

// Config holds every setting the service needs. Add new settings here, with a
// default and validation in Load.
type Config struct {
	// Port the HTTP server listens on.
	Port int
	// LogLevel is one of debug, info, warn, error.
	LogLevel slog.Level
	// DrainDelay is how long the service keeps serving after it reports
	// "not ready" during shutdown. Kubernetes removes a pod from Service
	// endpoints asynchronously; without this pause, load balancers can still
	// send requests to a server that has already stopped accepting them.
	DrainDelay time.Duration
	// ShutdownTimeout is the maximum time in-flight requests get to finish.
	// DrainDelay + ShutdownTimeout must stay below the pod's
	// terminationGracePeriodSeconds (30s by default), or Kubernetes sends
	// SIGKILL first.
	ShutdownTimeout time.Duration
}

// Defaults used when a variable is not set.
const (
	DefaultPort            = 8080
	DefaultLogLevel        = slog.LevelInfo
	DefaultDrainDelay      = 5 * time.Second
	DefaultShutdownTimeout = 20 * time.Second
)

// Load reads configuration using getenv (pass os.Getenv in production; tests
// pass a fake). It returns every validation problem at once, so an operator
// can fix them all in one go.
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		Port:            DefaultPort,
		LogLevel:        DefaultLogLevel,
		DrainDelay:      DefaultDrainDelay,
		ShutdownTimeout: DefaultShutdownTimeout,
	}
	var errs []error

	if v := getenv("PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil || p < 1 || p > 65535 {
			errs = append(errs, fmt.Errorf("PORT must be a number between 1 and 65535, got %q", v))
		} else {
			cfg.Port = p
		}
	}

	if v := getenv("LOG_LEVEL"); v != "" {
		if err := cfg.LogLevel.UnmarshalText([]byte(v)); err != nil {
			errs = append(errs, fmt.Errorf("LOG_LEVEL must be debug, info, warn or error, got %q", v))
		}
	}

	if v := getenv("DRAIN_DELAY"); v != "" {
		d, err := parseNonNegativeDuration(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("DRAIN_DELAY: %w", err))
		} else {
			cfg.DrainDelay = d
		}
	}

	if v := getenv("SHUTDOWN_TIMEOUT"); v != "" {
		d, err := parseNonNegativeDuration(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("SHUTDOWN_TIMEOUT: %w", err))
		} else {
			cfg.ShutdownTimeout = d
		}
	}

	if err := errors.Join(errs...); err != nil {
		return Config{}, fmt.Errorf("invalid configuration: %w", err)
	}
	return cfg, nil
}

// parseNonNegativeDuration accepts Go duration strings such as "500ms", "5s", "1m".
func parseNonNegativeDuration(v string) (time.Duration, error) {
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("must be a duration like 5s or 500ms, got %q", v)
	}
	if d < 0 {
		return 0, fmt.Errorf("must not be negative, got %q", v)
	}
	return d, nil
}
