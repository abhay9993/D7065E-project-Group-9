package model

type Person struct {
	ID       string      `json:"id"`
	Name     string      `json:"name"`
	Icon     string      `json:"icon,omitempty"` // "man", "woman", or empty (defaults to "man")
	Position *[2]float64 `json:"position,omitempty"`
}

type Alien struct {
	ID string `json:"id"`
}
