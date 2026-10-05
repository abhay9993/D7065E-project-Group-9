package sim

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"time"
)

// Role of a simulated person. Staff and lecturers have offices; students only
// attend lectures; guards patrol at night.
type Role string

const (
	RoleStaff    Role = "staff"
	RoleLecturer Role = "lecturer"
	RoleStudent  Role = "student"
	RoleGuard    Role = "guard"
)

// Activity is one entry of a person's daily timeline: be in Room from Start to
// End (minutes after midnight). An empty Room means outside the building. Gaps
// between activities also mean outside.
type Activity struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Room  string  `json:"room"`
	Kind  string  `json:"kind"` // office, lecture, fika, lunch, patrol
}

// Lecture is one booked slot in a lecture room.
type Lecture struct {
	Level    string  `json:"level"`
	Room     string  `json:"room"`
	Start    float64 `json:"start"`
	End      float64 `json:"end"`
	Lecturer string  `json:"lecturer"`
	Students int     `json:"students"`
	Seats    int     `json:"seats"`
}

var menNames = []string{"Anders", "Erik", "Johan", "Lars", "Mikael", "Oskar", "Per", "Karl", "Nils", "Simon", "Ali", "David", "Emil", "Hugo", "Viktor", "Leo", "Adam", "Samuel", "Elias", "Oliver"}
var womenNames = []string{"Anna", "Maria", "Sara", "Elin", "Karin", "Emma", "Lisa", "Eva", "Ida", "Julia", "Fatima", "Hanna", "Linnea", "Maja", "Alva", "Ebba", "Nora", "Astrid", "Wilma", "Signe"}
var surnames = []string{"Andersson", "Johansson", "Karlsson", "Nilsson", "Eriksson", "Larsson", "Olsson", "Persson", "Svensson", "Gustafsson", "Pettersson", "Jonsson", "Jansson", "Hansson", "Bengtsson", "Lindberg", "Lindström", "Berg", "Nyström", "Sandström"}

type planner struct {
	p      *Parameters
	rng    *rand.Rand
	g      *Graph
	rooms  map[string]*Room
	byRole map[RoomRole][]*Room

	arriveStart, arriveEnd float64
	lunchStart, lunchEnd   float64
	leaveStart, leaveEnd   float64
	slots                  [][2]float64

	fikaCache map[int]string
}

func newPlanner(p *Parameters, rooms []Room, g *Graph, seed int64) *planner {
	pl := &planner{
		p: p, rng: rand.New(rand.NewSource(seed)), g: g,
		rooms: map[string]*Room{}, byRole: map[RoomRole][]*Room{}, fikaCache: map[int]string{},
	}
	for i := range rooms {
		room := &rooms[i]
		pl.rooms[room.Name] = room
		pl.byRole[room.Role] = append(pl.byRole[room.Role], room)
	}
	pl.arriveStart, _ = ParseClock(p.ArriveStart)
	pl.arriveEnd, _ = ParseClock(p.ArriveEnd)
	pl.lunchStart, _ = ParseClock(p.LunchStart)
	pl.lunchEnd, _ = ParseClock(p.LunchEnd)
	pl.leaveStart, _ = ParseClock(p.LeaveStart)
	pl.leaveEnd, _ = ParseClock(p.LeaveEnd)
	for _, slot := range p.LectureSlots {
		start, end, _ := ParseSlot(slot)
		pl.slots = append(pl.slots, [2]float64{start, end})
	}
	sort.Slice(pl.slots, func(a, b int) bool { return pl.slots[a][0] < pl.slots[b][0] })
	return pl
}

func (pl *planner) uniform(lo, hi float64) float64 {
	if hi <= lo {
		return lo
	}
	return lo + pl.rng.Float64()*(hi-lo)
}

func (pl *planner) chance(p float64) bool { return pl.rng.Float64() < p }

// nearestFika finds the fika room with the shortest walk from a node.
func (pl *planner) nearestFika(from int) string {
	if name, ok := pl.fikaCache[from]; ok {
		return name
	}
	best, bestLen := "", math.Inf(1)
	for _, room := range pl.byRole[RoleFika] {
		route := pl.g.Route(from, room.Node)
		if route == nil {
			continue
		}
		if l := pl.g.Length(route); l < bestLen {
			best, bestLen = room.Name, l
		}
	}
	pl.fikaCache[from] = best
	return best
}

