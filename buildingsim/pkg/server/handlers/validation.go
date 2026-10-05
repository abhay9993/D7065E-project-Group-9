package handlers

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/eislab-cps/buildingsim/pkg/model"
	"github.com/eislab-cps/buildingsim/pkg/store"
)

const (
	maxRoomLayers     = 32
	maxVisualEffects  = 1000
	maxMobileEntities = 2000
	maxRoomAppearance = 2000
	maxDecisionAlerts = 100
	maxDoors          = 2000
)

var hexColorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func normalizeEquipmentLocation(memory *store.MemoryStore, equipment *model.Equipment) error {
	equipment.Level = strings.TrimSpace(equipment.Level)
	equipment.Room = strings.TrimSpace(equipment.Room)
	if equipment.Level == "" {
		return fmt.Errorf("level is required")
	}
	if equipment.Room == "" {
		return fmt.Errorf("room is required")
	}
	key, found, ambiguous := memory.ResolveRoomKey(equipment.Level + "/" + equipment.Room)
	if ambiguous {
		return fmt.Errorf("room %q is ambiguous; include a floor", equipment.Room)
	}
	if !found {
		return fmt.Errorf("room %q does not exist on level %q", equipment.Room, equipment.Level)
	}
	equipment.Level, equipment.Room = splitRoomKey(key)
	if equipment.Position != nil {
		if err := validateFloorPosition(memory, equipment.Level, *equipment.Position); err != nil {
			return fmt.Errorf("position: %w", err)
		}
	}
	if !finite(equipment.Height) || equipment.Height < 0 || equipment.Height > 100 {
		return fmt.Errorf("height must be between 0 and 100")
	}
	if !finite(equipment.Heading) {
		return fmt.Errorf("heading must be finite")
	}
	return normalizeEquipmentChildren(equipment)
}

func normalizeEquipmentChildren(equipment *model.Equipment) error {
	for i := range equipment.Sensors {
		sensor := &equipment.Sensors[i]
		if sensor.DataType == "" {
			sensor.DataType = "text"
		}
		if sensor.DataType != "text" && sensor.DataType != "binary" {
			return fmt.Errorf("sensor %q has invalid data_type %q", sensor.ID, sensor.DataType)
		}
	}
	return nil
}

func normalizeOccupancy(memory *store.MemoryStore, input map[string]model.RoomOccupancy) (map[string]model.RoomOccupancy, error) {
	result := make(map[string]model.RoomOccupancy, len(input))
	for requested, occupancy := range input {
		canonical, found, ambiguous := memory.ResolveRoomKey(requested)
		if ambiguous {
			return nil, fmt.Errorf("room key %q is ambiguous; use <level>/<room>", requested)
		}
		if !found {
			return nil, fmt.Errorf("room key %q does not identify a room", requested)
		}
		if _, duplicate := result[canonical]; duplicate {
			return nil, fmt.Errorf("more than one key resolves to %q", canonical)
		}
		if occupancy.Persons == nil {
			occupancy.Persons = []model.Person{}
		}
		if occupancy.Aliens == nil {
			occupancy.Aliens = []model.Alien{}
		}
		result[canonical] = occupancy
	}
	return result, nil
}

func normalizeHighlights(memory *store.MemoryStore, highlights []model.RoomHighlight) error {
	for i := range highlights {
		highlight := &highlights[i]
		var requested string
		if strings.TrimSpace(highlight.Room) != "" {
			requested = strings.TrimSpace(highlight.Room)
		} else {
			requested = fmt.Sprintf("%d", highlight.RoomID)
		}
		if strings.TrimSpace(highlight.Level) != "" {
			requested = strings.TrimSpace(highlight.Level) + "/" + requested
		}
		canonical, found, ambiguous := memory.ResolveRoomKey(requested)
		if ambiguous {
			return fmt.Errorf("highlight %d is ambiguous; include level", i)
		}
		if !found {
			return fmt.Errorf("highlight %d references an unknown room", i)
		}
		highlight.Level, highlight.Room = splitRoomKey(canonical)
		if highlight.Color == "" {
			highlight.Color = "#ffcc00"
		}
		if highlight.Opacity < 0 || highlight.Opacity > 1 {
			return fmt.Errorf("highlight %d opacity must be between 0 and 1", i)
		}
	}
	return nil
}

