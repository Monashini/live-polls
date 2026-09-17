package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"livepolls/internal/config"
	"livepolls/internal/services"
)

func testRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		Env:            "test",
		Port:           "8080",
		JWTSecret:      []byte("test-secret-that-is-long-enough-000"),
		JWTTTL:         time.Hour,
		AllowedOrigins: []string{"http://localhost:5173"},
		CookieSameSite: "lax",
		IPHashSalt:     []byte("salt"),
	}

	auth, err := services.NewAuthService(nil, cfg.JWTSecret, cfg.JWTTTL)
	if err != nil {
		t.Fatalf("auth service: %v", err)
	}

	// nil store / live / hub: these tests exercise routing, guards and CORS,
	// none of which touch a database. The health handler already tolerates a
	// nil Redis store and hub, and no test below calls a route that would
	// dereference them.
	r, err := NewRouter(cfg, nil, nil, nil,
		auth,
		services.NewPollService(nil, nil, nil, 30, time.Minute),
	)
	if err != nil {
		t.Fatalf("router: %v", err)
	}
	return r
}

func TestRoutesRegister(t *testing.T) {
	r := testRouter(t)

	want := map[string]bool{
		"GET /healthz":               false,
		"POST /api/auth/signup":      false,
		"POST /api/auth/login":       false,
		"GET /api/auth/me":           false,
		"POST /api/polls":            false,
		"GET /api/polls":             false,
		"GET /api/polls/:id":         false,
		"PATCH /api/polls/:id/close": false,
		"DELETE /api/polls/:id":      false,
		"POST /api/polls/:id/vote":   false,
		"GET /ws/polls/:id":          false,
	}

	for _, route := range r.Routes() {
		key := route.Method + " " + route.Path
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}

	for key, found := range want {
		if !found {
			t.Errorf("route not registered: %s", key)
		}
	}
}

func TestProtectedRoutesRejectAnonymous(t *testing.T) {
	r := testRouter(t)

	cases := []struct{ method, path string }{
		{http.MethodPost, "/api/polls"},
		{http.MethodGet, "/api/polls"},
		{http.MethodGet, "/api/auth/me"},
		{http.MethodPatch, "/api/polls/abc/close"},
		{http.MethodDelete, "/api/polls/abc"},
	}

	for _, tc := range cases {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))

		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: got %d, want 401", tc.method, tc.path, w.Code)
		}
	}
}

func TestUnknownRouteReturnsJSONEnvelope(t *testing.T) {
	r := testRouter(t)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/nope", nil))

	if w.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct[:16] != "application/json" {
		t.Errorf("content-type = %q, want JSON", ct)
	}
}

func TestCORSPreflight(t *testing.T) {
	r := testRouter(t)

	req := httptest.NewRequest(http.MethodOptions, "/api/polls", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set("Access-Control-Request-Method", "POST")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 204", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Errorf("allow-origin = %q", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("allow-credentials = %q, want true", got)
	}
}

func TestCORSRejectsUnknownOrigin(t *testing.T) {
	r := testRouter(t)

	req := httptest.NewRequest(http.MethodOptions, "/api/polls", nil)
	req.Header.Set("Origin", "https://evil.example.com")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("allow-origin leaked for untrusted origin: %q", got)
	}
}

func TestTokenRoundTrip(t *testing.T) {
	auth, err := services.NewAuthService(nil, []byte("test-secret-that-is-long-enough-000"), time.Hour)
	if err != nil {
		t.Fatalf("auth service: %v", err)
	}

	// An unsigned / garbage token must be rejected.
	if _, err := auth.ParseToken("not.a.token"); err == nil {
		t.Error("garbage token accepted")
	}
}

func TestWebSocketRouteIsPublic(t *testing.T) {
	r := testRouter(t)

	// No Authorization header. A plain GET cannot complete a WebSocket
	// handshake, so this must NOT come back 401 -- anything other than 401
	// proves the route is not sitting behind RequireAuth.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ws/polls/abc123", nil))

	if w.Code == http.StatusUnauthorized {
		t.Errorf("the live feed requires auth; anyone with the link must be able to watch")
	}
}

func TestWebSocketRejectsForeignOrigin(t *testing.T) {
	cfg := &config.Config{
		Env:            "test",
		JWTSecret:      []byte("test-secret-that-is-long-enough-000"),
		JWTTTL:         time.Hour,
		AllowedOrigins: []string{"http://localhost:5173"},
		CookieSameSite: "lax",
		IPHashSalt:     []byte("salt"),
	}

	handler := NewWSHandler(nil, nil, cfg)

	allowed := httptest.NewRequest(http.MethodGet, "/ws/polls/abc", nil)
	allowed.Header.Set("Origin", "http://localhost:5173")
	if !handler.upgrader.CheckOrigin(allowed) {
		t.Error("configured origin was rejected")
	}

	// WebSocket handshakes are not covered by the same-origin policy, so this
	// check is the only thing standing between an arbitrary page and a live
	// feed of someone else's poll.
	foreign := httptest.NewRequest(http.MethodGet, "/ws/polls/abc", nil)
	foreign.Header.Set("Origin", "https://evil.example.com")
	if handler.upgrader.CheckOrigin(foreign) {
		t.Error("foreign origin was accepted for a websocket upgrade")
	}

	// No Origin at all is a non-browser client (curl, a test), which the
	// confused-deputy problem does not apply to.
	none := httptest.NewRequest(http.MethodGet, "/ws/polls/abc", nil)
	if !handler.upgrader.CheckOrigin(none) {
		t.Error("originless client was rejected")
	}
}
