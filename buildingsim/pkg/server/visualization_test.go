package server

import (
	"net/http"
	"testing"

	"github.com/eislab-cps/buildingsim/pkg/model"
	"github.com/gin-gonic/gin"
)

func TestVisualizationCollectionsRoundTripAndCanonicalize(t *testing.T) {
	router, _ := setupTestRouter()

	layers := []model.RoomLayer{{
		ID: "temperature", Label: "Temperature", Unit: "°C", Source: "simulation truth",
		Minimum: 18, Maximum: 30, Values: map[string]float64{"R002": 23.5},
	}}
	assertPutOK(t, router, "/api/room-layers", layers)
	var storedLayers []model.RoomLayer
	parseJSON(doRequest(router, http.MethodGet, "/api/room-layers", nil), &storedLayers)
	if len(storedLayers) != 1 || storedLayers[0].Values["level0/R002"] != 23.5 || len(storedLayers[0].Palette) != 4 {
		t.Fatalf("room layers were not normalized: %+v", storedLayers)
	}

	position := [2]float64{52, 49}
	assertPutOK(t, router, "/api/effects", []model.VisualEffect{{
		ID: "fire", Type: "fire", Level: "level0", Room: "R002", Position: &position,
	}})
	var effects []model.VisualEffect
	parseJSON(doRequest(router, http.MethodGet, "/api/effects", nil), &effects)
	if len(effects) != 1 || effects[0].Radius != 8 || effects[0].Intensity != 1 {
		t.Fatalf("effect defaults = %+v", effects)
	}

	assertPutOK(t, router, "/api/entities", []model.MobileEntity{{
		ID: "robot", Name: "Robot", Type: "cleaning_robot", Level: "level0", Room: "R003",
	}})
	var entities []model.MobileEntity
	parseJSON(doRequest(router, http.MethodGet, "/api/entities", nil), &entities)
	if len(entities) != 1 || entities[0].Room != "R003" || entities[0].TransitionMS != 700 || entities[0].Timestamp.IsZero() {
		t.Fatalf("entities = %+v", entities)
	}

	assertPutOK(t, router, "/api/room-appearance", []model.RoomAppearance{{
		Room: "R002", Color: "#ffd166", Brightness: 0.8,
	}})
	var appearance []model.RoomAppearance
	parseJSON(doRequest(router, http.MethodGet, "/api/room-appearance", nil), &appearance)
	if len(appearance) != 1 || appearance[0].Level != "level0" {
		t.Fatalf("appearance = %+v", appearance)
	}

	assertPutOK(t, router, "/api/alerts", []model.DecisionAlert{{
		ID: "warning", Severity: "warning", Title: "Check room", Room: "R002",
	}})
	var alerts []model.DecisionAlert
	parseJSON(doRequest(router, http.MethodGet, "/api/alerts", nil), &alerts)
	if len(alerts) != 1 || alerts[0].Level != "level0" || alerts[0].Timestamp.IsZero() {
		t.Fatalf("alerts = %+v", alerts)
	}

	assertPutOK(t, router, "/api/doors", []model.Door{{
		ID: "door", Name: "Door", Level: "level0", Room: "R002", LockState: "locked",
	}})
	var doors []model.Door
	parseJSON(doRequest(router, http.MethodGet, "/api/doors", nil), &doors)
	if len(doors) != 1 || doors[0].Kind != "door" || doors[0].State != "closed" || !doors[0].Blocked {
		t.Fatalf("doors = %+v", doors)
	}

	for _, endpoint := range []string{"room-layers", "effects", "entities", "room-appearance", "alerts", "doors"} {
		assertPutOK(t, router, "/api/"+endpoint, []any{})
		var empty []any
		parseJSON(doRequest(router, http.MethodGet, "/api/"+endpoint, nil), &empty)
		if len(empty) != 0 {
			t.Errorf("%s was not cleared: %+v", endpoint, empty)
		}
	}
}

