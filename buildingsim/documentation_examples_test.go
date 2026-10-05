package buildingsim_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	buildingsim "github.com/eislab-cps/buildingsim"
	"github.com/eislab-cps/buildingsim/pkg/model"
	buildserver "github.com/eislab-cps/buildingsim/pkg/server"
)

type documentationAPI struct {
	t      *testing.T
	base   string
	client *http.Client
}

func newDocumentationAPI(t *testing.T) (*documentationAPI, func()) {
	t.Helper()
	application := buildserver.New(0, buildingsim.DataFS, buildingsim.WebFS, false)
	router, err := application.Router()
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(router)
	return &documentationAPI{t: t, base: httpServer.URL, client: httpServer.Client()}, httpServer.Close
}

func (api *documentationAPI) json(method, path string, payload, result any, expectedStatus int) http.Header {
	api.t.Helper()
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			api.t.Fatal(err)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, api.base+path, body)
	if err != nil {
		api.t.Fatal(err)
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := api.client.Do(request)
	if err != nil {
		api.t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		api.t.Fatal(err)
	}
	if response.StatusCode != expectedStatus {
		api.t.Fatalf("%s %s: HTTP %d, want %d: %s", method, path, response.StatusCode, expectedStatus, data)
	}
	if result != nil {
		if err := json.Unmarshal(data, result); err != nil {
			api.t.Fatalf("decode %s %s: %v\n%s", method, path, err, data)
		}
	}
	return response.Header
}

func TestDocumentedDiscoveryAndRoutingExamples(t *testing.T) {
	api, closeServer := newDocumentationAPI(t)
	defer closeServer()

	var health map[string]string
	api.json(http.MethodGet, "/healthz", nil, &health, http.StatusOK)
	if health["status"] != "ok" {
		t.Fatalf("health response = %#v", health)
	}

	var config struct {
		UI   string `json:"ui"`
		Host string `json:"host"`
	}
	api.json(http.MethodGet, "/api/config", nil, &config, http.StatusOK)
	if config.UI != "modern" || config.Host != "127.0.0.1" {
		t.Fatalf("config = %+v", config)
	}

	var building model.Building
	api.json(http.MethodGet, "/api/building", nil, &building, http.StatusOK)
	if building.Name != "A-Building (LTU)" || len(building.Levels) != 3 {
		t.Fatalf("building = %+v", building)
	}

	var floor model.FloorData
	api.json(http.MethodGet, "/api/building/floors/level0", nil, &floor, http.StatusOK)
	if floor.Page.Width != 384.56000000000006 || floor.Page.Height != 365.92 || len(floor.Rooms) != 322 {
		t.Fatalf("level0 dimensions/count changed: page=%+v rooms=%d", floor.Page, len(floor.Rooms))
	}
	if floor.Rooms[0].Name != "1540" || floor.Rooms[0].Center != [2]float64{17.88000000000001, 187.75} {
		t.Fatalf("first documented room changed: %+v", floor.Rooms[0])
	}

	var crossFloor []model.CrossFloorEdge
	api.json(http.MethodGet, "/api/building/cross-floor-edges", nil, &crossFloor, http.StatusOK)
	if len(crossFloor) != 32 || crossFloor[0].FromName != "A1105" || crossFloor[0].ToName != "A2001" {
		t.Fatalf("cross-floor example changed: count=%d first=%+v", len(crossFloor), crossFloor[0])
	}

	var graph model.NavGraph
	api.json(http.MethodGet, "/api/graph?level=level0&type=walkable", nil, &graph, http.StatusOK)
	if len(graph.Nodes) == 0 || graph.Nodes[0].Name != "1540" || graph.Nodes[0].X != 17.33 {
		t.Fatalf("walkable graph example changed: %+v", graph.Nodes[0])
	}

	var sameFloor model.RouteResult
	api.json(http.MethodGet, "/api/graph/route?from_name=1542&to_name=A1123&level=level0&type=walkable", nil, &sameFloor, http.StatusOK)
	assertRouteEndpoints(t, sameFloor, "1542", "A1123")
	if sameFloor.Distance != 417.8 {
		t.Fatalf("same-floor distance = %v", sameFloor.Distance)
	}

	var crossRoute model.RouteResult
	api.json(http.MethodGet, "/api/graph/route?from_name=A2306&from_level=level1&to_name=A109&to_level=level0&type=walkable", nil, &crossRoute, http.StatusOK)
	assertRouteEndpoints(t, crossRoute, "A2306", "A109")
	if crossRoute.Distance != 516.7 || crossRoute.Path[0].Level != "level1" || crossRoute.Path[len(crossRoute.Path)-1].Level != "level0" {
		t.Fatalf("cross-floor route changed: distance=%v endpoints=%+v/%+v", crossRoute.Distance, crossRoute.Path[0], crossRoute.Path[len(crossRoute.Path)-1])
	}

	var nodeRoute model.RouteResult
	api.json(http.MethodGet, "/api/graph/route?from=0&to=5&level=level0", nil, &nodeRoute, http.StatusOK)
	if len(nodeRoute.Path) == 0 {
		t.Fatal("node-ID route is empty")
	}

	response, err := api.client.Get(api.base + "/api/icons/compressor.svg")
	if err != nil {
		t.Fatal(err)
	}
	data, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil || response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "image/svg+xml" || !bytes.Contains(data, []byte("<svg")) {
		t.Fatalf("icon example failed: status=%d type=%q err=%v", response.StatusCode, response.Header.Get("Content-Type"), readErr)
	}
}