func normalizeViewport(memory *store.MemoryStore, viewport *model.Viewport) error {
	if viewport.Mode == "" {
		viewport.Mode = "3d"
	}
	if viewport.Mode != "2d" && viewport.Mode != "3d" {
		return fmt.Errorf("mode must be 2d or 3d")
	}
	if viewport.Zoom < 0 {
		return fmt.Errorf("zoom must not be negative")
	}
	if viewport.Room != "" {
		requested := viewport.Room
		if viewport.Floor != "" {
			requested = viewport.Floor + "/" + viewport.Room
		}
		canonical, found, ambiguous := memory.ResolveRoomKey(requested)
		if ambiguous {
			return fmt.Errorf("room %q is ambiguous; include floor", viewport.Room)
		}
		if !found {
			return fmt.Errorf("room %q does not exist", viewport.Room)
		}
		viewport.Floor, viewport.Room = splitRoomKey(canonical)
	} else if viewport.Floor != "" {
		if _, ok := memory.GetFloorData(viewport.Floor); !ok {
			return fmt.Errorf("floor %q does not exist", viewport.Floor)
		}
	}
	return nil
}

func normalizeCoverage(memory *store.MemoryStore, zones []model.CoverageZone) error {
	seen := make(map[string]bool, len(zones))
	for i := range zones {
		zone := &zones[i]
		if strings.TrimSpace(zone.ID) == "" {
			return fmt.Errorf("coverage zone %d requires an id", i)
		}
		if seen[zone.ID] {
			return fmt.Errorf("duplicate coverage zone id %q", zone.ID)
		}
		seen[zone.ID] = true
		if zone.Radius <= 0 {
			return fmt.Errorf("coverage zone %q radius must be positive", zone.ID)
		}
		if zone.Opacity < 0 || zone.Opacity > 1 {
			return fmt.Errorf("coverage zone %q opacity must be between 0 and 1", zone.ID)
		}
		if zone.Room == "" {
			if zone.Level != "" {
				if _, ok := memory.GetFloorData(zone.Level); !ok {
					return fmt.Errorf("coverage zone %q references unknown level %q", zone.ID, zone.Level)
				}
			}
			continue
		}
		requested := zone.Room
		if zone.Level != "" {
			requested = zone.Level + "/" + zone.Room
		}
		canonical, found, ambiguous := memory.ResolveRoomKey(requested)
		if ambiguous {
			return fmt.Errorf("coverage zone %q room is ambiguous; include level", zone.ID)
		}
		if !found {
			return fmt.Errorf("coverage zone %q references an unknown room", zone.ID)
		}
		zone.Level, zone.Room = splitRoomKey(canonical)
	}
	return nil
}

func normalizeRoomLayers(memory *store.MemoryStore, layers []model.RoomLayer) error {
	if len(layers) > maxRoomLayers {
		return fmt.Errorf("at most %d room layers are allowed", maxRoomLayers)
	}
	seen := make(map[string]bool, len(layers))
	for i := range layers {
		layer := &layers[i]
		layer.ID = strings.TrimSpace(layer.ID)
		layer.Label = strings.TrimSpace(layer.Label)
		if layer.ID == "" {
			return fmt.Errorf("room layer %d requires an id", i)
		}
		if seen[layer.ID] {
			return fmt.Errorf("duplicate room layer id %q", layer.ID)
		}
		seen[layer.ID] = true
		if layer.Label == "" {
			layer.Label = layer.ID
		}
		if !finite(layer.Minimum) || !finite(layer.Maximum) || layer.Maximum <= layer.Minimum {
			return fmt.Errorf("room layer %q maximum must be finite and greater than minimum", layer.ID)
		}
		if layer.Opacity == 0 {
			layer.Opacity = 0.72
		}
		if !finite(layer.Opacity) || layer.Opacity < 0 || layer.Opacity > 1 {
			return fmt.Errorf("room layer %q opacity must be between 0 and 1", layer.ID)
		}
		if len(layer.Palette) == 0 {
			layer.Palette = []string{"#2563eb", "#22c55e", "#facc15", "#ef4444"}
		}
		if len(layer.Palette) < 2 || len(layer.Palette) > 12 {
			return fmt.Errorf("room layer %q palette must contain 2 to 12 colors", layer.ID)
		}
		for _, color := range layer.Palette {
			if !validColor(color) {
				return fmt.Errorf("room layer %q has invalid color %q; use #RRGGBB", layer.ID, color)
			}
		}
		values := make(map[string]float64, len(layer.Values))
		for requested, value := range layer.Values {
			if !finite(value) {
				return fmt.Errorf("room layer %q value for %q must be finite", layer.ID, requested)
			}
			canonical, found, ambiguous := memory.ResolveRoomKey(requested)
			if ambiguous {
				return fmt.Errorf("room layer %q key %q is ambiguous; use <level>/<room>", layer.ID, requested)
			}
			if !found {
				return fmt.Errorf("room layer %q key %q does not identify a room", layer.ID, requested)
			}
			if _, duplicate := values[canonical]; duplicate {
				return fmt.Errorf("room layer %q contains duplicate room %q", layer.ID, canonical)
			}
			values[canonical] = value
		}
		layer.Values = values
	}
	return nil
}

