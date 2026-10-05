# The occupancy model

Every number below is a parameter with a
default; see [Parameters](#parameters) at the end and the tool's own UI.

The simulator owns one building. It loads every floor from BuildSim once at
start-up, classifies the rooms, generates a population for the current day, and
then advances that population on a simulated clock. Nothing is read back from
BuildSim afterwards: the simulator is the source of truth for where people are,
and BuildSim is the display.

## One day at a time

A day is generated in full, in advance, the moment the clock enters it. Each
person receives a complete timeline for that day; walking is what happens
between two entries of the timeline.

The generator is seeded from `seed` and the calendar date:

```
day seed = seed*1000003 + day-of-year + year*367     (+ 7919 per floor)
```

so a given date always produces the same day, no matter how the clock reached
it. Jumping the clock backwards or to another date regenerates that day and
replays it from midnight; jumping forwards inside the current day simulates the
intervening minutes. Both are therefore consistent with a plan — a person is
never somewhere their timeline does not put them.

## Population

`weekday_population` is the whole building, not one floor. On a weekend it
shrinks to `round(weekday_population × weekend_fraction)` and only staff appear.

| Cohort | Size |
|---|---|
| Students | `round(total × student_share)`, weekdays only |
| Lecturers | `round((total − students) × lecturer_share)`, weekdays only |
| Staff | whatever is left |
| Guards | `night_guards`, **added on top** of `weekday_population` |

### Spread over the floors

BuildSim's walkable graphs are per level and contain no stairs, so a person
belongs to one floor for the whole day. Each cohort is divided over the floors
by a weight, using largest-remainder rounding so the parts add up to exactly the
configured number:

| Cohort | Weight per floor |
|---|---|
| Staff, lecturers | number of offices |
| Students | total lecture-room seats |
| Guards | equal |

With the default building that gives roughly 148 / 129 / 86 people on
`level0` / `level1` / `level2`.

## Roles and their day

| Role | Day |
|---|---|
| Staff | Arrive in the arrival window, work in their own office, optional fika breaks around 09:45 and 14:30, lunch either outside the building or in the nearest fika room, leave in the departure window. |
| Lecturer | As staff, plus the lectures assigned to them; they arrive 15 min before a lecture and the day stretches to cover it. |
| Student | Attend between `lectures_per_student_min` and `…_max` lectures in distinct slots, arriving 10 min early. A gap up to 90 min is spent in the nearest fika room; the lunch hour and longer gaps are spent outside the building. |
| Guard | Outside working hours, walk to a random office or lecture room every eight minutes and stay five. |

Arrival and departure times are drawn uniformly from their windows, so people
trickle in and out rather than switching state together.

## Lectures, and why students are concentrated

Level 0 classifies as 47 lecture rooms. A per-room booking chance over that
many rooms produces dozens of lectures with a handful of students each —
technically a full timetable, but it does not look like teaching. Booking is therefore driven by demand rather than
by rooms:

```
expected  = students on the floor × mean(lectures_per_student) / number of slots
seats     = expected / lecture_fill_target
max rooms = round(lecture_room_utilisation × lecture rooms on the floor)
```

For each slot the floor's lecture rooms are drawn in random order and booked
until their combined capacity reaches `seats`, or `max rooms` is reached.
Drawing at random (rather than largest-first) means a different room hosts the
lecture each slot and each day.

Students then pick their slots at random and, within a slot, join **the fullest
lecture that still has a free seat**. One room fills before the next is used. A
lecture nobody signed up for is dropped before lecturers are assigned, so no
lecturer talks to an empty hall. The remaining lectures go to lecturers
round-robin.

With the defaults this yields roughly one lecture per floor per slot with 20–60
students in it, instead of most of a floor's lecture rooms hosting two or three
people apiece.

## Rooms

BuildSim only distinguishes rooms from corridors, so roles come from floor
area. Classification runs per floor.

| Role | Rule | Capacity |
|---|---|---|
| office | `min_room_m2` ≤ area ≤ `office_max_m2` | **1 person** |
| lecture | area ≥ `lecture_min_m2` | `area / 2` (2 m² per seat), at least 4 |
| fika | `fika_room_count` rooms at or above `fika_min_m2`, spread apart | `area / 4`, at least 4 |
| corridor | BuildSim says so | — |
| unused | too small, between office and lecture size, or unreachable | — |

Fika rooms are chosen among the rooms that qualify as lecture rooms: the
smallest qualifying room first, then the one furthest from those already
picked, so they end up spread across the floor. `fika_room_count` is per floor.

Offices are handed out in name order, one person each; the assignment only
wraps around and starts sharing if a floor has more office workers than
offices. `room_roles` overrides any room by name — a name that exists on several
floors is overridden on all of them, and a name no floor knows is an error.

The plan is in drawing units; `metres_per_unit` (0.5) converts to m², which
makes the median room on level 0 about 18 m² — a plausible office.

## Entrances

The drawings do not mark entrances, so the seven on level 0 were read off the
plan (wall gaps, door-swing arcs, vestibules — see `level0-entrances.png`).
Each is a level, a name, and a plan coordinate that is snapped to the nearest
walkable node.

A person enters through the entrance with the shortest walk to the first room
of their day and leaves through the one nearest their last. A floor with no
entrance configured falls back to its largest corridor — the lobby on the
ground floor, the stair hall above it. An entrance naming a level that is not
simulated is ignored; one with no level belongs to the first simulated floor.

## Movement

Routes are computed locally with Dijkstra on the floor's walkable graph and
cached, rather than through BuildSim's `/api/graph/route`, so that hundreds of
people can replan every simulated day without an HTTP round trip each, and so
the simulation keeps running while BuildSim restarts.

The clock advances in steps of at most 10 simulated seconds. In each step a
person moves `walking_speed_mps / metres_per_unit × step` plan units along their
route. Corridor nodes carry the corridor's name, so someone walking is reported
as being in that corridor.

Two details keep the motion honest:

- **Replanning mid-corridor.** If the timeline changes target while a person is
  between two nodes, they walk back to the node behind them and continue from
  there, instead of snapping onto the new route.
- **Standing in a room.** The graph has one node per room, so everyone in a room
  would otherwise share one coordinate and a lecture of fifty would render as
  one person. Occupants are spread over a golden-angle spiral around the room
  node: the first person stands on the node, and occupant *n* stands at

  ```
  radius = 0.4 × √area_in_units × √(n / max(capacity, n+1))
  angle  = n × 2.39996 rad
  ```

  A place is reserved when someone sets off for the room and released when they
  leave, so two people are never given the same spot, and the walk's last leg
  ends at the person's own place rather than at the room node.

## Parameters

| Parameter | Default | Meaning |
|---|---|---|
| `seed` | 7 | Reproducibility; combined with the date |
| `weekday_population` | 360 | People in the whole building on a weekday |
| `weekend_fraction` | 0.10 | Share of that on Saturday and Sunday |
| `night_guards` | 3 | Guards, added on top and spread over the floors |
| `student_share` | 0.60 | Share of the population that are students |
| `lecturer_share` | 0.40 | Share of the non-students that are lecturers |
| `arrive_start` / `arrive_end` | 08:00 / 10:00 | Arrival window |
| `lunch_start` / `lunch_end` | 12:00 / 13:00 | Lunch window |
| `leave_start` / `leave_end` | 16:00 / 19:00 | Departure window |
| `lunch_minutes` | 45 | Length of lunch |
| `lunch_out_probability` | 0.50 | Chance of leaving the building for lunch |
| `fika_probability` | 0.70 | Chance per fika break |
| `fika_minutes` | 20 | Length of a fika break |
| `lectures_per_student_min` / `_max` | 1 / 3 | Lectures a student attends per day |
| `lecture_room_utilisation` | 0.60 | Cap on the share of a floor's lecture rooms booked in one slot |
| `lecture_fill_target` | 0.75 | How full a booked lecture room should end up |
| `lecture_slots` | 08:15-10:00, 10:15-12:00, 13:15-15:00, 15:15-17:00 | Timetable slots |
| `metres_per_unit` | 0.5 | Plan units to metres |
| `min_room_m2` | 6 | Below this a room is unused |
| `office_max_m2` | 40 | Office upper bound |
| `lecture_min_m2` | 60 | Lecture room lower bound |
| `fika_min_m2` | 100 | Fika room lower bound |
| `fika_room_count` | 3 | Fika rooms **per floor** |
| `room_roles` | `A1123`, `A105` unused | Explicit per-room overrides |
| `entrances` | the seven level 0 doors | Level, name, plan coordinate |
| `walking_speed_mps` | 1.3 | Walking speed |
| `publish_interval_ms` | 1000 | How often entities are written to BuildSim |
| `occupancy_interval_ms` | 5000 | Shortest gap between occupancy writes |

Changing any of them through `PUT /api/config` or the UI regenerates the
current day and replays it up to the current time, so the effect is immediate
and still consistent with a plan.

See [buildsim-api.md](buildsim-api.md) for what is then published.
