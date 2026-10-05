# What the simulator sends to BuildSim

The simulator reads a building from BuildSim once at start-up and then writes
two collections to it for as long as it runs. This page is the complete wire
contract: the HTTP calls, the JSON on them, and the rules BuildSim enforces.

Base URL is `-buildsim` / `BUILDSIM_URL`, default `http://127.0.0.1:9090`.

| Direction | Call | When |
|---|---|---|
| read | `GET /api/building` | once at start-up, unless `-levels` names the floors |
| read | `GET /api/building/floors/{level}` | once per floor at start-up |
| write | `PUT /api/entities` | every `publish_interval_ms` (1 s) |
| write | `PUT /api/occupancy` | at most every `occupancy_interval_ms` (5 s), and only when it changed |

Both writes replace the **complete** collection, so the simulator must be the
only writer of these two. Room layers, effects, equipment, and alerts are left
to other tools.

---

## Reading the building

### `GET /api/building`

Used to discover the floors when `-levels` is not given.

```json
{
  "name": "A-Building (LTU)",
  "levels": [
    { "id": "level0", "label": "Floor 0" },
    { "id": "level1", "label": "Floor 1" },
    { "id": "level2", "label": "Floor 2" }
  ]
}
```

Every `id` becomes a simulated floor, in this order; the first is the primary
level, which owns entrances that name no level of their own.

### `GET /api/building/floors/level0`

The simulator uses three parts of the response and ignores the rest:

```json
{
  "page":  { "width": 384.56, "height": 365.92 },
  "rooms": [ { "id": 4, "name": "1541", "type": "room", "area": 142.4, "center": [31.8, 183.08] } ],
  "walkable_graph": {
    "nodes": [ { "id": 375, "name": "1541", "x": 31.8, "y": 183.08, "type": "room" } ],
    "edges": [ { "from": 374, "to": 335, "weight": 13.6 } ]
  }
}
```

`page` bounds every position published later, `rooms` drives the classification
(`area` is in plan units² — 142.4 units² is 35.6 m² at the default 0.5 m per
unit, an office — and rooms also carry a `polygon` the simulator ignores), and
`walkable_graph` is copied into memory and routed on locally; a node of type
`room` is the point the simulator treats as that room. A floor without a
walkable graph is rejected. Start-up retries every 2 s until BuildSim answers,
so the two services can start in any order.

---

## `PUT /api/entities` — one entity per person inside

Sent on every publish interval, whether or not anything moved: positions change
continuously and the viewer interpolates between two writes.

```http
PUT /api/entities HTTP/1.1
Host: 127.0.0.1:9090
Content-Type: application/json
```

```json
[
  {
    "id": "level0-staff-1",
    "name": "Hugo Gustafsson",
    "type": "man",
    "level": "level0",
    "room": "1541",
    "position": [31.8, 183.08],
    "heading": 86.13,
    "status": "staff: at office 1541",
    "transition_ms": 1000
  },
  {
    "id": "level0-staff-4",
    "name": "Samuel Sandström",
    "type": "man",
    "level": "level0",
    "position": [30.61, 67.14],
    "heading": -90,
    "status": "staff: leaving",
    "transition_ms": 1000
  }
]
```

The array is the whole building: people on every floor, in one request — never
one call per person or per floor. People outside the building are simply
absent. Around the middle of a default weekday that is about 250 entities in a
50 KB body.

| Field | Meaning |
|---|---|
| `id` | `<level>-<role>-<n>`, stable for the whole simulated day |
| `name` | Generated Swedish name, shown as the label under the icon |
| `type` | `man` or `woman`; picks the viewer icon |
| `level` | The floor the person is on; they never change it |
| `room` | Set only while standing **in** a room. Omitted while walking, because the person is then in a corridor and the corridor is not where the viewer should anchor them |
| `position` | `[x, y]` in plan units, inside the floor page. Occupants of a room get their own spot in it, never the shared room node |
| `heading` | Degrees, from the direction of the current route segment |
| `status` | Human-readable, e.g. `student: lecture in A1505`, `guard: patrolling A109`, `staff: leaving` |
| `transition_ms` | Equal to `publish_interval_ms`, so the viewer glides from the previous position over exactly the time until the next write |

Response:

```json
{ "status": "updated", "version": 42 }
```

BuildSim stamps each entity with a `timestamp` of its own; the simulator does
not send one.