func normalizeEffects(memory *store.MemoryStore, effects []model.VisualEffect) error {
	if len(effects) > maxVisualEffects {
		return fmt.Errorf("at most %d effects are allowed", maxVisualEffects)
	}
	seen := make(map[string]bool, len(effects))
	allowed := map[string]bool{"fire": true, "smoke": true, "gas": true, "sprinkler": true, "water": true, "warning": true}
	for i := range effects {
		effect := &effects[i]
		effect.ID = strings.TrimSpace(effect.ID)
		effect.Type = strings.ToLower(strings.TrimSpace(effect.Type))
		if effect.ID == "" || seen[effect.ID] {
			return requiredOrDuplicate("effect", i, effect.ID, seen[effect.ID])
		}
		seen[effect.ID] = true
		if !allowed[effect.Type] {
			return fmt.Errorf("effect %q has unsupported type %q", effect.ID, effect.Type)
		}
		if err := normalizeLocation(memory, &effect.Level, &effect.Room, effect.Position, true); err != nil {
			return fmt.Errorf("effect %q: %w", effect.ID, err)
		}
		if effect.Radius == 0 {
			effect.Radius = 8
		}
		if !finite(effect.Radius) || effect.Radius <= 0 || effect.Radius > 200 {
			return fmt.Errorf("effect %q radius must be between 0 and 200", effect.ID)
		}
		if !finite(effect.Height) || effect.Height < 0 || effect.Height > 200 {
			return fmt.Errorf("effect %q height must be between 0 and 200", effect.ID)
		}
		if effect.Intensity == 0 {
			effect.Intensity = 1
		}
		if !finite(effect.Intensity) || effect.Intensity < 0 || effect.Intensity > 1 {
			return fmt.Errorf("effect %q intensity must be between 0 and 1", effect.ID)
		}
		if effect.Color != "" && !validColor(effect.Color) {
			return fmt.Errorf("effect %q has invalid color %q; use #RRGGBB", effect.ID, effect.Color)
		}
	}
	return nil
}

func normalizeEntities(memory *store.MemoryStore, entities []model.MobileEntity) error {
	if len(entities) > maxMobileEntities {
		return fmt.Errorf("at most %d mobile entities are allowed", maxMobileEntities)
	}
	seen := make(map[string]bool, len(entities))
	allowed := map[string]bool{"man": true, "woman": true, "group": true, "robot": true, "cleaning_robot": true, "generic": true}
	now := time.Now()
	for i := range entities {
		entity := &entities[i]
		entity.ID = strings.TrimSpace(entity.ID)
		entity.Type = strings.ToLower(strings.TrimSpace(entity.Type))
		if entity.ID == "" || seen[entity.ID] {
			return requiredOrDuplicate("entity", i, entity.ID, seen[entity.ID])
		}
		seen[entity.ID] = true
		if !allowed[entity.Type] {
			return fmt.Errorf("entity %q has unsupported type %q", entity.ID, entity.Type)
		}
		if strings.TrimSpace(entity.Name) == "" {
			entity.Name = entity.ID
		}
		if err := normalizeLocation(memory, &entity.Level, &entity.Room, entity.Position, true); err != nil {
			return fmt.Errorf("entity %q: %w", entity.ID, err)
		}
		if !finite(entity.Heading) {
			return fmt.Errorf("entity %q heading must be finite", entity.ID)
		}
		if entity.TransitionMS == 0 {
			entity.TransitionMS = 700
		}
		if entity.TransitionMS < 0 || entity.TransitionMS > 60000 {
			return fmt.Errorf("entity %q transition_ms must be between 0 and 60000", entity.ID)
		}
		if entity.Timestamp.IsZero() {
			entity.Timestamp = now
		}
	}
	return nil
}

