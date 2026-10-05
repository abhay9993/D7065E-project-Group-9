package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/eislab-cps/buildingsim/pkg/model"
	"github.com/eislab-cps/buildingsim/pkg/store"
	"github.com/gin-gonic/gin"
)

func TestEditorSavesValidatedFloorToMemoryAndDisk(t *testing.T) {
	memory := editorStore()
	output := t.TempDir()
	router := editorRouter(memory, output)
	floor := validEditedFloor()
	response := editorRequest(router, "/api/editor/floors/level0", floor)
	if response.Code != http.StatusOK {
		t.Fatalf("save=%d: %s", response.Code, response.Body.String())
	}
	stored, _ := memory.GetFloorData("level0")
	if len(stored.Rooms) != 1 || stored.Rooms[0].Name != "Edited" || memory.GetMultiFloorGraph() == nil {
		t.Fatalf("memory was not updated: %+v", stored)
	}
	data, err := os.ReadFile(filepath.Join(output, "level0", "floorplan_data.json"))
	if err != nil || !bytes.Contains(data, []byte(`"Edited"`)) {
		t.Fatalf("persistent floor=%s err=%v", data, err)
	}
}

func TestEditorRejectsInvalidFloorWithoutChangingMemory(t *testing.T) {
	tests := []struct {
		name  string
		floor model.FloorData
	}{
		{"bad page", model.FloorData{Page: model.Page{Width: 0, Height: 10}}},
		{"missing name", model.FloorData{Page: model.Page{Width: 10, Height: 10}, Rooms: []model.Room{{ID: 1}}}},
		{"duplicate id", model.FloorData{Page: model.Page{Width: 10, Height: 10}, Rooms: []model.Room{{ID: 1, Name: "A"}, {ID: 1, Name: "B"}}}},
		{"short polygon", model.FloorData{Page: model.Page{Width: 10, Height: 10}, Rooms: []model.Room{{ID: 1, Name: "A", Polygon: [][2]float64{{0, 0}, {1, 1}}}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			memory := editorStore()
			response := editorRequest(editorRouter(memory, ""), "/api/editor/floors/level0", tt.floor)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", response.Code, response.Body.String())
			}
			stored, _ := memory.GetFloorData("level0")
			if stored.Rooms[0].Name != "Original" {
				t.Fatal("invalid edit changed memory")
			}
		})
	}
}

func TestEditorAllowsRepeatedLogicalRoomNames(t *testing.T) {
	memory := editorStore()
	floor := model.FloorData{
		Page: model.Page{Width: 10, Height: 10},
		Rooms: []model.Room{
			{ID: 1, Name: "Corridor", Polygon: [][2]float64{{0, 0}, {1, 0}, {1, 1}}},
			{ID: 2, Name: "Corridor", Polygon: [][2]float64{{2, 0}, {3, 0}, {3, 1}}},
		},
	}
	response := editorRequest(editorRouter(memory, ""), "/api/editor/floors/level0", floor)
	if response.Code != http.StatusOK {
		t.Fatalf("repeated logical room name should be valid: %d %s", response.Code, response.Body.String())
	}
}

func TestEditorRejectsUnknownAndUnsafeLevels(t *testing.T) {
	router := editorRouter(editorStore(), "")
	for _, path := range []string{"/api/editor/floors/missing", "/api/editor/floors/bad.name"} {
		response := editorRequest(router, path, validEditedFloor())
		if response.Code != http.StatusNotFound && response.Code != http.StatusBadRequest {
			t.Fatalf("%s status=%d", path, response.Code)
		}
	}
}

func TestEditorDiskFailureDoesNotPublishFloor(t *testing.T) {
	memory := editorStore()
	dir := t.TempDir()
	blocked := filepath.Join(dir, "file")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	response := editorRequest(editorRouter(memory, blocked), "/api/editor/floors/level0", validEditedFloor())
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", response.Code, response.Body.String())
	}
	stored, _ := memory.GetFloorData("level0")
	if stored.Rooms[0].Name != "Original" {
		t.Fatal("failed disk edit changed memory")
	}
}

func TestEditorSavesNotesAsJSONArray(t *testing.T) {
	output := t.TempDir()
	router := editorRouter(editorStore(), output)
	response := editorRequest(router, "/api/editor/notes/abuilding", []map[string]string{{"text": "checked"}})
	if response.Code != http.StatusOK {
		t.Fatalf("notes=%d: %s", response.Code, response.Body.String())
	}
	data, err := os.ReadFile(filepath.Join(output, "abuilding-inspection-notes.json"))
	if err != nil || !bytes.Contains(data, []byte("checked")) {
		t.Fatalf("notes=%s err=%v", data, err)
	}

	response = editorRequest(router, "/api/editor/notes/abuilding", map[string]string{"text": "not an array"})
	if response.Code != http.StatusBadRequest {
		t.Fatalf("object notes status=%d", response.Code)
	}
}

func editorStore() *store.MemoryStore {
	memory := store.NewMemoryStore()
	memory.SetBuilding(model.Building{Levels: []model.Level{{ID: "level0", Label: "Floor 0"}}})
	memory.SetFloorData("level0", &model.FloorData{
		Page:          model.Page{Width: 100, Height: 100},
		Rooms:         []model.Room{{ID: 1, Name: "Original", Polygon: [][2]float64{{0, 0}, {1, 0}, {1, 1}}}},
		WalkableGraph: &model.NavGraph{},
	})
	return memory
}

func validEditedFloor() model.FloorData {
	return model.FloorData{
		Page:          model.Page{Width: 100, Height: 100},
		Rooms:         []model.Room{{ID: 2, Name: "Edited", Polygon: [][2]float64{{0, 0}, {2, 0}, {2, 2}}}},
		WalkableGraph: &model.NavGraph{},
	}
}

func editorRouter(memory *store.MemoryStore, output string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewEditorHandlers(memory, output)
	router.POST("/api/editor/floors/:level", handler.SaveFloor)
	router.POST("/api/editor/notes/:building", handler.SaveNotes)
	return router
}

func editorRequest(router http.Handler, path string, body any) *httptest.ResponseRecorder {
	data, _ := json.Marshal(body)
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(data))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}
