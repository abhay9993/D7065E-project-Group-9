package sim

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"
)

type State string

const (
	StateOutside State = "outside"
	StateWalking State = "walking"
	StateInRoom  State = "in_room"
)

// Person is one simulated occupant with a daily plan and a movement state. A
// person belongs to one floor for the whole day: BuildSim's walkable graphs are
// per level and carry no stairs, so nobody changes level.
type Person struct {
	ID     string
	Name   string
	Gender string // man or woman, selects the viewer icon
	Role   Role
	Level  string
	Office string
	Plan   []Activity

	state       State
	targetRoom  string
	targetKind  string
	node        int // last node reached on the walkable graph
	pos         [2]float64
	heading     float64
	route       []int
	routeIndex  int
	segProgress float64
	roomName    string // room or corridor the person is currently in
	seatRoom    string // room whose place the person holds, empty when none
	seat        int
	seatOffset  [2]float64 // where in that room the person stands, from its node
}

func (p *Person) activityAt(minute float64) *Activity {
	for i := range p.Plan {
		if minute >= p.Plan[i].Start && minute < p.Plan[i].End {
			return &p.Plan[i]
		}
	}
	return nil
}

// Sample is one point of the daily occupancy curve for the whole building.
type Sample struct {
	Minute    int `json:"minute"`
	Inside    int `json:"inside"`
	Staff     int `json:"staff"`
	Lecturers int `json:"lecturers"`
	Students  int `json:"students"`
	Guards    int `json:"guards"`
}

// floor is one simulated level: its plan, walkable graph, classified rooms,
// entrances, and the people whose day happens on it.
type floor struct {
	plan         FloorPlan
	graph        *Graph
	rooms        []Room
	roomByName   map[string]*Room
	entrances    []entrance
	planner      *planner
	people       []*Person
	lectures     []Lecture
	seats        map[string]map[int]bool // room -> places taken by an occupant or someone on the way
	offices      int
	lectureSeats int // total lecture-room seats, used to spread students over floors
}

// Simulation advances a population over the walkable floors of one building on
// a simulated clock.
type Simulation struct {
	mu sync.Mutex

	plans  []FloorPlan
	params Parameters
	floors []*floor

	now     time.Time
	factor  float64
	running bool
	day     time.Time

	people           []*Person // every person on every floor, for counts and snapshots
	samples          []Sample
	lastSampleMinute int
}

const maxStepSeconds = 10.0

// entrance is a configured entrance snapped to the walkable graph.
type entrance struct {
	Name     string
	Position [2]float64
	Node     int
}

// New builds a simulation from the floor plans of a building, validated
// parameters, and a simulated start time. The population is spread over all
// floors; the first plan is the primary level, which owns entrances that name
// no level of their own.
func New(plans []FloorPlan, params Parameters, start time.Time) (*Simulation, error) {
	if len(plans) == 0 {
		return nil, errors.New("no floor plans to simulate")
	}
	if err := params.Normalize(); err != nil {
		return nil, err
	}
	s := &Simulation{plans: plans, factor: 60, running: true, lastSampleMinute: -1}
	if err := s.configure(params); err != nil {
		return nil, err
	}
	s.now = start
	s.regenerate(start)
	return s, nil
}

