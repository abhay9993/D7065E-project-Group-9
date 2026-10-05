package store

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/eislab-cps/buildingsim/pkg/model"
)

var (
	ErrEquipmentExists   = errors.New("equipment already exists")
	ErrEquipmentNotFound = errors.New("equipment not found")
	ErrSensorExists      = errors.New("sensor already exists")
	ErrActuatorExists    = errors.New("actuator already exists")
	ErrInvalidEquipment  = errors.New("invalid equipment")
)

// MemoryStore is an in-memory, concurrency-safe snapshot store. Mutable input
// and internal pointers never escape: every read returns a private copy. This
// matters because handlers encode responses after a method's lock is released.
type MemoryStore struct {
	mu sync.RWMutex

	building        model.Building
	floors          map[string]*model.FloorData
	crossFloorEdges []model.CrossFloorEdge
	multiFloorGraph *model.NavGraph

	equipment         map[string]*model.Equipment
	sensorEquipment   map[string]string
	actuatorEquipment map[string]string
	equipmentVersion  int64

	occupancy         map[string]model.RoomOccupancy
	occupancyVersion  int64
	coverage          []model.CoverageZone
	coverageVersion   int64
	roomLayers        []model.RoomLayer
	roomLayerVersion  int64
	effects           []model.VisualEffect
	effectsVersion    int64
	entities          []model.MobileEntity
	entitiesVersion   int64
	roomAppearance    []model.RoomAppearance
	appearanceVersion int64
	alerts            []model.DecisionAlert
	alertsVersion     int64
	doors             []model.Door
	doorsVersion      int64

	sessions map[string]*model.Session
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		floors:            make(map[string]*model.FloorData),
		equipment:         make(map[string]*model.Equipment),
		sensorEquipment:   make(map[string]string),
		actuatorEquipment: make(map[string]string),
		occupancy:         make(map[string]model.RoomOccupancy),
		coverage:          []model.CoverageZone{},
		roomLayers:        []model.RoomLayer{},
		effects:           []model.VisualEffect{},
		entities:          []model.MobileEntity{},
		roomAppearance:    []model.RoomAppearance{},
		alerts:            []model.DecisionAlert{},
		doors:             []model.Door{},
		sessions:          make(map[string]*model.Session),
	}
}

// === Building ===

func (s *MemoryStore) SetBuilding(building model.Building) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.building = cloneBuilding(building)
}

func (s *MemoryStore) GetBuilding() model.Building {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneBuilding(s.building)
}

func (s *MemoryStore) SetFloorData(levelID string, data *model.FloorData) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.floors[levelID] = cloneFloorData(data)
}

func (s *MemoryStore) GetFloorData(levelID string) (*model.FloorData, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, ok := s.floors[levelID]
	if !ok {
		return nil, false
	}
	return cloneFloorData(data), true
}

func (s *MemoryStore) SetCrossFloorEdges(edges []model.CrossFloorEdge) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.crossFloorEdges = append([]model.CrossFloorEdge(nil), edges...)
}

func (s *MemoryStore) GetCrossFloorEdges() []model.CrossFloorEdge {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]model.CrossFloorEdge(nil), s.crossFloorEdges...)
}

func (s *MemoryStore) SetMultiFloorGraph(graph *model.NavGraph) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.multiFloorGraph = cloneNavGraph(graph)
}

func (s *MemoryStore) GetMultiFloorGraph() *model.NavGraph {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneNavGraph(s.multiFloorGraph)
}

func (s *MemoryStore) GetFloors() map[string]*model.FloorData {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]*model.FloorData, len(s.floors))
	for level, floor := range s.floors {
		result[level] = cloneFloorData(floor)
	}
	return result
}

// ResolveRoomKey validates a room reference and returns its canonical
// "<level>/<room name>" key. An unqualified room name or numeric room ID is
// accepted for compatibility only when it resolves to exactly one floor.
func (s *MemoryStore) ResolveRoomKey(key string) (canonical string, found, ambiguous bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.resolveRoomKeyLocked(strings.TrimSpace(key))
}

func (s *MemoryStore) RoomExists(level, room string) bool {
	canonical, found, ambiguous := s.ResolveRoomKey(normalizeLevel(level) + "/" + strings.TrimSpace(room))
	return found && !ambiguous && canonical != ""
}

