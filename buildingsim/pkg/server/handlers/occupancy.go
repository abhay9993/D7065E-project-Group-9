package handlers

import (
	"net/http"

	"github.com/eislab-cps/buildingsim/pkg/model"
	"github.com/eislab-cps/buildingsim/pkg/server/websocket"
	"github.com/eislab-cps/buildingsim/pkg/store"
	"github.com/gin-gonic/gin"
)

type OccupancyHandlers struct {
	Store *store.MemoryStore
	Hub   *websocket.Hub
}

func (h *OccupancyHandlers) Get(c *gin.Context) {
	c.JSON(http.StatusOK, h.Store.GetOccupancy())
}

func (h *OccupancyHandlers) Set(c *gin.Context) {
	var occ map[string]model.RoomOccupancy
	if err := c.ShouldBindJSON(&occ); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if occ == nil {
		occ = make(map[string]model.RoomOccupancy)
	}
	normalized, err := normalizeOccupancy(h.Store, occ)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	version := h.Store.SetOccupancy(normalized)
	h.Hub.BroadcastToAll(websocket.Message{Type: "occupancy", Version: version})
	c.JSON(http.StatusOK, gin.H{"status": "updated", "version": version})
}