func (s *Simulation) configure(params Parameters) error {
	floors := make([]*floor, 0, len(s.plans))
	for i, plan := range s.plans {
		f, err := newFloor(plan, params, i == 0)
		if err != nil {
			return err
		}
		floors = append(floors, f)
	}
	// A room override has to name a room somewhere in the building; floors
	// that do not have that room simply ignore it.
	for name := range params.RoomRoles {
		found := false
		for _, f := range floors {
			if _, ok := f.roomByName[name]; ok {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("room_roles[%q]: no room with that name in the building", name)
		}
	}
	s.params = params
	s.floors = floors
	return nil
}

// newFloor classifies one level and snaps its entrances to the walkable graph.
func newFloor(plan FloorPlan, params Parameters, primary bool) (*floor, error) {
	graph := NewGraph(plan.Nodes, plan.Edges)
	rooms, err := ClassifyRooms(plan, graph, params)
	if err != nil {
		return nil, err
	}
	f := &floor{plan: plan, graph: graph, rooms: rooms, roomByName: make(map[string]*Room, len(rooms)), seats: map[string]map[int]bool{}}
	for i := range f.rooms {
		room := &f.rooms[i]
		f.roomByName[room.Name] = room
		switch room.Role {
		case RoleOffice:
			f.offices++
		case RoleLecture:
			f.lectureSeats += room.Capacity
		}
	}
	for _, configured := range params.Entrances {
		if configured.Level != plan.Level && !(configured.Level == "" && primary) {
			continue // an entrance on another floor of the building
		}
		node := graph.Nearest(configured.Position)
		if node < 0 {
			return nil, fmt.Errorf("entrance %q: no walkable node near %v", configured.Name, configured.Position)
		}
		if configured.Position[0] > plan.Page[0] || configured.Position[1] > plan.Page[1] {
			return nil, fmt.Errorf("entrance %q lies outside the %s floor plan", configured.Name, plan.Level)
		}
		f.entrances = append(f.entrances, entrance{Name: configured.Name, Position: configured.Position, Node: node})
	}
	if len(f.entrances) == 0 {
		// The largest corridor is assumed to be the lobby, or on an upper
		// floor the hall people reach the level through.
		best := -1.0
		for i := range f.rooms {
			if f.rooms[i].Role == RoleCorridor && f.rooms[i].Node >= 0 && f.rooms[i].AreaM2 > best {
				node := f.rooms[i].Node
				best = f.rooms[i].AreaM2
				f.entrances = []entrance{{Name: "Largest corridor " + f.rooms[i].Name, Position: [2]float64{graph.Nodes[node].X, graph.Nodes[node].Y}, Node: node}}
			}
		}
		if len(f.entrances) == 0 {
			return nil, fmt.Errorf("no corridor on %s can serve as the entrance", plan.Level)
		}
	}
	if f.offices == 0 {
		return nil, fmt.Errorf("classification produced no offices on %s; lower office_max_m2 or add room_roles", plan.Level)
	}
	return f, nil
}

// Levels lists the simulated floors in order; the first one is the primary
// level.
func (s *Simulation) Levels() []string {
	levels := make([]string, 0, len(s.plans))
	for _, plan := range s.plans {
		levels = append(levels, plan.Level)
	}
	return levels
}

func midnight(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func minuteOfDay(t time.Time) float64 {
	return float64(t.Hour()*60+t.Minute()) + float64(t.Second())/60 + float64(t.Nanosecond())/6e10
}

// regenerate creates a fresh population for the day containing t. The planner
// is reseeded per day so that a day is reproducible regardless of history.
func (s *Simulation) regenerate(t time.Time) {
	s.day = midnight(t)
	seed := s.params.Seed*1000003 + int64(s.day.YearDay()) + int64(s.day.Year())*367
	quotas := s.quotas(s.day)
	s.people = s.people[:0]
	for i, f := range s.floors {
		f.seats = map[string]map[int]bool{}
		f.planner = newPlanner(&s.params, f.rooms, f.graph, seed+int64(i)*7919)
		f.people, f.lectures = f.planner.generate(s.day, f.plan.Level, quotas[i])
		for _, p := range f.people {
			p.state = StateOutside
			p.node = -1
		}
		s.people = append(s.people, f.people...)
	}
	s.samples = s.samples[:0]
	s.lastSampleMinute = -1
}

// quota is one floor's share of the building population for a day.
type quota struct {
	staff, lecturers, students, guards int
}

// quotas splits the building population over the floors: office workers follow
// the number of offices, students the lecture seats, and guards are spread
// evenly. The parts add up exactly to the configured population.
func (s *Simulation) quotas(day time.Time) []quota {
	weekend := day.Weekday() == time.Saturday || day.Weekday() == time.Sunday
	total := s.params.WeekdayPopulation
	if weekend {
		total = int(math.Round(float64(total) * s.params.WeekendFraction))
	}
	students, lecturers := 0, 0
	if !weekend {
		students = int(math.Round(float64(total) * s.params.StudentShare))
		lecturers = int(math.Round(float64(total-students) * s.params.LecturerShare))
	}
	staff := total - students - lecturers

	offices := make([]int, len(s.floors))
	seats := make([]int, len(s.floors))
	even := make([]int, len(s.floors))
	for i, f := range s.floors {
		offices[i], seats[i], even[i] = f.offices, f.lectureSeats, 1
	}
	out := make([]quota, len(s.floors))
	for i, n := range spread(staff, offices) {
		out[i].staff = n
	}
	for i, n := range spread(lecturers, offices) {
		out[i].lecturers = n
	}
	for i, n := range spread(students, seats) {
		out[i].students = n
	}
	for i, n := range spread(s.params.NightGuards, even) {
		out[i].guards = n
	}
	return out
}

// spread divides total over len(weights) parts in proportion to the weights,
// giving the remainder to the largest fractions so that the parts sum to total.
// Weights that are all zero are treated as equal.
func spread(total int, weights []int) []int {
	out := make([]int, len(weights))
	if len(weights) == 0 || total <= 0 {
		return out
	}
	w := make([]float64, len(weights))
	sum := 0.0
	for i, weight := range weights {
		if weight > 0 {
			w[i] = float64(weight)
			sum += w[i]
		}
	}
	if sum == 0 {
		for i := range w {
			w[i] = 1
		}
		sum = float64(len(w))
	}
	type part struct {
		index int
		frac  float64
	}
	parts := make([]part, len(w))
	assigned := 0
	for i := range w {
		exact := float64(total) * w[i] / sum
		out[i] = int(exact)
		assigned += out[i]
		parts[i] = part{i, exact - float64(out[i])}
	}
	sort.SliceStable(parts, func(a, b int) bool { return parts[a].frac > parts[b].frac })
	for i := 0; assigned < total; i, assigned = i+1, assigned+1 {
		out[parts[i%len(parts)].index]++
	}
	return out
}

// Advance moves the simulated clock forward by a simulated duration.
func (s *Simulation) Advance(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.advanceLocked(d)
}

func (s *Simulation) advanceLocked(d time.Duration) {
	target := s.now.Add(d)
	for s.now.Before(target) {
		step := min(target.Sub(s.now), time.Duration(maxStepSeconds*float64(time.Second)))
		next := s.now.Add(step)
		if midnight(next) != s.day {
			s.now = midnight(next)
			s.regenerate(s.now)
			continue
		}
		minute := minuteOfDay(s.now)
		units := s.params.WalkingSpeedMps / s.params.MetresPerUnit * step.Seconds()
		for _, f := range s.floors {
			for _, p := range f.people {
				s.stepPerson(f, p, minute, units)
			}
		}
		s.now = next
		if whole := int(minuteOfDay(s.now)); whole != s.lastSampleMinute {
			s.lastSampleMinute = whole
			s.samples = append(s.samples, s.sample(whole))
		}
	}
}

func (s *Simulation) sample(minute int) Sample {
	sample := Sample{Minute: minute}
	for _, p := range s.people {
		if p.state == StateOutside {
			continue
		}
		sample.Inside++
		switch p.Role {
		case RoleStaff:
			sample.Staff++
		case RoleLecturer:
			sample.Lecturers++
		case RoleStudent:
			sample.Students++
		case RoleGuard:
			sample.Guards++
		}
	}
	return sample
}

func (s *Simulation) stepPerson(f *floor, p *Person, minute float64, units float64) {
	targetRoom, targetKind := "", ""
	if act := p.activityAt(minute); act != nil {
		targetRoom, targetKind = act.Room, act.Kind
	}
	if targetRoom != p.targetRoom || targetKind != p.targetKind {
		p.targetRoom, p.targetKind = targetRoom, targetKind
		s.startWalking(f, p)
	}
	if p.state == StateWalking {
		s.walk(f, p, units)
	}
}

// destination resolves the current target to a graph node. Leaving the
// building means walking to the entrance nearest the person's current node.
func (s *Simulation) destination(f *floor, p *Person) int {
	if p.targetRoom == "" {
		return s.nearestEntrance(f, p.node)
	}
	room, ok := f.roomByName[p.targetRoom]
	if !ok || room.Node < 0 {
		return s.nearestEntrance(f, p.node)
	}
	return room.Node
}

// nearestEntrance returns the entrance node with the shortest walk from a node.
func (s *Simulation) nearestEntrance(f *floor, from int) int {
	best, bestLen := f.entrances[0].Node, math.Inf(1)
	if from < 0 {
		return best
	}
	for _, e := range f.entrances {
		route := f.graph.Route(from, e.Node)
		if route == nil {
			continue
		}
		if l := f.graph.Length(route); l < bestLen {
			best, bestLen = e.Node, l
		}
	}
	return best
}

// takeSeat reserves the lowest free place in a room. Places are reserved when
// someone sets off for the room and released when they leave it, so two people
// never stand on the same spot.
func (f *floor) takeSeat(room string) int {
	taken := f.seats[room]
	if taken == nil {
		taken = map[int]bool{}
		f.seats[room] = taken
	}
	for seat := 0; ; seat++ {
		if !taken[seat] {
			taken[seat] = true
			return seat
		}
	}
}

func (f *floor) freeSeat(room string, seat int) {
	if taken := f.seats[room]; taken != nil {
		delete(taken, seat)
	}
}

// seatOffset spreads the occupants of a room over a spiral around its graph
// node: the first one stands on the node itself and the rest fan out over the
// room, so a full lecture room does not render as one person standing in fifty.
func seatOffset(room *Room, seat int, metresPerUnit float64) [2]float64 {
	if seat <= 0 {
		return [2]float64{}
	}
	const goldenAngle = 2.399963229728653
	side := math.Sqrt(room.AreaM2) / metresPerUnit // the room's width in plan units
	radius := 0.4 * side * math.Sqrt(float64(seat)/float64(max(room.Capacity, seat+1)))
	angle := float64(seat) * goldenAngle
	return [2]float64{radius * math.Cos(angle), radius * math.Sin(angle)}
}

// place returns a position on the walkable graph, moved to the person's own
// spot when the node is the room they are heading for.
func (s *Simulation) place(f *floor, node int, offset [2]float64) [2]float64 {
	pos := s.nodePos(f, node)
	return [2]float64{
		math.Min(math.Max(pos[0]+offset[0], 0), f.plan.Page[0]),
		math.Min(math.Max(pos[1]+offset[1], 0), f.plan.Page[1]),
	}
}

// waypoint is the position of one node of a route; the last one is the
// person's place in the room they are walking to.
func (s *Simulation) waypoint(f *floor, p *Person, index int) [2]float64 {
	if index == len(p.route)-1 {
		return s.place(f, p.route[index], p.seatOffset)
	}
	return s.nodePos(f, p.route[index])
}

// reserve gives up the place the person held and takes one in the room they are
// setting off for.
func (s *Simulation) reserve(f *floor, p *Person) {
	if p.seatRoom != "" {
		f.freeSeat(p.seatRoom, p.seat)
		p.seatRoom, p.seat, p.seatOffset = "", 0, [2]float64{}
	}
	room, ok := f.roomByName[p.targetRoom]
	if !ok || room.Node < 0 || room.Role == RoleCorridor {
		return
	}
	p.seatRoom = room.Name
	p.seat = f.takeSeat(room.Name)
	p.seatOffset = seatOffset(room, p.seat, s.params.MetresPerUnit)
}

func (s *Simulation) startWalking(f *floor, p *Person) {
	s.reserve(f, p)
	if p.state == StateOutside {
		if p.targetRoom == "" {
			return
		}
		// Enter through the entrance closest to the first destination.
		p.node = s.nearestEntrance(f, s.destination(f, p))
		p.pos = s.nodePos(f, p.node)
		p.roomName = f.graph.Nodes[p.node].Name
	}
	route := f.graph.Route(p.node, s.destination(f, p))
	// Someone whose plan changes between two nodes walks back to the node
	// behind them instead of jumping onto the new route.
	if p.state == StateWalking && p.segProgress > 0 && p.routeIndex+1 < len(p.route) && len(route) > 0 {
		ahead := p.route[p.routeIndex+1]
		a, b := s.nodePos(f, p.node), s.nodePos(f, ahead)
		p.segProgress = math.Max(0, math.Hypot(b[0]-a[0], b[1]-a[1])-p.segProgress)
		p.route = append([]int{ahead}, route...)
		p.routeIndex = 0
		return
	}
	p.route = route
	p.routeIndex = 0
	p.segProgress = 0
	if len(p.route) <= 1 {
		s.arrive(f, p)
		return
	}
	p.state = StateWalking
}

func (s *Simulation) arrive(f *floor, p *Person) {
	p.route = nil
	if p.targetRoom == "" {
		p.state = StateOutside
		return
	}
	p.state = StateInRoom
	p.node = s.destination(f, p)
	p.pos = s.place(f, p.node, p.seatOffset)
	p.roomName = p.targetRoom
}

func (s *Simulation) nodePos(f *floor, idx int) [2]float64 {
	n := f.graph.Nodes[idx]
	return [2]float64{n.X, n.Y}
}

func (s *Simulation) walk(f *floor, p *Person, units float64) {
	for units > 0 && p.routeIndex < len(p.route)-1 {
		a := s.waypoint(f, p, p.routeIndex)
		b := s.waypoint(f, p, p.routeIndex+1)
		segment := math.Hypot(b[0]-a[0], b[1]-a[1])
		remaining := segment - p.segProgress
		if units >= remaining {
			units -= remaining
			p.routeIndex++
			p.segProgress = 0
			p.node = p.route[p.routeIndex]
			p.roomName = f.graph.Nodes[p.node].Name
			p.pos = b
			if segment > 0 {
				p.heading = math.Atan2(b[1]-a[1], b[0]-a[0]) * 180 / math.Pi
			}
			continue
		}
		p.segProgress += units
		units = 0
		t := p.segProgress / math.Max(segment, 1e-9)
		p.pos = [2]float64{a[0] + (b[0]-a[0])*t, a[1] + (b[1]-a[1])*t}
		p.heading = math.Atan2(b[1]-a[1], b[0]-a[0]) * 180 / math.Pi
	}
	if p.routeIndex >= len(p.route)-1 {
		s.arrive(f, p)
	}
}

// --- clock control ---

type ClockState struct {
	Time        string  `json:"time"`
	Date        string  `json:"date"`
	TimeOfDay   string  `json:"time_of_day"`
	Weekday     string  `json:"weekday"`
	Weekend     bool    `json:"weekend"`
	MinuteOfDay float64 `json:"minute_of_day"`
	Factor      float64 `json:"factor"`
	Running     bool    `json:"running"`
}

func (s *Simulation) clockLocked() ClockState {
	wd := s.now.Weekday()
	return ClockState{
		Time: s.now.Format(time.RFC3339), Date: s.now.Format("2006-01-02"),
		TimeOfDay: s.now.Format("15:04"), Weekday: wd.String(),
		Weekend:     wd == time.Saturday || wd == time.Sunday,
		MinuteOfDay: minuteOfDay(s.now), Factor: s.factor, Running: s.running,
	}
}

func (s *Simulation) Clock() ClockState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.clockLocked()
}

func (s *Simulation) Factor() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.factor
}