func (s *MemoryStore) resolveRoomKeyLocked(key string) (string, bool, bool) {
	if key == "" {
		return "", false, false
	}

	for level := range s.floors {
		for _, separator := range []string{"/", ":"} {
			prefix := level + separator
			if !strings.HasPrefix(key, prefix) {
				continue
			}
			token := strings.TrimSpace(strings.TrimPrefix(key, prefix))
			room, ok := s.findRoomOnLevelLocked(level, token)
			if !ok {
				return "", false, false
			}
			return level + "/" + room.Name, true, false
		}
	}

	type match struct {
		level string
		name  string
	}
	matches := make([]match, 0, 2)
	for level := range s.floors {
		if room, ok := s.findRoomOnLevelLocked(level, key); ok {
			matches = append(matches, match{level: level, name: room.Name})
		}
	}
	if len(matches) == 0 {
		return "", false, false
	}
	if len(matches) > 1 {
		return "", false, true
	}
	return matches[0].level + "/" + matches[0].name, true, false
}

func (s *MemoryStore) findRoomOnLevelLocked(level, token string) (model.Room, bool) {
	floor, ok := s.floors[normalizeLevel(level)]
	if !ok || token == "" {
		return model.Room{}, false
	}
	if roomID, err := strconv.Atoi(token); err == nil {
		for _, room := range floor.Rooms {
			if room.ID == roomID {
				return room, true
			}
		}
	}
	for _, room := range floor.Rooms {
		if strings.EqualFold(room.Name, token) {
			return room, true
		}
	}
	return model.Room{}, false
}

func normalizeLevel(level string) string {
	level = strings.TrimSpace(level)
	if i := strings.LastIndex(level, "/"); i >= 0 {
		return level[i+1:]
	}
	return level
}

// === Equipment ===

func (s *MemoryStore) CreateEquipment(equipment *model.Equipment) error {
	if err := prepareEquipment(equipment, time.Now()); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.equipment[equipment.ID]; exists {
		return ErrEquipmentExists
	}
	if err := s.validateChildOwnershipLocked(equipment, "", s.sensorEquipment, s.actuatorEquipment); err != nil {
		return err
	}
	equipment.Version = s.nextEquipmentVersionLocked()
	stored := cloneEquipment(equipment)
	s.equipment[equipment.ID] = stored
	s.indexEquipmentLocked(stored)
	return nil
}