func TestVisualizationValidationRejectsUnsafeOrAmbiguousInput(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		body     any
	}{
		{"layer range", "/api/room-layers", []model.RoomLayer{{ID: "bad", Minimum: 5, Maximum: 5, Values: map[string]float64{}}}},
		{"layer room", "/api/room-layers", []model.RoomLayer{{ID: "bad", Minimum: 0, Maximum: 1, Values: map[string]float64{"missing": 1}}}},
		{"effect type", "/api/effects", []model.VisualEffect{{ID: "bad", Type: "explosion", Level: "level0", Room: "R002"}}},
		{"effect position", "/api/effects", []model.VisualEffect{{ID: "bad", Type: "fire", Level: "level0", Position: point(101, 2)}}},
		{"entity type", "/api/entities", []model.MobileEntity{{ID: "bad", Type: "dragon", Level: "level0", Room: "R002"}}},
		{"appearance brightness", "/api/room-appearance", []model.RoomAppearance{{Level: "level0", Room: "R002", Brightness: 2}}},
		{"alert severity", "/api/alerts", []model.DecisionAlert{{ID: "bad", Severity: "urgent", Title: "Bad"}}},
		{"door state", "/api/doors", []model.Door{{ID: "bad", Level: "level0", Room: "R002", State: "ajar"}}},
		{"door location", "/api/doors", []model.Door{{ID: "bad", Level: "level0"}}},
		{"duplicate doors", "/api/doors", []model.Door{{ID: "same", Level: "level0", Room: "R002"}, {ID: "same", Level: "level0", Room: "R003"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router, _ := setupTestRouter()
			response := doRequest(router, http.MethodPut, test.endpoint, test.body)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("HTTP %d, want 400: %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestLockedDoorRemovesEntryFromWalkableRoutes(t *testing.T) {
	router, memory := setupTestRouter()
	memory.SetFloorData("level0", &model.FloorData{
		Page: model.Page{Width: 100, Height: 100},
		Rooms: []model.Room{
			{ID: 0, Name: "R001", Center: [2]float64{20, 50}, Type: "corridor"},
			{ID: 1, Name: "R002", Center: [2]float64{60, 30}, Type: "room"},
			{ID: 2, Name: "R003", Center: [2]float64{60, 70}, Type: "room"},
		},
		WalkableGraph: &model.NavGraph{
			Nodes: []model.NavNode{
				{ID: 0, Name: "R001", X: 20, Y: 50, Type: "corridor"},
				{ID: 10, Name: "R002", X: 50, Y: 30, Type: "entry"},
				{ID: 1, Name: "R002", X: 60, Y: 30, Type: "room"},
				{ID: 20, Name: "R003", X: 50, Y: 70, Type: "entry"},
				{ID: 2, Name: "R003", X: 60, Y: 70, Type: "room"},
			},
			Edges: []model.NavEdge{
				{From: 0, To: 10, Weight: 1}, {From: 10, To: 1, Weight: 1},
				{From: 0, To: 20, Weight: 1}, {From: 20, To: 2, Weight: 1},
			},
		},
	})

	route := "/api/graph/route?type=walkable&level=level0&from_name=R003&to_name=R002"
	if response := doRequest(router, http.MethodGet, route, nil); response.Code != http.StatusOK {
		t.Fatalf("route before lock: %d %s", response.Code, response.Body.String())
	}
	entry := 10
	assertPutOK(t, router, "/api/doors", []model.Door{{
		ID: "R002-door", Level: "level0", Room: "R002", EntryNodeID: &entry,
		State: "closed", LockState: "locked",
	}})
	if response := doRequest(router, http.MethodGet, route, nil); response.Code != http.StatusNotFound {
		t.Fatalf("route through locked door: %d %s", response.Code, response.Body.String())
	}
	assertPutOK(t, router, "/api/doors", []model.Door{{
		ID: "R002-door", Level: "level0", Room: "R002", EntryNodeID: &entry,
		State: "closed", LockState: "unlocked",
	}})
	if response := doRequest(router, http.MethodGet, route, nil); response.Code != http.StatusOK {
		t.Fatalf("route after unlock: %d %s", response.Code, response.Body.String())
	}
}

func TestEquipmentAcceptsExactPositionAndRejectsOffFloorPosition(t *testing.T) {
	router, _ := setupTestRouter()
	equipment := model.Equipment{
		ID: "positioned", Level: "level0", Room: "R002", Position: point(51, 49), Height: 8, Heading: 90,
	}
	response := doRequest(router, http.MethodPost, "/api/equipment", equipment)
	if response.Code != http.StatusCreated {
		t.Fatalf("positioned equipment: %d %s", response.Code, response.Body.String())
	}
	equipment.ID = "outside"
	equipment.Position = point(-1, 49)
	response = doRequest(router, http.MethodPost, "/api/equipment", equipment)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("off-floor equipment: %d %s", response.Code, response.Body.String())
	}
}

func assertPutOK(t *testing.T, router *gin.Engine, path string, body any) {
	t.Helper()
	request := doRequest(router, http.MethodPut, path, body)
	if request.Code != http.StatusOK {
		t.Fatalf("PUT %s: HTTP %d: %s", path, request.Code, request.Body.String())
	}
}

func point(x, y float64) *[2]float64 {
	value := [2]float64{x, y}
	return &value
}
