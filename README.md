# Smart HVAC Optimisation (Group 9)

This repo holds our D7065E project: a smart HVAC system for a simulated office building,
built on top of BuildSim.

The idea is simple. Rooms fill up and empty out during the day. Instead of running
heating, cooling, and ventilation on a fixed schedule, we want the system to look at
occupancy, temperature, and humidity in each room and decide when to actually turn
things up or down. When a meeting room fills up, it should get more cooling or
ventilation. When it empties, it should drop into a low-energy mode. The goal is to
keep people comfortable without wasting energy.

See the project proposal for the full write-up of the use case, scenario, and plan.

## What's in here

- `services/simulator` - generates the physical behaviour behind the sensors: room
  temperature from a heat balance model, humidity, and occupancy patterns through the day.
- `services/sensors/temperature`, `services/sensors/humidity`, `services/sensors/occupancy` -
  one process per sensor type, handling every managed room.
- `services/actuators/hvac` - the only process that writes HVAC actuator state into
  BuildSim; validates and clamps whatever a controller proposes.
- `services/controller` - the adaptive autonomous decision-maker. Watches sensor data
  and a short-horizon occupancy/temperature forecast and decides when to change HVAC
  settings, room by room.
- `services/baseline-controller` - a fixed Mon-Fri 08:00-18:00 schedule with no sensor
  feedback at all, run on its own room purely as the evaluation baseline.
- `services/pipeline` - collects sensor readings, keeps recent history, and computes a
  simple linear-trend forecast per room/signal.
- `services/evaluation` - continuously measures energy (mean fan speed) and comfort
  (% time in-band while occupied) per room, and compares the adaptive rooms against
  the baseline room.
- `services/dashboard` - shows room state, forecasts, controller/actuator decisions,
  and the evaluation comparison, aggregated server-side.
- `buildingsim/`, `occupancysim/` - the course-provided BuildSim server and occupancy
  simulator.
- `docs/final-report/` - the architecture document (LaTeX + D2 diagrams).

Each service is meant to run as its own process (containerized with Docker) and talk to
the others through a defined interface, not by importing each other's code. BuildSim
provides the building itself and the API we plug into.

## Running the stack

Everything (BuildSim, the occupancy simulator, and all our services) runs via the root
`docker-compose.yaml`, wired to four office rooms on `level0`: `A1009`, `A1010`, and
`A1013` under the adaptive controller, `A1015` under the fixed-schedule baseline.

```bash
docker compose up --build
```

Open:

- <http://127.0.0.1:9090> - BuildSim 3D viewer (the building, live sensor/actuator state).
- <http://127.0.0.1:8081> - occupancy simulator UI (controls how many people are in the
  building and how they move; also controls the simulated clock).
- <http://127.0.0.1:8093> - our dashboard (per-room readings, forecasts, controller and
  actuator decisions, and the live adaptive-vs-baseline evaluation comparison).
- <http://127.0.0.1:8099/api/report> - raw evaluation numbers (energy/comfort).

`services/simulator` seeds `hvac-<room>` equipment (temperature/humidity sensors,
setpoint/fan actuators) automatically on startup for every room in `ROOM_NAMES`, then
closes the loop every `TICK_SECONDS` by reading occupancy and actuator state back from
BuildSim and writing new sensor values. The sensor processes republish those values to
`services/pipeline`; `services/controller` decides a setpoint/fan speed every
`DECIDE_SECONDS` for its rooms, `services/baseline-controller` does the same on a fixed
schedule for its own room, and `services/actuators/hvac` is the only process that
writes actuator state back into BuildSim, for either controller.

All service ports (8090-8099, 9090) are published to `127.0.0.1` only. To change which
rooms are managed, edit the `ROOM_NAMES` (adaptive) / `ROOM_NAME` (baseline) /
`ADAPTIVE_ROOMS` / `BASELINE_ROOMS` environment variables in `docker-compose.yaml` -
any room BuildSim's floor plan reports as `type: "room"` with area under the
office threshold works; check via
`GET /api/building/floors/{level}` before picking one (see the architecture
document's Section 1 for why this matters).

