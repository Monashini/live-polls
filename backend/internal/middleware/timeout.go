package middleware

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
)

// NewTimeout bounds how long a single REST request may take.
//
// This exists because the HTTP server's ReadTimeout and WriteTimeout had to be
// switched off for WebSockets: they are whole-connection deadlines, and a live
// feed is a connection that stays open for hours. Removing them would have left
// ordinary API requests unbounded, so the bound moves here, where it can be
// applied to /api and not to /ws.
//
// It sets a deadline on the request context, which is the context every handler
// passes down to MongoDB and Redis. A query that overruns is therefore actually
// cancelled, rather than continuing to burn a connection while nobody waits for
// the answer.
//
// Note it does not forcibly write a response: doing that safely would mean
// racing the handler for the ResponseWriter. The handler notices the cancelled
// context, its database call fails, and the normal error path takes over --
// which is also how a client disconnect is already handled.
func NewTimeout(d time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), d)
		defer cancel()

		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
