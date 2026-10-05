package handlers

import (
	"errors"
	"net/http"

	"github.com/eislab-cps/buildingsim/pkg/model"
	"github.com/eislab-cps/buildingsim/pkg/server/websocket"
	"github.com/eislab-cps/buildingsim/pkg/store"
	"github.com/gin-gonic/gin"
)

type EquipmentHandlers struct {
	Store *store.MemoryStore
	Hub   *websocket.Hub
}

func (h *EquipmentHandlers) Create(c *gin.Context) {
	var eq model.Equipment
	if err := c.ShouldBindJSON(&eq); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := normalizeEquipmentLocation(h.Store, &eq); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.Store.CreateEquipment(&eq); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, store.ErrEquipmentExists) || errors.Is(err, store.ErrSensorExists) || errors.Is(err, store.ErrActuatorExists) {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	version := broadcastEquipment(h.Hub, eq.Version)
	setMutationVersion(c, version)
	c.JSON(http.StatusCreated, eq)
}

func (h *EquipmentHandlers) List(c *gin.Context) {
	level := c.Query("level")
	room := c.Query("room")
	typ := c.Query("type")
	category := c.Query("category")
	result := h.Store.ListEquipment(level, room, typ, category)
	if result == nil {
		result = []*model.Equipment{}
	}
	c.JSON(http.StatusOK, result)
}

func (h *EquipmentHandlers) Get(c *gin.Context) {
	id := c.Param("id")
	eq, ok := h.Store.GetEquipment(id)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "equipment not found"})
		return
	}
	c.JSON(http.StatusOK, eq)
}

func (h *EquipmentHandlers) Update(c *gin.Context) {
	id := c.Param("id")
	old, exists := h.Store.GetEquipment(id)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "equipment not found"})
		return
	}
	var eq model.Equipment
	if err := c.ShouldBindJSON(&eq); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	eq.ID = id
	// PUT updates equipment metadata. Omitted child collections are preserved;
	// explicit [] replaces them and intentionally removes the old children.
	if eq.Sensors == nil {
		eq.Sensors = old.Sensors
	}
	if eq.Actuators == nil {
		eq.Actuators = old.Actuators
	}
	if err := normalizeEquipmentLocation(h.Store, &eq); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.Store.UpdateEquipment(&eq); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, store.ErrEquipmentNotFound) {
			status = http.StatusNotFound
		} else if errors.Is(err, store.ErrSensorExists) || errors.Is(err, store.ErrActuatorExists) {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	version := broadcastEquipment(h.Hub, eq.Version)
	setMutationVersion(c, version)
	c.JSON(http.StatusOK, eq)
}

func (h *EquipmentHandlers) Delete(c *gin.Context) {
	id := c.Param("id")
	version, ok := h.Store.DeleteEquipment(id)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "equipment not found"})
		return
	}
	version = broadcastEquipment(h.Hub, version)
	setMutationVersion(c, version)
	c.JSON(http.StatusOK, gin.H{"status": "deleted", "version": version})
}

func (h *EquipmentHandlers) BulkCreate(c *gin.Context) {
	var items []model.Equipment
	if err := c.ShouldBindJSON(&items); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(items) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "at least one equipment item is required"})
		return
	}
	for i := range items {
		if err := normalizeEquipmentLocation(h.Store, &items[i]); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "item": i})
			return
		}
	}
	created, skipped, version, err := h.Store.CreateEquipmentBatch(items)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, store.ErrSensorExists) || errors.Is(err, store.ErrActuatorExists) {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	if created > 0 {
		version = broadcastEquipment(h.Hub, version)
	}
	setMutationVersion(c, version)
	status := http.StatusCreated
	if created == 0 {
		status = http.StatusOK
	}
	c.JSON(status, gin.H{"created": created, "skipped": skipped, "total": len(items), "version": version})
}

func (h *EquipmentHandlers) Notify(c *gin.Context) {
	version := notifyEquipment(h.Store, h.Hub)
	setMutationVersion(c, version)
	c.JSON(http.StatusOK, gin.H{"version": version})
}
