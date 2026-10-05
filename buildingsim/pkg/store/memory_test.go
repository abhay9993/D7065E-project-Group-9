package store

import (
	"errors"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/eislab-cps/buildingsim/pkg/model"
)

func TestRoomResolution(t *testing.T) {
	memory := storeWithRooms()

	tests := []struct {
		input     string
		canonical string
		found     bool
		ambiguous bool
	}{
		{"level0/Shared", "level0/Shared", true, false},
		{"level1:7", "level1/Shared", true, false},
		{"Unique", "level0/Unique", true, false},
		{"Shared", "", false, true},
		{"7", "", false, true},
		{"level9/Shared", "", false, false},
		{"", "", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			canonical, found, ambiguous := memory.ResolveRoomKey(tt.input)
			if canonical != tt.canonical || found != tt.found || ambiguous != tt.ambiguous {
				t.Fatalf("got (%q,%v,%v), want (%q,%v,%v)", canonical, found, ambiguous, tt.canonical, tt.found, tt.ambiguous)
			}
		})
	}
}

func TestSnapshotsDoNotExposeStoreState(t *testing.T) {
	memory := storeWithRooms()
	memory.SetBuilding(model.Building{Name: "Original", Levels: []model.Level{{ID: "level0", Label: "Floor 0"}}})

	building := memory.GetBuilding()
	building.Name = "Changed"
	building.Levels[0].Label = "Changed"
	if got := memory.GetBuilding(); got.Name != "Original" || got.Levels[0].Label != "Floor 0" {
		t.Fatalf("building snapshot mutated store: %+v", got)
	}

	floor, _ := memory.GetFloorData("level0")
	floor.Rooms[0].Name = "Changed"
	floor.Rooms[0].Polygon[0][0] = 999
	floor.Walls[0][0][0] = 999
	unchanged, _ := memory.GetFloorData("level0")
	if unchanged.Rooms[0].Name != "Shared" || unchanged.Rooms[0].Polygon[0][0] == 999 || unchanged.Walls[0][0][0] == 999 {
		t.Fatalf("floor snapshot mutated store: %+v", unchanged.Rooms[0])
	}

	equipment := &model.Equipment{
		ID: "eq", Sensors: []model.Sensor{{ID: "sensor", DataType: "text", Value: "1"}},
		Actuators: []model.Actuator{{ID: "actuator", State: "off"}},
	}
	if err := memory.CreateEquipment(equipment); err != nil {
		t.Fatal(err)
	}
	copyOfEquipment, _ := memory.GetEquipment("eq")
	copyOfEquipment.Sensors[0].Value = "changed"
	copyOfEquipment.Actuators[0].State = "changed"
	storedEquipment, _ := memory.GetEquipment("eq")
	if storedEquipment.Sensors[0].Value != "1" || storedEquipment.Actuators[0].State != "off" {
		t.Fatalf("equipment snapshot mutated store: %+v", storedEquipment)
	}

	memory.SetOccupancy(map[string]model.RoomOccupancy{
		"level0/Shared": {Persons: []model.Person{{ID: "p", Name: "Original"}}},
	})
	occupancy := memory.GetOccupancy()
	room := occupancy["level0/Shared"]
	room.Persons[0].Name = "Changed"
	occupancy["level0/Shared"] = room
	if got := memory.GetOccupancy()["level0/Shared"].Persons[0].Name; got != "Original" {
		t.Fatalf("occupancy snapshot mutated store: %q", got)
	}
}

