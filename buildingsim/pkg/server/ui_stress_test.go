package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/eislab-cps/buildingsim/pkg/model"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// TestUIEquipmentNotificationStorm drives both real viewers in headless
// Chromium. It is opt-in because it requires a browser executable; CI and
// `make stress-ui` enable it. The test proves that a sensor-update storm is
// coalesced into a bounded number of full equipment snapshots while the page,
// WebSocket, final value, and session remain live.
func TestUIEquipmentNotificationStorm(t *testing.T) {
	if os.Getenv("BUILDSIM_UI_STRESS") != "1" {
		t.Skip("set BUILDSIM_UI_STRESS=1 or run make stress-ui")
	}
	browser := findChromium(t)
	for _, ui := range []string{UIModern, UIClassic} {
		t.Run(ui, func(t *testing.T) {
			runUIEquipmentNotificationStorm(t, browser, ui)
		})
	}
}

func runUIEquipmentNotificationStorm(t *testing.T, browser, ui string) {
	t.Helper()

	previousGinWriter := gin.DefaultWriter
	gin.DefaultWriter = io.Discard
	t.Cleanup(func() { gin.DefaultWriter = previousGinWriter })

	app := New(0, os.DirFS("../.."), os.DirFS("../.."), false)
	app.UI = ui
	router, err := app.Router()
	if err != nil {
		t.Fatal(err)
	}
	var equipmentGETs atomic.Int64
	var entityGETs atomic.Int64
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet && request.URL.Path == "/api/equipment" {
			equipmentGETs.Add(1)
		}
		if request.Method == http.MethodGet && request.URL.Path == "/api/entities" {
			entityGETs.Add(1)
		}
		router.ServeHTTP(writer, request)
	})
	service := httptest.NewServer(handler)
	defer service.Close()

	floor, ok := app.store.GetFloorData("level0")
	if !ok || len(floor.Rooms) == 0 {
		t.Fatal("level0 has no room for stress-test equipment")
	}
	equipment := model.Equipment{
		ID: "ui-stress-equipment", Name: "UI stress sensor", Type: "sensor",
		Category: "test", Level: "level0", Room: floor.Rooms[0].Name,
		Sensors: []model.Sensor{{
			ID: "ui-stress-sensor", Name: "Storm", Type: "temperature",
			DataType: "text", Value: "initial", Unit: "test",
		}},
	}
	client := &http.Client{Timeout: 10 * time.Second}
	if err := sendJSON(client, http.MethodPost, service.URL+"/api/equipment", equipment); err != nil {
		t.Fatal(err)
	}

	debugPort := freeTCPPort(t)
	chromiumDir := t.TempDir()
	profile := filepath.Join(chromiumDir, "profile")
	logPath := filepath.Join(chromiumDir, "chromium.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(browser,
		"--headless",
		"--no-sandbox",
		"--no-proxy-server",
		"--disable-dev-shm-usage",
		"--disable-background-networking",
		"--disable-default-apps",
		"--disable-extensions",
		"--disable-sync",
		"--metrics-recording-only",
		"--no-first-run",
		"--use-gl=angle",
		"--use-angle=swiftshader",
		"--enable-unsafe-swiftshader",
		"--remote-allow-origins=*",
		fmt.Sprintf("--remote-debugging-port=%d", debugPort),
		"--remote-debugging-address=127.0.0.1",
		"--user-data-dir="+profile,
		"--window-size=1280,900",
		service.URL+"/?stress-ui=1",
	)
	command.Stdout = logFile
	command.Stderr = logFile
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		_ = logFile.Close()
		t.Fatalf("start %s: %v", browser, err)
	}
	processDone := make(chan error, 1)
	go func() { processDone <- command.Wait() }()
	t.Cleanup(func() {
		if command.Process != nil {
			_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		}
		select {
		case <-processDone:
		case <-time.After(5 * time.Second):
		}
		_ = logFile.Close()
	})

	pageSocket, err := waitForPageSocket(debugPort, service.URL, 20*time.Second)
	if err != nil {
		t.Fatalf("%v\nChromium log:\n%s", err, readTestLog(logPath))
	}
	connection, _, err := websocket.DefaultDialer.Dial(pageSocket, nil)
	if err != nil {
		t.Fatalf("connect to Chromium DevTools: %v", err)
	}
	defer connection.Close()
	devtools := &cdpConnection{socket: connection}

	readyExpression := `JSON.stringify({
		ready: !!((window.buildsim && window.buildsim.ready()) || (window._buildsimDiagnostics || {}).viewerReady),
		session: window._viewerSessionId || "",
		chip: (() => { const e=document.querySelector("#sessionChip,#session-chip"); return !!(e && !e.disabled && e.textContent.includes((window._viewerSessionId || "").slice(0,8))); })(),
		booting: document.body.classList.contains("booting"),
		ws: (window._buildsimDiagnostics || {}).websocketState || ""
	})`
	type readyState struct {
		Ready   bool   `json:"ready"`
		Session string `json:"session"`
		Chip    bool   `json:"chip"`
		Booting bool   `json:"booting"`
		WS      string `json:"ws"`
	}
	var ready readyState
	if err := pollCDP(devtools, 30*time.Second, readyExpression, &ready, func() bool {
		return ready.Ready && ready.Session != "" && ready.Chip && !ready.Booting && ready.WS == "connected"
	}); err != nil {
		t.Fatalf("viewer did not become ready: %v; state=%+v\nChromium log:\n%s", err, ready, readTestLog(logPath))
	}

	const updates = 1000
	const workers = 20
	jobs := make(chan int)
	errorsFound := make(chan error, workers)
	var wait sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for value := range jobs {
				payload := model.SensorValue{DataType: "text", Value: fmt.Sprintf("storm-%04d", value)}
				if requestErr := sendJSON(client, http.MethodPut, service.URL+"/api/sensors/ui-stress-sensor/value", payload); requestErr != nil {
					select {
					case errorsFound <- requestErr:
					default:
					}
					return
				}
			}
		}()
	}
	for update := 0; update < updates; update++ {
		jobs <- update
	}
	close(jobs)
	wait.Wait()
	close(errorsFound)
	for requestErr := range errorsFound {
		t.Fatalf("sensor storm request: %v", requestErr)
	}

	// Let the WebSocket writer drain, then send a deterministic trailing value.
	// This also proves that the page is still processing notifications after the
	// burst, rather than merely displaying a snapshot captured during it.
	time.Sleep(300 * time.Millisecond)
	if err := sendJSON(client, http.MethodPut, service.URL+"/api/sensors/ui-stress-sensor/value", model.SensorValue{
		DataType: "text", Value: "final-after-storm",
	}); err != nil {
		t.Fatal(err)
	}

	stateExpression := `(() => {
		const diagnostics = window._buildsimDiagnostics || {};
		return JSON.stringify({
			ready: !!((window.buildsim && window.buildsim.ready()) || diagnostics.viewerReady),
			session: window._viewerSessionId || "",
			value: (diagnostics.sensorValues || {})["ui-stress-sensor"] || "",
			diagnostics
		});
	})()`
	type finalState struct {
		Ready       bool   `json:"ready"`
		Session     string `json:"session"`
		Value       string `json:"value"`
		Diagnostics struct {
			EquipmentNotifications       int64  `json:"equipmentNotifications"`
			EquipmentRefreshes           int64  `json:"equipmentRefreshes"`
			LastNotifiedEquipmentVersion int64  `json:"lastNotifiedEquipmentVersion"`
			LastAppliedEquipmentVersion  int64  `json:"lastAppliedEquipmentVersion"`
			WebSocketState               string `json:"websocketState"`
		} `json:"diagnostics"`
	}
	var final finalState
	if err := pollCDP(devtools, 15*time.Second, stateExpression, &final, func() bool {
		return final.Ready && final.Value == "final-after-storm" &&
			final.Diagnostics.WebSocketState == "connected" &&
			final.Diagnostics.LastAppliedEquipmentVersion >= final.Diagnostics.LastNotifiedEquipmentVersion
	}); err != nil {
		t.Fatalf("viewer did not converge after storm: %v; state=%+v\nChromium log:\n%s", err, final, readTestLog(logPath))
	}

	if final.Session != ready.Session {
		t.Fatalf("viewer session changed from %q to %q", ready.Session, final.Session)
	}
	if final.Diagnostics.EquipmentNotifications == 0 {
		t.Fatal("viewer did not receive an equipment notification")
	}
	if final.Diagnostics.EquipmentRefreshes >= updates/10 {
		t.Fatalf("coalescing ineffective: %d refreshes for %d updates", final.Diagnostics.EquipmentRefreshes, updates)
	}
	if got := equipmentGETs.Load(); got >= updates/10 {
		t.Fatalf("coalescing ineffective: %d GET /api/equipment requests for %d updates", got, updates)
	}
	t.Logf("viewer remained responsive: %d notifications -> %d refreshes (%d equipment GETs)",
		final.Diagnostics.EquipmentNotifications, final.Diagnostics.EquipmentRefreshes, equipmentGETs.Load())

	if ui == UIModern {
		runUIVisualizationStorm(t, client, service.URL, devtools, floor.Rooms, &entityGETs)
	}
}

