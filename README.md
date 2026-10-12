# Smart HVAC Optimisation (Group 9)

Our project for D7065E at LTU, by Monika Shrivastava and Abhay Soni.

Offices are often heated on a fixed schedule, whether anyone is there or not. We built a
controller that heats each room based on what is actually happening in it. It reads
temperature, humidity and occupancy, learns when each room is usually used, pre-heats
before people arrive and turns the heat down after they leave. If it cannot trust the
sensor data, it falls back to a safe setting instead of guessing.

Everything runs on top of BuildSim, the course's simulated building. To see whether our
controller is any better, a fixed schedule controls two other offices of the same size,
and we compare the two.

**Report:** [docs/D7065E_Group9_Final_Report.pdf](docs/D7065E_Group9_Final_Report.pdf)
(architecture, requirements, tests, results and what we learned). The original proposal is
[docs/proposal.md](docs/proposal.md).

## What's in the repo

| Folder | What it does |
|---|---|
| `services/simulator` | The physics: heat and moisture in each room, the room heating unit, the fan and an energy meter |
| `services/sensors` | One sensor program, run as four containers (temperature, humidity, occupancy, energy). Adds noise, keeps readings while the pipeline is down, and can be told to lie for testing |
| `services/pipeline` | Checks each reading, drops duplicates, saves everything to a file, and marks data that is old or stuck |
| `services/controller` | Our controller: simple rules, the learned table of when rooms are used, and the data check |
| `services/baseline-controller` | The fixed schedule we compare against (Mon–Fri, 07:00–18:00) |
| `services/actuators/hvac` | The only service that changes the heating. It limits values, checks which controller owns a room, and goes to a safe setting if the controller goes quiet for 60 s |
| `services/evaluation` | Measures comfort and energy for both controllers |
| `services/dashboard` | Our web page, plus a temperature layer and alerts in the BuildSim 3D view |
| `buildingsim/`, `occupancysim/` | From the course, not changed by us |
| `tests/` | System tests (end to end, fault injection, stopping services) and the stress test |
| `scripts/` | Start a clean experiment, make the plots, build the report |
| `docs/` | The report, its diagrams (`diagrams/`, Mermaid) and pictures (`figures/`), and the measured results (`evidence/`) |

## How to run it

You need Docker (with Compose). For the tests you also need Python 3. Run everything from
the top folder of the repo.

```bash
docker compose up --build -d     # first time takes a few minutes
docker compose ps                # wait a minute, everything should be "healthy"
```

`occupancy-sim` has no health check, so it only shows "Up". That's fine.

Then open:

- <http://127.0.0.1:8093>: our dashboard
- <http://127.0.0.1:9090/?layer=hvac-temperature&floor=level0>: the BuildSim 3D view with our layer
- <http://127.0.0.1:8081>: people in the building and the clock (runs 60× faster than real time)

Our controller runs A1009 and A1013, the fixed schedule runs A1010 and A1015. You can change
the rooms and timings in `docker-compose.yaml`.

```bash
docker compose logs -f controller   # see what the controller is doing
docker compose down                 # stop (data is kept)
docker compose down -v              # stop and wipe the data
```

The pipeline, controller and evaluation save their data on Docker volumes, so restarting
doesn't lose anything. Only `down -v` deletes it.

## Tests

Unit tests (you don't need Go installed, they run in a Go container):

```bash
for s in simulator sensors pipeline controller actuators/hvac baseline-controller evaluation dashboard; do
  docker run --rm -v "$PWD/services/$s":/src -w /src golang:1.23-alpine go test ./... ; done
```

System tests, with the system running. They walk a person in end to end, make sensors
lie or go quiet, and stop each service one by one. Takes about 30 minutes.

```bash
python3 tests/system/system_test.py
python3 tests/system/system_test.py e2e f2    # just the ones you name
```

Stress test. It starts its own copy of the system, so your running one isn't touched, and
keeps adding rooms and speeding up until it can't keep up. About 20 minutes.

```bash
python3 tests/stress/stress_test.py
```

Heads up: both of these overwrite our results in `docs/evidence/`, so copy them first if you
want to keep them.

## Other useful commands

```bash
# rebuild and restart just one service
# (keep --no-deps, otherwise BuildSim restarts too and the building state is gone)
docker compose up -d --build --no-deps controller

# start a fresh comparison run from Monday 00:00
# (this wipes the learned table and the measurements so far; one model day = 24 real minutes)
./scripts/start_experiment.sh

# redraw the plots from docs/evidence/ (needs matplotlib)
python3 scripts/plot_results.py

# redraw the diagrams and rebuild the report PDF (needs Node.js, and Docker or pdflatex)
python3 scripts/build_report.py
python3 scripts/build_report.py --diagrams-only   # only the diagrams
```
