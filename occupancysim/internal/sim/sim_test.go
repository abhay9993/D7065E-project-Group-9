package sim_test

import (
	"math"
	"os"
	"testing"
	"time"

	"github.com/eislab-cps/D7065E/occupancysim/internal/buildsim"
	"github.com/eislab-cps/D7065E/occupancysim/internal/sim"
)

var (
	monday   = time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	saturday = time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)
)

func at(day time.Time, clock string) time.Time {
	minutes, err := sim.ParseClock(clock)
	if err != nil {
		panic(err)
	}
	return day.Add(time.Duration(minutes) * time.Minute)
}

func loadPlan(t *testing.T) sim.FloorPlan {
	t.Helper()
	return loadPlans(t, "level0")[0]
}

// loadPlans reads the level 0 test drawing once per requested level. Naming the
// same drawing twice gives a two-floor building, which is all the multi-floor
// tests need.
func loadPlans(t *testing.T, levels ...string) []sim.FloorPlan {
	t.Helper()
	data, err := os.ReadFile("../../testdata/level0.json")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	var plans []sim.FloorPlan
	for _, level := range levels {
		plan, err := buildsim.ParseFloor(data, level)
		if err != nil {
			t.Fatal(err)
		}
		plans = append(plans, plan)
	}
	return plans
}

func newSim(t *testing.T, params sim.Parameters, start time.Time) *sim.Simulation {
	t.Helper()
	return newBuilding(t, params, start, loadPlan(t))
}

