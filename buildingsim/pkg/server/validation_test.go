package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eislab-cps/buildingsim/pkg/model"
)

func TestEquipmentLocationValidation(t *testing.T) {
	tests := []struct {
		name      string
		equipment model.Equipment
	}{
		{"missing level", model.Equipment{ID: "eq", Room: "R001"}},
		{"missing room", model.Equipment{ID: "eq", Level: "level0"}},
		{"unknown level", model.Equipment{ID: "eq", Level: "level9", Room: "R001"}},
		{"unknown room", model.Equipment{ID: "eq", Level: "level0", Room: "missing"}},
		{"invalid sensor type", model.Equipment{ID: "eq", Level: "level0", Room: "R001", Sensors: []model.Sensor{{ID: "s", DataType: "number"}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router, _ := setupTestRouter()
			response := doRequest(router, http.MethodPost, "/api/equipment", tt.equipment)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestEquipmentUpdatePreservesOmittedChildrenAndRemovesExplicitChildren(t *testing.T) {
	router, _ := setupTestRouter()
	created := model.Equipment{
		ID: "eq", Name: "Original", Type: "test", Level: "level0", Room: "R001",
		Sensors:   []model.Sensor{{ID: "sensor", DataType: "text", Value: "1"}},
		Actuators: []model.Actuator{{ID: "actuator", State: "off"}},
	}
	if response := doRequest(router, http.MethodPost, "/api/equipment", created); response.Code != http.StatusCreated {
		t.Fatal(response.Body.String())
	}

	metadataOnly := map[string]any{
		"name": "Updated", "type": "test", "level": "level0", "room": "R002", "status": "running",
	}
	response := doRequest(router, http.MethodPut, "/api/equipment/eq", metadataOnly)
	if response.Code != http.StatusOK {
		t.Fatalf("metadata update: %d %s", response.Code, response.Body.String())
	}
	var equipment model.Equipment
	parseJSON(doRequest(router, http.MethodGet, "/api/equipment/eq", nil), &equipment)
	if equipment.Name != "Updated" || equipment.Room != "R002" || len(equipment.Sensors) != 1 || len(equipment.Actuators) != 1 {
		t.Fatalf("omitted children were not preserved: %+v", equipment)
	}

	metadataOnly["sensors"] = []model.Sensor{}
	response = doRequest(router, http.MethodPut, "/api/equipment/eq", metadataOnly)
	if response.Code != http.StatusOK {
		t.Fatalf("explicit child removal: %d %s", response.Code, response.Body.String())
	}
	parseJSON(doRequest(router, http.MethodGet, "/api/equipment/eq", nil), &equipment)
	if len(equipment.Sensors) != 0 || len(equipment.Actuators) != 1 {
		t.Fatalf("explicit child update failed: %+v", equipment)
	}
	if response := doRequest(router, http.MethodGet, "/api/sensors/sensor", nil); response.Code != http.StatusNotFound {
		t.Fatalf("removed sensor still indexed: %d", response.Code)
	}
}

func TestBulkCreateConflictIsAtomicAndNestedResourcesAreIndexed(t *testing.T) {
	router, _ := setupTestRouter()
	doRequest(router, http.MethodPost, "/api/equipment", model.Equipment{
		ID: "existing", Level: "level0", Room: "R001",
		Sensors: []model.Sensor{{ID: "shared", DataType: "text"}},
	})
	batch := []model.Equipment{
		{ID: "candidate", Level: "level0", Room: "R001", Sensors: []model.Sensor{{ID: "new", DataType: "text"}}},
		{ID: "conflict", Level: "level0", Room: "R002", Sensors: []model.Sensor{{ID: "shared", DataType: "text"}}},
	}
	response := doRequest(router, http.MethodPost, "/api/equipment/bulk", batch)
	if response.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", response.Code, response.Body.String())
	}
	if response := doRequest(router, http.MethodGet, "/api/equipment/candidate", nil); response.Code != http.StatusNotFound {
		t.Fatalf("batch was partially committed: %d", response.Code)
	}

	good := []model.Equipment{{
		ID: "good", Level: "level0", Room: "R003",
		Sensors:   []model.Sensor{{ID: "bulk-sensor", DataType: "text", Value: "4"}},
		Actuators: []model.Actuator{{ID: "bulk-actuator", State: "off"}},
	}}
	if response := doRequest(router, http.MethodPost, "/api/equipment/bulk", good); response.Code != http.StatusCreated {
		t.Fatalf("good batch: %d %s", response.Code, response.Body.String())
	}
	if response := doRequest(router, http.MethodGet, "/api/sensors/bulk-sensor", nil); response.Code != http.StatusOK {
		t.Fatalf("bulk sensor not indexed: %d", response.Code)
	}
	if response := doRequest(router, http.MethodPut, "/api/actuators/bulk-actuator/state", model.ActuatorState{State: "on"}); response.Code != http.StatusOK {
		t.Fatalf("bulk actuator not indexed: %d %s", response.Code, response.Body.String())
	}
}

func TestChildIDsAreUniqueAcrossEquipment(t *testing.T) {
	router, _ := setupTestRouter()
	for _, id := range []string{"one", "two"} {
		doRequest(router, http.MethodPost, "/api/equipment", model.Equipment{ID: id, Level: "level0", Room: "R001"})
	}
	sensor := model.Sensor{ID: "sensor", DataType: "text"}
	if response := doRequest(router, http.MethodPost, "/api/equipment/one/sensors", sensor); response.Code != http.StatusCreated {
		t.Fatal(response.Body.String())
	}
	if response := doRequest(router, http.MethodPost, "/api/equipment/two/sensors", sensor); response.Code != http.StatusConflict {
		t.Fatalf("expected global sensor conflict, got %d", response.Code)
	}
	actuator := model.Actuator{ID: "actuator", State: "off"}
	if response := doRequest(router, http.MethodPost, "/api/equipment/one/actuators", actuator); response.Code != http.StatusCreated {
		t.Fatal(response.Body.String())
	}
	if response := doRequest(router, http.MethodPost, "/api/equipment/two/actuators", actuator); response.Code != http.StatusConflict {
		t.Fatalf("expected global actuator conflict, got %d", response.Code)
	}
}

func TestSensorAndActuatorInputValidation(t *testing.T) {
	router, _ := setupTestRouter()
	doRequest(router, http.MethodPost, "/api/equipment", model.Equipment{ID: "eq", Level: "level0", Room: "R001"})
	tests := []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodPost, "/api/equipment/eq/sensors", model.Sensor{ID: "   ", DataType: "text"}},
		{http.MethodPost, "/api/equipment/eq/sensors", model.Sensor{ID: "bad", DataType: "float"}},
		{http.MethodPost, "/api/equipment/eq/actuators", model.Actuator{ID: "   ", State: "off"}},
		{http.MethodPut, "/api/sensors/missing/value", model.SensorValue{DataType: "binary"}},
		{http.MethodPut, "/api/sensors/missing/value", model.SensorValue{DataType: "float"}},
		{http.MethodPut, "/api/actuators/missing/state", model.ActuatorState{}},
		{http.MethodPut, "/api/actuators/missing/state", model.ActuatorState{State: "   "}},
	}
	for _, tt := range tests {
		response := doRequest(router, tt.method, tt.path, tt.body)
		if response.Code != http.StatusBadRequest {
			t.Errorf("%s %s: expected 400, got %d: %s", tt.method, tt.path, response.Code, response.Body.String())
		}
	}
}

func TestOccupancyUsesCanonicalFloorQualifiedKeys(t *testing.T) {
	router, memory := setupTestRouter()
	response := doRequest(router, http.MethodPut, "/api/occupancy", map[string]model.RoomOccupancy{
		"1": {Persons: []model.Person{{ID: "p"}}},
	})
	if response.Code != http.StatusOK {
		t.Fatalf("legacy unique ID: %d %s", response.Code, response.Body.String())
	}
	if _, ok := memory.GetOccupancy()["level0/R002"]; !ok {
		t.Fatalf("occupancy was not canonicalized: %+v", memory.GetOccupancy())
	}

	addAmbiguousFloor(memory)
	response = doRequest(router, http.MethodPut, "/api/occupancy", map[string]model.RoomOccupancy{"R002": {}})
	if response.Code != http.StatusBadRequest {
		t.Fatalf("ambiguous room should fail: %d %s", response.Code, response.Body.String())
	}
	response = doRequest(router, http.MethodPut, "/api/occupancy", map[string]model.RoomOccupancy{"level1/R002": {}})
	if response.Code != http.StatusOK {
		t.Fatalf("qualified room should work: %d %s", response.Code, response.Body.String())
	}
}

func TestMalformedOccupancyAndCoverageJSON(t *testing.T) {
	router, _ := setupTestRouter()
	for _, path := range []string{"/api/occupancy", "/api/coverage"} {
		response := doRawRequest(router, http.MethodPut, path, "{broken")
		if response.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d", path, response.Code)
		}
	}
}

func TestSessionValidationAndCanonicalization(t *testing.T) {
	router, memory := setupTestRouter()
	var session model.Session
	parseJSON(doRequest(router, http.MethodPost, "/api/sessions", nil), &session)
	base := "/api/sessions/" + session.ID

	tests := []struct {
		path string
		body any
	}{
		{"/viewport", model.Viewport{Mode: "sideways"}},
		{"/viewport", model.Viewport{Mode: "2d", Floor: "missing"}},
		{"/viewport", model.Viewport{Mode: "2d", Zoom: -1}},
		{"/highlights", []model.RoomHighlight{{Level: "level0", Room: "missing"}}},
		{"/highlights", []model.RoomHighlight{{Level: "level0", Room: "R001", Opacity: 2}}},
		{"/coverage", []model.CoverageZone{{ID: "zone", Level: "level0", Room: "R001", Radius: 0}}},
		{"/coverage", []model.CoverageZone{{ID: "same", Radius: 1}, {ID: "same", Radius: 2}}},
	}
	for _, tt := range tests {
		response := doRequest(router, http.MethodPut, base+tt.path, tt.body)
		if response.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d: %s", tt.path, response.Code, response.Body.String())
		}
	}

	if response := doRequest(router, http.MethodPut, base+"/viewport", model.Viewport{Mode: "2d", Floor: "level0", Room: "R002", Zoom: 2}); response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	if response := doRequest(router, http.MethodPut, base+"/highlights", []model.RoomHighlight{{Level: "level0", Room: "R002"}}); response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	stored, _ := memory.GetSession(session.ID)
	if stored.Viewport.Floor != "level0" || stored.Viewport.Room != "R002" || stored.Highlights[0].Level != "level0" || stored.Highlights[0].Room != "R002" {
		t.Fatalf("session values were not canonicalized: %+v", stored)
	}

	response := doRawRequest(router, http.MethodPut, base+"/highlights", `[{"level":"level0","room":"R001"}]`)
	if response.Code != http.StatusOK {
		t.Fatalf("default highlight: %d %s", response.Code, response.Body.String())
	}
	stored, _ = memory.GetSession(session.ID)
	if stored.Highlights[0].Opacity != 0.8 || stored.Highlights[0].Color != "#ffcc00" {
		t.Fatalf("highlight defaults = %+v", stored.Highlights[0])
	}

	response = doRawRequest(router, http.MethodPut, base+"/highlights", `[{"level":"level0","room":"R001","opacity":0}]`)
	if response.Code != http.StatusOK {
		t.Fatalf("transparent highlight: %d %s", response.Code, response.Body.String())
	}
	stored, _ = memory.GetSession(session.ID)
	if stored.Highlights[0].Opacity != 0 {
		t.Fatalf("explicit zero opacity became %v", stored.Highlights[0].Opacity)
	}
}

