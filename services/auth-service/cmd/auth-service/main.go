// Command auth-service runs the identity platform's login and token API.
//
// main only wires pieces together: load config, build the logger and HTTP
// server, run until a shutdown signal arrives, then shut down gracefully.
// All behaviour lives in internal/ packages so it can be tested.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/palak53/hybrid-identity-platform/services/auth-service/internal/config"
	"github.com/palak53/hybrid-identity-platform/services/auth-service/internal/server"
)

func main() {
	// run returns an error instead of exiting itself, so deferred cleanup
	// inside run always executes. Only main decides the exit code.
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "auth-service:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}

	// JSON logs on stdout: Kubernetes, systemd and CloudWatch all collect
	// stdout, and Datadog/Loki parse JSON without custom rules.
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)

	srv := server.New(logger)

	httpServer := &http.Server{
		Addr:    net.JoinHostPort("", strconv.Itoa(cfg.Port)),
		Handler: srv.Handler(),
		// Go's defaults are NO timeouts. Without these, slow or malicious
		// clients can hold connections open forever and exhaust the server
		// (a "slowloris" attack).
		ReadHeaderTimeout: 5 * time.Second,  // time to send request headers
		ReadTimeout:       10 * time.Second, // time to send the whole request
		WriteTimeout:      15 * time.Second, // time for the handler + response
		IdleTimeout:       60 * time.Second, // keep-alive connections between requests
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}

	// Open the port BEFORE reporting ready. If the port is taken we fail
	// immediately with a clear error instead of claiming to be ready.
	listener, err := net.Listen("tcp", httpServer.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", httpServer.Addr, err)
	}

	// ctx is cancelled when SIGTERM (sent by Kubernetes, systemd, or an ASG
	// instance refresh) or SIGINT (Ctrl-C) arrives.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	// Serve in a background goroutine so this one can wait for a signal.
	serveErr := make(chan error, 1)
	go func() { serveErr <- httpServer.Serve(listener) }()

	srv.SetReady(true)
	logger.Info("auth-service started",
		slog.String("addr", listener.Addr().String()),
		slog.String("drain_delay", cfg.DrainDelay.String()),
		slog.String("shutdown_timeout", cfg.ShutdownTimeout.String()),
	)

	// Wait for whichever happens first: the server fails, or a signal arrives.
	select {
	case err := <-serveErr:
		return fmt.Errorf("server stopped unexpectedly: %w", err)
	case <-ctx.Done():
	}
	// Restore default signal handling: a second Ctrl-C now kills immediately.
	stop()

	// Graceful shutdown, in three steps.
	//
	// 1. Report not ready, so the readiness probe fails and Kubernetes
	//    removes this pod from Service endpoints.
	logger.Info("shutdown started: readiness set to false")
	srv.SetReady(false)

	// 2. Keep serving for DrainDelay. Endpoint removal reaches kube-proxy and
	//    load balancers asynchronously; requests already on their way here
	//    must still succeed.
	logger.Info("draining", slog.String("drain_delay", cfg.DrainDelay.String()))
	time.Sleep(cfg.DrainDelay)

	// 3. Stop accepting new connections and wait for in-flight requests,
	//    up to ShutdownTimeout.
	logger.Info("stopping server: waiting for in-flight requests")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown did not finish in %s: %w", cfg.ShutdownTimeout, err)
	}
	if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("server error during shutdown: %w", err)
	}

	logger.Info("shutdown complete")
	return nil
}
