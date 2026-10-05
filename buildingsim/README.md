# BuildSim

BuildSim is a laptop-local building simulation API and browser viewer. One Go
binary embeds the A-building floor plans, navigation graphs, equipment icons,
two viewer interfaces, and all browser dependencies.

It runs without an account, cloud service, API key, or internet connection.
Runtime state is kept in memory and resets when the process stops.

## Quick start

BuildSim requires Go 1.25 or newer.

```bash
make build
./bin/buildsim start
```

Open <http://127.0.0.1:9090>. The modern 3D viewer is the default. To use the
original interface instead:

```bash
./bin/buildsim start --ui classic
```

Both interfaces use exactly the same REST API, WebSocket protocol, embedded
floor data, and in-memory state.

## Useful commands

```bash
make run          # modern viewer on 127.0.0.1:9090
make run-classic  # original viewer, same API
make run-edit     # enable floor-plan editing
make test         # complete regular test suite
make docs-test    # executable API/tutorial documentation examples
make race         # complete suite with the Go race detector
make stress       # bounded test plus a 30-second concurrent soak
make stress-ui    # real Chromium notification-storm test
make docker-build # local container image
make release      # cross-platform binaries in dist/
make checksums    # release binaries plus dist/SHA256SUMS
```

The normal test suite includes a bounded concurrent API stress test. The soak
test is opt-in and its duration can be changed:

```bash
BUILDSIM_STRESS=1 BUILDSIM_STRESS_DURATION=2m \
  go test -race ./pkg/server -run 'TestStress(Bounded|Soak)API' -count=1
```

The UI stress test requires Chromium or Chrome. It opens both actual viewers,
sends 1,001 sensor mutations to each, verifies the final rendered value and
live session, and checks that notifications are coalesced into a bounded number
of full equipment fetches. Set `BUILDSIM_CHROMIUM=/path/to/chromium` if it is
not discoverable on `PATH`.

## Docker Compose

```bash
docker compose up --build
```

Open <http://127.0.0.1:9090>. The published port remains loopback-only. Student
services added to this Compose project reach the API at
`http://buildsim:9090`; they must not use `127.0.0.1` for another container.
The image includes a health check backed by `GET /healthz` and the
`buildsim health` command.

## Network behavior

The default address is `127.0.0.1:9090`, so other machines cannot connect.
There is no authentication. Binding to a LAN interface must therefore be an
explicit decision, for example:

```bash
./bin/buildsim start --all-interfaces --port 9090
```

This is equivalent to `--host 0.0.0.0`. Open the viewer using the laptop's LAN
address, such as `http://192.168.1.25:9090`. Because BuildSim has no
authentication, use this only on a trusted network and check the laptop
firewall. `--all-interfaces` and `--host` cannot be combined.

Browser CORS and WebSocket origins are restricted to the same origin, with a
loopback-to-loopback allowance for local development.

## State and room references

Equipment uses separate `level` and `room` fields:

```json
{"level":"level0","room":"A109"}
```

Occupancy maps use canonical `<level>/<room-name>` keys, and highlights include
both fields:

```json
{
  "level0/A109": {
    "persons": [{"id":"p1","name":"Alice"}],
    "aliens": []
  }
}
```

```json
[
  {"level":"level0","room":"A109","color":"#e53935","opacity":0.5}
]
```

An unqualified room name or legacy numeric room ID is accepted only when it is
unique across the building. New code should always qualify the floor.

## Main interfaces

| Area | Endpoints |
|---|---|
| Building | `GET /api/building`, `/api/building/floors/{level}`, `/api/building/cross-floor-edges` |
| Equipment | CRUD at `/api/equipment`; bulk create at `/api/equipment/bulk` |
| Sensors | `GET /api/sensors/{id}`, `PUT /api/sensors/{id}/value` |
| Actuators | `GET /api/actuators/{id}`, `PUT /api/actuators/{id}/state` |
| Global overlays | `GET/PUT /api/occupancy`, `GET/PUT /api/coverage` |
| Simulation views | `GET/PUT /api/room-layers`, `/effects`, `/entities`, `/room-appearance`, `/alerts` |
| Doors and locks | `GET/PUT /api/doors` (locked entry nodes constrain walkable routes) |
| Sessions | CRUD at `/api/sessions`; viewport, highlights, occupancy, coverage, and routes below each session |
| Live updates | `GET /ws/{session-id}` (WebSocket upgrade) |
| Navigation | `GET /api/graph`, `GET /api/graph/route` |
| Health | `GET /healthz` |

Every successful equipment, sensor, or actuator mutation automatically updates
the equipment version and notifies connected viewers. The legacy
`POST /api/equipment/notify` endpoint remains available but is normally not
needed.

## Editing

Editing endpoints do not exist unless the server starts with `--edit`. Without
`--edit-output`, saved floors affect only the running process. To persist edited
JSON separately from the embedded source data:

```bash
./bin/buildsim start --edit --edit-output ./edited-data
```

BuildSim writes exports atomically under that directory. It never overwrites
the embedded source floor plans.

## Go client and documentation

The standard-library client in [`pkg/client`](pkg/client/) is intended as a
small, readable starting point for student projects:

```bash
go run ./examples/go-client
```

More detail:

- [student lab quickstart](docs/lab-quickstart.md)
- [API reference](docs/api/)
- [simulation and visualization API](docs/api/visualization.md)
- [entrances, doors, and locks](docs/api/doors.md)
- [architecture](docs/architecture.md)
- [equipment icons](docs/icons.md)

When changing files under `web/modern/src`, run `go generate ./...` before
building. `make build`, `make test`, and `make release` do this automatically.
