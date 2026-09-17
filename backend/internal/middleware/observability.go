package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"livepolls/internal/apperr"
)

const requestIDHeader = "X-Request-ID"

// NewRequestID attaches a correlation ID to every request and echoes it back
// in a header. When a client reports "I got a 500", that ID is what turns a
// vague complaint into one grep-able line in the logs.
//
// An inbound X-Request-ID is honoured only if it looks sane, so a caller
// cannot inject newlines into our log output through the header.
func NewRequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(requestIDHeader)
		if !isSafeRequestID(id) {
			id = randomHex(8)
		}
		c.Set(ctxKeyRequestID, id)
		c.Writer.Header().Set(requestIDHeader, id)
		c.Next()
	}
}

func isSafeRequestID(id string) bool {
	if len(id) == 0 || len(id) > 64 {
		return false
	}
	for i := 0; i < len(id); i++ {
		ch := id[i]
		isAlnum := (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
		if !isAlnum && ch != '-' && ch != '_' {
			return false
		}
	}
	return true
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand failing means the OS entropy source is broken. A
		// timestamp is a poor but non-fatal fallback for a log correlation ID.
		return hex.EncodeToString([]byte(time.Now().UTC().Format("150405.000000")))
	}
	return hex.EncodeToString(buf)
}

// NewLogger emits one structured line per request.
//
// Structured (key/value) rather than formatted text, because in production
// these go to a log aggregator that can filter on status>=500 or a specific
// requestId. Gin's built-in logger writes human-readable text that is awkward
// to query. slog is in the standard library, so this costs no dependency.
func NewLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path

		c.Next()

		status := c.Writer.Status()

		attrs := []any{
			"requestId", RequestID(c),
			"method", c.Request.Method,
			"path", path,
			"status", status,
			"durationMs", time.Since(start).Milliseconds(),
			"ip", c.ClientIP(),
			"bytes", c.Writer.Size(),
		}

		// Query strings are logged separately and only when present, so a
		// normal line stays readable.
		if raw := c.Request.URL.RawQuery; raw != "" {
			attrs = append(attrs, "query", raw)
		}

		switch {
		// 499 means the client disconnected before we answered. It sorts above
		// 400 numerically but is not a problem, so it is checked first and
		// logged quietly -- otherwise every abandoned request looks like an
		// error in the dashboards.
		case status == apperr.StatusClientClosed:
			slog.Debug("request", attrs...)
		case status >= http.StatusInternalServerError:
			slog.Error("request", attrs...)
		case status >= http.StatusBadRequest:
			slog.Warn("request", attrs...)
		default:
			slog.Info("request", attrs...)
		}
	}
}

// NewRecovery converts a panic into a logged 500 instead of killing the
// process. Gin ships gin.Recovery(), but it writes an empty body; this one
// returns the same JSON envelope as every other error so the frontend has a
// single response shape to handle.
func NewRecovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				slog.Error("panic recovered",
					"requestId", RequestID(c),
					"method", c.Request.Method,
					"path", c.Request.URL.Path,
					"panic", recovered,
				)

				// The panic value itself is never sent to the client: it
				// routinely contains file paths and internal state.
				err := apperr.Internal(nil)
				c.AbortWithStatusJSON(err.Status, err.Envelope(RequestID(c)))
			}
		}()
		c.Next()
	}
}

// NewBodyLimit caps how much of a request body the server will read. Without
// it, one client streaming an endless body ties up a connection and memory.
// The limit is generous for JSON: the largest legitimate payload here is a
// poll with ten options.
func NewBodyLimit(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		}
		c.Next()
	}
}