func (s *Simulation) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

func (s *Simulation) SetFactor(factor float64) error {
	if factor <= 0 || factor > 3600 {
		return fmt.Errorf("factor must be between 0 and 3600")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.factor = factor
	return nil
}

func (s *Simulation) SetRunning(running bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = running
}

// SetTime moves the clock. Moving forward within the day simulates the
// intervening time so that people are where the plan puts them. Moving to an
// earlier time or another day regenerates that day and simulates from
// midnight, which keeps every reachable state consistent with a plan.
func (s *Simulation) SetTime(t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if midnight(t) == s.day && !t.Before(s.now) {
		s.advanceLocked(t.Sub(s.now))
		return
	}
	s.now = midnight(t)
	s.regenerate(s.now)
	s.advanceLocked(t.Sub(s.now))
}

// --- parameters ---

func (s *Simulation) Parameters() Parameters {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneParameters(s.params)
}

// SetParameters replaces the model parameters and regenerates the current day
// up to the current time, so the change is visible immediately.
func (s *Simulation) SetParameters(params Parameters) error {
	if err := params.Normalize(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.configure(params); err != nil {
		return err
	}
	now := s.now
	s.now = midnight(now)
	s.regenerate(s.now)
	s.advanceLocked(now.Sub(s.now))
	return nil
}

func cloneParameters(p Parameters) Parameters {
	p.LectureSlots = append([]string(nil), p.LectureSlots...)
	p.Entrances = append([]Entrance(nil), p.Entrances...)
	roles := make(map[string]string, len(p.RoomRoles))
	for k, v := range p.RoomRoles {
		roles[k] = v
	}
	p.RoomRoles = roles
	return p
}

// --- snapshots ---

type PersonState struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Role   Role   `json:"role"`
	Gender string `json:"gender"`
	Level  string `json:"level"`
	Office string `json:"office,omitempty"`
	State  State  `json:"state"`
	Room   string `json:"room,omitempty"`
	Status string `json:"status"`
}

type RoomState struct {
	Room
	Occupants int `json:"occupants"`
}

type Counts struct {
	Population    int            `json:"population"`
	Inside        int            `json:"inside"`
	Walking       int            `json:"walking"`
	InRoom        int            `json:"in_room"`
	ByRole        map[Role]int   `json:"by_role"`
	InsideByRole  map[Role]int   `json:"inside_by_role"`
	ByLevel       map[string]int `json:"by_level"`
	InsideByLevel map[string]int `json:"inside_by_level"`
}

type Snapshot struct {
	Levels    []string        `json:"levels"`
	Clock     ClockState      `json:"clock"`
	Counts    Counts          `json:"counts"`
	Rooms     []RoomState     `json:"rooms"`
	Lectures  []Lecture       `json:"lectures"`
	Timeline  []Sample        `json:"timeline"`
	People    []PersonState   `json:"people"`
	Entrances []EntranceState `json:"entrances"`
}

// EntranceState reports where a configured entrance landed on the graph.
type EntranceState struct {
	Name     string     `json:"name"`
	Level    string     `json:"level"`
	Position [2]float64 `json:"position"`
	Node     string     `json:"node"`
}

func statusText(p *Person) string {
	role := string(p.Role)
	switch p.state {
	case StateWalking:
		if p.targetRoom == "" {
			return role + ": leaving"
		}
		return role + ": walking to " + p.targetRoom
	case StateInRoom:
		switch p.targetKind {
		case "office":
			return role + ": at office " + p.roomName
		case "lecture":
			return role + ": lecture in " + p.roomName
		case "fika":
			return role + ": fika in " + p.roomName
		case "lunch":
			return role + ": lunch in " + p.roomName
		case "patrol":
			return role + ": patrolling " + p.roomName
		}
		return role + ": in " + p.roomName
	}
	return role + ": outside"
}

// Snapshot returns the state used by the UI. People are limited to keep the
// payload small at 1 Hz; occupants are listed before those outside.
func (s *Simulation) Snapshot(maxPeople int) Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap := Snapshot{
		Levels: s.Levels(), Clock: s.clockLocked(),
		Counts: Counts{
			ByRole: map[Role]int{}, InsideByRole: map[Role]int{},
			ByLevel: map[string]int{}, InsideByLevel: map[string]int{},
		},
		Timeline: append([]Sample(nil), s.samples...),
	}
	occupants := map[string]int{} // level/room
	for _, f := range s.floors {
		snap.Lectures = append(snap.Lectures, f.lectures...)
		for _, e := range f.entrances {
			snap.Entrances = append(snap.Entrances, EntranceState{Name: e.Name, Level: f.plan.Level, Position: e.Position, Node: f.graph.Nodes[e.Node].Name})
		}
		for _, p := range f.people {
			snap.Counts.Population++
			snap.Counts.ByRole[p.Role]++
			snap.Counts.ByLevel[p.Level]++
			if p.state == StateOutside {
				continue
			}
			snap.Counts.Inside++
			snap.Counts.InsideByRole[p.Role]++
			snap.Counts.InsideByLevel[p.Level]++
			if p.state == StateWalking {
				snap.Counts.Walking++
			} else {
				snap.Counts.InRoom++
			}
			occupants[p.Level+"/"+p.roomName]++
		}
		for _, room := range f.rooms {
			if room.Role == RoleUnused {
				continue
			}
			snap.Rooms = append(snap.Rooms, RoomState{Room: room, Occupants: occupants[room.Level+"/"+room.Name]})
		}
	}
	people := make([]*Person, len(s.people))
	copy(people, s.people)
	sort.SliceStable(people, func(a, b int) bool {
		return people[a].state != StateOutside && people[b].state == StateOutside
	})
	for i, p := range people {
		if i >= maxPeople {
			break
		}
		state := PersonState{ID: p.ID, Name: p.Name, Role: p.Role, Gender: p.Gender, Level: p.Level, Office: p.Office, State: p.state, Status: statusText(p)}
		if p.state != StateOutside {
			state.Room = p.roomName
		}
		snap.People = append(snap.People, state)
	}
	return snap
}