// CreateEquipmentBatch validates the entire batch before publishing anything.
// Existing equipment IDs and repeated IDs are skipped to keep seed programs
// idempotent. A child-ID conflict aborts the complete operation.
func (s *MemoryStore) CreateEquipmentBatch(items []model.Equipment) (created, skipped int, version int64, err error) {
	now := time.Now()
	for i := range items {
		if prepErr := prepareEquipment(&items[i], now); prepErr != nil {
			return 0, 0, 0, fmt.Errorf("item %d: %w", i, prepErr)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	tempSensors := cloneStringMap(s.sensorEquipment)
	tempActuators := cloneStringMap(s.actuatorEquipment)
	seenEquipment := make(map[string]bool, len(items))
	candidates := make([]*model.Equipment, 0, len(items))
	for i := range items {
		equipment := &items[i]
		if _, exists := s.equipment[equipment.ID]; exists || seenEquipment[equipment.ID] {
			skipped++
			continue
		}
		seenEquipment[equipment.ID] = true
		if ownershipErr := s.validateChildOwnershipLocked(equipment, "", tempSensors, tempActuators); ownershipErr != nil {
			return 0, 0, 0, fmt.Errorf("item %d (%s): %w", i, equipment.ID, ownershipErr)
		}
		for _, sensor := range equipment.Sensors {
			tempSensors[sensor.ID] = equipment.ID
		}
		for _, actuator := range equipment.Actuators {
			tempActuators[actuator.ID] = equipment.ID
		}
		candidates = append(candidates, equipment)
	}

	version = s.equipmentVersion
	if len(candidates) > 0 {
		version = s.nextEquipmentVersionLocked()
	}
	for _, equipment := range candidates {
		equipment.Version = version
		stored := cloneEquipment(equipment)
		s.equipment[equipment.ID] = stored
		s.indexEquipmentLocked(stored)
		created++
	}
	return created, skipped, version, nil
}

func (s *MemoryStore) GetEquipment(id string) (*model.Equipment, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	equipment, ok := s.equipment[id]
	if !ok {
		return nil, false
	}
	return cloneEquipment(equipment), true
}

func (s *MemoryStore) ListEquipment(level, roomFilter, typeFilter, category string) []*model.Equipment {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*model.Equipment, 0, len(s.equipment))
	for _, equipment := range s.equipment {
		if level != "" && equipment.Level != level {
			continue
		}
		if roomFilter != "" && !strings.EqualFold(equipment.Room, roomFilter) {
			continue
		}
		if typeFilter != "" && equipment.Type != typeFilter {
			continue
		}
		if category != "" && equipment.Category != category {
			continue
		}
		result = append(result, cloneEquipment(equipment))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func (s *MemoryStore) UpdateEquipment(equipment *model.Equipment) error {
	if err := prepareEquipment(equipment, time.Now()); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.equipment[equipment.ID]
	if !ok {
		return ErrEquipmentNotFound
	}
	if err := s.validateChildOwnershipLocked(equipment, equipment.ID, s.sensorEquipment, s.actuatorEquipment); err != nil {
		return err
	}
	s.removeEquipmentIndexesLocked(old)
	equipment.Version = s.nextEquipmentVersionLocked()
	stored := cloneEquipment(equipment)
	s.equipment[equipment.ID] = stored
	s.indexEquipmentLocked(stored)
	return nil
}

func (s *MemoryStore) DeleteEquipment(id string) (int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	equipment, ok := s.equipment[id]
	if !ok {
		return 0, false
	}
	s.removeEquipmentIndexesLocked(equipment)
	delete(s.equipment, id)
	version := s.nextEquipmentVersionLocked()
	return version, true
}

func (s *MemoryStore) BumpEquipmentVersion() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nextEquipmentVersionLocked()
}

func (s *MemoryStore) nextEquipmentVersionLocked() int64 {
	s.equipmentVersion++
	return s.equipmentVersion
}

func (s *MemoryStore) GetEquipmentVersion() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.equipmentVersion
}

func prepareEquipment(equipment *model.Equipment, now time.Time) error {
	if equipment == nil || strings.TrimSpace(equipment.ID) == "" {
		return fmt.Errorf("%w: id is required", ErrInvalidEquipment)
	}
	equipment.ID = strings.TrimSpace(equipment.ID)
	if equipment.Sensors == nil {
		equipment.Sensors = []model.Sensor{}
	}
	if equipment.Actuators == nil {
		equipment.Actuators = []model.Actuator{}
	}
	seenSensors := make(map[string]bool, len(equipment.Sensors))
	for i := range equipment.Sensors {
		equipment.Sensors[i].ID = strings.TrimSpace(equipment.Sensors[i].ID)
		if equipment.Sensors[i].ID == "" {
			return fmt.Errorf("%w: sensor id is required", ErrInvalidEquipment)
		}
		if seenSensors[equipment.Sensors[i].ID] {
			return fmt.Errorf("%w: duplicate sensor id %q", ErrInvalidEquipment, equipment.Sensors[i].ID)
		}
		seenSensors[equipment.Sensors[i].ID] = true
		if equipment.Sensors[i].Timestamp.IsZero() {
			equipment.Sensors[i].Timestamp = now
		}
	}
	seenActuators := make(map[string]bool, len(equipment.Actuators))
	for i := range equipment.Actuators {
		equipment.Actuators[i].ID = strings.TrimSpace(equipment.Actuators[i].ID)
		if equipment.Actuators[i].ID == "" {
			return fmt.Errorf("%w: actuator id is required", ErrInvalidEquipment)
		}
		if seenActuators[equipment.Actuators[i].ID] {
			return fmt.Errorf("%w: duplicate actuator id %q", ErrInvalidEquipment, equipment.Actuators[i].ID)
		}
		seenActuators[equipment.Actuators[i].ID] = true
		if equipment.Actuators[i].Timestamp.IsZero() {
			equipment.Actuators[i].Timestamp = now
		}
	}
	return nil
}

func (s *MemoryStore) validateChildOwnershipLocked(equipment *model.Equipment, allowedOwner string, sensors, actuators map[string]string) error {
	for _, sensor := range equipment.Sensors {
		if owner, exists := sensors[sensor.ID]; exists && owner != allowedOwner {
			return fmt.Errorf("%w: %s", ErrSensorExists, sensor.ID)
		}
	}
	for _, actuator := range equipment.Actuators {
		if owner, exists := actuators[actuator.ID]; exists && owner != allowedOwner {
			return fmt.Errorf("%w: %s", ErrActuatorExists, actuator.ID)
		}
	}
	return nil
}

func (s *MemoryStore) indexEquipmentLocked(equipment *model.Equipment) {
	for _, sensor := range equipment.Sensors {
		s.sensorEquipment[sensor.ID] = equipment.ID
	}
	for _, actuator := range equipment.Actuators {
		s.actuatorEquipment[actuator.ID] = equipment.ID
	}
}

func (s *MemoryStore) removeEquipmentIndexesLocked(equipment *model.Equipment) {
	for _, sensor := range equipment.Sensors {
		delete(s.sensorEquipment, sensor.ID)
	}
	for _, actuator := range equipment.Actuators {
		delete(s.actuatorEquipment, actuator.ID)
	}
}

// === Sensors ===

func (s *MemoryStore) AddSensor(equipmentID string, sensor *model.Sensor) (int64, bool, string) {
	if sensor == nil || strings.TrimSpace(sensor.ID) == "" {
		return 0, false, "id is required"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	equipment, ok := s.equipment[equipmentID]
	if !ok {
		return 0, false, "equipment not found"
	}
	sensor.ID = strings.TrimSpace(sensor.ID)
	if _, exists := s.sensorEquipment[sensor.ID]; exists {
		return 0, false, "sensor already exists"
	}
	sensor.Timestamp = time.Now()
	replacement := cloneEquipment(equipment)
	replacement.Sensors = append(replacement.Sensors, *sensor)
	version := s.nextEquipmentVersionLocked()
	replacement.Version = version
	s.equipment[equipmentID] = replacement
	s.indexEquipmentLocked(replacement)
	return version, true, ""
}

func (s *MemoryStore) GetSensor(sensorID string) (model.Sensor, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	equipmentID, ok := s.sensorEquipment[sensorID]
	if !ok {
		return model.Sensor{}, false
	}
	for _, sensor := range s.equipment[equipmentID].Sensors {
		if sensor.ID == sensorID {
			return sensor, true
		}
	}
	return model.Sensor{}, false
}

func (s *MemoryStore) GetSensorsForEquipment(equipmentID string) ([]model.Sensor, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	equipment, ok := s.equipment[equipmentID]
	if !ok {
		return nil, false
	}
	return append([]model.Sensor(nil), equipment.Sensors...), true
}

func (s *MemoryStore) DeleteSensor(sensorID string) (int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	equipmentID, ok := s.sensorEquipment[sensorID]
	if !ok {
		return 0, false
	}
	replacement := cloneEquipment(s.equipment[equipmentID])
	for i, sensor := range replacement.Sensors {
		if sensor.ID == sensorID {
			replacement.Sensors = append(replacement.Sensors[:i], replacement.Sensors[i+1:]...)
			break
		}
	}
	delete(s.sensorEquipment, sensorID)
	version := s.nextEquipmentVersionLocked()
	replacement.Version = version
	s.equipment[equipmentID] = replacement
	s.indexEquipmentLocked(replacement)
	return version, true
}

func (s *MemoryStore) SetSensorValue(sensorID string, value model.SensorValue) (int64, bool) {
	if value.DataType == "binary" && value.BinaryValue == nil {
		return 0, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	equipmentID, ok := s.sensorEquipment[sensorID]
	if !ok {
		return 0, false
	}
	replacement := cloneEquipment(s.equipment[equipmentID])
	for i := range replacement.Sensors {
		if replacement.Sensors[i].ID != sensorID {
			continue
		}
		replacement.Sensors[i].DataType = value.DataType
		if value.DataType == "binary" {
			replacement.Sensors[i].BinaryValue = *value.BinaryValue
		} else {
			replacement.Sensors[i].Value = value.Value
		}
		replacement.Sensors[i].Timestamp = time.Now()
		version := s.nextEquipmentVersionLocked()
		replacement.Version = version
		s.equipment[equipmentID] = replacement
		s.indexEquipmentLocked(replacement)
		return version, true
	}
	return 0, false
}

// === Actuators ===

func (s *MemoryStore) AddActuator(equipmentID string, actuator *model.Actuator) (int64, bool, string) {
	if actuator == nil || strings.TrimSpace(actuator.ID) == "" {
		return 0, false, "id is required"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	equipment, ok := s.equipment[equipmentID]
	if !ok {
		return 0, false, "equipment not found"
	}
	actuator.ID = strings.TrimSpace(actuator.ID)
	if _, exists := s.actuatorEquipment[actuator.ID]; exists {
		return 0, false, "actuator already exists"
	}
	actuator.Timestamp = time.Now()
	replacement := cloneEquipment(equipment)
	replacement.Actuators = append(replacement.Actuators, *actuator)
	version := s.nextEquipmentVersionLocked()
	replacement.Version = version
	s.equipment[equipmentID] = replacement
	s.indexEquipmentLocked(replacement)
	return version, true, ""
}

func (s *MemoryStore) GetActuator(actuatorID string) (model.Actuator, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	equipmentID, ok := s.actuatorEquipment[actuatorID]
	if !ok {
		return model.Actuator{}, false
	}
	for _, actuator := range s.equipment[equipmentID].Actuators {
		if actuator.ID == actuatorID {
			return actuator, true
		}
	}
	return model.Actuator{}, false
}

func (s *MemoryStore) GetActuatorsForEquipment(equipmentID string) ([]model.Actuator, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	equipment, ok := s.equipment[equipmentID]
	if !ok {
		return nil, false
	}
	return append([]model.Actuator(nil), equipment.Actuators...), true
}

func (s *MemoryStore) DeleteActuator(actuatorID string) (int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	equipmentID, ok := s.actuatorEquipment[actuatorID]
	if !ok {
		return 0, false
	}
	replacement := cloneEquipment(s.equipment[equipmentID])
	for i, actuator := range replacement.Actuators {
		if actuator.ID == actuatorID {
			replacement.Actuators = append(replacement.Actuators[:i], replacement.Actuators[i+1:]...)
			break
		}
	}
	delete(s.actuatorEquipment, actuatorID)
	version := s.nextEquipmentVersionLocked()
	replacement.Version = version
	s.equipment[equipmentID] = replacement
	s.indexEquipmentLocked(replacement)
	return version, true
}

func (s *MemoryStore) SetActuatorState(actuatorID string, state model.ActuatorState) (int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	equipmentID, ok := s.actuatorEquipment[actuatorID]
	if !ok {
		return 0, false
	}
	replacement := cloneEquipment(s.equipment[equipmentID])
	for i := range replacement.Actuators {
		if replacement.Actuators[i].ID != actuatorID {
			continue
		}
		replacement.Actuators[i].State = state.State
		replacement.Actuators[i].Timestamp = time.Now()
		version := s.nextEquipmentVersionLocked()
		replacement.Version = version
		s.equipment[equipmentID] = replacement
		s.indexEquipmentLocked(replacement)
		return version, true
	}
	return 0, false
}

// === Global occupancy and coverage ===

func (s *MemoryStore) SetOccupancy(occupancy map[string]model.RoomOccupancy) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.occupancy = cloneOccupancy(occupancy)
	s.occupancyVersion++
	return s.occupancyVersion
}

func (s *MemoryStore) GetOccupancy() map[string]model.RoomOccupancy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneOccupancy(s.occupancy)
}

func (s *MemoryStore) GetOccupancyVersion() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.occupancyVersion
}

func (s *MemoryStore) SetCoverage(coverage []model.CoverageZone) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.coverage = append([]model.CoverageZone(nil), coverage...)
	s.coverageVersion++
	return s.coverageVersion
}

