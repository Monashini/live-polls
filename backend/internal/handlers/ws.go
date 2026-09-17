package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"livepolls/internal/config"
	"livepolls/internal/middleware"
	"livepolls/internal/redisstore"
	"livepolls/internal/services"
	"livepolls/internal/ws"
)

type WSHandler struct {
	hub      *ws.Hub
	polls    *services.PollService
	upgrader websocket.Upgrader
}

func NewWSHandler(hub *ws.Hub, polls *services.PollService, cfg *config.Config) *WSHandler {
	allowed := make(map[string]struct{}, len(cfg.AllowedOrigins))
	for _, origin := range cfg.AllowedOrigins {
		allowed[strings.TrimSpace(origin)] = struct{}{}
	}

	return &WSHandler{
		hub:   hub,
		polls: polls,
		upgrader: websocket.Upgrader{
			HandshakeTimeout: 10 * time.Second,
			ReadBufferSize:   1024,
			WriteBufferSize:  4096,

			// CheckOrigin is a real security boundary, not a formality.
			//
			// WebSocket handshakes are NOT covered by the same-origin policy.
			// Any page on any site can open a socket to this server, and
			// unlike a cross-origin fetch the browser will happily let that
			// page read everything sent back. Gorilla's default compares
			// Origin against the Host header, which breaks the moment there
			// is a proxy in front. Checking the configured allowlist instead
			// makes the policy explicit and identical to the CORS one.
			//
			// A request with no Origin header is allowed through: that is a
			// non-browser client (curl, a test), which is not subject to the
			// confused-deputy problem this check exists to prevent.
			CheckOrigin: func(r *http.Request) bool {
				origin := r.Header.Get("Origin")
				if origin == "" {
					return true
				}
				if _, ok := allowed[origin]; ok {
					return true
				}
				slog.Warn("ws: rejected handshake from disallowed origin", "origin", origin)
				return false
			},
		},
	}
}

// Serve handles GET /ws/polls/:id.
func (h *WSHandler) Serve(c *gin.Context) {
	requestID := middleware.RequestID(c)

	// Resolve the poll BEFORE upgrading. After the upgrade it is far too late
	// to send a 404 -- the client would have to interpret a close code, where
	// right now it gets the same JSON error envelope as every other endpoint.
	view, err := h.polls.Results(c.Request.Context(), c.Param("id"))
	if err != nil {
		fail(c, err)
		return
	}

	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		// Upgrade has already written its own HTTP error response.
		slog.Debug("ws: upgrade failed", "err", err, "requestId", requestID)
		return
	}

	// Rooms are keyed by the poll's canonical ID, never the slug the client
	// happened to use in the URL. Otherwise the same poll reached by its slug
	// and by its ObjectID would land in two separate rooms, and each event
	// would only reach half the viewers.
	pollID := view.ID

	// context.Background, not the request context. Gin cancels the request
	// context as soon as this handler returns, which for a hijacked
	// connection is immediately -- using it would unsubscribe from Redis the
	// instant we subscribed. The connection's lifetime is bounded by the
	// read/write deadlines in the ws package instead.
	bg := context.Background()

	client := ws.NewClient(h.hub, conn, pollID)

	if err := h.hub.Join(bg, pollID, client); err != nil {
		slog.Error("ws: join failed", "pollId", pollID, "err", err)
		client.Close()
		_ = conn.Close()
		return
	}

	// Queue the current results as the first message, before any Redis event
	// can arrive. Without it a client connects and then stares at nothing
	// until somebody happens to vote, which on a quiet poll could be minutes.
	snapshot := redisstore.Event{
		Type:   redisstore.EventResults,
		PollID: pollID,
		Poll:   view,
	}
	if payload, marshalErr := json.Marshal(snapshot); marshalErr == nil {
		client.Enqueue(payload)
	} else {
		slog.Error("ws: marshalling snapshot failed", "pollId", pollID, "err", marshalErr)
	}

	client.Start()

	slog.Debug("ws: client connected", "pollId", pollID, "requestId", requestID)
}
