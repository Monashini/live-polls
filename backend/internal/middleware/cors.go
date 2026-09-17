package middleware

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// NewCORS builds a CORS handler from an explicit allowlist of origins.
//
// Written by hand rather than pulling in gin-contrib/cors because the policy
// here is small and worth being able to read: an exact-match allowlist, no
// wildcards, credentials enabled. The credentials part is what rules out "*"
// entirely -- browsers refuse to send cookies to a wildcard origin, and the
// anonymous voter cookie means every vote is a credentialed request.
func NewCORS(allowedOrigins []string) gin.HandlerFunc {
	// A set, so the lookup is O(1) regardless of how many origins are listed.
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		allowed[strings.TrimSpace(o)] = struct{}{}
	}

	const (
		allowMethods = "GET, POST, PATCH, DELETE, OPTIONS"
		allowHeaders = "Authorization, Content-Type, X-Request-ID"
		maxAgeSecs   = 600
	)

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")

		// Vary tells caches that the response body depends on the Origin
		// header. Without it, a shared cache can serve a response computed for
		// one origin to a page on another.
		c.Writer.Header().Add("Vary", "Origin")

		_, isAllowed := allowed[origin]

		if origin != "" && isAllowed {
			// Echo the exact origin. Never "*": it is incompatible with
			// Allow-Credentials and would disable the browser's protection.
			c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
			c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
			c.Writer.Header().Set("Access-Control-Expose-Headers", "X-Request-ID")
		}

		if c.Request.Method == http.MethodOptions {
			// Preflight. If the origin is not allowed we still answer, just
			// without the allow headers, and the browser blocks the real
			// request. Answering 204 rather than falling through avoids Gin
			// reporting a 404 for every preflight on a valid route.
			if origin != "" && isAllowed {
				c.Writer.Header().Set("Access-Control-Allow-Methods", allowMethods)
				c.Writer.Header().Set("Access-Control-Allow-Headers", allowHeaders)
				c.Writer.Header().Set("Access-Control-Max-Age", strconv.Itoa(maxAgeSecs))
			}
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
