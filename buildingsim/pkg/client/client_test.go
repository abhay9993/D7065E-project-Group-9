package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/eislab-cps/buildingsim/pkg/model"
)

func TestBuilding(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/building" || r.Header.Get("Accept") != "application/json" {
			t.Errorf("unexpected request: %s", r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"Lab","levels":[{"id":"level0","label":"Floor 0"}]}`))
	}))
	defer server.Close()

	building, err := New(server.URL).Building(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if building.Name != "Lab" || len(building.Levels) != 1 {
		t.Fatalf("unexpected building: %+v", building)
	}
}

func TestEquipmentEncodesFilters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("room") != "R 1" || r.URL.Query().Get("type") != "sensor" {
			t.Errorf("unexpected query: %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	got, err := New(server.URL).Equipment(context.Background(), url.Values{"room": {"R 1"}, "type": {"sensor"}})
	if err != nil || len(got) != 0 {
		t.Fatalf("equipment: %v, %v", got, err)
	}
}

func TestCreateEquipmentSendsJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected request metadata")
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"eq-1","level":"level0","room":"R001","sensors":[],"actuators":[]}`))
	}))
	defer server.Close()

	created, err := New(server.URL).CreateEquipment(context.Background(), model.Equipment{ID: "eq-1"})
	if err != nil || created.ID != "eq-1" {
		t.Fatalf("created=%+v err=%v", created, err)
	}
}

func TestControlLoopAndOverlayMethods(t *testing.T) {
	tests := []struct {
		name      string
		method    string
		path      string
		response  string
		call      func(*Client) error
		wantsBody bool
	}{
		{"equipment", http.MethodGet, "/api/equipment/eq", `{"id":"eq"}`, func(c *Client) error { _, err := c.EquipmentByID(context.Background(), "eq"); return err }, false},
		{"bulk", http.MethodPost, "/api/equipment/bulk", `{"created":1,"skipped":0,"total":1,"version":1}`, func(c *Client) error {
			_, err := c.BulkCreateEquipment(context.Background(), []model.Equipment{{ID: "eq"}})
			return err
		}, true},
		{"sensor", http.MethodGet, "/api/sensors/sensor", `{"id":"sensor","data_type":"text"}`, func(c *Client) error { _, err := c.Sensor(context.Background(), "sensor"); return err }, false},
		{"set sensor", http.MethodPut, "/api/sensors/sensor/value", `{}`, func(c *Client) error {
			return c.SetSensorValue(context.Background(), "sensor", model.SensorValue{DataType: "text", Value: "21"})
		}, true},
		{"actuator", http.MethodGet, "/api/actuators/actuator", `{"id":"actuator","state":"off"}`, func(c *Client) error { _, err := c.Actuator(context.Background(), "actuator"); return err }, false},
		{"set actuator", http.MethodPut, "/api/actuators/actuator/state", `{}`, func(c *Client) error {
			return c.SetActuatorState(context.Background(), "actuator", model.ActuatorState{State: "on"})
		}, true},
		{"occupancy", http.MethodGet, "/api/occupancy", `{}`, func(c *Client) error { _, err := c.Occupancy(context.Background()); return err }, false},
		{"set occupancy", http.MethodPut, "/api/occupancy", `{}`, func(c *Client) error {
			return c.SetGlobalOccupancy(context.Background(), map[string]model.RoomOccupancy{})
		}, true},
		{"coverage", http.MethodGet, "/api/coverage", `[]`, func(c *Client) error { _, err := c.Coverage(context.Background()); return err }, false},
		{"set coverage", http.MethodPut, "/api/coverage", `{}`, func(c *Client) error { return c.SetGlobalCoverage(context.Background(), []model.CoverageZone{}) }, true},
		{"set session coverage", http.MethodPut, "/api/sessions/session/coverage", `{}`, func(c *Client) error { return c.SetCoverage(context.Background(), "session", []model.CoverageZone{}) }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != tt.method || r.URL.Path != tt.path {
					t.Errorf("request=%s %s, want %s %s", r.Method, r.URL.Path, tt.method, tt.path)
				}
				if tt.wantsBody && r.Header.Get("Content-Type") != "application/json" {
					t.Error("JSON request is missing its content type")
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()
			if err := tt.call(New(server.URL)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "missing", http.StatusNotFound)
	}))
	defer server.Close()

	_, err := New(server.URL).Floor(context.Background(), "missing")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusNotFound || !strings.Contains(apiErr.Body, "missing") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := New("http://127.0.0.1:1").Building(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}

func TestMissingBaseURL(t *testing.T) {
	_, err := New("").Building(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
}
