package handlers

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"livepolls/internal/apperr"
	"livepolls/internal/config"
	"livepolls/internal/db"
	"livepolls/internal/middleware"
	"livepolls/internal/services"
)

// maxRequestBody caps every inbound body. The largest legitimate request is a
// poll with ten 120-character options, which is well under a kilobyte.
const maxRequestBody = 32 * 1024

// NewRouter wires middleware and routes. Keeping this in one function means
// the answer to "is this endpoint protected?" is readable in a single screen
// rather than scattered across handler files.
func NewRouter(
	cfg *config.Config,
	store *db.Store,
	auth *services.AuthService,
	polls *services.PollService,
) (*gin.Engine, error) {
	if cfg.IsProd {
		gin.SetMode(gin.ReleaseMode)
	}

	// gin.New, not gin.Default: Default installs Gin's own logger and
	// recovery, and this app supplies both with structured logging instead.
	r := gin.New()

	// Gin derives the client IP from X-Forwarded-For. That header is
	// attacker-controlled unless we name which proxies may set it, so the
	// default here is to trust nobody and read the socket address directly.
	// Once deployed behind a platform proxy, TRUSTED_PROXIES must name it or
	// every request will appear to come from the proxy.
	if len(cfg.TrustedProxies) == 0 {
		if err := r.SetTrustedProxies(nil); err != nil {
			return nil, err
		}
	} else if err := r.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		return nil, err
	}

	// Order matters. RequestID first so every later log line and error
	// envelope can quote it; Recovery before anything that might panic;
	// CORS before routing so preflights never reach a handler.
	r.Use(
		middleware.NewRequestID(),
		middleware.NewRecovery(),
		middleware.NewLogger(),
		middleware.NewCORS(cfg.AllowedOrigins),
		middleware.NewBodyLimit(maxRequestBody),
	)

	// Unknown routes and wrong methods get the same JSON envelope as every
	// other error, so a client never has to parse Gin's plain-text default.
	r.NoRoute(func(c *gin.Context) {
		fail(c, apperr.NotFound("No such endpoint."))
	})
	r.NoMethod(func(c *gin.Context) {
		fail(c, &apperr.Error{
			Status:  http.StatusMethodNotAllowed,
			Code:    apperr.CodeNotFound,
			Message: "That method is not allowed on this endpoint.",
		})
	})

	healthHandler := NewHealthHandler(store)
	authHandler := NewAuthHandler(auth)
	pollHandler := NewPollHandler(polls, cfg)

	r.GET("/healthz", healthHandler.Check)

	api := r.Group("/api")
	{
		authRoutes := api.Group("/auth")
		{
			authRoutes.POST("/signup", authHandler.Signup)
			authRoutes.POST("/login", authHandler.Login)
			authRoutes.GET("/me", middleware.RequireAuth(auth), authHandler.Me)
		}

		pollRoutes := api.Group("/polls")
		{
			// Public: anyone with the link can read results and vote.
			pollRoutes.GET("/:id", pollHandler.Get)
			pollRoutes.POST("/:id/vote", pollHandler.Vote)

			// Protected: creating and managing a poll needs an account.
			protected := pollRoutes.Group("", middleware.RequireAuth(auth))
			{
				protected.POST("", pollHandler.Create)
				protected.GET("", pollHandler.ListMine)
				protected.PATCH("/:id/close", pollHandler.Close)
				protected.DELETE("/:id", pollHandler.Delete)
			}
		}
	}

	slog.Info("routes registered", "corsOrigins", cfg.AllowedOrigins, "mode", gin.Mode())
	return r, nil
}