func (pl *planner) randomRoom(roles ...RoomRole) *Room {
	var pool []*Room
	for _, role := range roles {
		pool = append(pool, pl.byRole[role]...)
	}
	if len(pool) == 0 {
		return nil
	}
	return pool[pl.rng.Intn(len(pool))]
}

// generate creates one floor's population and lecture timetable for one date.
// The quota comes from the building-wide population split over the floors.
func (pl *planner) generate(date time.Time, level string, q quota) ([]*Person, []Lecture) {
	weekend := date.Weekday() == time.Saturday || date.Weekday() == time.Sunday

	var people []*Person
	newPerson := func(role Role, index int) *Person {
		gender := "man"
		first := menNames[pl.rng.Intn(len(menNames))]
		if pl.chance(0.5) {
			gender = "woman"
			first = womenNames[pl.rng.Intn(len(womenNames))]
		}
		return &Person{
			ID:     fmt.Sprintf("%s-%s-%d", level, role, index),
			Name:   fmt.Sprintf("%s %s", first, surnames[pl.rng.Intn(len(surnames))]),
			Gender: gender,
			Role:   role,
			Level:  level,
			node:   -1,
		}
	}

	// Offices hold one person, and are handed out in name order; when the floor
	// is fuller than it has offices the assignment wraps around and offices are
	// shared.
	offices := pl.byRole[RoleOffice]
	deskRoom := func(desk int) string {
		if len(offices) == 0 {
			return ""
		}
		totalDesks := 0
		for _, office := range offices {
			totalDesks += office.Capacity
		}
		desk %= max(totalDesks, 1)
		for _, office := range offices {
			if desk < office.Capacity {
				return office.Name
			}
			desk -= office.Capacity
		}
		return offices[0].Name
	}
	desk := 0
	var staffPeople, lecturerPeople, studentPeople []*Person
	for i := 0; i < q.staff; i++ {
		person := newPerson(RoleStaff, i+1)
		person.Office = deskRoom(desk)
		desk++
		staffPeople = append(staffPeople, person)
	}
	for i := 0; i < q.lecturers; i++ {
		person := newPerson(RoleLecturer, i+1)
		person.Office = deskRoom(desk)
		desk++
		lecturerPeople = append(lecturerPeople, person)
	}
	for i := 0; i < q.students; i++ {
		studentPeople = append(studentPeople, newPerson(RoleStudent, i+1))
	}

	lectures := pl.bookLectures(level, len(studentPeople))
	bySlot := map[int][]int{}
	for i, lecture := range lectures {
		for si, slot := range pl.slots {
			if slot[0] == lecture.Start {
				bySlot[si] = append(bySlot[si], i)
			}
		}
	}

	// Students fill one lecture room before the next one is used, so a lecture
	// looks like a lecture instead of a handful of people in an empty hall.
	attendance := map[*Person][]Activity{}
	usable := []int{}
	for si := range pl.slots {
		if len(bySlot[si]) > 0 {
			usable = append(usable, si)
		}
	}
	for _, person := range studentPeople {
		count := pl.p.LecturesPerStudentMin
		if pl.p.LecturesPerStudentMax > count {
			count += pl.rng.Intn(pl.p.LecturesPerStudentMax - count + 1)
		}
		slots := append([]int(nil), usable...)
		pl.rng.Shuffle(len(slots), func(a, b int) { slots[a], slots[b] = slots[b], slots[a] })
		if count > len(slots) {
			count = len(slots)
		}
		for _, si := range slots[:count] {
			li := fullestWithRoom(lectures, bySlot[si])
			lectures[li].Students++
			l := lectures[li]
			attendance[person] = append(attendance[person], Activity{Start: l.Start - 10, End: l.End, Room: l.Room, Kind: "lecture"})
		}
	}

	// A lecture nobody signed up for is not held.
	kept := lectures[:0]
	for _, lecture := range lectures {
		if lecture.Students > 0 {
			kept = append(kept, lecture)
		}
	}
	lectures = kept

	// Lecturers take the remaining lectures round-robin.
	lecturerLectures := map[*Person][]int{}
	if len(lecturerPeople) > 0 {
		for i := range lectures {
			lecturer := lecturerPeople[i%len(lecturerPeople)]
			lectures[i].Lecturer = lecturer.Name
			lecturerLectures[lecturer] = append(lecturerLectures[lecturer], i)
		}
	}

	for _, person := range staffPeople {
		person.Plan = pl.staffPlan(person, weekend, nil)
		people = append(people, person)
	}
	for _, person := range lecturerPeople {
		var own []Activity
		for _, li := range lecturerLectures[person] {
			l := lectures[li]
			own = append(own, Activity{Start: l.Start - 15, End: l.End, Room: l.Room, Kind: "lecture"})
		}
		person.Plan = pl.staffPlan(person, weekend, own)
		people = append(people, person)
	}
	for _, person := range studentPeople {
		person.Plan = pl.studentPlan(attendance[person])
		people = append(people, person)
	}
	for i := 0; i < q.guards; i++ {
		person := newPerson(RoleGuard, i+1)
		person.Plan = pl.guardPlan()
		people = append(people, person)
	}
	sort.Slice(lectures, func(a, b int) bool {
		if lectures[a].Start != lectures[b].Start {
			return lectures[a].Start < lectures[b].Start
		}
		return lectures[a].Room < lectures[b].Room
	})
	return people, lectures
}

