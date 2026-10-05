package sim

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// Parameters is the complete, user-editable description of one simulated
// building population. Every field has a default so that an empty object is a
// valid configuration. Clock times use "HH:MM" and refer to the simulated day.
type Parameters struct {
	Seed int64 `json:"seed"`

	// Population
	WeekdayPopulation int     `json:"weekday_population"`
	WeekendFraction   float64 `json:"weekend_fraction"`
	NightGuards       int     `json:"night_guards"`
	StudentShare      float64 `json:"student_share"`
	LecturerShare     float64 `json:"lecturer_share"`

	// Working day
	ArriveStart string `json:"arrive_start"`
	ArriveEnd   string `json:"arrive_end"`
	LunchStart  string `json:"lunch_start"`
	LunchEnd    string `json:"lunch_end"`
	LeaveStart  string `json:"leave_start"`
	LeaveEnd    string `json:"leave_end"`

	// Behaviour
	LunchMinutes           int      `json:"lunch_minutes"`
	LunchOutProbability    float64  `json:"lunch_out_probability"`
	FikaProbability        float64  `json:"fika_probability"`
	FikaMinutes            int      `json:"fika_minutes"`
	LecturesPerStudentMin  int      `json:"lectures_per_student_min"`
	LecturesPerStudentMax  int      `json:"lectures_per_student_max"`
	LectureRoomUtilisation float64  `json:"lecture_room_utilisation"`
	LectureFillTarget      float64  `json:"lecture_fill_target"`
	LectureSlots           []string `json:"lecture_slots"`

	// Room classification
	MetresPerUnit float64           `json:"metres_per_unit"`
	MinRoomM2     float64           `json:"min_room_m2"`
	OfficeMaxM2   float64           `json:"office_max_m2"`
	LectureMinM2  float64           `json:"lecture_min_m2"`
	FikaMinM2     float64           `json:"fika_min_m2"`
	FikaRoomCount int               `json:"fika_room_count"`
	RoomRoles     map[string]string `json:"room_roles"`
	Entrances     []Entrance        `json:"entrances"`

	// Movement and publishing
	WalkingSpeedMps     float64 `json:"walking_speed_mps"`
	PublishIntervalMs   int     `json:"publish_interval_ms"`
	OccupancyIntervalMs int     `json:"occupancy_interval_ms"`
}

// Entrance is a door to the outside of one level. The position is a floor-plan
// coordinate; it is snapped to the nearest walkable node. An empty level means
// the first simulated level, and entrances on levels that are not simulated are
// ignored. A floor without any entrance uses its largest corridor, which is the
// lobby on the ground floor and the stair hall higher up.
type Entrance struct {
	Name     string     `json:"name"`
	Level    string     `json:"level,omitempty"`
	Position [2]float64 `json:"position"`
}

// Level0Entrances are the entrances identified on the level 0 drawing: wall
// gaps, door-swing arcs, and vestibules on the outer walls (see
// docs/level0-entrances.png).
func Level0Entrances() []Entrance {
	return []Entrance{
		{Name: "Main entrance (north), hall A1016", Level: "level0", Position: [2]float64{175, 2}},
		{Name: "East entrance, foyer A1123", Level: "level0", Position: [2]float64{345, 176}},
		{Name: "North-east entrance, vestibule A105", Level: "level0", Position: [2]float64{338, 214}},
		{Name: "South entrance, vestibule A10", Level: "level0", Position: [2]float64{230, 345}},
		{Name: "North-east corner, corridor A1000A", Level: "level0", Position: [2]float64{360, 25}},
		{Name: "North-west corner, corridor A1000E", Level: "level0", Position: [2]float64{3, 28}},
		{Name: "South-west entrance, corridor A170", Level: "level0", Position: [2]float64{124, 333}},
	}
}

