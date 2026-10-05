# Occupancy simulator

Simulates the people in a BuildSim building: who is present, where they are,
and how they move between rooms along the walkable graph. Every floor of the
building is simulated at once. The result is
published to BuildSim as moving entities (`PUT /api/entities`) and room
occupancy (`PUT /api/occupancy`). Every other part of the system reads
occupancy from BuildSim, so this tool is the single source of "how many people
are in room X".

The tool has its own web UI on port 8081 for the model parameters, the
simulated clock, and live statistics.

| Documentation | |
|---|---|
| [docs/model.md](docs/model.md) | How a simulated day is built: population, floors, roles, lectures, rooms, movement, and every parameter |
| [docs/buildsim-api.md](docs/buildsim-api.md) | The wire contract with BuildSim: the HTTP calls, the JSON on them, and the rules BuildSim enforces |

## Running

Against a BuildSim started from [`../buildingsim`](../buildingsim) with
`go run ./cmd start`:

```bash
make build      # builds the Svelte UI (needs Node.js) and the Go binary
./bin/occupancy
```

Open <http://127.0.0.1:8081>. Without Node.js, `go run .` still works; the JSON
API is available and the UI page explains how to build it.

| Flag | Environment | Default | Meaning |
|---|---|---|---|
| `-buildsim` | `BUILDSIM_URL` | `http://127.0.0.1:9090` | BuildSim as seen from this process |
| `-buildsim-public` | `BUILDSIM_PUBLIC_URL` | same as `-buildsim` | BuildSim as seen from the browser (differs inside Docker) |
| `-listen` | `OCCUPANCY_LISTEN` | `:8081` | UI and API address |
| `-levels` | `OCCUPANCY_LEVELS` | every floor | comma-separated floors to simulate, e.g. `level0,level1` |
| `-factor` | `OCCUPANCY_FACTOR` | `60` | simulated seconds per real second |
| `-start` | `OCCUPANCY_START` | `07:30` next weekday | initial simulated time, `HH:MM` or RFC3339 |
| `-config` | `OCCUPANCY_CONFIG` | none | JSON file with parameters, rewritten when changed in the UI |
| `-paused` | | off | start with the clock stopped |

The simulator waits for BuildSim at start-up and keeps running if BuildSim
restarts later; both endpoints receive complete snapshots, so the next
successful publish restores the full picture.

## The model

A summary; [docs/model.md](docs/model.md) has the full description with the
formulas and the complete parameter reference.

**Population.** Each simulated day has a configurable number of people for the
whole building. On weekdays they are students, lecturers, and other staff
according to the configured shares; on weekends only a fraction of the staff
appears. A configurable number of guards patrol at night.

**Floors.** The population is spread over every simulated floor: office workers
in proportion to the offices on the floor, students in proportion to its lecture
seats, and guards evenly. The parts add up exactly to the configured population.
BuildSim's walkable graphs are per level and contain no stairs, so a person
belongs to one floor for the whole day; each floor has its own rooms, lecture
timetable, and entrances.

**Daily plans.** Every person receives a timeline for the day, generated once
per day from the parameters and a seed, so a day is reproducible.

| Role | Day |
|---|---|
| Staff | Arrive in the arrival window, work in their own office, optional fika breaks around 09:45 and 14:30, lunch in the lunch window either outside the building or in the nearest fika room, leave in the departure window. |
| Lecturer | As staff, plus the lectures assigned to them. |
| Student | Attend one to three lectures in distinct slots. Short gaps are spent in the nearest fika room, the lunch hour and long gaps outside the building. |
| Guard | Outside working hours, walk to a random office or lecture room every eight minutes. |

Lectures are booked per floor and slot: rooms are drawn at random, but only
until their seats cover the students expected in that slot, filled to the
lecture fill target. Students then pack one lecture before the next one is used,
so a lecture looks like a lecture instead of a handful of people scattered over
every hall on the floor; utilisation caps how much of a floor may be booked at
once, and a lecture nobody signed up for is not held. Lecturers take the
remaining lectures round-robin.

**Rooms.** BuildSim only distinguishes rooms from corridors, so roles are
derived from floor area with configurable thresholds: small rooms are offices,
rooms above the lecture threshold are lecture rooms, and a configurable number
of rooms above the fika threshold, spread across the floor, become fika rooms.
Rooms are classified per floor, so the fika count is per floor as well. An
office holds one person; the assignment only starts sharing offices when a floor
has more office workers than offices.
The plan uses drawing units; the default of 0.5 m per unit gives a median room
of about 18 m² on level 0, which is a plausible office. Any room can be
reassigned with an explicit override; by default the east foyer A1123 and the
vestibule A105 are excluded from the room pool.

**Entrances.** The floor plan does not mark entrances, so they were read off
the drawing: gaps in the outer wall, door-swing arcs on it, and vestibules.
Seven are configured for level 0 (see `docs/level0-entrances.png`); each is a
level, a name, and a plan coordinate that is snapped to the nearest walkable
node. A person enters through the entrance with the shortest walk to the first
room of the day and leaves through the one nearest the last. A floor with no
entrance of its own uses its largest corridor, which is the lobby on the ground
floor and the stair hall above it; an entrance naming a level that is not
simulated is ignored, and one with no level at all belongs to the first
simulated floor.

