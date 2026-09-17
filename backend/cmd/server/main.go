// Command server is the entry point for the Live Polls API.
//
// It does four things and nothing else: load configuration, open the database,
// construct the dependency graph, and run an HTTP server that shuts down
// cleanly. All behaviour lives in internal packages.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"livepolls/internal/config"
	"livepolls/internal/db"
	"livepolls/internal/handlers"
	"livepolls/internal/services"
)

func main() {
	// Exit codes are set by run() so that every deferred cleanup still runs;
	// calling os.Exit directly inside main would skip them.
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Startup gets its own deadline. Without one, an unreachable database
	// leaves the process hanging instead of failing a deploy honestly.
	startupCtx, cancelStartup := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelStartup()

	store, err := db.Connect(startupCtx, cfg.MongoURI, cfg.MongoDatabase)
	if err != nil {
		return err
	}
	slog.Info("mongodb connected", "database", cfg.MongoDatabase)

	if err := store.EnsureIndexes(startupCtx); err != nil {
		return err
	}
	slog.Info("indexes ensured")

	authService, err := services.NewAuthService(store.Users, cfg.JWTSecret, cfg.JWTTTL)
	if err != nil {
		return err
	}
	pollService := services.NewPollService(store.Polls, store.Votes)

	router, err := handlers.NewRouter(cfg, store, authService, pollService)
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: router,

		// Explicit timeouts. Go's defaults are "no timeout", which lets a slow
		// or malicious client hold a connection open indefinitely.
		// Note for a later phase: WriteTimeout has to be lifted or scoped once
		// WebSocket connections live on this server, since those stay open by
		// design.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       20 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// NotifyContext cancels when the process is asked to stop. SIGTERM is what
	// container platforms send before killing an instance; catching it is what
	// makes a redeploy drain in-flight requests instead of dropping them.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serverErr := make(chan error, 1)
	go func() {
		slog.Info("http server listening", "addr", srv.Addr, "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		// Failed to bind, usually a port already in use.
		return err
	case <-ctx.Done():
		slog.Info("shutdown signal received, draining connections")
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelShutdown()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("graceful shutdown timed out", "err", err)
	}
	if err := store.Disconnect(shutdownCtx); err != nil {
		slog.Error("mongodb disconnect failed", "err", err)
	}

	slog.Info("shutdown complete")
	return nil
}
