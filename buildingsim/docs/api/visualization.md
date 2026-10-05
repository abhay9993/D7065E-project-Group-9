# Simulation and visualization state

BuildSim can display state calculated by student services. These endpoints are
available in the modern viewer and update connected browsers automatically.
They are deliberately separate from equipment sensors:

- a **room layer** is physical state or an estimate, such as simulated room
  temperature or predicted fire risk;
- a **sensor** is an observation made by a particular device;
- an **effect** is a visual rendering instruction, not a physical model;
- an **alert** is current decision output, not a persistent event log.

BuildSim stores only the latest snapshots. The student system owns physics,
movement planning, estimation, control decisions, and historical evidence.

All collection `PUT` operations replace that complete collection. Send `[]` to
clear it. Successful changes generate a small WebSocket notification; viewers
then fetch the canonical REST snapshot.

## Room layers

`GET/PUT /api/room-layers`

```bash
curl -X PUT http://127.0.0.1:9090/api/room-layers \
  -H 'Content-Type: application/json' -d '[{
    "id":"temperature",
    "label":"Simulated room temperature",
    "unit":"°C",
    "source":"simulation truth",
    "minimum":18,
    "maximum":30,
    "opacity":0.75,
    "palette":["#2563eb","#22c55e","#facc15","#dc2626"],
    "values":{"level0/A109":27.4,"level0/A110":23.1}
  }]'
```

The values map uses canonical `<level>/<room>` keys. Open **Layers** in the
viewer and click a named room heatmap to display it; click **Off** to remove the
overlay. Newly published layers appear in this list automatically. If none are
published, the older sensor-derived temperature and CO₂ views remain available.

Run the minimal temperature-heatmap example from the `buildingsim/` directory:

```bash
./examples/api/room-layers/set_temperature_heatmap.sh
```

Then open
<http://127.0.0.1:9090/?layer=temperature&floor=level0>. The viewer linearly
interpolates between adjacent palette colours after mapping each value onto the
`minimum`–`maximum` range. Values outside that range use an endpoint colour;
rooms omitted from `values` remain uncoloured. Keep the range fixed when
publishing successive snapshots if colours must remain comparable over time.

The `PUT` replaces the complete room-layer collection. A simulator that
publishes several layers should therefore send all of them together rather
than issuing one request per layer.

Useful `source` descriptions include `simulation truth`, `sensor estimate`,
`prediction`, and `decision-service estimate`. The field is descriptive: it
does not grant authority or change how BuildSim treats the data.

## Physical effects

`GET/PUT /api/effects`

```json
[
  {
    "id": "fire-A109",
    "type": "fire",
    "label": "Fire source",
    "level": "level0",
    "room": "A109",
    "radius": 7,
    "height": 15,
    "intensity": 0.9
  },
  {
    "id": "smoke-A109",
    "type": "smoke",
    "level": "level0",
    "position": [269, 247],
    "radius": 8,
    "height": 18,
    "intensity": 0.8
  }
]
```

Supported types are `fire`, `smoke`, `gas`, `sprinkler`, `water`, and
`warning`. Give either a room, an exact `[x,y]` floor-plan position, or both.
BuildSim animates the primitive but does not spread fire, diffuse gas, or apply
sprinkler physics. A simulator must update the snapshot as the process evolves.

## Moving people and robots

`GET/PUT /api/entities`

```json
[
  {
    "id": "cleaner-1",
    "name": "Cleaning robot",
    "type": "cleaning_robot",
    "level": "level0",
    "room": "A108",
    "position": [258, 273],
    "heading": 35,
    "status": "returning to dock",
    "transition_ms": 1200
  }
]
```

Types are `man`, `woman`, `group`, `robot`, `cleaning_robot`, and `generic`.
The browser interpolates from the previous position over `transition_ms`.
Route selection, speed, collision handling, and occupancy updates belong to the
student service. Up to 2,000 entities may be present in one snapshot.

## Room illumination

`GET/PUT /api/room-appearance`

```json
[
  {"level":"level0","room":"A110","color":"#ffd166","brightness":0.85}
]
```

`brightness` is between 0 and 1. This is useful for lighting control and for
showing a visible actuator effect. It is not an illuminance calculation; store
measured or simulated lux in a sensor or room layer.

## Current decision alerts

`GET/PUT /api/alerts`

```json
[
  {
    "id":"fire-confirmed",
    "severity":"critical",
    "title":"Fire confirmed in A109",
    "message":"Sprinkler active; access door is locked.",
    "level":"level0",
    "room":"A109"
  }
]
```

Severity is `info`, `warning`, or `critical`. Alerts appear as small cards and
can navigate to their room. The endpoint is capped at 100 current alerts. Use
the project data pipeline—not this endpoint—as the audit trail.

## Exact equipment positions

Equipment still requires `level` and `room`, but it may now also include:

```json
{
  "position": [260, 246],
  "height": 10,
  "heading": 90
}
```

Coordinates use the same floor-plan coordinate system returned by
`GET /api/building/floors/{level}`. If `position` is omitted, the viewer lays
equipment out around the room centre as before. `height` is local height above
the floor; `heading` is in degrees and is retained for clients that render
oriented equipment.

## Reproducible example

With BuildSim running, publish all of the effects above:

```bash
./examples/api/scenario/show_visual_effects.sh
```

Open
<http://127.0.0.1:9090/?layer=temperature&floor=level0&room=A109>.
Clear the transient state with `clear_visual_effects.sh`. Demo equipment remains
registered until BuildSim restarts.