| Entrance | Position | Evidence |
|---|---|---|
| Main entrance (north), hall A1016 | (175, 2) | 24 m glazed opening in the north wall |
| East entrance, foyer A1123 | (345, 176) | double door in a recessed vestibule |
| North-east entrance, vestibule A105 | (338, 214) | windbreak with folding-door marks |
| South entrance, vestibule A10 | (230, 345) | windbreak at the tip of the south wing |
| North-east corner, corridor A1000A | (360, 25) | door on the end wall and an exterior stair |
| North-west corner, corridor A1000E | (3, 28) | door arcs on the recessed end wall |
| South-west entrance, corridor A170 | (124, 333) | door on the end wall and an exterior ramp |

**Movement.** A person starts walking when the timeline changes target. Routes
follow the walkable graph of their floor, computed locally with Dijkstra and
cached, at a configurable walking speed. Someone whose plan changes between two
nodes walks back to the node behind them rather than jumping onto the new route.
The walkable graph has one node per room, so occupants are spread over a spiral
around it — the first person on the node itself, the rest fanned out over the
room — and walk to their own spot. Without that, a lecture with fifty students
is fifty entities on one coordinate and the viewer shows one person. Corridor nodes carry the corridor's name, so
transient corridor occupancy is published as well.

**Clock.** The simulated clock runs at a configurable multiple of real time.
Jumping forward simulates the intervening time; jumping backwards or to
another date regenerates that day and replays it from midnight, so the state
is always consistent with a plan. Changing parameters regenerates the current
day up to the current time.

## Default assumptions

| Assumption | Default |
|---|---|
| Floors | every floor of the building (`level0`, `level1`, `level2`) |
| People per weekday, building-wide | 360 |
| Weekend fraction | 0.10 |
| Student share | 0.60 |
| Lecturer share of staff | 0.40 |
| Night guards | 3, spread over the floors |
| Arrival window | 08:00–10:00 |
| Lunch window | 12:00–13:00, 45 min, 50 % leave the building |
| Departure window | 16:00–19:00 |
| Fika | 70 % per break, 20 min |
| Lecture slots | 08:15–10:00, 10:15–12:00, 13:15–15:00, 15:15–17:00 |
| Lecture room utilisation (cap per slot) | 0.60 |
| Lecture fill target | 0.75 |
| Office / lecture / fika thresholds | ≤ 40 m² / ≥ 60 m² / ≥ 100 m² (3 fika rooms per floor) |
| Entrances | the seven level 0 entrances above |
| Walking speed | 1.3 m/s |
| Publish interval (entities) | 1 s |
| Occupancy interval | 5 s, and only when it changed |

All of these can be changed in the UI or in the config file.

## API

| Method and path | Purpose |
|---|---|
| `GET /api/state` | Clock, counts, rooms, lectures, today's occupancy curve, people, BuildSim status |
| `GET /api/events` | The same as server-sent events, once per second |
| `GET /api/config`, `PUT /api/config` | Read or replace the parameters; a `PUT` regenerates the current day |
| `GET /api/defaults` | The default parameters |
| `POST /api/clock` | `{"factor": 300}`, `{"running": false}`, `{"time": "11:25"}`, `{"date": "2026-09-12"}` in any combination |
| `GET /healthz` | Liveness |

## Interfaces to BuildSim

The calls and their JSON are documented in full, with examples, in
[docs/buildsim-api.md](docs/buildsim-api.md).

| Direction | Endpoint | Notes |
|---|---|---|
| read | `GET /api/building` | the levels of the building, unless `-levels` names them |
| read | `GET /api/building/floors/{level}` | rooms, page size, walkable graph; read once per floor at start |
| write | `PUT /api/entities` | one entity per person inside, every publish interval; `transition_ms` equals that interval, so the viewer interpolates between two writes |
| write | `PUT /api/occupancy` | `<level>/<room>` keys for every floor, including corridors; at most once per occupancy interval and only when the document changed, plus a refresh every 30 s |

Both writes replace the complete collection, so this tool must be the only
writer of these two collections. That is also why occupancy can be skipped when
nothing changed: BuildSim still holds the last complete document. A failed
write, or half a minute without one, forces the next one out, so a restarted
BuildSim recovers within one interval. Room layers, effects, and alerts are left to
the other tools.

## Tests

```bash
go test ./...
BUILDSIM_URL=http://127.0.0.1:9090 go test ./internal/buildsim -run Live
```

The unit tests run a whole simulated day on a copy of the level 0 floor plan
(`testdata/level0.json`, named twice to make a two-floor building) and check the
shape of the day: nobody inside at night except guards, most staff at their
desks mid-morning, a lunch dip, full lectures with an audience, one person per
office, people on every floor, fewer people and no students on Saturday,
movement that never jumps further than the walking speed allows, no two
occupants of a room on the same spot, and published snapshots that satisfy
BuildSim's validation rules. The second command
additionally publishes to a running BuildSim.

## Development

```bash
go run .                 # API on :8081
cd ui && npm run dev     # UI on :5173, proxies /api to :8081
```
