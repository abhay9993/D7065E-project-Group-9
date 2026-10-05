package handlers

import (
	"net/http"
	"strings"

	"github.com/eislab-cps/buildingsim/pkg/model"
	"github.com/eislab-cps/buildingsim/pkg/server/websocket"
	"github.com/eislab-cps/buildingsim/pkg/store"
	"github.com/gin-gonic/gin"
)

type ActuatorHandlers struct {
	Store *store.MemoryStore
	Hub   *websocket.Hub
}

func (h *ActuatorHandlers) AddActuator(c *gin.Context) {
	equipmentID := c.Param("id")
	var act model.Actuator
	if err := c.ShouldBindJSON(&act); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	act.ID = strings.TrimSpace(act.ID)
	if act.ID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
		return
	}
	version, ok, errMsg := h.Store.AddActuator(equipmentID, &act)
	if !ok {
		if errMsg == "actuator already exists" {
			c.JSON(http.StatusConflict, gin.H{"error": errMsg})
		} else {
			c.JSON(http.StatusNotFound, gin.H{"error": errMsg})
		}
		return
	}
	version = broadcastEquipment(h.Hub, version)
	setMutationVersion(c, version)
	c.JSON(http.StatusCreated, act)
}

func (h *ActuatorHandlers) GetActuator(c *gin.Context) {
	actuator, ok := h.Store.GetActuator(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "actuator not found"})
		return
	}
	c.JSON(http.StatusOK, actuator)
}

func (h *ActuatorHandlers) ListActuators(c *gin.Context) {
	equipmentID := c.Param("id")
	actuators, ok := h.Store.GetActuatorsForEquipment(equipmentID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "equipment not found"})
		return
	}
	c.JSON(http.StatusOK, actuators)
}

func (h *ActuatorHandlers) DeleteActuator(c *gin.Context) {
	actuatorID := c.Param("id")
	version, ok := h.Store.DeleteActuator(actuatorID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "actuator not found"})
		return
	}
	version = broadcastEquipment(h.Hub, version)
	setMutationVersion(c, version)
	c.JSON(http.StatusOK, gin.H{"status": "deleted", "version": version})
}

func (h *ActuatorHandlers) SetState(c *gin.Context) {
	actuatorID := c.Param("id")
	var state model.ActuatorState
	if err := c.ShouldBindJSON(&state); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	state.State = strings.TrimSpace(state.State)
	if state.State == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "state is required"})
		return
	}
	version, ok := h.Store.SetActuatorState(actuatorID, state)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "actuator not found"})
		return
	}
	version = broadcastEquipment(h.Hub, version)
	setMutationVersion(c, version)
	c.JSON(http.StatusOK, gin.H{"status": "updated", "version": version})
}
