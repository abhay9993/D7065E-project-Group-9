// Package buildsim is a small REST client for the parts of BuildSim the
// occupancy simulator uses: reading a floor plan and publishing entities and
// occupancy snapshots.
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

	"github.com/eislab-cps/D7065E/occupancysim/internal/sim"
)

type Client struct {
	BaseURL string
	HTTP    *http.Client
}

func New(baseURL string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), HTTP: &http.Client{Timeout: 5 * time.Second}}
}

// buildingJSON mirrors GET /api/building.
type buildingJSON struct {
	Name   string `json:"name"`
	Levels []struct {
		ID    string `json:"id"`
		Label string `json:"label"`
	} `json:"levels"`
}

// Levels lists the ids of every floor of the building, ground floor first.
func (c *Client) Levels(ctx context.Context) ([]string, error) {
	body, err := c.get(ctx, "/api/building")
	if err != nil {
		return nil, err
	}
	var doc buildingJSON
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("decode building: %w", err)
	}
	levels := make([]string, 0, len(doc.Levels))
	for _, level := range doc.Levels {
		if level.ID != "" {
			levels = append(levels, level.ID)
		}
	}
	if len(levels) == 0 {
		return nil, fmt.Errorf("BuildSim reports no levels")
	}
	return levels, nil
}

// floorJSON mirrors GET /api/building/floors/{level}.
type floorJSON struct {
	Page struct {
		Width  float64 `json:"width"`
		Height float64 `json:"height"`
	} `json:"page"`
	Rooms []struct {
		Name   string     `json:"name"`
		Area   float64    `json:"area"`
		Center [2]float64 `json:"center"`
		Type   string     `json:"type"`
	} `json:"rooms"`
	WalkableGraph *struct {
		Nodes []struct {
			ID   int     `json:"id"`
			Name string  `json:"name"`
			X    float64 `json:"x"`
			Y    float64 `json:"y"`
			Type string  `json:"type"`
		} `json:"nodes"`
		Edges []struct {
			From   int     `json:"from"`
			To     int     `json:"to"`
			Weight float64 `json:"weight"`
		} `json:"edges"`
	} `json:"walkable_graph"`
}

// ParseFloor converts a BuildSim floor document into the simulator's plan.
func ParseFloor(data []byte, level string) (sim.FloorPlan, error) {
	var doc floorJSON
	if err := json.Unmarshal(data, &doc); err != nil {
		return sim.FloorPlan{}, fmt.Errorf("decode floor %s: %w", level, err)
	}
	if doc.WalkableGraph == nil {
		return sim.FloorPlan{}, fmt.Errorf("floor %s has no walkable graph", level)
	}
	plan := sim.FloorPlan{Level: level, Page: [2]float64{doc.Page.Width, doc.Page.Height}}
	for _, room := range doc.Rooms {
		plan.Rooms = append(plan.Rooms, sim.PlanRoom{Name: room.Name, Type: room.Type, Area: room.Area, Center: room.Center})
	}
	for _, node := range doc.WalkableGraph.Nodes {
		plan.Nodes = append(plan.Nodes, sim.Node{ID: node.ID, Name: node.Name, X: node.X, Y: node.Y, Type: node.Type})
	}
	for _, edge := range doc.WalkableGraph.Edges {
		plan.Edges = append(plan.Edges, sim.Edge{From: edge.From, To: edge.To, Weight: edge.Weight})
	}
	return plan, nil
}

// Floor downloads and parses one floor.
func (c *Client) Floor(ctx context.Context, level string) (sim.FloorPlan, error) {
	body, err := c.get(ctx, "/api/building/floors/"+url.PathEscape(level))
	if err != nil {
		return sim.FloorPlan{}, fmt.Errorf("floor %s: %w", level, err)
	}
	return ParseFloor(body, level)
}

func (c *Client) get(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reach BuildSim at %s: %w", c.BaseURL, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: BuildSim returned %s: %s", path, resp.Status, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func (c *Client) PutEntities(ctx context.Context, entities []sim.Entity) error {
	if entities == nil {
		entities = []sim.Entity{}
	}
	return c.put(ctx, "/api/entities", entities)
}

func (c *Client) PutOccupancy(ctx context.Context, occupancy map[string]sim.RoomOccupancy) error {
	if occupancy == nil {
		occupancy = map[string]sim.RoomOccupancy{}
	}
	return c.put(ctx, "/api/occupancy", occupancy)
}

// PutOccupancyEncoded sends an already encoded occupancy document, so a caller
// that has to encode it anyway to see whether it changed does not encode twice.
func (c *Client) PutOccupancyEncoded(ctx context.Context, encoded []byte) error {
	return c.send(ctx, "/api/occupancy", encoded)
}

func (c *Client) put(ctx context.Context, path string, body any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return c.send(ctx, path, encoded)
}

func (c *Client) send(ctx context.Context, path string, encoded []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.BaseURL+path, bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("PUT %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return fmt.Errorf("PUT %s: BuildSim returned %s: %s", path, resp.Status, strings.TrimSpace(string(data)))
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}