// runUIVisualizationStorm exercises the renderer-specific APIs after the
// equipment storm. Rapid entity snapshots test WebSocket coalescing; the
// final large snapshot proves that effects, moving entities, door state, room
// illumination, room layers, and alerts can coexist in the actual viewer.
func runUIVisualizationStorm(t *testing.T, client *http.Client, baseURL string, devtools *cdpConnection, rooms []model.Room, entityGETs *atomic.Int64) {
	t.Helper()
	uniqueRooms := make([]model.Room, 0, len(rooms))
	seenRoom := make(map[string]bool, len(rooms))
	for _, room := range rooms {
		if room.Name != "" && !seenRoom[room.Name] {
			seenRoom[room.Name] = true
			uniqueRooms = append(uniqueRooms, room)
		}
	}
	rooms = uniqueRooms
	if len(rooms) < 20 {
		t.Fatalf("need at least 20 uniquely named rooms for visualization stress test, got %d", len(rooms))
	}

	const updates = 400
	const workers = 16
	jobs := make(chan int)
	errorsFound := make(chan error, workers)
	var wait sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for update := range jobs {
				payload := []model.MobileEntity{{
					ID: "moving-storm", Name: "Moving stress entity", Type: "generic",
					Level: "level0", Room: rooms[update%len(rooms)].Name,
					Heading: float64(update % 360), TransitionMS: 25,
				}}
				if err := sendJSON(client, http.MethodPut, baseURL+"/api/entities", payload); err != nil {
					select {
					case errorsFound <- err:
					default:
					}
					return
				}
			}
		}()
	}
	for update := 0; update < updates; update++ {
		jobs <- update
	}
	close(jobs)
	wait.Wait()
	close(errorsFound)
	for requestErr := range errorsFound {
		t.Fatalf("visualization storm request: %v", requestErr)
	}

	// Allow the notification queue to drain before publishing a deterministic
	// final scene. Every collection PUT is a complete replacement snapshot.
	time.Sleep(300 * time.Millisecond)

	const entityCount = 80
	entities := make([]model.MobileEntity, 0, entityCount)
	for index := 0; index < entityCount; index++ {
		entityType := "generic"
		if index%17 == 0 {
			entityType = "cleaning_robot"
		}
		entities = append(entities, model.MobileEntity{
			ID: fmt.Sprintf("entity-%03d", index), Name: fmt.Sprintf("Entity %03d", index), Type: entityType,
			Level: "level0", Room: rooms[index%len(rooms)].Name,
			Heading: float64(index % 360), TransitionMS: 50,
		})
	}
	if err := sendJSON(client, http.MethodPut, baseURL+"/api/entities", entities); err != nil {
		t.Fatal(err)
	}

	const effectCount = 24
	effectTypes := []string{"fire", "smoke", "gas", "sprinkler", "water", "warning"}
	effects := make([]model.VisualEffect, 0, effectCount)
	for index := 0; index < effectCount; index++ {
		effects = append(effects, model.VisualEffect{
			ID: fmt.Sprintf("effect-%03d", index), Type: effectTypes[index%len(effectTypes)],
			Level: "level0", Room: rooms[index%len(rooms)].Name,
			Radius: 2.5, Height: 5, Intensity: 0.6,
		})
	}
	if err := sendJSON(client, http.MethodPut, baseURL+"/api/effects", effects); err != nil {
		t.Fatal(err)
	}

	const doorCount = 24
	doors := make([]model.Door, 0, doorCount)
	for index := 0; index < doorCount; index++ {
		lockState := "unlocked"
		if index%7 == 0 {
			lockState = "locked"
		}
		doors = append(doors, model.Door{
			ID: fmt.Sprintf("door-%03d", index), Name: fmt.Sprintf("Door %03d", index), Kind: "door",
			Level: "level0", Room: rooms[index%len(rooms)].Name,
			State: "closed", LockState: lockState,
		})
	}
	if err := sendJSON(client, http.MethodPut, baseURL+"/api/doors", doors); err != nil {
		t.Fatal(err)
	}

	const appearanceCount = 12
	appearances := make([]model.RoomAppearance, 0, appearanceCount)
	for index := 0; index < appearanceCount; index++ {
		appearances = append(appearances, model.RoomAppearance{
			Level: "level0", Room: rooms[index].Name, Color: "#ffd166", Brightness: 0.6,
		})
	}
	if err := sendJSON(client, http.MethodPut, baseURL+"/api/room-appearance", appearances); err != nil {
		t.Fatal(err)
	}

	const alertCount = 8
	alerts := make([]model.DecisionAlert, 0, alertCount)
	for index := 0; index < alertCount; index++ {
		alerts = append(alerts, model.DecisionAlert{
			ID: fmt.Sprintf("alert-%02d", index), Severity: "warning", Title: fmt.Sprintf("Stress alert %02d", index),
			Level: "level0", Room: rooms[index].Name,
		})
	}
	if err := sendJSON(client, http.MethodPut, baseURL+"/api/alerts", alerts); err != nil {
		t.Fatal(err)
	}

	layerValues := make(map[string]float64, 20)
	for index := 0; index < 20; index++ {
		layerValues["level0/"+rooms[index].Name] = 18 + float64(index)
	}
	layers := []model.RoomLayer{{
		ID: "stress-temperature", Label: "Stress temperature", Unit: "°C", Source: "stress test",
		Minimum: 18, Maximum: 40, Opacity: 0.7,
		Palette: []string{"#2563eb", "#22c55e", "#facc15", "#dc2626"}, Values: layerValues,
	}}
	if err := sendJSON(client, http.MethodPut, baseURL+"/api/room-layers", layers); err != nil {
		t.Fatal(err)
	}

	stateExpression := `(() => {
		const diagnostics = window._buildsimDiagnostics || {};
		const counts = window.buildsim && window.buildsim.dev ? window.buildsim.dev.counts() : {};
		const layerButton = document.querySelector('#roomLayerButtons [data-layer-id="stress-temperature"]');
		if (layerButton && window.buildsim && window.buildsim.dev &&
			(!window.buildsim.dev.heatOn || window.buildsim.dev.heatMetric !== 'stress-temperature')) {
			layerButton.click();
		}
		return JSON.stringify({
			ready: !!(window.buildsim && window.buildsim.ready()),
			ws: diagnostics.websocketState || "",
			notifications: diagnostics.visualNotifications || 0,
			refreshes: diagnostics.visualRefreshes || 0,
			entities: (window.buildsim && window.buildsim.entities ? window.buildsim.entities().length : 0),
			effects: (window.buildsim && window.buildsim.effects ? window.buildsim.effects().length : 0),
			doors: (window.buildsim && window.buildsim.doors ? window.buildsim.doors().length : 0),
			alerts: (window.buildsim && window.buildsim.alerts ? window.buildsim.alerts().length : 0),
			renderedEntities: counts.entities || 0,
			renderedEffects: counts.effects || 0,
			renderedDoors: counts.doors || 0,
			renderedAlerts: counts.alerts || 0,
			layerButton: !!layerButton,
			heatOn: !!(window.buildsim && window.buildsim.dev && window.buildsim.dev.heatOn),
			heatMetric: window.buildsim && window.buildsim.dev ? window.buildsim.dev.heatMetric : ''
		});
	})()`
	type visualState struct {
		Ready                                                            bool   `json:"ready"`
		WS                                                               string `json:"ws"`
		LayerButton, HeatOn                                              bool
		HeatMetric                                                       string
		Notifications, Refreshes                                         int64
		Entities, Effects, Doors, Alerts                                 int
		RenderedEntities, RenderedEffects, RenderedDoors, RenderedAlerts int
	}
	var final visualState
	if err := pollCDP(devtools, 30*time.Second, stateExpression, &final, func() bool {
		return final.Ready && final.WS == "connected" &&
			final.Entities == entityCount && final.RenderedEntities == entityCount &&
			final.Effects == effectCount && final.RenderedEffects == effectCount &&
			final.Doors == doorCount && final.RenderedDoors == doorCount &&
			final.Alerts == alertCount && final.RenderedAlerts == alertCount &&
			final.LayerButton && final.HeatOn && final.HeatMetric == "stress-temperature"
	}); err != nil {
		t.Fatalf("modern viewer did not converge after visualization storm: %v; state=%+v", err, final)
	}
	if final.Notifications == 0 {
		t.Fatal("viewer did not receive visualization notifications")
	}
	if final.Refreshes >= updates/5 {
		t.Fatalf("visualization coalescing ineffective: %d refreshes for %d entity updates", final.Refreshes, updates)
	}
	if got := entityGETs.Load(); got >= updates/5 {
		t.Fatalf("visualization coalescing ineffective: %d GET /api/entities requests for %d updates", got, updates)
	}
	t.Logf("modern viewer rendered %d entities, %d effects, %d doors, and %d alerts after %d notifications (%d refreshes)",
		final.RenderedEntities, final.RenderedEffects, final.RenderedDoors, final.RenderedAlerts, final.Notifications, final.Refreshes)
}

