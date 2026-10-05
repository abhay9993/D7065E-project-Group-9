# Sessions and overlays API

A viewer creates a session and connects to `/ws/{session-id}`. REST clients can
then change the state rendered by that particular browser.

## Session lifecycle

| Method | Endpoint | Result |
|---|---|---|
| `POST` | `/api/sessions` | Create and return a session |
| `GET` | `/api/sessions` | List active sessions |
| `GET` | `/api/sessions/{id}` | Return a state snapshot |
| `DELETE` | `/api/sessions/{id}` | Delete a session |
| `GET` upgrade | `/ws/{id}` | Subscribe to ordered state messages |

```bash
curl -X POST http://127.0.0.1:9090/api/sessions
```

Sessions with no WebSocket activity are removed after one hour. A newly
connected socket first receives the current viewport, highlights, occupancy,
coverage, and route (when present), so connecting after a REST update does not
lose that update.

Messages use this envelope:

```json
{"type":"highlights","data":[],"version":3}
```

Every successful session mutation returns the same monotonically increasing
`version` in its JSON response and WebSocket message. Clients can therefore
discard stale messages after reconnecting.

Equipment and global occupancy may use a version-only notification, after
which the viewer fetches the latest global state. Session-specific occupancy is
sent directly in `data`.

## Viewport

```http
PUT /api/sessions/{id}/viewport
```

```json
{"floor":"level0","room":"A109","zoom":2.0,"mode":"2d"}
```

`mode` is `2d` or `3d`; `zoom` must be non-negative. When `room` is present,
the floor and room are validated and normalized.

## Highlights

```http
PUT /api/sessions/{id}/highlights
```

```json
[
  {"level":"level0","room":"A109","color":"#ff0000","opacity":0.8},
  {"level":"level1","room":"A2306","color":"#00ff00","opacity":0.5}
]
```

`opacity` must be between 0 and 1. Omitted color and opacity default to
`#ffcc00` and `0.8`; an explicit opacity of `0` remains fully transparent.
Send `[]` to clear all highlights. `room_id` remains a
legacy alternative to `room`, but should be accompanied by `level`.

## Occupancy

There are two scopes:

| Endpoint | Scope |
|---|---|
| `GET/PUT /api/occupancy` | Global; visible to every viewer |
| `PUT /api/sessions/{id}/occupancy` | One browser session |

Both accept a map keyed by canonical `<level>/<room-name>` strings:

```json
{
  "level0/A109": {
    "persons": [
      {"id":"person-1","name":"Alice","icon":"woman"},
      {"id":"person-2","name":"Bob","icon":"man"}
    ],
    "aliens": []
  }
}
```

Send `{}` to clear the session layer and reveal the global baseline. A
session-specific room replaces the global value for that room. Unqualified
names and numeric IDs are accepted only when they resolve uniquely;
qualification is recommended for every new client.

## Coverage

Coverage has the same global and session scopes:

```http
PUT /api/coverage
PUT /api/sessions/{id}/coverage
```

```json
[
  {
    "id":"wifi-A109",
    "name":"Wi-Fi A109",
    "level":"level0",
    "room":"A109",
    "center":[0,0],
    "radius":25,
    "color":"#00aaff",
    "opacity":0.15,
    "height":20
  }
]
```

IDs must be unique within the request, radius must be positive, and opacity
must be between 0 and 1. If `room` is omitted, `center` is interpreted in the
floor plan's PDF coordinate system. Session zones are added to the global
baseline and replace global zones with the same ID.

## Route display

Use [`GET /api/graph/route`](graph.md) to compute a path, then send that result
to one viewer:

```http
PUT /api/sessions/{id}/route
```

```json
{
  "path":[
    {"room_id":0,"name":"1540","x":17.88,"y":187.75},
    {"room_id":5,"name":"1542","x":6.3,"y":179.1}
  ],
  "distance":14.5
}
```

This is the exact result of
`GET /api/graph/route?from=0&to=5&level=level0`; sending a computed result
avoids hand-writing inconsistent waypoints.

## Complete browser example

```bash
BASE=127.0.0.1:9090
SESSION=$(curl -s http://$BASE/api/sessions | python3 -c '
import json,sys
s=sorted(json.load(sys.stdin),key=lambda x:x.get("last_ws_active",""))
print(s[-1]["id"] if s else "")
')

if [ -z "$SESSION" ]; then
  echo "No viewer session found; open http://$BASE first" >&2
  exit 1
fi

curl -X PUT http://$BASE/api/sessions/$SESSION/viewport \
  -H 'Content-Type: application/json' \
  -d '{"floor":"level0","room":"A109","zoom":2,"mode":"3d"}'

curl -X PUT http://$BASE/api/sessions/$SESSION/highlights \
  -H 'Content-Type: application/json' \
  -d '[{"level":"level0","room":"A109","color":"#ff0000","opacity":0.8}]'

curl -X PUT http://$BASE/api/sessions/$SESSION/occupancy \
  -H 'Content-Type: application/json' \
  -d '{"level0/A109":{"persons":[{"id":"p1","name":"Alice"}],"aliens":[]}}'
```