// bookLectures decides which lecture rooms host a lecture in each slot. Rooms
// are drawn at random but only until their seats cover the students expected in
// that slot, so the day's students gather in a few well-filled rooms instead of
// spreading one or two per room over every lecture room on the floor.
// LectureRoomUtilisation caps how much of the floor may be booked in one slot,
// and LectureFillTarget says how full a booked room should end up.
func (pl *planner) bookLectures(level string, students int) []Lecture {
	rooms := pl.byRole[RoleLecture]
	if len(rooms) == 0 || len(pl.slots) == 0 || students == 0 {
		return nil
	}
	perStudent := float64(pl.p.LecturesPerStudentMin+pl.p.LecturesPerStudentMax) / 2
	expected := float64(students) * perStudent / float64(len(pl.slots))
	seatsNeeded := expected / pl.p.LectureFillTarget
	maxRooms := max(1, int(math.Round(pl.p.LectureRoomUtilisation*float64(len(rooms)))))

	var lectures []Lecture
	for _, slot := range pl.slots {
		seats, booked := 0.0, 0
		for _, idx := range pl.rng.Perm(len(rooms)) {
			if booked >= maxRooms || seats >= seatsNeeded {
				break
			}
			room := rooms[idx]
			lectures = append(lectures, Lecture{
				Level: level, Room: room.Name, Start: slot[0], End: slot[1], Seats: room.Capacity,
			})
			seats += float64(room.Capacity)
			booked++
		}
	}
	return lectures
}

// fullestWithRoom returns the lecture that is closest to full while still
// having a free seat, so students pack one room before starting the next. When
// every lecture in the slot is full the emptiest one takes the overflow.
func fullestWithRoom(lectures []Lecture, candidates []int) int {
	best, fallback := -1, candidates[0]
	for _, li := range candidates {
		if lectures[li].Students < lectures[fallback].Students {
			fallback = li
		}
		if lectures[li].Students >= lectures[li].Seats {
			continue
		}
		if best < 0 || lectures[li].Students > lectures[best].Students {
			best = li
		}
	}
	if best < 0 {
		return fallback
	}
	return best
}

