// Package client provides a small standard-library Go client for BuildSim.
// It is intentionally simple enough for students to read, copy, and extend.
package client

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

	"github.com/eislab-cps/buildingsim/pkg/model"
)

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

type APIError struct {
	StatusCode int
	Body       string
}

type BulkCreateResult struct {
	Created int   `json:"created"`
	Skipped int   `json:"skipped"`
	Total   int   `json:"total"`
	Version int64 `json:"version"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("buildsim returned HTTP %d: %s", e.StatusCode, e.Body)
}

func New(baseURL string) *Client {
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *Client) Building(ctx context.Context) (model.Building, error) {
	var building model.Building
	err := c.do(ctx, http.MethodGet, "/api/building", nil, &building)
	return building, err
}

func (c *Client) Floor(ctx context.Context, level string) (model.FloorData, error) {
	var floor model.FloorData
	err := c.do(ctx, http.MethodGet, "/api/building/floors/"+url.PathEscape(level), nil, &floor)
	return floor, err
}

func (c *Client) Equipment(ctx context.Context, filters url.Values) ([]model.Equipment, error) {
	endpoint := "/api/equipment"
	if len(filters) > 0 {
		endpoint += "?" + filters.Encode()
	}
	var equipment []model.Equipment
	err := c.do(ctx, http.MethodGet, endpoint, nil, &equipment)
	return equipment, err
}

func (c *Client) CreateEquipment(ctx context.Context, equipment model.Equipment) (model.Equipment, error) {
	var created model.Equipment
	err := c.do(ctx, http.MethodPost, "/api/equipment", equipment, &created)
	return created, err
}

func (c *Client) EquipmentByID(ctx context.Context, id string) (model.Equipment, error) {
	var equipment model.Equipment
	err := c.do(ctx, http.MethodGet, "/api/equipment/"+url.PathEscape(id), nil, &equipment)
	return equipment, err
}

func (c *Client) BulkCreateEquipment(ctx context.Context, equipment []model.Equipment) (BulkCreateResult, error) {
	var result BulkCreateResult
	err := c.do(ctx, http.MethodPost, "/api/equipment/bulk", equipment, &result)
	return result, err
}

func (c *Client) Sensor(ctx context.Context, id string) (model.Sensor, error) {
	var sensor model.Sensor
	err := c.do(ctx, http.MethodGet, "/api/sensors/"+url.PathEscape(id), nil, &sensor)
	return sensor, err
}

func (c *Client) SetSensorValue(ctx context.Context, id string, value model.SensorValue) error {
	return c.do(ctx, http.MethodPut, "/api/sensors/"+url.PathEscape(id)+"/value", value, nil)
}

func (c *Client) Actuator(ctx context.Context, id string) (model.Actuator, error) {
	var actuator model.Actuator
	err := c.do(ctx, http.MethodGet, "/api/actuators/"+url.PathEscape(id), nil, &actuator)
	return actuator, err
}

func (c *Client) SetActuatorState(ctx context.Context, id string, state model.ActuatorState) error {
	return c.do(ctx, http.MethodPut, "/api/actuators/"+url.PathEscape(id)+"/state", state, nil)
}

func (c *Client) CreateSession(ctx context.Context) (model.Session, error) {
	var session model.Session
	err := c.do(ctx, http.MethodPost, "/api/sessions", nil, &session)
	return session, err
}

func (c *Client) Occupancy(ctx context.Context) (map[string]model.RoomOccupancy, error) {
	var occupancy map[string]model.RoomOccupancy
	err := c.do(ctx, http.MethodGet, "/api/occupancy", nil, &occupancy)
	return occupancy, err
}

func (c *Client) SetGlobalOccupancy(ctx context.Context, occupancy map[string]model.RoomOccupancy) error {
	return c.do(ctx, http.MethodPut, "/api/occupancy", occupancy, nil)
}

func (c *Client) Coverage(ctx context.Context) ([]model.CoverageZone, error) {
	var coverage []model.CoverageZone
	err := c.do(ctx, http.MethodGet, "/api/coverage", nil, &coverage)
	return coverage, err
}

func (c *Client) SetGlobalCoverage(ctx context.Context, coverage []model.CoverageZone) error {
	return c.do(ctx, http.MethodPut, "/api/coverage", coverage, nil)
}

func (c *Client) SetViewport(ctx context.Context, sessionID string, viewport model.Viewport) error {
	return c.do(ctx, http.MethodPut, sessionPath(sessionID, "viewport"), viewport, nil)
}

func (c *Client) SetHighlights(ctx context.Context, sessionID string, highlights []model.RoomHighlight) error {
	return c.do(ctx, http.MethodPut, sessionPath(sessionID, "highlights"), highlights, nil)
}

func (c *Client) SetOccupancy(ctx context.Context, sessionID string, occupancy map[string]model.RoomOccupancy) error {
	return c.do(ctx, http.MethodPut, sessionPath(sessionID, "occupancy"), occupancy, nil)
}

func (c *Client) SetCoverage(ctx context.Context, sessionID string, coverage []model.CoverageZone) error {
	return c.do(ctx, http.MethodPut, sessionPath(sessionID, "coverage"), coverage, nil)
}

func sessionPath(id, resource string) string {
	return "/api/sessions/" + url.PathEscape(id) + "/" + resource
}

func (c *Client) do(ctx context.Context, method, endpoint string, requestBody, responseBody any) error {
	if c == nil || strings.TrimSpace(c.BaseURL) == "" {
		return fmt.Errorf("base URL is required")
	}
	var body io.Reader
	if requestBody != nil {
		encoded, err := json.Marshal(requestBody)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+endpoint, body)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	if requestBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return &APIError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(data))}
	}
	if responseBody == nil || resp.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(responseBody); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