func TestMutationsAutomaticallyIncrementEquipmentVersion(t *testing.T) {
	router, memory := setupTestRouter()
	assertVersion := func(want int64) {
		t.Helper()
		if got := memory.GetEquipmentVersion(); got != want {
			t.Fatalf("version=%d, want %d", got, want)
		}
	}

	doRequest(router, http.MethodPost, "/api/equipment", model.Equipment{ID: "eq", Level: "level0", Room: "R001"})
	assertVersion(1)
	doRequest(router, http.MethodPost, "/api/equipment/eq/sensors", model.Sensor{ID: "sensor", DataType: "text"})
	assertVersion(2)
	doRequest(router, http.MethodPut, "/api/sensors/sensor/value", model.SensorValue{DataType: "text", Value: "2"})
	assertVersion(3)
	doRequest(router, http.MethodPost, "/api/equipment/eq/actuators", model.Actuator{ID: "actuator", State: "off"})
	assertVersion(4)
	doRequest(router, http.MethodPut, "/api/actuators/actuator/state", model.ActuatorState{State: "on"})
	assertVersion(5)
	doRequest(router, http.MethodDelete, "/api/equipment/eq", nil)
	assertVersion(6)
}

func doRawRequest(router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	return response
}

func addAmbiguousFloor(memory interface {
	SetFloorData(string, *model.FloorData)
}) {
	memory.SetFloorData("level1", &model.FloorData{
		Page:  model.Page{Width: 100, Height: 100},
		Rooms: []model.Room{{ID: 1, Name: "R002", Center: [2]float64{10, 10}}},
	})
}

func decodeBody(t *testing.T, response *httptest.ResponseRecorder, value any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), value); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, response.Body.String())
	}
}
