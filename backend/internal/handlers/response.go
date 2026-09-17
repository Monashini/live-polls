// Package handlers is the HTTP layer. Its only jobs are: parse the request,
// call exactly one service method, and turn the result into a status code plus
// JSON. No validation rules and no database calls live here.
package handlers

import (
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"livepolls/internal/apperr"
	"livepolls/internal/middleware"
)

// respond writes a success payload.
func respond(c *gin.Context, status int, payload any) {
	c.JSON(status, payload)
}

// fail is the single exit point for every error in the HTTP layer.
//
// It does two things that are easy to get wrong if repeated per handler:
// it logs the internal cause server-side, and it sends the client only the
// sanitised envelope. A raw MongoDB error reaching a browser would disclose
// index names and sometimes parts of the connection string.
func fail(c *gin.Context, err error) {
	appErr := apperr.From(err)
	requestID := middleware.RequestID(c)

	// A cancelled request context means the caller disconnected before we
	// finished -- a user navigating away, or React aborting a fetch on
	// unmount. Every database call in flight then fails with "context
	// canceled", which is normal client behaviour, not a server fault.
	//
	// Without this check those show up as 500s. In production that means
	// alerting on healthy traffic and burying the errors that matter. There is
	// no point writing a body either: nobody is on the other end.
	if c.Request.Context().Err() != nil {
		slog.Debug("request abandoned by client",
			"requestId", requestID,
			"path", c.Request.URL.Path,
		)
		c.AbortWithStatus(apperr.StatusClientClosed)
		return
	}

	if appErr.Status >= http.StatusInternalServerError {
		slog.Error("request failed",
			"requestId", requestID,
			"path", c.Request.URL.Path,
			"code", appErr.Code,
			"cause", appErr.Cause(),
		)
	}

	c.AbortWithStatusJSON(appErr.Status, appErr.Envelope(requestID))
}

// bindJSON decodes the request body and converts the several ways that can
// fail into one predictable error.
func bindJSON(c *gin.Context, target any) error {
	err := c.ShouldBindJSON(target)
	if err == nil {
		return nil
	}

	// The body-limit middleware wraps the reader; exceeding it surfaces here.
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return apperr.TooLarge("Request body is too large.")
	}

	if errors.Is(err, io.EOF) {
		return apperr.Validation("A JSON request body is required.", nil)
	}

	// Anything else is malformed JSON or a type mismatch. The driver message
	// is not returned: it can echo the submitted payload back to the caller.
	return apperr.Validation("Request body is not valid JSON.", nil)
}
