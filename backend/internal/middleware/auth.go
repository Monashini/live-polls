package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"

	"livepolls/internal/apperr"
	"livepolls/internal/services"
)

// RequireAuth rejects any request without a valid bearer token, and puts the
// caller's user ID into the request context for the handler to read.
//
// The token arrives in the Authorization header rather than a cookie. That
// choice means the API is immune to CSRF on authenticated routes: a browser
// will happily attach a cookie to a cross-site request, but it will never
// attach an Authorization header the attacker's page did not set.
func RequireAuth(auth *services.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, ok := bearerToken(c.GetHeader("Authorization"))
		if !ok {
			abort(c, apperr.Unauthorized("A valid access token is required."))
			return
		}

		userID, err := auth.ParseToken(token)
		if err != nil {
			// ParseToken already returns an apperr with a safe message, so
			// nothing about why the token failed leaks beyond "invalid".
			abort(c, apperr.From(err))
			return
		}

		c.Set(ctxKeyUserID, userID)
		c.Next()
	}
}

// bearerToken extracts the credential from "Bearer <token>". The scheme match
// is case-insensitive because RFC 7235 says the scheme is, and some clients
// send "bearer".
func bearerToken(header string) (string, bool) {
	const prefix = "bearer "

	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return "", false
	}
	token := strings.TrimSpace(header[len(prefix):])
	if token == "" {
		return "", false
	}
	return token, true
}

func abort(c *gin.Context, err *apperr.Error) {
	c.AbortWithStatusJSON(err.Status, err.Envelope(RequestID(c)))
}