func newBuilding(t *testing.T, params sim.Parameters, start time.Time, plans ...sim.FloorPlan) *sim.Simulation {
	t.Helper()
	s, err := sim.New(plans, params, start)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func countRoles(rooms []sim.Room) map[sim.RoomRole]int {
	counts := map[sim.RoomRole]int{}
	for _, room := range rooms {
		counts[room.Role]++
	}
	return counts
}

func TestClassificationFollowsAreaRules(t *testing.T) {
	params := sim.DefaultParameters()
	s := newSim(t, params, at(monday, "07:00"))
	rooms := s.Rooms()
	counts := countRoles(rooms)
	if counts[sim.RoleOffice] < 50 || counts[sim.RoleLecture] < 10 {
		t.Fatalf("unexpected classification: %v", counts)
	}
	if counts[sim.RoleFika] != params.FikaRoomCount {
		t.Fatalf("fika rooms = %d, want %d", counts[sim.RoleFika], params.FikaRoomCount)
	}
	for _, room := range rooms {
		switch room.Role {
		case sim.RoleFika:
			if room.AreaM2 < params.FikaMinM2 {
				t.Errorf("fika room %s is only %.0f m2", room.Name, room.AreaM2)
			}
		case sim.RoleOffice:
			if room.AreaM2 > params.OfficeMaxM2 || room.AreaM2 < params.MinRoomM2 {
				t.Errorf("office %s has %.0f m2", room.Name, room.AreaM2)
			}
		case sim.RoleLecture:
			if room.AreaM2 < params.LectureMinM2 {
				t.Errorf("lecture room %s has %.0f m2", room.Name, room.AreaM2)
			}
		}
	}
}

func TestRoomRoleOverride(t *testing.T) {
	params := sim.DefaultParameters()
	params.RoomRoles = map[string]string{"A109": "fika"}
	s := newSim(t, params, at(monday, "07:00"))
	for _, room := range s.Rooms() {
		if room.Name == "A109" && room.Role != sim.RoleFika {
			t.Fatalf("A109 role = %s, want fika", room.Role)
		}
	}
	params.RoomRoles = map[string]string{"NOPE": "office"}
	if _, err := sim.New(loadPlans(t, "level0"), params, monday); err == nil {
		t.Fatal("unknown room override was accepted")
	}
}

func TestNightIsEmptyExceptGuards(t *testing.T) {
	params := sim.DefaultParameters()
	params.NightGuards = 2
	s := newSim(t, params, at(monday, "03:00"))
	s.Advance(30 * time.Minute)
	snap := s.Snapshot(0)
	if snap.Counts.Inside != 2 || snap.Counts.InsideByRole[sim.RoleGuard] != 2 {
		t.Fatalf("at 03:30 inside=%d by role=%v", snap.Counts.Inside, snap.Counts.InsideByRole)
	}
	params.NightGuards = 0
	s = newSim(t, params, at(monday, "03:00"))
	s.Advance(30 * time.Minute)
	if inside := s.Snapshot(0).Counts.Inside; inside != 0 {
		t.Fatalf("at 03:30 without guards inside=%d", inside)
	}
}

func TestWorkdayShape(t *testing.T) {
	params := sim.DefaultParameters()
	s := newSim(t, params, at(monday, "07:00"))
	check := func(clock string, minFraction, maxFraction float64) {
		s.SetTime(at(monday, clock))
		snap := s.Snapshot(0)
		fraction := float64(snap.Counts.Inside) / float64(snap.Counts.Population)
		if fraction < minFraction || fraction > maxFraction {
			t.Errorf("%s: %d of %d inside (%.2f), want between %.2f and %.2f",
				clock, snap.Counts.Inside, snap.Counts.Population, fraction, minFraction, maxFraction)
		}
	}
	check("07:30", 0, 0.05)
	check("10:30", 0.45, 1)
	check("12:30", 0.08, 0.9) // the lunch dip: half the staff and most students leave the building
	check("15:00", 0.4, 1)
	check("21:00", 0, 0.05)
}

func TestStaffWorkInTheirOffices(t *testing.T) {
	params := sim.DefaultParameters()
	s := newSim(t, params, at(monday, "07:00"))
	s.SetTime(at(monday, "10:45"))
	staff, atDesk := 0, 0
	for _, p := range s.People() {
		if p.Role != sim.RoleStaff {
			continue
		}
		staff++
		if p.State() == sim.StateInRoom && p.Room() == p.Office {
			atDesk++
		}
	}
	if staff == 0 || float64(atDesk)/float64(staff) < 0.5 {
		t.Fatalf("%d of %d staff at their desks at 10:45", atDesk, staff)
	}
}

func TestLecturesHaveAudience(t *testing.T) {
	params := sim.DefaultParameters()
	s := newSim(t, params, at(monday, "07:00"))
	s.SetTime(at(monday, "11:00"))
	snap := s.Snapshot(0)
	occupants := map[string]int{}
	for _, room := range snap.Rooms {
		occupants[room.Name] = room.Occupants
	}
	checked := 0
	for _, lecture := range snap.Lectures {
		if lecture.Start > 660 || lecture.End <= 660 || lecture.Students == 0 {
			continue
		}
		checked++
		if occupants[lecture.Room] == 0 {
			t.Errorf("lecture in %s (%d students) has no one present at 11:00", lecture.Room, lecture.Students)
		}
	}
	if checked == 0 {
		t.Fatal("no lecture with students was scheduled in the 10:15 slot")
	}
}

func TestWeekendHasFewerPeopleAndNoStudents(t *testing.T) {
	params := sim.DefaultParameters()
	s := newSim(t, params, at(saturday, "07:00"))
	s.SetTime(at(saturday, "10:30"))
	snap := s.Snapshot(0)
	expected := int(math.Round(float64(params.WeekdayPopulation)*params.WeekendFraction)) + params.NightGuards
	if snap.Counts.Population != expected {
		t.Fatalf("saturday population = %d, want %d", snap.Counts.Population, expected)
	}
	if snap.Counts.ByRole[sim.RoleStudent] != 0 || snap.Counts.ByRole[sim.RoleLecturer] != 0 {
		t.Fatalf("saturday has students or lecturers: %v", snap.Counts.ByRole)
	}
}

func TestMovementIsContinuous(t *testing.T) {
	params := sim.DefaultParameters()
	s := newSim(t, params, at(monday, "07:55"))
	step := 10 * time.Second
	maxUnits := params.WalkingSpeedMps / params.MetresPerUnit * step.Seconds()
	last := map[string][2]float64{}
	for i := 0; i < 6*60; i++ { // one hour
		for _, p := range s.People() {
			if p.State() == sim.StateOutside {
				delete(last, p.ID)
				continue
			}
			pos := p.Position()
			if prev, ok := last[p.ID]; ok {
				if d := math.Hypot(pos[0]-prev[0], pos[1]-prev[1]); d > maxUnits*1.01+1e-6 {
					t.Fatalf("%s jumped %.2f units in one step (max %.2f)", p.ID, d, maxUnits)
				}
			}
			last[p.ID] = pos
		}
		s.Advance(step)
	}
}

func TestPublishableMatchesBuildSimContract(t *testing.T) {
	params := sim.DefaultParameters()
	plan := loadPlan(t)
	s, err := sim.New([]sim.FloorPlan{plan}, params, at(monday, "07:00"))
	if err != nil {
		t.Fatal(err)
	}
	s.SetTime(at(monday, "10:00"))
	rooms := map[string]bool{}
	for _, room := range plan.Rooms {
		rooms[room.Name] = true
	}
	entities, occupancy := s.Publishable()
	if len(entities) == 0 || len(occupancy) == 0 {
		t.Fatal("nothing to publish at 10:00")
	}
	for _, entity := range entities {
		if entity.Position[0] < 0 || entity.Position[1] < 0 || entity.Position[0] > plan.Page[0] || entity.Position[1] > plan.Page[1] {
			t.Errorf("entity %s outside the page: %v", entity.ID, entity.Position)
		}
		if entity.Room != "" && !rooms[entity.Room] {
			t.Errorf("entity %s names unknown room %q", entity.ID, entity.Room)
		}
		if entity.Type != "man" && entity.Type != "woman" {
			t.Errorf("entity %s has type %q", entity.ID, entity.Type)
		}
	}
	for key, entry := range occupancy {
		if len(key) < len("level0/")+1 || key[:7] != "level0/" || !rooms[key[7:]] {
			t.Errorf("occupancy key %q does not name a room", key)
		}
		if entry.Aliens == nil || len(entry.Persons) == 0 {
			t.Errorf("occupancy %q is malformed: %+v", key, entry)
		}
	}
}

func TestSameSeedSameDay(t *testing.T) {
	params := sim.DefaultParameters()
	a := newSim(t, params, at(monday, "07:00"))
	b := newSim(t, params, at(monday, "07:00"))
	a.SetTime(at(monday, "13:37"))
	b.SetTime(at(monday, "13:37"))
	ea, _ := a.Publishable()
	eb, _ := b.Publishable()
	if len(ea) != len(eb) {
		t.Fatalf("different entity counts: %d vs %d", len(ea), len(eb))
	}
	for i := range ea {
		if ea[i] != eb[i] {
			t.Fatalf("entity %d differs: %+v vs %+v", i, ea[i], eb[i])
		}
	}
	// Jumping backwards regenerates the day and must land in the same state.
	a.SetTime(at(monday, "09:00"))
	a.SetTime(at(monday, "13:37"))
	ec, _ := a.Publishable()
	if len(ec) != len(ea) {
		t.Fatalf("replayed day differs: %d vs %d entities", len(ec), len(ea))
	}
}

func TestSetParametersRegeneratesTheDay(t *testing.T) {
	params := sim.DefaultParameters()
	s := newSim(t, params, at(monday, "07:00"))
	s.SetTime(at(monday, "10:30"))
	params.WeekdayPopulation = 40
	if err := s.SetParameters(params); err != nil {
		t.Fatal(err)
	}
	snap := s.Snapshot(0)
	if snap.Counts.Population != 40+params.NightGuards || snap.Clock.TimeOfDay != "10:30" {
		t.Fatalf("after SetParameters: population=%d clock=%s", snap.Counts.Population, snap.Clock.TimeOfDay)
	}
	if snap.Counts.Inside == 0 {
		t.Fatal("regenerated day was not simulated up to the current time")
	}
}

func TestParameterValidation(t *testing.T) {
	bad := sim.DefaultParameters()
	bad.LunchStart = "07:00"
	if err := bad.Normalize(); err == nil {
		t.Fatal("lunch before arrival was accepted")
	}
	bad = sim.DefaultParameters()
	bad.ArriveEnd = "9:30"
	if err := bad.Normalize(); err != nil {
		t.Fatalf("H:MM should parse: %v", err)
	}
	bad = sim.DefaultParameters()
	bad.StudentShare = 1.5
	if err := bad.Normalize(); err == nil {
		t.Fatal("student_share above 1 was accepted")
	}
	bad = sim.DefaultParameters()
	bad.LectureFillTarget = 1.5
	if err := bad.Normalize(); err == nil {
		t.Fatal("lecture_fill_target above 1 was accepted")
	}
	empty := sim.Parameters{}
	if err := empty.Normalize(); err != nil {
		t.Fatalf("empty parameters should normalise to defaults: %v", err)
	}
	if empty.WeekdayPopulation != 0 || empty.ArriveStart != "08:00" {
		t.Fatalf("unexpected normalised values: %+v", empty)
	}
}

func TestDefaultEntrancesSnapToTheRightNodes(t *testing.T) {
	s := newSim(t, sim.DefaultParameters(), at(monday, "07:00"))
	got := map[string]string{}
	for _, e := range s.Snapshot(0).Entrances {
		got[e.Name] = e.Node
	}
	want := map[string]string{
		"Main entrance (north), hall A1016":   "A1016",
		"East entrance, foyer A1123":          "A1500",
		"North-east entrance, vestibule A105": "A105",
		"South entrance, vestibule A10":       "A10",
		"North-east corner, corridor A1000A":  "A1000A",
		"North-west corner, corridor A1000E":  "A1000E",
		"South-west entrance, corridor A170":  "A183",
	}
	if len(got) != len(want) {
		t.Fatalf("%d entrances, want %d: %v", len(got), len(want), got)
	}
	for name, node := range want {
		if got[name] != node {
			t.Errorf("%s snapped to %q, want %q", name, got[name], node)
		}
	}
	for _, room := range s.Rooms() {
		if (room.Name == "A1123" || room.Name == "A105") && room.Role != sim.RoleUnused {
			t.Errorf("%s should be excluded from the room pool, got %s", room.Name, room.Role)
		}
	}
}

func TestPeopleUseSeveralEntrances(t *testing.T) {
	s := newSim(t, sim.DefaultParameters(), at(monday, "07:59"))
	used := map[string]bool{}
	entrances := map[string]bool{}
	for _, e := range s.Snapshot(0).Entrances {
		entrances[e.Node] = true
	}
	for i := 0; i < 12*60; i++ { // 08:00 to 10:00 in 10 s steps
		for _, p := range s.People() {
			if p.State() == sim.StateWalking && entrances[p.Room()] {
				used[p.Room()] = true
			}
		}
		s.Advance(10 * time.Second)
	}
	if len(used) < 3 {
		t.Fatalf("only %d entrances were used during the morning: %v", len(used), used)
	}
}

func TestEntranceFallbackAndValidation(t *testing.T) {
	params := sim.DefaultParameters()
	params.Entrances = []sim.Entrance{}
	s := newSim(t, params, at(monday, "07:00"))
	entrances := s.Snapshot(0).Entrances
	if len(entrances) != 1 || entrances[0].Node != "A1016" {
		t.Fatalf("fallback entrance = %+v, want the largest corridor A1016", entrances)
	}
	params.Entrances = []sim.Entrance{{Name: "off the page", Position: [2]float64{9999, 9999}}}
	if _, err := sim.New(loadPlans(t, "level0"), params, monday); err == nil {
		t.Fatal("an entrance outside the floor plan was accepted")
	}
	params.Entrances = []sim.Entrance{{Position: [2]float64{-1, 5}}}
	if err := params.Normalize(); err == nil {
		t.Fatal("a negative entrance position was accepted")
	}
}

func TestPopulationIsSpreadOverEveryFloor(t *testing.T) {
	params := sim.DefaultParameters()
	s := newBuilding(t, params, at(monday, "07:00"), loadPlans(t, "level0", "level1")...)
	s.SetTime(at(monday, "10:30"))
	snap := s.Snapshot(0)
	if len(snap.Levels) != 2 {
		t.Fatalf("levels = %v", snap.Levels)
	}
	total := 0
	for _, level := range snap.Levels {
		total += snap.Counts.ByLevel[level]
		if snap.Counts.InsideByLevel[level] < 20 {
			t.Errorf("only %d people inside on %s at 10:30", snap.Counts.InsideByLevel[level], level)
		}
	}
	if total != snap.Counts.Population {
		t.Fatalf("floors hold %d people, population is %d", total, snap.Counts.Population)
	}
	if snap.Counts.Population != params.WeekdayPopulation+params.NightGuards {
		t.Fatalf("population = %d, want %d", snap.Counts.Population, params.WeekdayPopulation+params.NightGuards)
	}
	// Every floor must have its own rooms, lectures, and entrances.
	for _, level := range snap.Levels {
		rooms, lectures, entrances := 0, 0, 0
		for _, room := range snap.Rooms {
			if room.Level == level {
				rooms++
			}
		}
		for _, lecture := range snap.Lectures {
			if lecture.Level == level {
				lectures++
			}
		}
		for _, e := range snap.Entrances {
			if e.Level == level {
				entrances++
			}
		}
		if rooms == 0 || lectures == 0 || entrances == 0 {
			t.Errorf("%s has %d rooms, %d lectures, %d entrances", level, rooms, lectures, entrances)
		}
	}
	// The upper floor has no configured entrance and falls back to its largest
	// corridor.
	for _, e := range snap.Entrances {
		if e.Level == "level1" && e.Node != "A1016" {
			t.Errorf("level1 fallback entrance = %+v", e)
		}
	}
}

func TestOfficesHoldOnePerson(t *testing.T) {
	s := newSim(t, sim.DefaultParameters(), at(monday, "07:00"))
	s.SetTime(at(monday, "10:45"))
	for _, room := range s.Snapshot(0).Rooms {
		if room.Role != sim.RoleOffice {
			continue
		}
		if room.Capacity != 1 {
			t.Fatalf("office %s has capacity %d", room.Name, room.Capacity)
		}
		if room.Occupants > 1 {
			t.Errorf("office %s holds %d people at 10:45", room.Name, room.Occupants)
		}
	}
	offices := map[string]int{}
	for _, p := range s.People() {
		if p.Office != "" {
			offices[p.Level+"/"+p.Office]++
		}
	}
	for office, people := range offices {
		if people > 1 {
			t.Errorf("%d people share office %s", people, office)
		}
	}
}

func TestStudentsConcentrateInLectureRooms(t *testing.T) {
	s := newSim(t, sim.DefaultParameters(), at(monday, "07:00"))
	s.SetTime(at(monday, "11:00"))
	snap := s.Snapshot(0)
	lectureRooms := 0
	for _, room := range snap.Rooms {
		if room.Role == sim.RoleLecture {
			lectureRooms++
		}
	}
	students, running, biggest := 0, 0, 0
	for _, lecture := range snap.Lectures {
		if lecture.Start > 660 || lecture.End <= 660 {
			continue
		}
		running++
		students += lecture.Students
		biggest = max(biggest, lecture.Students)
		if lecture.Students == 0 {
			t.Errorf("lecture in %s is held for nobody", lecture.Room)
		}
		if lecture.Students > lecture.Seats {
			t.Errorf("lecture in %s has %d students in %d seats", lecture.Room, lecture.Students, lecture.Seats)
		}
	}
	if running == 0 {
		t.Fatal("no lecture is running at 11:00")
	}
	if running > lectureRooms/4 {
		t.Errorf("%d of %d lecture rooms are in use at once; students are not concentrated", running, lectureRooms)
	}
	if average := students / running; average < 15 {
		t.Errorf("average lecture has %d students (%d in %d lectures)", average, students, running)
	}
	if biggest < 20 {
		t.Errorf("the largest lecture has only %d students", biggest)
	}
}

func TestRoomOccupantsDoNotStandOnTopOfEachOther(t *testing.T) {
	s := newSim(t, sim.DefaultParameters(), at(monday, "07:00"))
	s.SetTime(at(monday, "11:00"))
	spots := map[string]map[[2]float64]string{}
	busiest := 0
	for _, p := range s.People() {
		if p.State() != sim.StateInRoom {
			continue
		}
		key := p.Level + "/" + p.Room()
		if spots[key] == nil {
			spots[key] = map[[2]float64]string{}
		}
		if other, taken := spots[key][p.Position()]; taken {
			t.Errorf("%s and %s stand on the same spot %v in %s", other, p.ID, p.Position(), key)
		}
		spots[key][p.Position()] = p.ID
		busiest = max(busiest, len(spots[key]))
	}
	if busiest < 20 {
		t.Fatalf("the busiest room holds %d people; the test says nothing about crowds", busiest)
	}
}
