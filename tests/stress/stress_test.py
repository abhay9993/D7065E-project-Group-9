#!/usr/bin/env python3
"""Stress test: how many rooms (and how fast an update rate) before the loop misses its deadlines?

It starts a separate copy of the stack (project "g9stress", no host ports, so it does not
touch the normal one), puts N rooms under the adaptive controller, and reads:

  - simulator  last_tick_ms   budget: TICK_SECONDS   (5 s)  - physics for every room
  - controller last_cycle_ms  budget: DECIDE_SECONDS (10 s) - decisions for every room (NFR-07)
  - how many rooms the controller had to put in failsafe (their data went stale)
  - pipeline readings stored per second

Run from the repository root:  python3 tests/stress/stress_test.py         (rooms 10 -> 300, then 5x rate)
                               python3 tests/stress/stress_test.py fast    (300 rooms at 10x, 25x, 50x rate)
Results go to docs/evidence/stress_results.json. Takes ~20 minutes (first run builds images).
"""

import json
import os
import subprocess
import sys
import time
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
OVERRIDE = Path(__file__).resolve().parent / "no-host-ports.yaml"
RESULTS = ROOT / "docs" / "evidence" / "stress_results.json"
PROJECT = ["docker", "compose", "-p", "g9stress", "-f", str(ROOT / "docker-compose.yaml"), "-f", str(OVERRIDE)]

# (rooms, tick s, poll s, decide s)
STEPS = [(10, 5, 5, 10), (50, 5, 5, 10), (100, 5, 5, 10), (200, 5, 5, 10), (300, 5, 5, 10),
         (300, 1, 1, 2), (100, 1, 1, 2)]
# Faster than the default 5 s / 10 s: 10x, 25x and 50x the update rate.
FAST_STEPS = [(300, 0.5, 0.5, 1), (300, 0.2, 0.2, 0.4), (300, 0.1, 0.1, 0.2)]


def dur(seconds):
    """Whole seconds as before ("5"), fractions as a Go duration ("500ms")."""
    return str(seconds) if float(seconds).is_integer() else f"{round(seconds * 1000)}ms"


def level0_rooms():
    """Every room with an area on level 0, read from the running BuildSim (or the floor-plan file)."""
    try:
        with urllib.request.urlopen("http://127.0.0.1:9090/api/building/floors/level0", timeout=10) as r:
            plan = json.load(r)
    except Exception:
        plan = json.loads((ROOT / "buildingsim/data/abuilding/level0/floorplan_data.json").read_text())
    names = sorted({room["name"] for room in plan["rooms"] if room.get("area", 0) > 0 and room.get("name")})
    return names


def inside(url):
    """GET a URL from inside the stress stack's network (no host ports there)."""
    out = subprocess.run([*PROJECT, "exec", "-T", "dashboard", "wget", "-qO-", url], capture_output=True, text=True)
    return json.loads(out.stdout)


def run_step(rooms, tick, poll, decide):
    env = dict(os.environ, ALL_ROOMS=",".join(rooms), ADAPTIVE_ROOMS=",".join(rooms), BASELINE_ROOMS="NONE",
               ROOM_OWNERS=",".join(f"{r}=controller" for r in rooms),
               TICK_SECONDS=dur(tick), POLL_SECONDS=dur(poll), DECIDE_SECONDS=dur(decide))
    subprocess.run([*PROJECT, "up", "-d", "--build"], env=env, check=True, capture_output=True)
    time.sleep(max(60, 6 * decide))  # let every service run several cycles with the new settings

    ticks, cycles, failsafe, rates = [], [], [], []
    accepted_before = inside("http://pipeline:8090/api/metrics")["accepted"]
    t0 = time.monotonic()
    for _ in range(6):
        time.sleep(5)
        ticks.append(inside("http://simulator:8094/api/status")["last_tick_ms"])
        status = inside("http://controller:8092/api/status")
        cycles.append(status["last_cycle_ms"])
        failsafe.append(sum(1 for r in status["rooms"].values() if r["last_command"]["mode"] == "failsafe"))
    rate = (inside("http://pipeline:8090/api/metrics")["accepted"] - accepted_before) / (time.monotonic() - t0)

    result = {"rooms": len(rooms), "tick_s": tick, "poll_s": poll, "decide_s": decide,
              "sim_tick_ms_max": max(ticks), "controller_cycle_ms_max": max(cycles),
              "rooms_in_failsafe_max": max(failsafe), "readings_per_s": round(rate, 1),
              "expected_readings_per_s": round(4 * len(rooms) / poll, 1)}
    result["tick_within_budget"] = result["sim_tick_ms_max"] <= tick * 1000
    result["decisions_within_budget"] = result["controller_cycle_ms_max"] <= decide * 1000
    result["data_keeps_up"] = result["rooms_in_failsafe_max"] == 0 and rate >= 0.9 * result["expected_readings_per_s"]
    return result


def main():
    all_rooms = level0_rooms()
    fast = "fast" in sys.argv
    results = json.loads(RESULTS.read_text()) if fast and RESULTS.exists() else []
    try:
        for n, tick, poll, decide in (FAST_STEPS if fast else STEPS):
            rooms = all_rooms[:n]
            print(f"--- {len(rooms)} rooms, tick {tick}s, poll {poll}s, decide {decide}s", flush=True)
            result = run_step(rooms, tick, poll, decide)
            print("    " + json.dumps(result), flush=True)
            results.append(result)
            RESULTS.parent.mkdir(parents=True, exist_ok=True)
            RESULTS.write_text(json.dumps(results, indent=2))
    finally:
        if "--keep" not in sys.argv:
            subprocess.run([*PROJECT, "down", "-v"], capture_output=True)
    print(f"results written to {RESULTS}")


if __name__ == "__main__":
    main()
