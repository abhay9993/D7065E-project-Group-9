package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eislab-cps/buildingsim/pkg/model"
	buildsocket "github.com/eislab-cps/buildingsim/pkg/server/websocket"
	"github.com/gorilla/websocket"
)

func TestWebSocketReceivesInitialSnapshotAndSessionUpdates(t *testing.T) {
	router, _ := setupTestRouter()
	var session model.Session
	parseJSON(doRequest(router, http.MethodPost, "/api/sessions", nil), &session)
	doRequest(router, http.MethodPut, "/api/sessions/"+session.ID+"/viewport", model.Viewport{
		Mode: "2d", Floor: "level0", Room: "R002", Zoom: 2,
	})

	server := httptest.NewServer(router)
	defer server.Close()
	header := http.Header{"Origin": {server.URL}}
	connection, response, err := websocket.DefaultDialer.Dial(
		"ws"+strings.TrimPrefix(server.URL, "http")+"/ws/"+session.ID,
		header,
	)
	if err != nil {
		status := 0
		if response != nil {
			status = response.StatusCode
		}
		t.Fatalf("dial websocket (HTTP %d): %v", status, err)
	}
	defer connection.Close()
	_ = connection.SetReadDeadline(time.Now().Add(3 * time.Second))

	for _, wantType := range []string{"viewport", "highlights", "occupancy", "coverage"} {
		var message struct {
			Type string          `json:"type"`
			Data json.RawMessage `json:"data"`
		}
		if err := connection.ReadJSON(&message); err != nil {
			t.Fatalf("read initial %s: %v", wantType, err)
		}
		if message.Type != wantType {
			t.Fatalf("initial message type=%q, want %q", message.Type, wantType)
		}
		if wantType == "viewport" {
			var viewport model.Viewport
			if err := json.Unmarshal(message.Data, &viewport); err != nil || viewport.Room != "R002" || viewport.Floor != "level0" {
				t.Fatalf("unexpected initial viewport: %+v, %v", viewport, err)
			}
		}
	}

	occupancy := map[string]model.RoomOccupancy{
		"level0/R002": {Persons: []model.Person{{ID: "person", Name: "Student"}}},
	}
	responseRecorder := doRequest(router, http.MethodPut, "/api/sessions/"+session.ID+"/occupancy", occupancy)
	if responseRecorder.Code != http.StatusOK {
		t.Fatal(responseRecorder.Body.String())
	}
	var update struct {
		Type    string                         `json:"type"`
		Data    map[string]model.RoomOccupancy `json:"data"`
		Version int64                          `json:"version"`
	}
	if err := connection.ReadJSON(&update); err != nil {
		t.Fatal(err)
	}
	if update.Type != "occupancy" || update.Version != 2 || update.Data["level0/R002"].Persons[0].Name != "Student" {
		t.Fatalf("websocket did not carry occupancy data: %+v", update)
	}

	// Global overlays are version-only messages. This lets viewers keep their
	// per-session layer separate and fetch the canonical global snapshot.
	if response := doRequest(router, http.MethodPut, "/api/occupancy", occupancy); response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	assertVersionOnlyMessage(t, connection, "occupancy")

	globalCoverage := []model.CoverageZone{{ID: "global", Radius: 10, Opacity: 0.2}}
	if response := doRequest(router, http.MethodPut, "/api/coverage", globalCoverage); response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	assertVersionOnlyMessage(t, connection, "coverage")

	sessionCoverage := []model.CoverageZone{{ID: "session", Level: "level0", Room: "R003", Radius: 5, Opacity: 0.3}}
	if response := doRequest(router, http.MethodPut, "/api/sessions/"+session.ID+"/coverage", sessionCoverage); response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	var coverageUpdate struct {
		Type    string               `json:"type"`
		Data    []model.CoverageZone `json:"data"`
		Version int64                `json:"version"`
	}
	if err := connection.ReadJSON(&coverageUpdate); err != nil {
		t.Fatal(err)
	}
	if coverageUpdate.Type != "coverage" || coverageUpdate.Version != 3 || len(coverageUpdate.Data) != 1 || coverageUpdate.Data[0].ID != "session" {
		t.Fatalf("websocket did not carry session coverage: %+v", coverageUpdate)
	}

	equipmentResponse := doRequest(router, http.MethodPost, "/api/equipment", model.Equipment{
		ID: "ws-version", Level: "level0", Room: "R001",
	})
	if equipmentResponse.Code != http.StatusCreated {
		t.Fatal(equipmentResponse.Body.String())
	}
	var equipmentUpdate struct {
		Type    string `json:"type"`
		Version int64  `json:"version"`
	}
	if err := connection.ReadJSON(&equipmentUpdate); err != nil {
		t.Fatal(err)
	}
	if equipmentUpdate.Type != "equipment" || equipmentUpdate.Version != 1 ||
		equipmentResponse.Header().Get("X-BuildSim-Version") != "1" {
		t.Fatalf("equipment notification=%+v header=%q", equipmentUpdate, equipmentResponse.Header().Get("X-BuildSim-Version"))
	}
}

func assertVersionOnlyMessage(t *testing.T, connection *websocket.Conn, wantType string) {
	t.Helper()
	var message struct {
		Type    string          `json:"type"`
		Data    json.RawMessage `json:"data"`
		Version int64           `json:"version"`
	}
	if err := connection.ReadJSON(&message); err != nil {
		t.Fatal(err)
	}
	if message.Type != wantType || len(message.Data) != 0 || message.Version < 1 {
		t.Fatalf("message=%+v, want version-only %s notification", message, wantType)
	}
}

func TestWebSocketRejectsUnrelatedBrowserOrigin(t *testing.T) {
	router, _ := setupTestRouter()
	var session model.Session
	parseJSON(doRequest(router, http.MethodPost, "/api/sessions", nil), &session)
	server := httptest.NewServer(router)
	defer server.Close()

	header := http.Header{"Origin": {"https://example.invalid"}}
	connection, response, err := websocket.DefaultDialer.Dial(
		"ws"+strings.TrimPrefix(server.URL, "http")+"/ws/"+session.ID,
		header,
	)
	if connection != nil {
		_ = connection.Close()
	}
	if err == nil || response == nil || response.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 handshake rejection, response=%v err=%v", response, err)
	}
}

func TestAllowedOrigins(t *testing.T) {
	tests := []struct {
		name   string
		host   string
		origin string
		want   bool
	}{
		{"non-browser", "127.0.0.1:9090", "", true},
		{"same origin", "127.0.0.1:9090", "http://127.0.0.1:9090", true},
		{"loopback aliases", "127.0.0.1:9090", "http://localhost:3000", true},
		{"IPv6 loopback", "[::1]:9090", "http://[::1]:3000", true},
		{"remote origin", "127.0.0.1:9090", "https://example.com", false},
		{"remote host", "lab.example:9090", "http://localhost:9090", false},
		{"bad scheme", "127.0.0.1:9090", "file:///tmp/viewer", false},
		{"malformed", "127.0.0.1:9090", "://", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "http://"+tt.host+"/ws/id", nil)
			request.Host = tt.host
			if tt.origin != "" {
				request.Header.Set("Origin", tt.origin)
			}
			if got := buildsocket.IsAllowedOrigin(request); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}