// Entity and RoomOccupancy are the BuildSim wire formats for PUT /api/entities
// and PUT /api/occupancy.
type Entity struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Type         string     `json:"type"`
	Level        string     `json:"level"`
	Room         string     `json:"room,omitempty"`
	Position     [2]float64 `json:"position"`
	Heading      float64    `json:"heading"`
	Status       string     `json:"status"`
	TransitionMS int        `json:"transition_ms"`
}

type OccupantRef struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Icon     string     `json:"icon"`
	Position [2]float64 `json:"position"`
}

type RoomOccupancy struct {
	Persons []OccupantRef `json:"persons"`
	Aliens  []struct{}    `json:"aliens"`
}

// Publishable returns everything BuildSim should show: one entity per person
// inside the building and the occupants of every room and corridor, on every
// simulated floor.
func (s *Simulation) Publishable() ([]Entity, map[string]RoomOccupancy) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entities := []Entity{}
	occupancy := map[string]RoomOccupancy{}
	for _, f := range s.floors {
		for _, p := range f.people {
			if p.state == StateOutside {
				continue
			}
			entity := Entity{
				ID: p.ID, Name: p.Name, Type: p.Gender, Level: f.plan.Level,
				Position: p.pos, Heading: p.heading, Status: statusText(p),
				TransitionMS: s.params.PublishIntervalMs,
			}
			if p.state == StateInRoom {
				entity.Room = p.roomName
			}
			entities = append(entities, entity)
			if _, known := f.roomByName[p.roomName]; known {
				key := f.plan.Level + "/" + p.roomName
				entry := occupancy[key]
				entry.Persons = append(entry.Persons, OccupantRef{ID: p.ID, Name: p.Name, Icon: p.Gender, Position: p.pos})
				occupancy[key] = entry
			}
		}
	}
	for key, entry := range occupancy {
		if entry.Aliens == nil {
			entry.Aliens = []struct{}{}
			occupancy[key] = entry
		}
	}
	return entities, occupancy
}

// Rooms returns the classification of every floor for the UI and tests.
func (s *Simulation) Rooms() []Room {
	s.mu.Lock()
	defer s.mu.Unlock()
	var rooms []Room
	for _, f := range s.floors {
		rooms = append(rooms, f.rooms...)
	}
	return rooms
}

// People returns the current population for tests.
func (s *Simulation) People() []*Person {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*Person(nil), s.people...)
}

// Position is exported for tests.
func (p *Person) Position() [2]float64 { return p.pos }
func (p *Person) State() State         { return p.state }
func (p *Person) Room() string         { return p.roomName }