// DefaultParameters returns the documented assumptions of the demo project.
func DefaultParameters() Parameters {
	return Parameters{
		Seed:                   7,
		WeekdayPopulation:      360,
		WeekendFraction:        0.1,
		NightGuards:            3,
		StudentShare:           0.6,
		LecturerShare:          0.4,
		ArriveStart:            "08:00",
		ArriveEnd:              "10:00",
		LunchStart:             "12:00",
		LunchEnd:               "13:00",
		LeaveStart:             "16:00",
		LeaveEnd:               "19:00",
		LunchMinutes:           45,
		LunchOutProbability:    0.5,
		FikaProbability:        0.7,
		FikaMinutes:            20,
		LecturesPerStudentMin:  1,
		LecturesPerStudentMax:  3,
		LectureRoomUtilisation: 0.6,
		LectureFillTarget:      0.75,
		LectureSlots:           []string{"08:15-10:00", "10:15-12:00", "13:15-15:00", "15:15-17:00"},
		MetresPerUnit:          0.5,
		MinRoomM2:              6,
		OfficeMaxM2:            40,
		LectureMinM2:           60,
		FikaMinM2:              100,
		FikaRoomCount:          3,
		// A1123 is the east foyer and A105 a vestibule; neither is an office.
		RoomRoles:           map[string]string{"A1123": "unused", "A105": "unused"},
		Entrances:           Level0Entrances(),
		WalkingSpeedMps:     1.3,
		PublishIntervalMs:   1000,
		OccupancyIntervalMs: 5000,
	}
}

