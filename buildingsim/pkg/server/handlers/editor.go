package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sync"

	"github.com/eislab-cps/buildingsim/pkg/graph"
	"github.com/eislab-cps/buildingsim/pkg/model"
	"github.com/eislab-cps/buildingsim/pkg/store"
	"github.com/gin-gonic/gin"
)

var safeEditorName = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

type EditorHandlers struct {
	Store     *store.MemoryStore
	OutputDir string

	mu    sync.Mutex
	notes map[string]json.RawMessage
}

func NewEditorHandlers(memory *store.MemoryStore, outputDir string) *EditorHandlers {
	return &EditorHandlers{Store: memory, OutputDir: outputDir, notes: make(map[string]json.RawMessage)}
}

func (h *EditorHandlers) SaveFloor(c *gin.Context) {
	level := c.Param("level")
	if !safeEditorName.MatchString(level) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid level"})
		return
	}
	if _, exists := h.Store.GetFloorData(level); !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "level not found"})
		return
	}
	var floor model.FloorData
	if err := c.ShouldBindJSON(&floor); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validateFloorData(&floor); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	persistent := h.OutputDir != ""
	if persistent {
		filename := filepath.Join(h.OutputDir, level, "floorplan_data.json")
		if err := writeJSONAtomically(filename, floor); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	h.Store.SetFloorData(level, &floor)
	rebuildMultiFloorGraph(h.Store)
	c.JSON(http.StatusOK, gin.H{"status": "saved", "level": level, "persistent": persistent})
}

func (h *EditorHandlers) SaveNotes(c *gin.Context) {
	building := c.Param("building")
	if !safeEditorName.MatchString(building) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid building"})
		return
	}
	var notes json.RawMessage
	if err := c.ShouldBindJSON(&notes); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(notes) == 0 || notes[0] != '[' {
		c.JSON(http.StatusBadRequest, gin.H{"error": "notes must be a JSON array"})
		return
	}
	persistent := h.OutputDir != ""
	if persistent {
		filename := filepath.Join(h.OutputDir, building+"-inspection-notes.json")
		var value any
		if err := json.Unmarshal(notes, &value); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := writeJSONAtomically(filename, value); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	h.mu.Lock()
	h.notes[building] = append(json.RawMessage(nil), notes...)
	h.mu.Unlock()
	c.JSON(http.StatusOK, gin.H{"status": "saved", "persistent": persistent})
}

func validateFloorData(floor *model.FloorData) error {
	if floor.Page.Width <= 0 || floor.Page.Height <= 0 {
		return fmt.Errorf("page width and height must be positive")
	}
	ids := make(map[int]bool, len(floor.Rooms))
	for i, room := range floor.Rooms {
		if room.Name == "" {
			return fmt.Errorf("room %d requires a name", i)
		}
		if ids[room.ID] {
			return fmt.Errorf("duplicate room id %d", room.ID)
		}
		if len(room.Polygon) > 0 && len(room.Polygon) < 3 {
			return fmt.Errorf("room %q polygon requires at least three points", room.Name)
		}
		ids[room.ID] = true
	}
	return nil
}

func rebuildMultiFloorGraph(memory *store.MemoryStore) {
	building := memory.GetBuilding()
	levels := make([]string, 0, len(building.Levels))
	for _, level := range building.Levels {
		levels = append(levels, level.ID)
	}
	memory.SetMultiFloorGraph(graph.BuildMultiFloorGraph(memory.GetFloors(), levels, memory.GetCrossFloorEdges()))
}

func writeJSONAtomically(filename string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", filename, err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(filename), ".buildsim-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write temporary file: %w", err)
	}
	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set output permissions: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary file: %w", err)
	}
	if err := os.Rename(temporaryName, filename); err != nil {
		return fmt.Errorf("replace %s: %w", filename, err)
	}
	return nil
}
