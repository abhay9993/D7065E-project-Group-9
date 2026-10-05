package model

import "time"

type Equipment struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Type      string      `json:"type"`
	Category  string      `json:"category"`
	Level     string      `json:"level"`
	Room      string      `json:"room"` // room label/name from the map (e.g. "1542")
	Position  *[2]float64 `json:"position,omitempty"`
	Height    float64     `json:"height,omitempty"`
	Heading   float64     `json:"heading,omitempty"`
	Status    string      `json:"status"`
	Version   int64       `json:"version"`
	Sensors   []Sensor    `json:"sensors"`
	Actuators []Actuator  `json:"actuators"`
}

type Sensor struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Type        string    `json:"type"`
	Value       string    `json:"value"`
	BinaryValue bool      `json:"binary_value"`
	DataType    string    `json:"data_type"` // "binary" or "text"
	Unit        string    `json:"unit"`
	Timestamp   time.Time `json:"timestamp"`
}

type Actuator struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	State     string    `json:"state"`
	Timestamp time.Time `json:"timestamp"`
}

type SensorValue struct {
	Value       string `json:"value,omitempty"`
	BinaryValue *bool  `json:"binary_value,omitempty"`
	DataType    string `json:"data_type"` // "binary" or "text"
}

type ActuatorState struct {
	State string `json:"state"`
}
