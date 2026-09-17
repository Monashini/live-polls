// Package middleware holds cross-cutting HTTP concerns: identity, logging,
// panic containment, CORS and request limits. Handlers stay free of them.
package middleware

import (
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Context keys. Typed accessors below are the only way these are read, so a
// typo in a key string cannot silently produce a zero value at a call site.
const (
	ctxKeyUserID    = "auth.userID"
	ctxKeyRequestID = "req.id"
)

// UserID returns the authenticated user's ID. The boolean is false on any
// route that did not run RequireAuth, which makes "did I remember to protect
// this route" impossible to get wrong by accident.
func UserID(c *gin.Context) (bson.ObjectID, bool) {
	raw, exists := c.Get(ctxKeyUserID)
	if !exists {
		return bson.NilObjectID, false
	}
	id, ok := raw.(bson.ObjectID)
	if !ok || id.IsZero() {
		return bson.NilObjectID, false
	}
	return id, true
}

// RequestID returns the correlation ID attached to this request, or "" if the
// RequestID middleware is not installed.
func RequestID(c *gin.Context) string {
	if raw, exists := c.Get(ctxKeyRequestID); exists {
		if id, ok := raw.(string); ok {
			return id
		}
	}
	return ""
}