func findChromium(t *testing.T) string {
	t.Helper()
	candidates := []string{}
	if configured := strings.TrimSpace(os.Getenv("BUILDSIM_CHROMIUM")); configured != "" {
		candidates = append(candidates, configured)
	}
	candidates = append(candidates, "chromium", "chromium-browser", "google-chrome", "google-chrome-stable")
	for _, candidate := range candidates {
		if path, err := exec.LookPath(candidate); err == nil {
			return path
		}
	}
	t.Fatalf("Chromium not found; set BUILDSIM_CHROMIUM to its executable")
	return ""
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	return port
}

func sendJSON(client *http.Client, method, target string, payload interface{}) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	request, err := http.NewRequest(method, target, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("%s %s returned %s: %s", method, target, response.Status, strings.TrimSpace(string(responseBody)))
	}
	return nil
}

type devtoolsTarget struct {
	Type                 string `json:"type"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

func waitForPageSocket(port int, pageURL string, timeout time.Duration) (string, error) {
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		response, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/json/list", port))
		if err == nil {
			var targets []devtoolsTarget
			decodeErr := json.NewDecoder(response.Body).Decode(&targets)
			_ = response.Body.Close()
			if decodeErr == nil {
				for _, target := range targets {
					if target.Type == "page" && strings.HasPrefix(target.URL, pageURL) && target.WebSocketDebuggerURL != "" {
						return target.WebSocketDebuggerURL, nil
					}
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return "", fmt.Errorf("timed out waiting for Chromium DevTools page target")
}

type cdpConnection struct {
	socket *websocket.Conn
	nextID int64
}

func (connection *cdpConnection) evaluateJSON(expression string, destination interface{}) error {
	connection.nextID++
	id := connection.nextID
	if err := connection.socket.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	if err := connection.socket.WriteJSON(map[string]interface{}{
		"id": id, "method": "Runtime.evaluate",
		"params": map[string]interface{}{
			"expression": expression, "returnByValue": true, "awaitPromise": true,
		},
	}); err != nil {
		return err
	}
	if err := connection.socket.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	for {
		var message struct {
			ID    int64 `json:"id"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
			Result struct {
				Result struct {
					Value json.RawMessage `json:"value"`
				} `json:"result"`
				Exception json.RawMessage `json:"exceptionDetails"`
			} `json:"result"`
		}
		if err := connection.socket.ReadJSON(&message); err != nil {
			return err
		}
		if message.ID != id {
			continue
		}
		if message.Error != nil {
			return fmt.Errorf("DevTools evaluate: %s", message.Error.Message)
		}
		if len(message.Result.Exception) > 0 && string(message.Result.Exception) != "null" {
			return fmt.Errorf("DevTools JavaScript exception: %s", message.Result.Exception)
		}
		var encoded string
		if err := json.Unmarshal(message.Result.Result.Value, &encoded); err != nil {
			return fmt.Errorf("decode DevTools result %s: %w", message.Result.Result.Value, err)
		}
		if err := json.Unmarshal([]byte(encoded), destination); err != nil {
			return fmt.Errorf("decode evaluated JSON %q: %w", encoded, err)
		}
		return nil
	}
}

func pollCDP(connection *cdpConnection, timeout time.Duration, expression string, destination interface{}, done func() bool) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		if err := connection.evaluateJSON(expression, destination); err != nil {
			lastErr = err
		} else if done() {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	if lastErr != nil {
		return fmt.Errorf("timed out (last evaluation error: %w)", lastErr)
	}
	return fmt.Errorf("timed out waiting for condition")
}

func readTestLog(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return err.Error()
	}
	const limit = 16 << 10
	if len(data) > limit {
		data = data[len(data)-limit:]
	}
	return string(data)
}