func (s *MemoryStore) GetCoverage() []model.CoverageZone {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]model.CoverageZone(nil), s.coverage...)
}

// === Global simulation and visualization state ===

func (s *MemoryStore) SetRoomLayers(layers []model.RoomLayer) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.roomLayers = cloneRoomLayers(layers)
	s.roomLayerVersion++
	return s.roomLayerVersion
}

func (s *MemoryStore) GetRoomLayers() []model.RoomLayer {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneRoomLayers(s.roomLayers)
}

func (s *MemoryStore) SetEffects(effects []model.VisualEffect) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.effects = cloneEffects(effects)
	s.effectsVersion++
	return s.effectsVersion
}

func (s *MemoryStore) GetEffects() []model.VisualEffect {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneEffects(s.effects)
}

func (s *MemoryStore) SetEntities(entities []model.MobileEntity) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entities = cloneEntities(entities)
	s.entitiesVersion++
	return s.entitiesVersion
}

func (s *MemoryStore) GetEntities() []model.MobileEntity {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneEntities(s.entities)
}

func (s *MemoryStore) SetRoomAppearance(appearance []model.RoomAppearance) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.roomAppearance = append([]model.RoomAppearance(nil), appearance...)
	s.appearanceVersion++
	return s.appearanceVersion
}