func TestVisualizationSnapshotsDoNotExposeStoreState(t *testing.T) {
	memory := NewMemoryStore()
	position := [2]float64{10, 20}
	nodeID := 7

	memory.SetRoomLayers([]model.RoomLayer{{
		ID: "temperature", Palette: []string{"#000000", "#ffffff"},
		Values: map[string]float64{"level0/R": 21},
	}})
	layers := memory.GetRoomLayers()
	layers[0].Palette[0] = "#ff0000"
	layers[0].Values["level0/R"] = 99
	unchangedLayers := memory.GetRoomLayers()
	if unchangedLayers[0].Palette[0] != "#000000" || unchangedLayers[0].Values["level0/R"] != 21 {
		t.Fatalf("room layer snapshot mutated store: %+v", unchangedLayers)
	}

	memory.SetEffects([]model.VisualEffect{{ID: "fire", Position: &position}})
	effects := memory.GetEffects()
	effects[0].Position[0] = 99
	if got := memory.GetEffects()[0].Position[0]; got != 10 {
		t.Fatalf("effect position snapshot mutated store: %v", got)
	}

	memory.SetEntities([]model.MobileEntity{{ID: "robot", Position: &position}})
	entities := memory.GetEntities()
	entities[0].Position[1] = 99
	if got := memory.GetEntities()[0].Position[1]; got != 20 {
		t.Fatalf("entity position snapshot mutated store: %v", got)
	}

	memory.SetDoors([]model.Door{{ID: "door", Position: &position, EntryNodeID: &nodeID}})
	doors := memory.GetDoors()
	doors[0].Position[0] = 88
	*doors[0].EntryNodeID = 88
	storedDoor := memory.GetDoors()[0]
	if storedDoor.Position[0] != 10 || *storedDoor.EntryNodeID != 7 {
		t.Fatalf("door snapshot mutated store: %+v", storedDoor)
	}
}

func TestBatchCreateIsAtomicOnChildConflict(t *testing.T) {
	memory := NewMemoryStore()
	if err := memory.CreateEquipment(&model.Equipment{
		ID: "existing", Sensors: []model.Sensor{{ID: "shared", DataType: "text"}},
	}); err != nil {
		t.Fatal(err)
	}

	created, skipped, _, err := memory.CreateEquipmentBatch([]model.Equipment{
		{ID: "candidate-1", Sensors: []model.Sensor{{ID: "new", DataType: "text"}}},
		{ID: "candidate-2", Sensors: []model.Sensor{{ID: "shared", DataType: "text"}}},
	})
	if !errors.Is(err, ErrSensorExists) || created != 0 || skipped != 0 {
		t.Fatalf("created=%d skipped=%d err=%v", created, skipped, err)
	}
	if _, exists := memory.GetEquipment("candidate-1"); exists {
		t.Fatal("batch partially published before the conflict")
	}
}

func TestBatchCreateSkipsRepeatedEquipmentIDs(t *testing.T) {
	memory := NewMemoryStore()
	items := []model.Equipment{{ID: "one"}, {ID: "one"}, {ID: "two"}}
	created, skipped, _, err := memory.CreateEquipmentBatch(items)
	if err != nil || created != 2 || skipped != 1 {
		t.Fatalf("created=%d skipped=%d err=%v", created, skipped, err)
	}
	created, skipped, _, err = memory.CreateEquipmentBatch(items)
	if err != nil || created != 0 || skipped != 3 {
		t.Fatalf("second call: created=%d skipped=%d err=%v", created, skipped, err)
	}
}

