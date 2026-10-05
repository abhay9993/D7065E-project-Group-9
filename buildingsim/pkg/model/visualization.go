package model

import "time"

// RoomLayer is a scalar field published by a simulator or analytics service.
// Values are keyed by canonical room key (for example "level0/A109"). A layer
// may represent simulated physical state, a sensor-derived estimate, or risk;
// it is presentation state and is not treated as a sensor observation.
type RoomLayer struct {
	ID      string             `json:"id"`
	Label   string             `json:"label"`
	Unit    string             `json:"unit,omitempty"`
	Source  string             `json:"source,omitempty"`
	Minimum float64            `json:"minimum"`
	Maximum float64            `json:"maximum"`
	Opacity float64            `json:"opacity"`
	Palette []string           `json:"palette"`
	Values  map[string]float64 `json:"values"`
}

// VisualEffect describes a renderer primitive. BuildSim displays the effect;
// the student simulator remains responsible for calculating its evolution.
type VisualEffect struct {
	ID        string      `json:"id"`
	Type      string      `json:"type"` // fire, smoke, gas, sprinkler, water, warning
	Label     string      `json:"label,omitempty"`
	Level     string      `json:"level"`
	Room      string      `json:"room,omitempty"`
	Position  *[2]float64 `json:"position,omitempty"`
	Radius    float64     `json:"radius"`
	Height    float64     `json:"height,omitempty"`
	Intensity float64     `json:"intensity"`
	Color     string      `json:"color,omitempty"`
}

// MobileEntity is a person or robot whose position can be updated without
// recreating equipment. The browser interpolates between successive updates.
type MobileEntity struct {
	ID           string      `json:"id"`
	Name         string      `json:"name"`
	Type         string      `json:"type"` // man, woman, group, robot, cleaning_robot, generic
	Level        string      `json:"level"`
	Room         string      `json:"room,omitempty"`
	Position     *[2]float64 `json:"position,omitempty"`
	Heading      float64     `json:"heading,omitempty"`
	Status       string      `json:"status,omitempty"`
	TransitionMS int         `json:"transition_ms,omitempty"`
	Timestamp    time.Time   `json:"timestamp"`
}

// RoomAppearance controls visible room illumination independently from a
// numeric room layer. Brightness is in the range 0..1.
type RoomAppearance struct {
	Level      string  `json:"level"`
	Room       string  `json:"room"`
	Color      string  `json:"color"`
	Brightness float64 `json:"brightness"`
}

// DecisionAlert is a short, current operational message shown in the viewer.
// It is intentionally not a historical log; applications should persist their
// evidence in their own data pipeline.
type DecisionAlert struct {
	ID        string    `json:"id"`
	Severity  string    `json:"severity"` // info, warning, critical
	Title     string    `json:"title"`
	Message   string    `json:"message,omitempty"`
	Level     string    `json:"level,omitempty"`
	Room      string    `json:"room,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// Door represents a physical doorway, exterior entrance, or fire exit. Node
// IDs refer to the walkable graph on Level (not the merged multi-floor graph).
// Blocked is derived from State and LockState during validation.
type Door struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Kind        string      `json:"kind"` // door, entrance, fire_exit
	Level       string      `json:"level"`
	Room        string      `json:"room,omitempty"`
	EntryNodeID *int        `json:"entry_node_id,omitempty"`
	Position    *[2]float64 `json:"position,omitempty"`
	State       string      `json:"state"`      // open, closed, blocked
	LockState   string      `json:"lock_state"` // unlocked, locked, jammed
	Blocked     bool        `json:"blocked"`
	Timestamp   time.Time   `json:"timestamp"`
}