func (s *MemoryStore) GetRoomAppearance() []model.RoomAppearance {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]model.RoomAppearance(nil), s.roomAppearance...)
}

func (s *MemoryStore) SetAlerts(alerts []model.DecisionAlert) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.alerts = append([]model.DecisionAlert(nil), alerts...)
	s.alertsVersion++
	return s.alertsVersion
}

func (s *MemoryStore) GetAlerts() []model.DecisionAlert {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]model.DecisionAlert(nil), s.alerts...)
}

func (s *MemoryStore) SetDoors(doors []model.Door) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.doors = cloneDoors(doors)
	s.doorsVersion++
	return s.doorsVersion
}

func (s *MemoryStore) GetDoors() []model.Door {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneDoors(s.doors)
}

// === Sessions ===

func (s *MemoryStore) CreateSession(id string) *model.Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	session := &model.Session{
		ID:           id,
		Viewport:     model.Viewport{Mode: "3d", Floor: "level0", Zoom: 1.0},
		Highlights:   []model.RoomHighlight{},
		Occupancy:    make(map[string]model.RoomOccupancy),
		Coverage:     []model.CoverageZone{},
		LastWSActive: time.Now(),
	}
	s.sessions[id] = session
	return cloneSession(session)
}

func (s *MemoryStore) ListSessions() []*model.Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*model.Session, 0, len(s.sessions))
	for _, session := range s.sessions {
		result = append(result, cloneSession(session))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func (s *MemoryStore) GetSession(id string) (*model.Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.sessions[id]
	if !ok {
		return nil, false
	}
	return cloneSession(session), true
}

func (s *MemoryStore) DeleteSession(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[id]; !ok {
		return false
	}
	delete(s.sessions, id)
	return true
}

func (s *MemoryStore) UpdateSessionViewport(id string, viewport model.Viewport) (int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[id]
	if !ok {
		return 0, false
	}
	session.Viewport = viewport
	session.Version++
	return session.Version, true
}

func (s *MemoryStore) UpdateSessionHighlights(id string, highlights []model.RoomHighlight) (int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[id]
	if !ok {
		return 0, false
	}
	session.Highlights = append([]model.RoomHighlight(nil), highlights...)
	session.Version++
	return session.Version, true
}

func (s *MemoryStore) UpdateSessionOccupancy(id string, occupancy map[string]model.RoomOccupancy) (int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[id]
	if !ok {
		return 0, false
	}
	session.Occupancy = cloneOccupancy(occupancy)
	session.Version++
	return session.Version, true
}

func (s *MemoryStore) UpdateSessionCoverage(id string, coverage []model.CoverageZone) (int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[id]
	if !ok {
		return 0, false
	}
	session.Coverage = append([]model.CoverageZone(nil), coverage...)
	session.Version++
	return session.Version, true
}

func (s *MemoryStore) UpdateSessionRoute(id string, route *model.RouteResult) (int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[id]
	if !ok {
		return 0, false
	}
	session.Route = cloneRoute(route)
	session.Version++
	return session.Version, true
}

func (s *MemoryStore) TouchSession(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if session, ok := s.sessions[id]; ok {
		session.LastWSActive = time.Now()
	}
}

func (s *MemoryStore) PurgeInactiveSessions(maxAge time.Duration) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	purged := []string{}
	now := time.Now()
	for id, session := range s.sessions {
		if now.Sub(session.LastWSActive) > maxAge {
			delete(s.sessions, id)
			purged = append(purged, id)
		}
	}
	sort.Strings(purged)
	return purged
}

