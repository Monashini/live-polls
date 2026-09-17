package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"livepolls/internal/db"
)

type HealthHandler struct {
	store *db.Store
}

func NewHealthHandler(store *db.Store) *HealthHandler {
	return &HealthHandler{store: store}
}

// GET /healthz
//
// This actually pings MongoDB rather than returning a hardcoded "ok". A health
// check that cannot fail is worse than none: a deployment platform will keep
// routing traffic to an instance whose database connection died. Redis will be
// added to the same report in a later phase.
func (h *HealthHandler) Check(c *gin.Context) {
	report := gin.H{"status": "ok", "mongo": "ok"}
	status := http.StatusOK

	if err := h.store.Ping(c.Request.Context()); err != nil {
		report["status"] = "degraded"
		// The error text is not included: it can contain the cluster hostname
		// and replica set details, and /healthz is typically public.
		report["mongo"] = "unreachable"
		status = http.StatusServiceUnavailable
	}

	respond(c, status, report)
}
