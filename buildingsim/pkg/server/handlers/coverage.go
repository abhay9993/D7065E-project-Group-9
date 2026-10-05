package handlers

import (
	"net/http"

	"github.com/eislab-cps/buildingsim/pkg/model"
	"github.com/eislab-cps/buildingsim/pkg/server/websocket"
	"github.com/eislab-cps/buildingsim/pkg/store"
	"github.com/gin-gonic/gin"
)

type CoverageHandlers struct {
	Store *store.MemoryStore
	Hub   *websocket.Hub
}

func (h *CoverageHandlers) Get(c *gin.Context) {
	c.JSON(http.StatusOK, h.Store.GetCoverage())
}

func (h *CoverageHandlers) Set(c *gin.Context) {
	var zones []model.CoverageZone
	if err := c.ShouldBindJSON(&zones); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := normalizeCoverage(h.Store, zones); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	version := h.Store.SetCoverage(zones)
	// Global state is fetched from the canonical REST snapshot. Omitting Data
	// distinguishes this broadcast from a session-specific coverage overlay.
	h.Hub.BroadcastToAll(websocket.Message{Type: "coverage", Version: version})
	c.JSON(http.StatusOK, gin.H{"status": "updated", "version": version})
}