func normalizeRoomAppearance(memory *store.MemoryStore, appearance []model.RoomAppearance) error {
	if len(appearance) > maxRoomAppearance {
		return fmt.Errorf("at most %d room appearances are allowed", maxRoomAppearance)
	}
	seen := make(map[string]bool, len(appearance))
	for i := range appearance {
		item := &appearance[i]
		requested := strings.TrimSpace(item.Room)
		if strings.TrimSpace(item.Level) != "" {
			requested = strings.TrimSpace(item.Level) + "/" + requested
		}
		canonical, found, ambiguous := memory.ResolveRoomKey(requested)
		if ambiguous || !found {
			return fmt.Errorf("room appearance %d references an unknown or ambiguous room", i)
		}
		if seen[canonical] {
			return fmt.Errorf("duplicate room appearance for %q", canonical)
		}
		seen[canonical] = true
		item.Level, item.Room = splitRoomKey(canonical)
		if item.Color == "" {
			item.Color = "#ffd36a"
		}
		if !validColor(item.Color) {
			return fmt.Errorf("room appearance %q has invalid color %q; use #RRGGBB", canonical, item.Color)
		}
		if !finite(item.Brightness) || item.Brightness < 0 || item.Brightness > 1 {
			return fmt.Errorf("room appearance %q brightness must be between 0 and 1", canonical)
		}
	}
	return nil
}

func normalizeAlerts(memory *store.MemoryStore, alerts []model.DecisionAlert) error {
	if len(alerts) > maxDecisionAlerts {
		return fmt.Errorf("at most %d alerts are allowed", maxDecisionAlerts)
	}
	seen := make(map[string]bool, len(alerts))
	allowed := map[string]bool{"info": true, "warning": true, "critical": true}
	now := time.Now()
	for i := range alerts {
		alert := &alerts[i]
		alert.ID = strings.TrimSpace(alert.ID)
		alert.Severity = strings.ToLower(strings.TrimSpace(alert.Severity))
		if alert.ID == "" || seen[alert.ID] {
			return requiredOrDuplicate("alert", i, alert.ID, seen[alert.ID])
		}
		seen[alert.ID] = true
		if !allowed[alert.Severity] {
			return fmt.Errorf("alert %q severity must be info, warning, or critical", alert.ID)
		}
		if strings.TrimSpace(alert.Title) == "" {
			return fmt.Errorf("alert %q requires a title", alert.ID)
		}
		if alert.Room != "" {
			if err := normalizeLocation(memory, &alert.Level, &alert.Room, nil, true); err != nil {
				return fmt.Errorf("alert %q: %w", alert.ID, err)
			}
		} else if alert.Level != "" {
			if _, ok := memory.GetFloorData(alert.Level); !ok {
				return fmt.Errorf("alert %q references unknown level %q", alert.ID, alert.Level)
			}
		}
		if alert.Timestamp.IsZero() {
			alert.Timestamp = now
		}
	}
	return nil
}

func normalizeDoors(memory *store.MemoryStore, doors []model.Door) error {
	if len(doors) > maxDoors {
		return fmt.Errorf("at most %d doors are allowed", maxDoors)
	}
	seen := make(map[string]bool, len(doors))
	kinds := map[string]bool{"door": true, "entrance": true, "fire_exit": true}
	states := map[string]bool{"open": true, "closed": true, "blocked": true}
	locks := map[string]bool{"unlocked": true, "locked": true, "jammed": true}
	now := time.Now()
	for i := range doors {
		door := &doors[i]
		door.ID = strings.TrimSpace(door.ID)
		if door.ID == "" || seen[door.ID] {
			return requiredOrDuplicate("door", i, door.ID, seen[door.ID])
		}
		seen[door.ID] = true
		door.Kind = strings.ToLower(strings.TrimSpace(door.Kind))
		if door.Kind == "" {
			door.Kind = "door"
		}
		if !kinds[door.Kind] {
			return fmt.Errorf("door %q kind must be door, entrance, or fire_exit", door.ID)
		}
		door.State = strings.ToLower(strings.TrimSpace(door.State))
		if door.State == "" {
			door.State = "closed"
		}
		if !states[door.State] {
			return fmt.Errorf("door %q state must be open, closed, or blocked", door.ID)
		}
		door.LockState = strings.ToLower(strings.TrimSpace(door.LockState))
		if door.LockState == "" {
			door.LockState = "unlocked"
		}
		if !locks[door.LockState] {
			return fmt.Errorf("door %q lock_state must be unlocked, locked, or jammed", door.ID)
		}
		door.Level = strings.TrimSpace(door.Level)
		floor, ok := memory.GetFloorData(door.Level)
		if !ok {
			return fmt.Errorf("door %q references unknown level %q", door.ID, door.Level)
		}

		if door.EntryNodeID != nil {
			var entry *model.NavNode
			if floor.WalkableGraph != nil {
				for j := range floor.WalkableGraph.Nodes {
					node := &floor.WalkableGraph.Nodes[j]
					if node.ID == *door.EntryNodeID && node.Type == "entry" {
						entry = node
						break
					}
				}
			}
			if entry == nil {
				return fmt.Errorf("door %q entry_node_id %d is not an entry node on %s", door.ID, *door.EntryNodeID, door.Level)
			}
			if door.Room != "" && !strings.EqualFold(strings.TrimSpace(door.Room), entry.Name) {
				return fmt.Errorf("door %q room %q does not match entry node room %q", door.ID, door.Room, entry.Name)
			}
			door.Room = entry.Name
			if door.Position == nil {
				position := [2]float64{entry.X, entry.Y}
				door.Position = &position
			}
		}

		if door.Room != "" {
			canonical, found, ambiguous := memory.ResolveRoomKey(door.Level + "/" + strings.TrimSpace(door.Room))
			if ambiguous || !found {
				return fmt.Errorf("door %q references unknown room %q", door.ID, door.Room)
			}
			door.Level, door.Room = splitRoomKey(canonical)
			if door.Position == nil && floor.WalkableGraph != nil {
				for _, node := range floor.WalkableGraph.Nodes {
					if node.Type == "entry" && node.Name == door.Room {
						position := [2]float64{node.X, node.Y}
						door.Position = &position
						break
					}
				}
			}
		}
		if door.Room == "" && door.EntryNodeID == nil && door.Position == nil {
			return fmt.Errorf("door %q requires room, entry_node_id, or position", door.ID)
		}
		if door.Position != nil {
			if err := validateFloorPosition(memory, door.Level, *door.Position); err != nil {
				return fmt.Errorf("door %q position: %w", door.ID, err)
			}
		}
		if strings.TrimSpace(door.Name) == "" {
			door.Name = door.ID
		}
		door.Blocked = door.State == "blocked" || door.LockState == "locked" || door.LockState == "jammed"
		if door.Timestamp.IsZero() {
			door.Timestamp = now
		}
	}
	return nil
}