### What BuildSim rejects

| Rule | Error |
|---|---|
| At most 2000 entities per write | `at most 2000 mobile entities are allowed` |
| `id` non-empty and unique | `entity 3 requires an id` / `duplicate entity id "x"` |
| `type` one of `man`, `woman`, `group`, `robot`, `cleaning_robot`, `generic` | `entity "x" has unsupported type "y"` |
| `room` must exist (and then fixes the level) | `room "1541" does not exist` |
| `level` required when there is no `room`, and must exist | `level is required` |
| `position` inside the floor page | `position: coordinates must be inside the floor page` |
| `heading` finite, `transition_ms` between 0 and 60000 | |

A rejected write is an all-or-nothing 400: the previous collection stays. The
simulator logs the failure and retries with the next snapshot.

---

## `PUT /api/occupancy` — who is in which room

Keys are `<level>/<room>`; the value lists the people the room holds. The
`aliens` array is part of BuildSim's format and is always empty here.

```http
PUT /api/occupancy HTTP/1.1
Host: 127.0.0.1:9090
Content-Type: application/json
```

```json
{
  "level0/1541": {
    "persons": [
      { "id": "level0-staff-1", "name": "Hugo Gustafsson", "icon": "man", "position": [31.8, 183.08] }
    ],
    "aliens": []
  },
  "level0/A1505": {
    "persons": [
      { "id": "level0-student-12", "name": "Ida Lindström", "icon": "woman", "position": [63.97, 12.16] },
      { "id": "level0-student-27", "name": "Maria Persson", "icon": "woman", "position": [66.41, 14.02] }
    ],
    "aliens": []
  }
}
```

Corridors are rooms to BuildSim, so people walking through one appear under
that corridor's key. Rooms with nobody in them are left out entirely rather
than sent as empty lists.

| Field | Meaning |
|---|---|
| `persons[].id` | Same id as the entity, so the two collections agree |
| `persons[].icon` | `man` or `woman` |
| `persons[].position` | The person's own spot in the room, same value as the entity's |
| `aliens` | Always `[]`; BuildSim requires the key to be present |

Response is the same `{"status": "updated", "version": N}`.

### What BuildSim rejects

| Rule | Error |
|---|---|
| Every key must resolve to a room | `room key "level0/nope" does not identify a room` |
| A bare room name that exists on several floors is ambiguous | `room key "A1119" is ambiguous; use <level>/<room>` |
| Two keys may not resolve to the same room | `more than one key resolves to "level0/1541"` |

The simulator always sends the canonical `<level>/<room>` form, so none of
these should occur.

### Why it is not sent every second

Room occupancy only changes when somebody enters or leaves a room, and BuildSim
rebuilds every room sprite of a floor when it receives a write. Repeating an
identical document once a second is load on both sides for nothing, so the
simulator sends one only when:

- at least `occupancy_interval_ms` has passed since the last write, **and**
- the document differs from the one BuildSim already has, **or** a write failed
  since, **or** the last write is more than 30 s old.

The refresh and the failure flag exist because the collection is a complete
snapshot with no acknowledgement: if BuildSim restarts unnoticed, the next
refresh restores the full picture within half a minute. `GET /api/state` on the
simulator reports `buildsim.last_occupancy` so you can see when it last went
out.

---

## Shutdown

On `SIGINT`/`SIGTERM` the simulator clears both collections, so a stopped
simulation does not leave people standing in the viewer forever:

```http
PUT /api/entities    []
PUT /api/occupancy   {}
```

---

## Reproducing a publish by hand

```bash
# what the simulator would send right now, from its own API
curl -s http://127.0.0.1:8081/api/state | jq '.sim.counts'

# the two collections as BuildSim currently holds them
curl -s http://127.0.0.1:9090/api/entities  | jq '.[0]'
curl -s http://127.0.0.1:9090/api/occupancy | jq 'to_entries[0]'

# a minimal write of your own (replaces everything the simulator published)
curl -X PUT http://127.0.0.1:9090/api/entities \
  -H 'Content-Type: application/json' \
  -d '[{"id":"demo-1","name":"Demo","type":"man","level":"level0",
        "position":[175,20],"heading":0,"status":"demo","transition_ms":1000}]'
```

The last call takes over the collection until the simulator's next publish
overwrites it a second later.

See [model.md](model.md) for how the people being published are generated.
