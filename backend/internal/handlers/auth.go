package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"livepolls/internal/apperr"
	"livepolls/internal/middleware"
	"livepolls/internal/services"
)

type AuthHandler struct {
	auth *services.AuthService
}

func NewAuthHandler(auth *services.AuthService) *AuthHandler {
	return &AuthHandler{auth: auth}
}

// credentialsRequest deliberately carries no `binding:"required"` tags. All
// rules live in services.ValidateCredentials, so there is exactly one place
// that decides what a valid email or password is.
type credentialsRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type authResponse struct {
	Token     string `json:"token"`
	ExpiresIn int    `json:"expiresIn"`
	User      any    `json:"user"`
}

// POST /api/auth/signup
func (h *AuthHandler) Signup(c *gin.Context) {
	var req credentialsRequest
	if err := bindJSON(c, &req); err != nil {
		fail(c, err)
		return
	}

	user, token, err := h.auth.Signup(c.Request.Context(), req.Email, req.Password)
	if err != nil {
		fail(c, err)
		return
	}

	respond(c, http.StatusCreated, authResponse{
		Token:     token,
		ExpiresIn: h.auth.TokenTTLSeconds(),
		User:      user.View(),
	})
}

// POST /api/auth/login
func (h *AuthHandler) Login(c *gin.Context) {
	var req credentialsRequest
	if err := bindJSON(c, &req); err != nil {
		fail(c, err)
		return
	}

	user, token, err := h.auth.Login(c.Request.Context(), req.Email, req.Password)
	if err != nil {
		fail(c, err)
		return
	}

	respond(c, http.StatusOK, authResponse{
		Token:     token,
		ExpiresIn: h.auth.TokenTTLSeconds(),
		User:      user.View(),
	})
}

// GET /api/auth/me returns the account behind the bearer token. It exists so
// the frontend can restore a session on page load without storing user data
// alongside the token.
func (h *AuthHandler) Me(c *gin.Context) {
	userID, ok := middleware.UserID(c)
	if !ok {
		// Unreachable while the route sits behind RequireAuth, but a handler
		// that assumes a middleware ran is a handler that breaks silently the
		// day someone reorders the router.
		fail(c, apperr.Unauthorized("A valid access token is required."))
		return
	}

	user, err := h.auth.UserByID(c.Request.Context(), userID)
	if err != nil {
		fail(c, err)
		return
	}

	respond(c, http.StatusOK, gin.H{"user": user.View()})
}
