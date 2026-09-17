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
	"livepolls/internal/redisstore"
	"livepolls/internal/services"
	"livepolls/internal/ws"
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

	live, err := redisstore.Connect(startupCtx, cfg.RedisURL, cfg.CountsTTL)
	if err != nil {
		return err
	}
	slog.Info("redis connected", "countsTTL", cfg.CountsTTL.String())

	// hubCtx outlives every request. It is cancelled during shutdown, which is
	// what stops the Pub/Sub pump and disconnects the sockets.
	hubCtx, stopHub := context.WithCancel(context.Background())
	defer stopHub()

	hub := ws.NewHub(live)
	go hub.Run(hubCtx)

	authService, err := services.NewAuthService(store.Users, cfg.JWTSecret, cfg.JWTTTL)
	if err != nil {
		return err
	}
	pollService := services.NewPollService(
		store.Polls, store.Votes, live, cfg.VoteRateLimit, cfg.VoteRateWindow,
	)

	router, err := handlers.NewRouter(cfg, store, live, hub, authService, pollService)
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: router,

		// ReadHeaderTimeout still applies: it bounds how long a client may
		// dawdle over the request line and headers, which is the classic
		// Slowloris vector, and it is evaluated before any upgrade happens.
		ReadHeaderTimeout: 10 * time.Second,

		// ReadTimeout and WriteTimeout are deliberately left at zero.
		//
		// They are whole-connection deadlines, and a WebSocket is a connection
		// that is supposed to stay open for hours. A 30s WriteTimeout would
		// sever every live viewer twice a minute. REST requests are bounded
		// instead by the per-request context deadline installed on the /api
		// group, and sockets by their own read/write deadlines in the ws
		// package -- both of which know the difference between the two.
		ReadTimeout:  0,
		WriteTimeout: 0,
		IdleTimeout:  120 * time.Second,
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

	// Close the sockets first. Shutdown waits for active connections, and a
	// WebSocket is active by definition -- without this the server would sit
	// out the full shutdown timeout on every deploy.
	stopHub()
	hub.Close()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("graceful shutdown timed out", "err", err)
	}
	if err := store.Disconnect(shutdownCtx); err != nil {
		slog.Error("mongodb disconnect failed", "err", err)
	}
	if err := live.Close(); err != nil {
		slog.Error("redis disconnect failed", "err", err)
	}

	slog.Info("shutdown complete")
	return nil
}