// === Clone helpers ===

func cloneBuilding(building model.Building) model.Building {
	building.Levels = append([]model.Level(nil), building.Levels...)
	return building
}

func cloneFloorData(data *model.FloorData) *model.FloorData {
	if data == nil {
		return nil
	}
	result := *data
	result.Rooms = append([]model.Room(nil), data.Rooms...)
	for i := range result.Rooms {
		result.Rooms[i].Polygon = append([][2]float64(nil), data.Rooms[i].Polygon...)
	}
	result.Walls = cloneLineSets(data.Walls)
	result.RedLines = cloneLineSets(data.RedLines)
	result.GreenLines = cloneLineSets(data.GreenLines)
	result.Labels = append([]model.Label(nil), data.Labels...)
	result.Graph = cloneNavGraph(data.Graph)
	result.WalkableGraph = cloneNavGraph(data.WalkableGraph)
	return &result
}

func cloneLineSets(lines [][][2]float64) [][][2]float64 {
	result := make([][][2]float64, len(lines))
	for i := range lines {
		result[i] = append([][2]float64(nil), lines[i]...)
	}
	return result
}

func cloneNavGraph(graph *model.NavGraph) *model.NavGraph {
	if graph == nil {
		return nil
	}
	result := *graph
	result.Nodes = append([]model.NavNode(nil), graph.Nodes...)
	result.Edges = append([]model.NavEdge(nil), graph.Edges...)
	return &result
}