func TestEquipmentMutationsAdvanceOneConsistentVersion(t *testing.T) {
	memory := NewMemoryStore()
	equipment := &model.Equipment{ID: "eq"}
	if err := memory.CreateEquipment(equipment); err != nil {
		t.Fatal(err)
	}
	assertEquipmentVersion(t, memory, "eq", 1)

	if _, ok, reason := memory.AddSensor("eq", &model.Sensor{ID: "sensor", DataType: "text"}); !ok {
		t.Fatal(reason)
	}
	assertEquipmentVersion(t, memory, "eq", 2)

	if _, ok := memory.SetSensorValue("sensor", model.SensorValue{DataType: "text", Value: "21.5"}); !ok {
		t.Fatal("sensor update failed")
	}
	assertEquipmentVersion(t, memory, "eq", 3)

	if _, ok, reason := memory.AddActuator("eq", &model.Actuator{ID: "actuator"}); !ok {
		t.Fatal(reason)
	}
	assertEquipmentVersion(t, memory, "eq", 4)

	if _, ok := memory.SetActuatorState("actuator", model.ActuatorState{State: "on"}); !ok {
		t.Fatal("actuator update failed")
	}
	assertEquipmentVersion(t, memory, "eq", 5)

	if _, ok := memory.DeleteSensor("sensor"); !ok {
		t.Fatal("sensor delete failed")
	}
	assertEquipmentVersion(t, memory, "eq", 6)

	if _, ok := memory.DeleteActuator("actuator"); !ok {
		t.Fatal("actuator delete failed")
	}
	assertEquipmentVersion(t, memory, "eq", 7)

	if _, ok := memory.SetSensorValue("missing", model.SensorValue{DataType: "text"}); ok {
		t.Fatal("missing sensor unexpectedly updated")
	}
	if got := memory.GetEquipmentVersion(); got != 7 {
		t.Fatalf("failed mutation advanced version to %d", got)
	}
	if _, ok := memory.DeleteEquipment("eq"); !ok || memory.GetEquipmentVersion() != 8 {
		t.Fatalf("equipment delete version=%d", memory.GetEquipmentVersion())
	}
}

func TestBatchCreateUsesOneVersionAndNoOpDoesNotAdvance(t *testing.T) {
	memory := NewMemoryStore()
	items := []model.Equipment{{ID: "one"}, {ID: "two"}}
	created, _, _, err := memory.CreateEquipmentBatch(items)
	if err != nil || created != 2 {
		t.Fatalf("created=%d err=%v", created, err)
	}
	assertEquipmentVersion(t, memory, "one", 1)
	assertEquipmentVersion(t, memory, "two", 1)
	created, _, _, err = memory.CreateEquipmentBatch(items)
	if err != nil || created != 0 || memory.GetEquipmentVersion() != 1 {
		t.Fatalf("no-op batch: created=%d version=%d err=%v", created, memory.GetEquipmentVersion(), err)
	}
}

func assertEquipmentVersion(t *testing.T, memory *MemoryStore, id string, want int64) {
	t.Helper()
	equipment, ok := memory.GetEquipment(id)
	if !ok {
		t.Fatalf("equipment %q not found", id)
	}
	if equipment.Version != want || memory.GetEquipmentVersion() != want {
		t.Fatalf("stored version=%d global version=%d, want %d", equipment.Version, memory.GetEquipmentVersion(), want)
	}
}