// Normalize fills omitted values from the defaults and validates the result.
func (p *Parameters) Normalize() error {
	d := DefaultParameters()
	if p.WeekdayPopulation < 0 || p.WeekdayPopulation > 1500 {
		return errors.New("weekday_population must be between 0 and 1500")
	}
	if p.WeekendFraction < 0 || p.WeekendFraction > 1 {
		return errors.New("weekend_fraction must be between 0 and 1")
	}
	if p.NightGuards < 0 || p.NightGuards > 20 {
		return errors.New("night_guards must be between 0 and 20")
	}
	for name, value := range map[string]float64{
		"student_share": p.StudentShare, "lecturer_share": p.LecturerShare,
		"lunch_out_probability": p.LunchOutProbability, "fika_probability": p.FikaProbability,
		"lecture_room_utilisation": p.LectureRoomUtilisation,
	} {
		if value < 0 || value > 1 {
			return fmt.Errorf("%s must be between 0 and 1", name)
		}
	}
	windows := []struct {
		name       string
		start, end *string
	}{
		{"arrive", &p.ArriveStart, &p.ArriveEnd},
		{"lunch", &p.LunchStart, &p.LunchEnd},
		{"leave", &p.LeaveStart, &p.LeaveEnd},
	}
	defaults := map[string][2]string{
		"arrive": {d.ArriveStart, d.ArriveEnd}, "lunch": {d.LunchStart, d.LunchEnd}, "leave": {d.LeaveStart, d.LeaveEnd},
	}
	for _, w := range windows {
		if strings.TrimSpace(*w.start) == "" {
			*w.start = defaults[w.name][0]
		}
		if strings.TrimSpace(*w.end) == "" {
			*w.end = defaults[w.name][1]
		}
		start, err := ParseClock(*w.start)
		if err != nil {
			return fmt.Errorf("%s_start: %w", w.name, err)
		}
		end, err := ParseClock(*w.end)
		if err != nil {
			return fmt.Errorf("%s_end: %w", w.name, err)
		}
		if end <= start {
			return fmt.Errorf("%s window must end after it starts", w.name)
		}
	}
	arriveEnd, _ := ParseClock(p.ArriveEnd)
	lunchStart, _ := ParseClock(p.LunchStart)
	lunchEnd, _ := ParseClock(p.LunchEnd)
	leaveStart, _ := ParseClock(p.LeaveStart)
	if lunchStart < arriveEnd || leaveStart < lunchEnd {
		return errors.New("windows must be ordered: arrive, lunch, leave")
	}
	if p.LunchMinutes <= 0 {
		p.LunchMinutes = d.LunchMinutes
	}
	if p.FikaMinutes <= 0 {
		p.FikaMinutes = d.FikaMinutes
	}
	if p.LecturesPerStudentMin < 0 || p.LecturesPerStudentMax < p.LecturesPerStudentMin {
		return errors.New("lectures_per_student_max must be at least lectures_per_student_min")
	}
	if p.LecturesPerStudentMax == 0 {
		p.LecturesPerStudentMin, p.LecturesPerStudentMax = d.LecturesPerStudentMin, d.LecturesPerStudentMax
	}
	if p.LectureFillTarget <= 0 {
		p.LectureFillTarget = d.LectureFillTarget
	}
	if p.LectureFillTarget > 1 {
		return errors.New("lecture_fill_target must be between 0 and 1")
	}
	if len(p.LectureSlots) == 0 {
		p.LectureSlots = append([]string(nil), d.LectureSlots...)
	}
	for _, slot := range p.LectureSlots {
		if _, _, err := ParseSlot(slot); err != nil {
			return err
		}
	}
	if p.MetresPerUnit <= 0 {
		p.MetresPerUnit = d.MetresPerUnit
	}
	if p.MinRoomM2 <= 0 {
		p.MinRoomM2 = d.MinRoomM2
	}
	if p.OfficeMaxM2 <= 0 {
		p.OfficeMaxM2 = d.OfficeMaxM2
	}
	if p.LectureMinM2 <= 0 {
		p.LectureMinM2 = d.LectureMinM2
	}
	if p.FikaMinM2 <= 0 {
		p.FikaMinM2 = d.FikaMinM2
	}
	if p.OfficeMaxM2 > p.LectureMinM2 {
		return errors.New("office_max_m2 must not exceed lecture_min_m2")
	}
	if p.FikaRoomCount < 0 {
		return errors.New("fika_room_count must not be negative")
	}
	if p.RoomRoles == nil {
		p.RoomRoles = map[string]string{}
	}
	for room, role := range p.RoomRoles {
		switch RoomRole(role) {
		case RoleOffice, RoleLecture, RoleFika, RoleUnused:
		default:
			return fmt.Errorf("room_roles[%q] has unknown role %q", room, role)
		}
	}
	for i, entrance := range p.Entrances {
		if strings.TrimSpace(entrance.Name) == "" {
			p.Entrances[i].Name = fmt.Sprintf("Entrance %d", i+1)
		}
		p.Entrances[i].Level = strings.TrimSpace(entrance.Level)
		if !finite(entrance.Position[0]) || !finite(entrance.Position[1]) || entrance.Position[0] < 0 || entrance.Position[1] < 0 {
			return fmt.Errorf("entrance %d has an invalid position", i+1)
		}
	}
	if p.WalkingSpeedMps <= 0 {
		p.WalkingSpeedMps = d.WalkingSpeedMps
	}
	if p.WalkingSpeedMps > 10 {
		return errors.New("walking_speed_mps must not exceed 10")
	}
	if p.PublishIntervalMs <= 0 {
		p.PublishIntervalMs = d.PublishIntervalMs
	}
	if p.PublishIntervalMs < 200 {
		return errors.New("publish_interval_ms must be at least 200")
	}
	if p.OccupancyIntervalMs <= 0 {
		p.OccupancyIntervalMs = d.OccupancyIntervalMs
	}
	if p.OccupancyIntervalMs < 200 {
		return errors.New("occupancy_interval_ms must be at least 200")
	}
	return nil
}

// ParseClock converts "HH:MM" into minutes after midnight.
func ParseClock(value string) (float64, error) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid time %q; use HH:MM", value)
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 24 {
		return 0, fmt.Errorf("invalid hour in %q", value)
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return 0, fmt.Errorf("invalid minute in %q", value)
	}
	return float64(hour*60 + minute), nil
}

// ParseSlot converts "HH:MM-HH:MM" into start and end minutes.
func ParseSlot(value string) (float64, float64, error) {
	parts := strings.Split(value, "-")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid lecture slot %q; use HH:MM-HH:MM", value)
	}
	start, err := ParseClock(parts[0])
	if err != nil {
		return 0, 0, err
	}
	end, err := ParseClock(parts[1])
	if err != nil {
		return 0, 0, err
	}
	if end <= start {
		return 0, 0, fmt.Errorf("lecture slot %q must end after it starts", value)
	}
	return start, end, nil
}

// FormatClock renders minutes after midnight as "HH:MM".
func FormatClock(minutes float64) string {
	if minutes < 0 {
		minutes = 0
	}
	total := int(minutes) % 1440
	return fmt.Sprintf("%02d:%02d", total/60, total%60)
}