func cloneEquipment(equipment *model.Equipment) *model.Equipment {
	if equipment == nil {
		return nil
	}
	result := *equipment
	if equipment.Position != nil {
		position := *equipment.Position
		result.Position = &position
	}
	result.Sensors = append([]model.Sensor(nil), equipment.Sensors...)
	result.Actuators = append([]model.Actuator(nil), equipment.Actuators...)
	if result.Sensors == nil {
		result.Sensors = []model.Sensor{}
	}
	if result.Actuators == nil {
		result.Actuators = []model.Actuator{}
	}
	return &result
}

func cloneOccupancy(occupancy map[string]model.RoomOccupancy) map[string]model.RoomOccupancy {
	result := make(map[string]model.RoomOccupancy, len(occupancy))
	for key, room := range occupancy {
		room.Persons = append([]model.Person(nil), room.Persons...)
		for i := range room.Persons {
			if occupancy[key].Persons[i].Position != nil {
				position := *occupancy[key].Persons[i].Position
				room.Persons[i].Position = &position
			}
		}
		room.Aliens = append([]model.Alien(nil), room.Aliens...)
		if room.Persons == nil {
			room.Persons = []model.Person{}
		}
		if room.Aliens == nil {
			room.Aliens = []model.Alien{}
		}
		result[key] = room
	}
	return result
}

func cloneRoomLayers(layers []model.RoomLayer) []model.RoomLayer {
	result := make([]model.RoomLayer, len(layers))
	for i := range layers {
		result[i] = layers[i]
		result[i].Palette = append([]string(nil), layers[i].Palette...)
		result[i].Values = make(map[string]float64, len(layers[i].Values))
		for key, value := range layers[i].Values {
			result[i].Values[key] = value
		}
	}
	return result
}

func cloneEffects(effects []model.VisualEffect) []model.VisualEffect {
	result := append([]model.VisualEffect(nil), effects...)
	for i := range result {
		if effects[i].Position != nil {
			position := *effects[i].Position
			result[i].Position = &position
		}
	}
	return result
}

func cloneEntities(entities []model.MobileEntity) []model.MobileEntity {
	result := append([]model.MobileEntity(nil), entities...)
	for i := range result {
		if entities[i].Position != nil {
			position := *entities[i].Position
			result[i].Position = &position
		}
	}
	return result
}

func cloneDoors(doors []model.Door) []model.Door {
	result := append([]model.Door(nil), doors...)
	for i := range result {
		if doors[i].EntryNodeID != nil {
			nodeID := *doors[i].EntryNodeID
			result[i].EntryNodeID = &nodeID
		}
		if doors[i].Position != nil {
			position := *doors[i].Position
			result[i].Position = &position
		}
	}
	return result
}

func cloneRoute(route *model.RouteResult) *model.RouteResult {
	if route == nil {
		return nil
	}
	result := *route
	result.Path = append([]model.RouteNode(nil), route.Path...)
	return &result
}

func cloneSession(session *model.Session) *model.Session {
	if session == nil {
		return nil
	}
	result := *session
	result.Highlights = append([]model.RoomHighlight(nil), session.Highlights...)
	result.Occupancy = cloneOccupancy(session.Occupancy)
	result.Route = cloneRoute(session.Route)
	result.Coverage = append([]model.CoverageZone(nil), session.Coverage...)
	if result.Highlights == nil {
		result.Highlights = []model.RoomHighlight{}
	}
	if result.Coverage == nil {
		result.Coverage = []model.CoverageZone{}
	}
	return &result
}

func cloneStringMap(values map[string]string) map[string]string {
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}