func assertRouteEndpoints(t *testing.T, route model.RouteResult, from, to string) {
	t.Helper()
	if len(route.Path) == 0 || route.Path[0].Name != from || route.Path[len(route.Path)-1].Name != to {
		t.Fatalf("route endpoints = %+v", route.Path)
	}
}

func TestDocumentedEquipmentSequence(t *testing.T) {
	api, closeServer := newDocumentationAPI(t)
	defer closeServer()

	create := model.Equipment{
		ID: "temp-1", Name: "Room Temperature Sensor", Type: "temperature_sensor",
		Category: "monitoring", Level: "level0", Room: "1542", Status: "running",
	}
	var created model.Equipment
	headers := api.json(http.MethodPost, "/api/equipment", create, &created, http.StatusCreated)
	if created.Version != 1 || headers.Get("X-BuildSim-Version") != "1" {
		t.Fatalf("single-create version=%d header=%q", created.Version, headers.Get("X-BuildSim-Version"))
	}

	batch := []model.Equipment{
		{
			ID: "bulk-temp-1", Name: "Bulk Temp Sensor", Type: "temperature_sensor", Category: "monitoring",
			Level: "level0", Room: "1542", Status: "running",
			Sensors: []model.Sensor{{ID: "bulk-temp-1-val", Name: "Temperature", Type: "temperature", DataType: "text", Unit: "°C", Value: "21.5"}},
		},
		{
			ID: "bulk-door-1", Name: "Bulk Door Lock", Type: "door_lock", Category: "access_control",
			Level: "level0", Room: "1542", Status: "running",
			Sensors:   []model.Sensor{{ID: "bulk-door-1-pos", Name: "Position", Type: "door_position", DataType: "binary"}},
			Actuators: []model.Actuator{{ID: "bulk-door-1-lock", Name: "Lock", Type: "lock_control", State: "locked"}},
		},
	}
	var bulk struct {
		Created int   `json:"created"`
		Skipped int   `json:"skipped"`
		Total   int   `json:"total"`
		Version int64 `json:"version"`
	}
	api.json(http.MethodPost, "/api/equipment/bulk", batch, &bulk, http.StatusCreated)
	if bulk.Created != 2 || bulk.Skipped != 0 || bulk.Total != 2 || bulk.Version != 2 {
		t.Fatalf("bulk response = %+v", bulk)
	}

	var listed []model.Equipment
	api.json(http.MethodGet, "/api/equipment?level=level0&category=monitoring", nil, &listed, http.StatusOK)
	if len(listed) != 2 {
		t.Fatalf("filtered equipment count = %d", len(listed))
	}
	api.json(http.MethodGet, "/api/equipment/temp-1", nil, &created, http.StatusOK)

	update := model.Equipment{
		Name: "Room Temperature Sensor (Updated)", Type: "temperature_sensor", Category: "monitoring",
		Level: "level0", Room: "1542", Status: "warning",
	}
	api.json(http.MethodPut, "/api/equipment/temp-1", update, &created, http.StatusOK)
	if created.Status != "warning" || created.Name != update.Name {
		t.Fatalf("updated equipment = %+v", created)
	}

	disposable := model.Equipment{ID: "delete-me", Name: "Disposable", Type: "test", Category: "monitoring", Level: "level0", Room: "1542", Status: "stopped"}
	api.json(http.MethodPost, "/api/equipment", disposable, nil, http.StatusCreated)
	api.json(http.MethodDelete, "/api/equipment/delete-me", nil, nil, http.StatusOK)
	api.json(http.MethodPost, "/api/equipment/notify", nil, nil, http.StatusOK)

	door := model.Equipment{ID: "door-1", Name: "Door 1", Type: "door_lock", Category: "access_control", Level: "level0", Room: "1542", Status: "running"}
	api.json(http.MethodPost, "/api/equipment", door, nil, http.StatusCreated)
	textSensor := model.Sensor{ID: "temp-1-reading", Name: "Temperature", Type: "temperature", DataType: "text", Unit: "°C", Value: "21.5"}
	binarySensor := model.Sensor{ID: "door-1-pos", Name: "Door Position", Type: "door_position", DataType: "binary"}
	api.json(http.MethodPost, "/api/equipment/temp-1/sensors", textSensor, nil, http.StatusCreated)
	api.json(http.MethodPost, "/api/equipment/door-1/sensors", binarySensor, nil, http.StatusCreated)

	var sensors []model.Sensor
	api.json(http.MethodGet, "/api/equipment/temp-1/sensors", nil, &sensors, http.StatusOK)
	if len(sensors) != 1 || sensors[0].ID != textSensor.ID {
		t.Fatalf("sensor list = %+v", sensors)
	}
	var sensor model.Sensor
	api.json(http.MethodGet, "/api/sensors/temp-1-reading", nil, &sensor, http.StatusOK)
	api.json(http.MethodPut, "/api/sensors/temp-1-reading/value", model.SensorValue{DataType: "text", Value: "23.7"}, nil, http.StatusOK)
	binaryValue := true
	api.json(http.MethodPut, "/api/sensors/door-1-pos/value", model.SensorValue{DataType: "binary", BinaryValue: &binaryValue}, nil, http.StatusOK)
	api.json(http.MethodDelete, "/api/sensors/temp-1-reading", nil, nil, http.StatusOK)

	actuator := model.Actuator{ID: "door-1-lock", Name: "Lock Control", Type: "lock_control", State: "locked"}
	api.json(http.MethodPost, "/api/equipment/door-1/actuators", actuator, nil, http.StatusCreated)
	var actuators []model.Actuator
	api.json(http.MethodGet, "/api/equipment/door-1/actuators", nil, &actuators, http.StatusOK)
	if len(actuators) != 1 || actuators[0].ID != actuator.ID {
		t.Fatalf("actuator list = %+v", actuators)
	}
	api.json(http.MethodGet, "/api/actuators/door-1-lock", nil, &actuator, http.StatusOK)
	api.json(http.MethodPut, "/api/actuators/door-1-lock/state", model.ActuatorState{State: "unlocked"}, nil, http.StatusOK)
	api.json(http.MethodDelete, "/api/actuators/door-1-lock", nil, nil, http.StatusOK)

	fullDoor := model.Equipment{ID: "door-main", Name: "Main Entrance", Type: "door_lock", Category: "access_control", Level: "level0", Room: "1542", Status: "running"}
	api.json(http.MethodPost, "/api/equipment", fullDoor, nil, http.StatusCreated)
	api.json(http.MethodPost, "/api/equipment/door-main/sensors", model.Sensor{ID: "door-main-pos", Name: "Position", Type: "door_position", DataType: "binary"}, nil, http.StatusCreated)
	api.json(http.MethodPost, "/api/equipment/door-main/sensors", model.Sensor{ID: "door-main-lock-st", Name: "Lock Status", Type: "lock_status", DataType: "binary"}, nil, http.StatusCreated)
	api.json(http.MethodPost, "/api/equipment/door-main/actuators", model.Actuator{ID: "door-main-lock", Name: "Lock", Type: "lock_control", State: "locked"}, nil, http.StatusCreated)
	api.json(http.MethodPut, "/api/sensors/door-main-pos/value", model.SensorValue{DataType: "binary", BinaryValue: &binaryValue}, nil, http.StatusOK)
	api.json(http.MethodPut, "/api/actuators/door-main-lock/state", model.ActuatorState{State: "unlocked"}, nil, http.StatusOK)
}

