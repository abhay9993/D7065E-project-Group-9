# BuildSim API Reference

All endpoints are relative to `http://127.0.0.1:9090` by default.

New to the D7065E lab? Start with the [Lab Quickstart](../lab-quickstart.md), which maps the
assignment onto these endpoints.

| Document | Endpoints |
|----------|-----------|
| [Building Data](building.md) | `GET /api/building`, `GET /api/building/floors/{level}`, `GET /api/building/cross-floor-edges` |
| [Equipment](equipment.md) | `POST/GET/PUT/DELETE /api/equipment`, `POST /api/equipment/bulk`, sensors, actuators |
| [Sessions](sessions.md) | `POST/GET/DELETE /api/sessions`, viewport, highlights, occupancy, coverage, route, WebSocket |
| [Navigation Graph](graph.md) | `GET /api/graph`, `GET /api/graph/route` (Dijkstra) |
| [Simulation & Visualization](visualization.md) | Room layers, effects, entities, illumination, alerts, exact positions |
| [Entrances, Doors & Locks](doors.md) | `GET/PUT /api/doors`, walkable-route constraints |
| [Icons](icons.md) | `GET /api/icons/{name}.svg` |
| [Config](config.md) | `GET /api/config` |

`GET /healthz` is intentionally outside `/api`; it returns `{"status":"ok"}`
for container and process health checks.
