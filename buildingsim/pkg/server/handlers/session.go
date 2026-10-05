package handlers

import (
	"net/http"

	"github.com/eislab-cps/buildingsim/pkg/model"
	"github.com/eislab-cps/buildingsim/pkg/server/websocket"
	"github.com/eislab-cps/buildingsim/pkg/store"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type SessionHandlers struct {
	Store *store.MemoryStore
	Hub   *websocket.Hub
}

// highlightRequest uses a pointer for opacity so JSON can distinguish an
// omitted value (default 0.8) from an explicit zero (fully transparent).
type highlightRequest struct {
	Level   string   `json:"level"`
	Room    string   `json:"room,omitempty"`
	RoomID  int      `json:"room_id,omitempty"`
	Color   string   `json:"color"`
	Opacity *float64 `json:"opacity"`
}

func (h *SessionHandlers) List(c *gin.Context) {
	sessions := h.Store.ListSessions()
	if sessions == nil {
		sessions = []*model.Session{}
	}
	c.JSON(http.StatusOK, sessions)
}

func (h *SessionHandlers) Create(c *gin.Context) {
	id := uuid.New().String()
	sess := h.Store.CreateSession(id)
	c.JSON(http.StatusCreated, sess)
}

func (h *SessionHandlers) Get(c *gin.Context) {
	id := c.Param("id")
	sess, ok := h.Store.GetSession(id)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}
	c.JSON(http.StatusOK, sess)
}

func (h *SessionHandlers) Delete(c *gin.Context) {
	id := c.Param("id")
	if !h.Store.DeleteSession(id) {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

func (h *SessionHandlers) SetViewport(c *gin.Context) {
	id := c.Param("id")
	var vp model.Viewport
	if err := c.ShouldBindJSON(&vp); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := normalizeViewport(h.Store, &vp); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	version, ok := h.Store.UpdateSessionViewport(id, vp)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}
	h.Hub.SendToSession(id, websocket.Message{Type: "viewport", Data: vp, Version: version})
	c.JSON(http.StatusOK, gin.H{"status": "updated", "version": version})
}

func (h *SessionHandlers) SetHighlights(c *gin.Context) {
	id := c.Param("id")
	var requested []highlightRequest
	if err := c.ShouldBindJSON(&requested); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	highlights := make([]model.RoomHighlight, len(requested))
	for i, request := range requested {
		opacity := 0.8
		if request.Opacity != nil {
			opacity = *request.Opacity
		}
		highlights[i] = model.RoomHighlight{
			Level: request.Level, Room: request.Room, RoomID: request.RoomID,
			Color: request.Color, Opacity: opacity,
		}
	}
	if err := normalizeHighlights(h.Store, highlights); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	version, ok := h.Store.UpdateSessionHighlights(id, highlights)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}
	h.Hub.SendToSession(id, websocket.Message{Type: "highlights", Data: highlights, Version: version})
	c.JSON(http.StatusOK, gin.H{"status": "updated", "version": version})
}

func (h *SessionHandlers) SetOccupancy(c *gin.Context) {
	id := c.Param("id")
	var occupancy map[string]model.RoomOccupancy
	if err := c.ShouldBindJSON(&occupancy); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if occupancy == nil {
		occupancy = make(map[string]model.RoomOccupancy)
	}
	normalized, err := normalizeOccupancy(h.Store, occupancy)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	version, ok := h.Store.UpdateSessionOccupancy(id, normalized)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}
	h.Hub.SendToSession(id, websocket.Message{Type: "occupancy", Data: normalized, Version: version})
	c.JSON(http.StatusOK, gin.H{"status": "updated", "version": version})
}

func (h *SessionHandlers) SetRoute(c *gin.Context) {
	id := c.Param("id")
	var route model.RouteResult
	if err := c.ShouldBindJSON(&route); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	version, ok := h.Store.UpdateSessionRoute(id, &route)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}
	h.Hub.SendToSession(id, websocket.Message{Type: "route", Data: route, Version: version})
	c.JSON(http.StatusOK, gin.H{"status": "updated", "version": version})
}

func (h *SessionHandlers) SetCoverage(c *gin.Context) {
	id := c.Param("id")
	var coverage []model.CoverageZone
	if err := c.ShouldBindJSON(&coverage); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := normalizeCoverage(h.Store, coverage); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	version, ok := h.Store.UpdateSessionCoverage(id, coverage)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}
	h.Hub.SendToSession(id, websocket.Message{Type: "coverage", Data: coverage, Version: version})
	c.JSON(http.StatusOK, gin.H{"status": "updated", "version": version})
}

func (h *SessionHandlers) HandleWebSocket(c *gin.Context) {
	id := c.Param("id")
	if _, ok := h.Store.GetSession(id); !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}

	upgrader := websocket.Upgrader()
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}

	h.Hub.HandleConnection(conn, id)
}
