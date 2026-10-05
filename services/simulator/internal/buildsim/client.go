// Package buildsim is a minimal HTTP client for the BuildSim API used by the simulator.
package buildsim

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

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

// SeedEquipment registers the room's HVAC equipment. Bulk creation is idempotent by ID.
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
			{ID: room + "-humidity", Name: "Humidity", Type: "humidity", DataType: "text", Unit: "%", Value: "45.0"},
		},
		Actuators: []Actuator{
			{ID: room + "-setpoint", Name: "Setpoint", Type: "setpoint", State: "21"},
			{ID: room + "-fan", Name: "Fan speed", Type: "fan_speed", State: "0"},
		},
	}}
	return c.do(ctx, http.MethodPost, "/api/equipment/bulk", payload, nil)
}

func (c *Client) SetSensorValue(ctx context.Context, id string, value float64) error {
	body := sensorValue{Value: fmt.Sprintf("%.2f", value), DataType: "text"}
	return c.do(ctx, http.MethodPut, "/api/sensors/"+url.PathEscape(id)+"/value", body, nil)
}

func (c *Client) ActuatorState(ctx context.Context, id string) (string, error) {
	var a actuatorState
	if err := c.do(ctx, http.MethodGet, "/api/actuators/"+url.PathEscape(id), nil, &a); err != nil {
		return "", err
	}
	return a.State, nil
}

// OccupancyCount returns how many persons BuildSim currently reports for "<level>/<room>".
func (c *Client) OccupancyCount(ctx context.Context, level, room string) (int, error) {
	var occ map[string]roomOccupancy
	if err := c.do(ctx, http.MethodGet, "/api/occupancy", nil, &occ); err != nil {
		return 0, err
	}
	key := level + "/" + room
	return len(occ[key].Persons), nil
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
