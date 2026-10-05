# Entrances, doors, and locks

`GET/PUT /api/doors`

Doors are semantic runtime objects. The static red lines in the floor plan show
architectural openings; this endpoint adds identity, operational state, lock
state, and—when linked to an entry node—a navigation constraint.

```bash
curl -X PUT http://127.0.0.1:9090/api/doors \
  -H 'Content-Type: application/json' -d '[
  {
    "id":"door-A109",
    "name":"A109 access door",
    "kind":"door",
    "level":"level0",
    "room":"A109",
    "entry_node_id":524,
    "state":"closed",
    "lock_state":"locked"
  },
  {
    "id":"east-entrance",
    "name":"East entrance",
    "kind":"entrance",
    "level":"level0",
    "position":[337.75,258.3],
    "state":"open",
    "lock_state":"unlocked"
  }
]'
```

| Field | Values and meaning |
|---|---|
| `kind` | `door`, `entrance`, or `fire_exit` |
| `state` | `open`, `closed`, or `blocked` |
| `lock_state` | `unlocked`, `locked`, or `jammed` |
| `entry_node_id` | Optional entry-node ID from that floor's walkable graph |
| `position` | Optional `[x,y]` floor-plan coordinate; inferred from an entry node when possible |
| `blocked` | Read-only derived value: true for blocked, locked, or jammed doors |

A closed but unlocked door remains routable. A locked, jammed, or explicitly
blocked door removes its entry node from routes returned by
`GET /api/graph/route?type=walkable`. This affects route calculation only; it
does not automatically command a lock actuator or move an occupant.

Room-only locks block every walkable entry carrying that room name. Use
`entry_node_id` when a room has several doors and only one should be blocked.
Node IDs are local to a floor; obtain them from:

```bash
curl -s 'http://127.0.0.1:9090/api/graph?level=level0&type=walkable'
```

An exterior entrance may have only a position. It will be shown in the viewer,
but it cannot constrain a route unless the floor's walkable graph contains a
corresponding entry node.

The door snapshot is a renderer/navigation input, not a security boundary.
BuildSim has no authentication. A project still needs an actuator service that
checks command authority, validates transitions, operates the simulated lock,
and reports the resulting door state.
