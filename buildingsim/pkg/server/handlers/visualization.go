package handlers

import (
	"net/http"

	"github.com/eislab-cps/buildingsim/pkg/model"
	"github.com/eislab-cps/buildingsim/pkg/server/websocket"
	"github.com/eislab-cps/buildingsim/pkg/store"
	"github.com/gin-gonic/gin"
)

// VisualizationHandlers exposes full-replacement snapshots. Producers can
// update each domain independently, and browsers fetch the canonical snapshot
// after receiving its small version notification over WebSocket.
type VisualizationHandlers struct {
	Store *store.MemoryStore
	Hub   *websocket.Hub
}

func (h *VisualizationHandlers) GetRoomLayers(c *gin.Context) {
	c.JSON(http.StatusOK, h.Store.GetRoomLayers())
}

func (h *VisualizationHandlers) SetRoomLayers(c *gin.Context) {
	var layers []model.RoomLayer
	if !bindCollection(c, &layers) || !validateCollection(c, normalizeRoomLayers(h.Store, layers)) {
		return
	}
	h.updated(c, "room_layers", h.Store.SetRoomLayers(layers))
}

func (h *VisualizationHandlers) GetEffects(c *gin.Context) {
	c.JSON(http.StatusOK, h.Store.GetEffects())
}

func (h *VisualizationHandlers) SetEffects(c *gin.Context) {
	var effects []model.VisualEffect
	if !bindCollection(c, &effects) || !validateCollection(c, normalizeEffects(h.Store, effects)) {
		return
	}
	h.updated(c, "effects", h.Store.SetEffects(effects))
}

func (h *VisualizationHandlers) GetEntities(c *gin.Context) {
	c.JSON(http.StatusOK, h.Store.GetEntities())
}

func (h *VisualizationHandlers) SetEntities(c *gin.Context) {
	var entities []model.MobileEntity
	if !bindCollection(c, &entities) || !validateCollection(c, normalizeEntities(h.Store, entities)) {
		return
	}
	h.updated(c, "entities", h.Store.SetEntities(entities))
}

func (h *VisualizationHandlers) GetRoomAppearance(c *gin.Context) {
	c.JSON(http.StatusOK, h.Store.GetRoomAppearance())
}

func (h *VisualizationHandlers) SetRoomAppearance(c *gin.Context) {
	var appearance []model.RoomAppearance
	if !bindCollection(c, &appearance) || !validateCollection(c, normalizeRoomAppearance(h.Store, appearance)) {
		return
	}
	h.updated(c, "room_appearance", h.Store.SetRoomAppearance(appearance))
}

func (h *VisualizationHandlers) GetAlerts(c *gin.Context) {
	c.JSON(http.StatusOK, h.Store.GetAlerts())
}

func (h *VisualizationHandlers) SetAlerts(c *gin.Context) {
	var alerts []model.DecisionAlert
	if !bindCollection(c, &alerts) || !validateCollection(c, normalizeAlerts(h.Store, alerts)) {
		return
	}
	h.updated(c, "alerts", h.Store.SetAlerts(alerts))
}

func (h *VisualizationHandlers) GetDoors(c *gin.Context) {
	c.JSON(http.StatusOK, h.Store.GetDoors())
}

func (h *VisualizationHandlers) SetDoors(c *gin.Context) {
	var doors []model.Door
	if !bindCollection(c, &doors) || !validateCollection(c, normalizeDoors(h.Store, doors)) {
		return
	}
	h.updated(c, "doors", h.Store.SetDoors(doors))
}

func (h *VisualizationHandlers) updated(c *gin.Context, messageType string, version int64) {
	if h.Hub != nil {
		h.Hub.BroadcastToAll(websocket.Message{Type: messageType, Version: version})
	}
	setMutationVersion(c, version)
	c.JSON(http.StatusOK, gin.H{"status": "updated", "version": version})
}

func bindCollection(c *gin.Context, destination interface{}) bool {
	if err := c.ShouldBindJSON(destination); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return false
	}
	return true
}

func validateCollection(c *gin.Context, err error) bool {
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return false
	}
	return true
}
