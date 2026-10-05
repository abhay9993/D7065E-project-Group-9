package handlers

import (
	"strconv"

	"github.com/eislab-cps/buildingsim/pkg/server/websocket"
	"github.com/eislab-cps/buildingsim/pkg/store"
	"github.com/gin-gonic/gin"
)

// notifyEquipment is retained for the legacy explicit notification endpoint.
// Normal mutations increment the version atomically in MemoryStore and use
// broadcastEquipment so that the stored object and notification agree.
func notifyEquipment(memory *store.MemoryStore, hub *websocket.Hub) int64 {
	version := memory.BumpEquipmentVersion()
	broadcastEquipmentVersion(hub, version)
	return version
}

func broadcastEquipment(hub *websocket.Hub, version int64) int64 {
	broadcastEquipmentVersion(hub, version)
	return version
}

func broadcastEquipmentVersion(hub *websocket.Hub, version int64) {
	if hub != nil {
		hub.BroadcastToAll(websocket.Message{Type: "equipment", Version: version})
	}
}

func setMutationVersion(c *gin.Context, version int64) {
	c.Header("X-BuildSim-Version", strconv.FormatInt(version, 10))
	c.Header("Access-Control-Expose-Headers", "X-BuildSim-Version")
	c.Header("Cache-Control", "no-store")
}
