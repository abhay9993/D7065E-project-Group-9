package handlers

import (
	"net/http"
	"strings"

	"github.com/eislab-cps/buildingsim/pkg/model"
	"github.com/eislab-cps/buildingsim/pkg/server/websocket"
	"github.com/eislab-cps/buildingsim/pkg/store"
	"github.com/gin-gonic/gin"
)

type SensorHandlers struct {
	Store *store.MemoryStore
	Hub   *websocket.Hub
}

func (h *SensorHandlers) AddSensor(c *gin.Context) {
	equipmentID := c.Param("id")
	var sen model.Sensor
	if err := c.ShouldBindJSON(&sen); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	sen.ID = strings.TrimSpace(sen.ID)
	if sen.ID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
		return
	}
	if sen.DataType == "" {
		sen.DataType = "text"
	}
	if sen.DataType != "text" && sen.DataType != "binary" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "data_type must be text or binary"})
		return
	}
	version, ok, errMsg := h.Store.AddSensor(equipmentID, &sen)
	if !ok {
		if errMsg == "sensor already exists" {
			c.JSON(http.StatusConflict, gin.H{"error": errMsg})
		} else {
			c.JSON(http.StatusNotFound, gin.H{"error": errMsg})
		}
		return
	}
	version = broadcastEquipment(h.Hub, version)
	setMutationVersion(c, version)
	c.JSON(http.StatusCreated, sen)
}

func (h *SensorHandlers) GetSensor(c *gin.Context) {
	sensor, ok := h.Store.GetSensor(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "sensor not found"})
		return
	}
	c.JSON(http.StatusOK, sensor)
}

func (h *SensorHandlers) ListSensors(c *gin.Context) {
	equipmentID := c.Param("id")
	sensors, ok := h.Store.GetSensorsForEquipment(equipmentID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "equipment not found"})
		return
	}
	c.JSON(http.StatusOK, sensors)
}

func (h *SensorHandlers) DeleteSensor(c *gin.Context) {
	sensorID := c.Param("id")
	version, ok := h.Store.DeleteSensor(sensorID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "sensor not found"})
		return
	}
	version = broadcastEquipment(h.Hub, version)
	setMutationVersion(c, version)
	c.JSON(http.StatusOK, gin.H{"status": "deleted", "version": version})
}

func (h *SensorHandlers) SetValue(c *gin.Context) {
	sensorID := c.Param("id")
	var val model.SensorValue
	if err := c.ShouldBindJSON(&val); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validateSensorValue(val); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	version, ok := h.Store.SetSensorValue(sensorID, val)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "sensor not found"})
		return
	}
	version = broadcastEquipment(h.Hub, version)
	setMutationVersion(c, version)
	c.JSON(http.StatusOK, gin.H{"status": "updated", "version": version})
}