// staffPlan builds an office-based day: arrive, work in the office, with
// optional fika breaks, lunch, and (for lecturers) fixed lecture slots.
func (pl *planner) staffPlan(person *Person, weekend bool, fixed []Activity) []Activity {
	arrive := pl.uniform(pl.arriveStart, pl.arriveEnd)
	leave := pl.uniform(pl.leaveStart, pl.leaveEnd)
	if weekend {
		leave = pl.uniform(math.Min(arrive+120, pl.leaveEnd), pl.leaveEnd)
	}
	timeline := append([]Activity(nil), fixed...)
	sort.Slice(timeline, func(a, b int) bool { return timeline[a].Start < timeline[b].Start })
	if len(timeline) > 0 {
		arrive = math.Min(arrive, timeline[0].Start-5)
		leave = math.Max(leave, timeline[len(timeline)-1].End+5)
	}
	officeNode := -1
	if office, ok := pl.rooms[person.Office]; ok {
		officeNode = office.Node
	}
	fika := pl.nearestFika(officeNode)

	if fika != "" {
		for _, centre := range []float64{585, 870} { // 09:45 and 14:30
			if !pl.chance(pl.p.FikaProbability) {
				continue
			}
			start := centre + pl.rng.NormFloat64()*10
			act := Activity{Start: start, End: start + float64(pl.p.FikaMinutes), Room: fika, Kind: "fika"}
			if act.Start >= arrive+15 && act.End <= leave-15 {
				timeline, _ = insertFree(timeline, act)
			}
		}
	}
	lunchStart := pl.uniform(pl.lunchStart, math.Max(pl.lunchStart, pl.lunchEnd-float64(pl.p.LunchMinutes)))
	lunch := Activity{Start: lunchStart, End: lunchStart + float64(pl.p.LunchMinutes), Room: fika, Kind: "lunch"}
	if pl.chance(pl.p.LunchOutProbability) || fika == "" {
		lunch.Room = "" // leaves the building
	}
	if lunch.Start >= arrive && lunch.End <= leave {
		timeline, _ = insertFree(timeline, lunch)
	}
	return fillGaps(timeline, arrive, leave, person.Office, "office")
}

// studentPlan chains lectures; short gaps are spent in the nearest fika room,
// long gaps and the lunch hour outside the building.
func (pl *planner) studentPlan(attend []Activity) []Activity {
	if len(attend) == 0 {
		return nil
	}
	sort.Slice(attend, func(a, b int) bool { return attend[a].Start < attend[b].Start })
	var timeline []Activity
	for i, lecture := range attend {
		timeline = append(timeline, lecture)
		if i+1 >= len(attend) {
			break
		}
		next := attend[i+1]
		gap := next.Start - lecture.End
		coversLunch := lecture.End <= pl.lunchStart+30 && next.Start >= pl.lunchEnd-30
		if gap <= 0 || (coversLunch && gap >= 45) || gap > 90 {
			continue
		}
		room, ok := pl.rooms[next.Room]
		if !ok {
			continue
		}
		if fika := pl.nearestFika(room.Node); fika != "" {
			timeline = append(timeline, Activity{Start: lecture.End, End: next.Start, Room: fika, Kind: "fika"})
		}
	}
	last := attend[len(attend)-1]
	if room, ok := pl.rooms[last.Room]; ok && pl.chance(pl.p.FikaProbability) {
		if fika := pl.nearestFika(room.Node); fika != "" {
			timeline = append(timeline, Activity{Start: last.End, End: last.End + float64(pl.p.FikaMinutes), Room: fika, Kind: "fika"})
		}
	}
	return timeline
}

// guardPlan patrols offices and lecture rooms outside working hours.
func (pl *planner) guardPlan() []Activity {
	var timeline []Activity
	patrol := func(from, to float64) {
		for t := from; t+5 <= to; t += 8 {
			room := pl.randomRoom(RoleOffice, RoleLecture)
			if room == nil {
				return
			}
			timeline = append(timeline, Activity{Start: t, End: t + 5, Room: room.Name, Kind: "patrol"})
		}
	}
	patrol(0, pl.arriveStart)
	patrol(pl.leaveEnd, 1440)
	return timeline
}

func overlaps(a, b Activity) bool { return a.Start < b.End && b.Start < a.End }

func insertFree(list []Activity, act Activity) ([]Activity, bool) {
	for _, existing := range list {
		if overlaps(existing, act) {
			return list, false
		}
	}
	list = append(list, act)
	sort.Slice(list, func(a, b int) bool { return list[a].Start < list[b].Start })
	return list, true
}

// fillGaps completes a timeline between arrive and leave with filler activities.
func fillGaps(fixed []Activity, arrive, leave float64, room, kind string) []Activity {
	sort.Slice(fixed, func(a, b int) bool { return fixed[a].Start < fixed[b].Start })
	var out []Activity
	cursor := arrive
	for _, act := range fixed {
		if act.Start > cursor && room != "" {
			out = append(out, Activity{Start: cursor, End: act.Start, Room: room, Kind: kind})
		}
		out = append(out, act)
		cursor = math.Max(cursor, act.End)
	}
	if leave > cursor && room != "" {
		out = append(out, Activity{Start: cursor, End: leave, Room: room, Kind: kind})
	}
	return out
}