func TestDocumentedSessionAndOverlaySequence(t *testing.T) {
	api, closeServer := newDocumentationAPI(t)
	defer closeServer()

	globalOccupancy := map[string]model.RoomOccupancy{
		"level0/A109": {Persons: []model.Person{{ID: "person-1", Name: "Alice", Icon: "woman"}, {ID: "person-2", Name: "Bob", Icon: "man"}}, Aliens: []model.Alien{}},
	}
	api.json(http.MethodPut, "/api/occupancy", globalOccupancy, nil, http.StatusOK)
	var occupancy map[string]model.RoomOccupancy
	api.json(http.MethodGet, "/api/occupancy", nil, &occupancy, http.StatusOK)
	if len(occupancy["level0/A109"].Persons) != 2 {
		t.Fatalf("global occupancy = %+v", occupancy)
	}

	globalCoverage := []model.CoverageZone{{ID: "wifi-A109", Name: "Wi-Fi A109", Level: "level0", Room: "A109", Radius: 25, Color: "#00aaff", Opacity: 0.15, Height: 20}}
	api.json(http.MethodPut, "/api/coverage", globalCoverage, nil, http.StatusOK)
	var coverage []model.CoverageZone
	api.json(http.MethodGet, "/api/coverage", nil, &coverage, http.StatusOK)
	if len(coverage) != 1 || coverage[0].Room != "A109" {
		t.Fatalf("global coverage = %+v", coverage)
	}

	var session model.Session
	api.json(http.MethodPost, "/api/sessions", nil, &session, http.StatusCreated)
	if session.ID == "" {
		t.Fatal("empty session ID")
	}
	path := "/api/sessions/" + session.ID
	api.json(http.MethodPut, path+"/viewport", model.Viewport{Floor: "level0", Room: "A109", Zoom: 2, Mode: "3d"}, nil, http.StatusOK)
	api.json(http.MethodPut, path+"/highlights", []model.RoomHighlight{{Level: "level0", Room: "A109", Color: "#ff0000", Opacity: 0.8}}, nil, http.StatusOK)
	api.json(http.MethodPut, path+"/occupancy", map[string]model.RoomOccupancy{"level0/A109": {Persons: []model.Person{{ID: "p1", Name: "Alice"}}, Aliens: []model.Alien{}}}, nil, http.StatusOK)
	api.json(http.MethodPut, path+"/coverage", globalCoverage, nil, http.StatusOK)

	var route model.RouteResult
	api.json(http.MethodGet, "/api/graph/route?from_name=1542&to_name=A1123&level=level0&type=walkable", nil, &route, http.StatusOK)
	api.json(http.MethodPut, path+"/route", route, nil, http.StatusOK)
	api.json(http.MethodGet, path, nil, &session, http.StatusOK)
	if session.Version != 5 || session.Viewport.Room != "A109" || len(session.Highlights) != 1 || session.Route == nil || len(session.Coverage) != 1 {
		t.Fatalf("session snapshot = %+v", session)
	}
	var sessions []model.Session
	api.json(http.MethodGet, "/api/sessions", nil, &sessions, http.StatusOK)
	if len(sessions) != 1 || sessions[0].ID != session.ID {
		t.Fatalf("session list = %+v", sessions)
	}
	api.json(http.MethodDelete, path, nil, nil, http.StatusOK)
}