func normalizeLocation(memory *store.MemoryStore, level, room *string, position *[2]float64, requireLocation bool) error {
	*level = strings.TrimSpace(*level)
	*room = strings.TrimSpace(*room)
	if *room != "" {
		requested := *room
		if *level != "" {
			requested = *level + "/" + *room
		}
		canonical, found, ambiguous := memory.ResolveRoomKey(requested)
		if ambiguous {
			return fmt.Errorf("room %q is ambiguous; include level", *room)
		}
		if !found {
			return fmt.Errorf("room %q does not exist", *room)
		}
		*level, *room = splitRoomKey(canonical)
	} else {
		if *level == "" {
			return fmt.Errorf("level is required")
		}
		if _, ok := memory.GetFloorData(*level); !ok {
			return fmt.Errorf("level %q does not exist", *level)
		}
		if requireLocation && position == nil {
			return fmt.Errorf("room or position is required")
		}
	}
	if position != nil {
		if err := validateFloorPosition(memory, *level, *position); err != nil {
			return fmt.Errorf("position: %w", err)
		}
	}
	return nil
}

func validateFloorPosition(memory *store.MemoryStore, level string, position [2]float64) error {
	if !finite(position[0]) || !finite(position[1]) {
		return fmt.Errorf("coordinates must be finite")
	}
	floor, ok := memory.GetFloorData(level)
	if !ok {
		return fmt.Errorf("level %q does not exist", level)
	}
	if position[0] < 0 || position[1] < 0 || position[0] > floor.Page.Width || position[1] > floor.Page.Height {
		return fmt.Errorf("coordinates must be inside the floor page")
	}
	return nil
}

func validColor(color string) bool {
	return hexColorPattern.MatchString(strings.TrimSpace(color))
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func requiredOrDuplicate(kind string, index int, id string, duplicate bool) error {
	if id == "" {
		return fmt.Errorf("%s %d requires an id", kind, index)
	}
	if duplicate {
		return fmt.Errorf("duplicate %s id %q", kind, id)
	}
	return nil
}

func validateSensorValue(value model.SensorValue) error {
	if value.DataType != "text" && value.DataType != "binary" {
		return fmt.Errorf("data_type must be text or binary")
	}
	if value.DataType == "binary" && value.BinaryValue == nil {
		return fmt.Errorf("binary_value is required when data_type is binary")
	}
	return nil
}

func splitRoomKey(key string) (string, string) {
	parts := strings.SplitN(key, "/", 2)
	if len(parts) != 2 {
		return "", key
	}
	return parts[0], parts[1]
}