func TestUpdateRebuildsChildIndexes(t *testing.T) {
	memory := NewMemoryStore()
	if err := memory.CreateEquipment(&model.Equipment{
		ID:        "eq",
		Sensors:   []model.Sensor{{ID: "old-sensor", DataType: "text"}},
		Actuators: []model.Actuator{{ID: "old-actuator"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := memory.UpdateEquipment(&model.Equipment{
		ID:        "eq",
		Sensors:   []model.Sensor{{ID: "new-sensor", DataType: "text"}},
		Actuators: []model.Actuator{{ID: "new-actuator"}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := memory.GetSensor("old-sensor"); ok {
		t.Fatal("old sensor index remains")
	}
	if _, ok := memory.GetActuator("old-actuator"); ok {
		t.Fatal("old actuator index remains")
	}
	if _, ok := memory.GetSensor("new-sensor"); !ok {
		t.Fatal("new sensor was not indexed")
	}
	if _, ok := memory.GetActuator("new-actuator"); !ok {
		t.Fatal("new actuator was not indexed")
	}
}

func TestDeleteEquipmentReleasesChildIDs(t *testing.T) {
	memory := NewMemoryStore()
	first := &model.Equipment{ID: "first", Sensors: []model.Sensor{{ID: "sensor", DataType: "text"}}}
	if err := memory.CreateEquipment(first); err != nil {
		t.Fatal(err)
	}
	if _, ok := memory.DeleteEquipment("first"); !ok {
		t.Fatal("delete failed")
	}
	if err := memory.CreateEquipment(&model.Equipment{ID: "second", Sensors: []model.Sensor{{ID: "sensor", DataType: "text"}}}); err != nil {
		t.Fatalf("child ID was not released: %v", err)
	}
}

func TestInvalidEquipmentAndChildren(t *testing.T) {
	memory := NewMemoryStore()
	tests := []struct {
		name string
		eq   *model.Equipment
	}{
		{"nil", nil},
		{"missing id", &model.Equipment{}},
		{"missing sensor id", &model.Equipment{ID: "eq", Sensors: []model.Sensor{{DataType: "text"}}}},
		{"duplicate sensor", &model.Equipment{ID: "eq", Sensors: []model.Sensor{{ID: "s"}, {ID: "s"}}}},
		{"missing actuator id", &model.Equipment{ID: "eq", Actuators: []model.Actuator{{}}}},
		{"duplicate actuator", &model.Equipment{ID: "eq", Actuators: []model.Actuator{{ID: "a"}, {ID: "a"}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := memory.CreateEquipment(tt.eq); !errors.Is(err, ErrInvalidEquipment) {
				t.Fatalf("expected invalid equipment, got %v", err)
			}
		})
	}
}

func TestPurgeInactiveSessionsIsSorted(t *testing.T) {
	memory := NewMemoryStore()
	memory.CreateSession("z")
	memory.CreateSession("a")
	got := memory.PurgeInactiveSessions(-time.Nanosecond)
	if fmt.Sprint(got) != "[a z]" || len(memory.ListSessions()) != 0 {
		t.Fatalf("unexpected purge result: %v", got)
	}
}

func TestConcurrentReadWriteSnapshots(t *testing.T) {
	memory := NewMemoryStore()
	if err := memory.CreateEquipment(&model.Equipment{
		ID: "eq", Sensors: []model.Sensor{{ID: "sensor", DataType: "text"}},
	}); err != nil {
		t.Fatal(err)
	}
	memory.CreateSession("session")

	const workers = 12
	const iterations = 200
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		worker := worker
		wg.Add(1)
		go func() {
			defer wg.Done()
			for iteration := 0; iteration < iterations; iteration++ {
				value := strconv.Itoa(worker*iterations + iteration)
				switch worker % 4 {
				case 0:
					memory.SetSensorValue("sensor", model.SensorValue{DataType: "text", Value: value})
				case 1:
					equipment, ok := memory.GetEquipment("eq")
					if !ok || len(equipment.Sensors) != 1 {
						t.Errorf("bad equipment snapshot")
						return
					}
					equipment.Sensors[0].Value = "caller mutation"
				case 2:
					memory.UpdateSessionOccupancy("session", map[string]model.RoomOccupancy{
						"level0/room": {Persons: []model.Person{{ID: value}}},
					})
				case 3:
					session, ok := memory.GetSession("session")
					if !ok || session.Occupancy == nil {
						t.Errorf("bad session snapshot")
						return
					}
				}
			}
		}()
	}
	wg.Wait()
	if _, ok := memory.GetEquipment("eq"); !ok {
		t.Fatal("equipment disappeared")
	}
}

func storeWithRooms() *MemoryStore {
	memory := NewMemoryStore()
	memory.SetFloorData("level0", &model.FloorData{
		Page: model.Page{Width: 100, Height: 100},
		Rooms: []model.Room{
			{ID: 7, Name: "Shared", Polygon: [][2]float64{{0, 0}, {1, 0}, {1, 1}}},
			{ID: 8, Name: "Unique", Polygon: [][2]float64{{1, 1}, {2, 1}, {2, 2}}},
		},
		Walls: [][][2]float64{{{0, 0}, {1, 1}}},
	})
	memory.SetFloorData("level1", &model.FloorData{
		Page:  model.Page{Width: 100, Height: 100},
		Rooms: []model.Room{{ID: 7, Name: "Shared"}},
	})
	return memory
}