func TestDocumentationLocalLinksAndTutorialSource(t *testing.T) {
	var markdownFiles []string
	err := filepath.WalkDir("docs", func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
			markdownFiles = append(markdownFiles, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	markdownFiles = append(markdownFiles, "../tutorials/buildsim.md")
	linkPattern := regexp.MustCompile(`!?\[[^]]*\]\(([^)]+)\)`)
	htmlAssetPattern := regexp.MustCompile(`<(?:img|a)\b[^>]*(?:src|href)="([^"]+)"`)
	for _, filename := range markdownFiles {
		data, readErr := os.ReadFile(filename)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if strings.Count(string(data), "```")%2 != 0 {
			t.Errorf("%s has an unclosed fenced block", filename)
		}
		for _, match := range linkPattern.FindAllStringSubmatch(string(data), -1) {
			target := strings.TrimSpace(strings.SplitN(match[1], "#", 2)[0])
			target = strings.Trim(target, "<>")
			if target == "" || strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			resolved := filepath.Clean(filepath.Join(filepath.Dir(filename), target))
			if _, statErr := os.Stat(resolved); statErr != nil {
				t.Errorf("%s: broken local link %q (%s)", filename, match[1], resolved)
			}
		}
		for _, match := range htmlAssetPattern.FindAllStringSubmatch(string(data), -1) {
			target := strings.TrimSpace(strings.SplitN(match[1], "#", 2)[0])
			if target == "" || strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			resolved := filepath.Clean(filepath.Join(filepath.Dir(filename), target))
			if _, statErr := os.Stat(resolved); statErr != nil {
				t.Errorf("%s: broken HTML asset %q (%s)", filename, match[1], resolved)
			}
		}
	}

	tutorial, err := os.ReadFile("../tutorials/buildsim.md")
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("../tutorials/buildsim/main.go")
	if err != nil {
		t.Fatal(err)
	}
	const start = "`main.go`:\n\n```go\n"
	position := strings.Index(string(tutorial), start)
	if position < 0 {
		t.Fatal("tutorial main.go code block not found")
	}
	remainder := string(tutorial[position+len(start):])
	code, _, found := strings.Cut(remainder, "\n```")
	if !found || strings.TrimSpace(code) != strings.TrimSpace(string(source)) {
		t.Fatal("tutorial main.go block differs from tutorials/buildsim/main.go")
	}
}

func TestDocumentedVisualizationScenario(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("visualization scenario scripts require a POSIX shell")
	}
	for _, executable := range []string{"bash", "curl"} {
		if _, err := exec.LookPath(executable); err != nil {
			t.Skipf("%s is unavailable", executable)
		}
	}
	api, closeServer := newDocumentationAPI(t)
	defer closeServer()

	show := exec.Command("bash", "examples/api/scenario/show_visual_effects.sh", api.base)
	if output, err := show.CombinedOutput(); err != nil {
		t.Fatalf("show_visual_effects.sh: %v\n%s", err, output)
	}

	var layers []model.RoomLayer
	api.json(http.MethodGet, "/api/room-layers", nil, &layers, http.StatusOK)
	var effects []model.VisualEffect
	api.json(http.MethodGet, "/api/effects", nil, &effects, http.StatusOK)
	var entities []model.MobileEntity
	api.json(http.MethodGet, "/api/entities", nil, &entities, http.StatusOK)
	var appearances []model.RoomAppearance
	api.json(http.MethodGet, "/api/room-appearance", nil, &appearances, http.StatusOK)
	var doors []model.Door
	api.json(http.MethodGet, "/api/doors", nil, &doors, http.StatusOK)
	var alerts []model.DecisionAlert
	api.json(http.MethodGet, "/api/alerts", nil, &alerts, http.StatusOK)
	if len(layers) != 2 || len(effects) != 4 || len(entities) != 2 || len(appearances) != 2 || len(doors) != 3 || len(alerts) != 2 {
		t.Fatalf("scenario collection sizes: layers=%d effects=%d entities=%d appearances=%d doors=%d alerts=%d",
			len(layers), len(effects), len(entities), len(appearances), len(doors), len(alerts))
	}
	if !doors[0].Blocked || doors[0].EntryNodeID == nil || *doors[0].EntryNodeID != 524 {
		t.Fatalf("scenario lock was not normalized as expected: %+v", doors[0])
	}
	api.json(http.MethodGet, "/api/graph/route?from_name=A108&to_name=A109&level=level0&type=walkable", nil, nil, http.StatusNotFound)

	clear := exec.Command("bash", "examples/api/scenario/clear_visual_effects.sh", api.base)
	if output, err := clear.CombinedOutput(); err != nil {
		t.Fatalf("clear_visual_effects.sh: %v\n%s", err, output)
	}
	api.json(http.MethodGet, "/api/effects", nil, &effects, http.StatusOK)
	api.json(http.MethodGet, "/api/entities", nil, &entities, http.StatusOK)
	api.json(http.MethodGet, "/api/doors", nil, &doors, http.StatusOK)
	if len(effects) != 0 || len(entities) != 0 || len(doors) != 0 {
		t.Fatalf("clear script left transient state: effects=%d entities=%d doors=%d", len(effects), len(entities), len(doors))
	}
}

func TestTemperatureHeatmapExample(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("temperature heatmap script requires a POSIX shell")
	}
	for _, executable := range []string{"bash", "curl"} {
		if _, err := exec.LookPath(executable); err != nil {
			t.Skipf("%s is unavailable", executable)
		}
	}
	api, closeServer := newDocumentationAPI(t)
	defer closeServer()

	command := exec.Command("bash", "examples/api/room-layers/set_temperature_heatmap.sh", api.base)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("set_temperature_heatmap.sh: %v\n%s", err, output)
	}

	var layers []model.RoomLayer
	api.json(http.MethodGet, "/api/room-layers", nil, &layers, http.StatusOK)
	if len(layers) != 1 {
		t.Fatalf("heatmap published %d room layers, want 1", len(layers))
	}
	layer := layers[0]
	if layer.ID != "temperature" || layer.Unit != "°C" || layer.Source != "simulation truth" {
		t.Fatalf("unexpected heatmap metadata: %+v", layer)
	}
	if layer.Minimum != 18 || layer.Maximum != 35 || layer.Opacity != 0.78 {
		t.Fatalf("unexpected heatmap scale: min=%v max=%v opacity=%v", layer.Minimum, layer.Maximum, layer.Opacity)
	}
	if len(layer.Palette) != 5 || len(layer.Values) != 11 {
		t.Fatalf("unexpected heatmap data: colors=%d rooms=%d", len(layer.Palette), len(layer.Values))
	}
	if value := layer.Values["level0/A109"]; value != 32.6 {
		t.Fatalf("A109 temperature=%v, want 32.6", value)
	}
}

func TestTutorialShellScriptsAgainstBuildSim(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tutorial scripts require a POSIX shell")
	}
	for _, executable := range []string{"bash", "curl", "python3"} {
		if _, err := exec.LookPath(executable); err != nil {
			t.Skipf("%s is unavailable", executable)
		}
	}
	api, closeServer := newDocumentationAPI(t)
	defer closeServer()

	setup := exec.Command("bash", "../tutorials/buildsim/setup.sh", api.base)
	if output, err := setup.CombinedOutput(); err != nil {
		t.Fatalf("setup.sh: %v\n%s", err, output)
	}
	// The documented setup is explicitly safe to rerun.
	setup = exec.Command("bash", "../tutorials/buildsim/setup.sh", api.base)
	if output, err := setup.CombinedOutput(); err != nil {
		t.Fatalf("second setup.sh: %v\n%s", err, output)
	}
	getTemperature := exec.Command("bash", "../tutorials/buildsim/gettemp.sh", api.base)
	output, err := getTemperature.CombinedOutput()
	if err != nil {
		t.Fatalf("gettemp.sh: %v\n%s", err, output)
	}
	if strings.TrimSpace(string(output)) != "18.0 °C" {
		t.Fatalf("gettemp.sh output = %q", output)
	}
}
