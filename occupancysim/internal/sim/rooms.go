package sim

import (
	"fmt"
	"math"
	"sort"
)

// FloorPlan is the subset of a BuildSim floor description the simulator needs.
// Coordinates are floor-plan units as returned by GET /api/building/floors/{level}.
type FloorPlan struct {
	Level string
	Page  [2]float64
	Rooms []PlanRoom
	Nodes []Node
	Edges []Edge
}

type PlanRoom struct {
	Name   string
	Type   string // "room" or "corridor"
	Area   float64
	Center [2]float64
}

type RoomRole string

const (
	RoleOffice   RoomRole = "office"
	RoleLecture  RoomRole = "lecture"
	RoleFika     RoomRole = "fika"
	RoleCorridor RoomRole = "corridor"
	RoleUnused   RoomRole = "unused"
)

// Room is a classified floor-plan room with its walkable-graph node.
type Room struct {
	Name     string     `json:"name"`
	Level    string     `json:"level"`
	Role     RoomRole   `json:"role"`
	AreaM2   float64    `json:"area_m2"`
	Center   [2]float64 `json:"center"`
	Node     int        `json:"-"` // index into Graph.Nodes, -1 when unreachable
	Capacity int        `json:"capacity"`
}

// ClassifyRooms assigns a role to every room on the floor. The rule set is
// deliberately simple and area-based, because BuildSim only distinguishes rooms
// from corridors: small rooms are offices, large rooms are lecture rooms, and a
// configurable number of the rooms above the fika threshold become fika rooms.
// Explicit overrides in Parameters.RoomRoles win over the rules.
func ClassifyRooms(plan FloorPlan, g *Graph, p Parameters) ([]Room, error) {
	scale := p.MetresPerUnit * p.MetresPerUnit
	rooms := make([]Room, 0, len(plan.Rooms))
	seen := map[string]bool{}
	for _, pr := range plan.Rooms {
		if seen[pr.Name] {
			continue // corridors can repeat a name; one entry per name is enough
		}
		seen[pr.Name] = true
		room := Room{Name: pr.Name, Level: plan.Level, AreaM2: pr.Area * scale, Center: pr.Center, Node: -1}
		if pr.Type == "corridor" {
			room.Role = RoleCorridor
			room.Node = g.NearestNamed(pr.Name, pr.Center)
			rooms = append(rooms, room)
			continue
		}
		room.Node = g.RoomNode(pr.Name)
		switch {
		case room.Node < 0 || room.AreaM2 < p.MinRoomM2:
			room.Role = RoleUnused
		case room.AreaM2 <= p.OfficeMaxM2:
			room.Role = RoleOffice
		case room.AreaM2 >= p.LectureMinM2:
			room.Role = RoleLecture
		default:
			room.Role = RoleUnused // between office and lecture size: storage, labs
		}
		rooms = append(rooms, room)
	}

	// Fika rooms: spread the configured number across the floor, choosing among
	// rooms at or above the fika threshold, starting with the smallest such room.
	candidates := []int{}
	for i, room := range rooms {
		if room.Role == RoleLecture && room.AreaM2 >= p.FikaMinM2 {
			candidates = append(candidates, i)
		}
	}
	sort.Slice(candidates, func(a, b int) bool { return rooms[candidates[a]].AreaM2 < rooms[candidates[b]].AreaM2 })
	picked := []int{}
	for len(picked) < p.FikaRoomCount && len(candidates) > 0 {
		best, bestScore := -1, -1.0
		for ci, idx := range candidates {
			score := math.Inf(1)
			if len(picked) == 0 {
				score = -float64(ci) // the smallest candidate first
			}
			for _, pi := range picked {
				score = math.Min(score, dist(rooms[idx].Center, rooms[pi].Center))
			}
			if score > bestScore {
				best, bestScore = ci, score
			}
		}
		picked = append(picked, candidates[best])
		candidates = append(candidates[:best], candidates[best+1:]...)
	}
	for _, idx := range picked {
		rooms[idx].Role = RoleFika
	}

	// Overrides name a room of the building; a floor that does not have that
	// room ignores it. Simulation.configure rejects names no floor knows.
	for name, role := range p.RoomRoles {
		for i := range rooms {
			if rooms[i].Name != name {
				continue
			}
			if rooms[i].Role == RoleCorridor {
				return nil, fmt.Errorf("room_roles[%q]: corridors cannot be reassigned", name)
			}
			if rooms[i].Node < 0 && RoomRole(role) != RoleUnused {
				return nil, fmt.Errorf("room_roles[%q]: the room on %s is not on the walkable graph", name, plan.Level)
			}
			rooms[i].Role = RoomRole(role)
		}
	}

	for i := range rooms {
		rooms[i].Capacity = capacityFor(rooms[i])
	}
	sort.Slice(rooms, func(a, b int) bool { return rooms[a].Name < rooms[b].Name })
	return rooms, nil
}

// capacityFor is how many people a room holds: one person per office, and
// roughly 2 m² per seat in a lecture room and 4 m² per seat in a fika room.
func capacityFor(room Room) int {
	switch room.Role {
	case RoleOffice:
		return 1 // one person per office
	case RoleLecture:
		return max(4, int(math.Round(room.AreaM2/2)))
	case RoleFika:
		return max(4, int(math.Round(room.AreaM2/4)))
	}
	return 0
}

func dist(a, b [2]float64) float64 {
	return math.Hypot(a[0]-b[0], a[1]-b[1])
}
