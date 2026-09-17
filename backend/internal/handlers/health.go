package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"livepolls/internal/db"
	"livepolls/internal/redisstore"
	"livepolls/internal/ws"
)

type HealthHandler struct {
	store *db.Store
	live  *redisstore.Store
	hub   *ws.Hub
}

func NewHealthHandler(store *db.Store, live *redisstore.Store, hub *ws.Hub) *HealthHandler {
	return &HealthHandler{store: store, live: live, hub: hub}
}

// GET /healthz
//
// This actually pings MongoDB rather than returning a hardcoded "ok". A health
// check that cannot fail is worse than none: a deployment platform will keep
// routing traffic to an instance whose database connection died.
func (h *HealthHandler) Check(c *gin.Context) {
	report := gin.H{"status": "ok", "mongo": "ok", "redis": "ok"}
	status := http.StatusOK

	if err := h.store.Ping(c.Request.Context()); err != nil {
		report["status"] = "degraded"
		// The error text is not included: it can contain the cluster hostname
		// and replica set details, and /healthz is typically public.
		report["mongo"] = "unreachable"
		status = http.StatusServiceUnavailable
	}

	// Redis being down is reported as degraded rather than unhealthy. Votes
	// still persist and REST still answers without it; only the live layer
	// stops working. A load balancer should keep sending traffic to a server
	// that can still serve a poll.
	if h.live == nil {
		report["redis"] = "disabled"
	} else if err := h.live.Ping(c.Request.Context()); err != nil {
		report["redis"] = "unreachable"
		if report["status"] == "ok" {
			report["status"] = "degraded"
		}
	}

	// Observable proof that the WebSocket layer is doing something, without
	// needing to attach a debugger to a running instance.
	if h.hub != nil {
		rooms, clients := h.hub.Stats()
		report["wsRooms"] = rooms
		report["wsClients"] = clients
	}

	respond(c, status, report)
}
