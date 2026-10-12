// Package buildsim is a minimal HTTP client for the BuildSim API used by the simulator.
package buildsim

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ErrNotFound is returned when BuildSim answers 404, which for us almost always
// means BuildSim restarted and forgot the equipment we registered.
var ErrNotFound = errors.New("not found in buildsim")

// metresPerUnit converts floor-plan drawing units to metres (the same scale the
// occupancy simulator uses: a median level0 room is then ~18 m²).
const metresPerUnit = 0.5

type Client struct {
	BaseURL string
	HTTP    *http.Client
}

func New(baseURL string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTP:    &http.Client{Timeout: 10 * time.Second},
	}
}

type Sensor struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	DataType string `json:"data_type"`
	Unit     string `json:"unit"`
	Value    string `json:"value"`
}

type Actuator struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Type  string `json:"type"`
	State string `json:"state"`
}

type Equipment struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Type      string     `json:"type"`
	Category  string     `json:"category"`
	Level     string     `json:"level"`
	Room      string     `json:"room"`
	Status    string     `json:"status"`
	Sensors   []Sensor   `json:"sensors"`
	Actuators []Actuator `json:"actuators"`
}

type sensorValue struct {
	Value    string `json:"value"`
	DataType string `json:"data_type"`
}

type actuatorState struct {
	State string `json:"state"`
}

type person struct {
	ID string `json:"id"`
}

type roomOccupancy struct {
	Persons []person `json:"persons"`
}

type floorPlan struct {
	Rooms []struct {
		Name string  `json:"name"`
		Area float64 `json:"area"`
		Type string  `json:"type"`
	} `json:"rooms"`
}

// WaitReady polls /healthz until BuildSim answers or the context is done.
func (c *Client) WaitReady(ctx context.Context) error {
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/healthz", nil)
		if err == nil {
			if resp, err := c.HTTP.Do(req); err == nil {
				resp.Body.Close()
				if resp.StatusCode < 300 {
					return nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// SeedEquipment registers the room's HVAC equipment. Bulk creation skips IDs that
// already exist, so calling it again (for example after a BuildSim restart) is safe.
// The actuators start in the same safe state the controller uses when it is blind.
func (c *Client) SeedEquipment(ctx context.Context, level, room string) error {
	id := "hvac-" + room
	payload := []Equipment{{
		ID:       id,
		Name:     "HVAC " + room,
		Type:     "ac_unit",
		Category: "hvac",
		Level:    level,
		Room:     room,
		Status:   "running",
		Sensors: []Sensor{
			{ID: room + "-temp", Name: "Temperature", Type: "temperature", DataType: "text", Unit: "°C", Value: "21.0"},
			{ID: room + "-humidity", Name: "Humidity", Type: "humidity", DataType: "text", Unit: "%", Value: "35.0"},
			{ID: room + "-energy", Name: "Energy meter", Type: "energy", DataType: "text", Unit: "kWh", Value: "0"},
		},
		Actuators: []Actuator{
			{ID: room + "-setpoint", Name: "Setpoint", Type: "setpoint", State: "21"},
			{ID: room + "-fan", Name: "Fan speed", Type: "fan_speed", State: "50"},
		},
	}}
	return c.do(ctx, http.MethodPost, "/api/equipment/bulk", payload, nil)
}

func (c *Client) SetSensorValue(ctx context.Context, id string, value float64) error {
	body := sensorValue{Value: fmt.Sprintf("%.3f", value), DataType: "text"}
	return c.do(ctx, http.MethodPut, "/api/sensors/"+url.PathEscape(id)+"/value", body, nil)
}

// SensorValue reads back a value we wrote earlier, so a restarted simulator can
// continue from where the building was instead of resetting every room.
func (c *Client) SensorValue(ctx context.Context, id string) (float64, error) {
	var s sensorValue
	if err := c.do(ctx, http.MethodGet, "/api/sensors/"+url.PathEscape(id), nil, &s); err != nil {
		return 0, err
	}
	return strconv.ParseFloat(s.Value, 64)
}

func (c *Client) ActuatorState(ctx context.Context, id string) (string, error) {
	var a actuatorState
	if err := c.do(ctx, http.MethodGet, "/api/actuators/"+url.PathEscape(id), nil, &a); err != nil {
		return "", err
	}
	return a.State, nil
}

// Occupancy returns the person count for every "<level>/<room>" key. One request
// per tick for the whole building, instead of one per room.
func (c *Client) Occupancy(ctx context.Context) (map[string]int, error) {
	var occ map[string]roomOccupancy
	if err := c.do(ctx, http.MethodGet, "/api/occupancy", nil, &occ); err != nil {
		return nil, err
	}
	counts := make(map[string]int, len(occ))
	for key, r := range occ {
		counts[key] = len(r.Persons)
	}
	return counts, nil
}

// RoomAreas returns the floor area in m² of every room on the level, so each room
// gets a heat capacity and heat loss that match its size.
func (c *Client) RoomAreas(ctx context.Context, level string) (map[string]float64, error) {
	var plan floorPlan
	if err := c.do(ctx, http.MethodGet, "/api/building/floors/"+url.PathEscape(level), nil, &plan); err != nil {
		return nil, err
	}
	areas := make(map[string]float64, len(plan.Rooms))
	for _, r := range plan.Rooms {
		areas[r.Name] = r.Area * metresPerUnit * metresPerUnit
	}
	return areas, nil
}

func (c *Client) do(ctx context.Context, method, path string, reqBody, respBody any) error {
	var body io.Reader
	if reqBody != nil {
		encoded, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("buildsim %s %s: %w", method, path, ErrNotFound)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<10))
		return fmt.Errorf("buildsim %s %s: HTTP %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if respBody == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(respBody)
}
