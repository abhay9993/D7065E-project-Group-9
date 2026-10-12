#!/usr/bin/env python3
"""System tests for the running stack: one end-to-end scenario and the fault-injection tests.

Run from the repository root while `docker compose up -d` is running:

    python3 tests/system/system_test.py            # everything (~30 minutes)
    python3 tests/system/system_test.py e2e f2     # only some tests

Every test measures time-to-detect and time-to-recover and is traced to a requirement ID
from the report (docs/D7065E_Group9_Final_Report.pdf, Section 10). Results go to docs/evidence/system_test_results.json.

The harness is Python (standard library only) instead of Go because it has to drive
`docker compose` on the host; it is not part of the system under test.
"""

import json
import subprocess
import sys
import time
import urllib.request
from datetime import datetime, timedelta, timezone
from pathlib import Path

PIPELINE = "http://127.0.0.1:8090"
ACTUATOR = "http://127.0.0.1:8091"
CONTROLLER = "http://127.0.0.1:8092"
OCCUPANCY = "http://127.0.0.1:8081"
SIMULATOR = "http://127.0.0.1:8094"
ADAPTIVE = ["A1009", "A1013"]
ALL_ROOMS = ["A1009", "A1010", "A1013", "A1015"]
SENSOR_PORTS = {"sensor-temperature": 8095, "sensor-humidity": 8096, "sensor-occupancy": 8097, "sensor-energy": 8089}
RESULTS = Path(__file__).resolve().parents[2] / "docs" / "evidence" / "system_test_results.json"


# ---------- small helpers ----------

def get(url):
    with urllib.request.urlopen(url, timeout=10) as r:
        return json.load(r)


def post(url, body):
    req = urllib.request.Request(url, data=json.dumps(body).encode(), method="POST",
                                 headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=10) as r:
        return r.status


def compose(*args):
    subprocess.run(["docker", "compose", *args], check=True, capture_output=True)


def room_state(room):
    return get(f"{PIPELINE}/api/rooms/state?room=level0/{room}")


def parse_time(s):
    return datetime.fromisoformat(s.replace("Z", "+00:00"))


def now():
    return datetime.now(timezone.utc)


def wait_until(check, timeout, every=1.0):
    """Poll check() until it returns something truthy; return (value, seconds waited)."""
    start = time.monotonic()
    while time.monotonic() - start < timeout:
        try:
            value = check()
            if value:
                return value, time.monotonic() - start
        except Exception:
            pass  # a component being down is exactly what some tests cause
        time.sleep(every)
    return None, None


def inject(service, mode, room=""):
    port = SENSOR_PORTS[service]
    body = json.dumps({"mode": mode, "room": room})
    subprocess.run(["docker", "compose", "exec", "-T", service, "wget", "-qO-", "--header", "Content-Type: application/json",
                    "--post-data", body, f"http://127.0.0.1:{port}/fault"], check=True, capture_output=True)


def controller_mode(room):
    return get(f"{CONTROLLER}/api/status")["rooms"][room]["last_command"]["mode"]


def fresh_temperature_after(t, rooms=ALL_ROOMS):
    for room in rooms:
        r = room_state(room)["readings"].get("temperature")
        if not r or parse_time(r["timestamp"]) <= t:
            return False
    return True


# ---------- the tests ----------

def test_e2e():
    """E2E (gate scenario, NFR-01): a person arrives in an adaptive office; the system senses it,
    decides, and the actuator applies 'occupied' within two decision cycles."""
    post(f"{OCCUPANCY}/api/clock", {"date": "2026-10-14", "time": "07:45", "factor": 60, "running": True})
    seen_empty = {room: False for room in ADAPTIVE}

    def arrival():
        for room in ADAPTIVE:
            occ = room_state(room)["readings"]["occupancy"]
            if occ["value"] == 0:
                seen_empty[room] = True
            elif seen_empty[room]:
                return room, parse_time(occ["timestamp"])
        return None

    found, _ = wait_until(arrival, timeout=600)
    if not found:
        return {"passed": False, "note": "no arrival observed in 10 minutes"}
    room, sensed_at = found

    def applied():
        a = get(f"{ACTUATOR}/state")[room]
        at = parse_time(a["applied_at"])
        return at if at > sensed_at and "occupant" in a["reason"] else None

    applied_at, _ = wait_until(applied, timeout=60, every=0.5)
    if not applied_at:
        return {"passed": False, "room": room, "note": "occupied command never applied"}
    latency = (applied_at - sensed_at).total_seconds()

    temp_at_arrival = room_state(room)["readings"]["temperature"]["value"]
    time.sleep(30)  # 30 model minutes
    temp_after = room_state(room)["readings"]["temperature"]["value"]
    return {"passed": latency <= 20, "requirement": "NFR-01", "room": room,
            "sense_to_actuate_s": round(latency, 1), "sense_to_actuate_model_min": round(latency, 1),
            "temp_at_arrival_c": temp_at_arrival, "temp_30_model_min_later_c": temp_after,
            "in_band_30_min_later": 20 <= temp_after <= 24}


def test_f1_buildsim_restart():
    """F1 (NFR-02): BuildSim restarts and forgets every registered device. The simulator must
    re-register and the readings and commands must flow again."""
    t = now()
    compose("restart", "buildsim")
    _, waited = wait_until(lambda: fresh_temperature_after(t), timeout=120)
    def command_lands():
        st = get(f"{CONTROLLER}/api/status")["rooms"]
        return all(st[r]["applied"] and parse_time(st[r]["decided_at"]) > t for r in ADAPTIVE)
    _, waited_cmd = wait_until(command_lands, timeout=120)
    recovery = max(waited or 999, waited_cmd or 999)
    return {"passed": recovery <= 60, "requirement": "NFR-02", "readings_back_s": waited, "commands_back_s": waited_cmd,
            "recovery_s": round(recovery, 1)}


def test_f2_pipeline_outage():
    """F2 (NFR-03, safety): the pipeline is down for 30 s. The controller must fail safe, and after
    recovery no reading may be missing (sensors buffer and re-send; the pipeline replays its file)."""
    replayed_before = get(f"{PIPELINE}/api/metrics")["replayed"]
    t_stop = now()
    compose("stop", "pipeline")
    time.sleep(22)
    modes = {}
    try:
        modes = {room: controller_mode(room) for room in ADAPTIVE}
    except Exception as e:
        modes = {"error": str(e)}
    time.sleep(8)
    compose("start", "pipeline")
    t_start = now()
    _, waited = wait_until(lambda: fresh_temperature_after(t_start), timeout=60)
    time.sleep(6)

    history = get(f"{PIPELINE}/api/rooms/history?room=level0/A1009&type=temperature&limit=200")
    stamps = [parse_time(r["timestamp"]) for r in history]
    window = [s for s in stamps if (t_stop - s).total_seconds() < 15 and (s - t_start).total_seconds() < 15]
    gaps = [(b - a).total_seconds() for a, b in zip(window, window[1:])]
    max_gap = max(gaps) if gaps else None
    metrics = get(f"{PIPELINE}/api/metrics")
    failsafe = all(m == "failsafe" for m in modes.values())
    return {"passed": failsafe and max_gap is not None and max_gap <= 7, "requirement": "NFR-03",
            "controller_modes_during_outage": modes, "readings_in_window": len(window),
            "max_gap_between_readings_s": max_gap, "readings_back_s": waited,
            "replayed_at_restart": metrics["replayed"], "replayed_before": replayed_before}


def test_f3_controller_down():
    """F3 (NFR-06): the controller dies. The actuator's lease must put its rooms in the safe state
    instead of holding the last command forever."""
    compose("stop", "controller")
    t = time.monotonic()

    def all_safe():
        state = get(f"{ACTUATOR}/state")
        return all(state[r]["watchdog"] for r in ADAPTIVE)

    _, waited = wait_until(all_safe, timeout=120, every=2)
    state = get(f"{ACTUATOR}/state")
    compose("start", "controller")
    _, back = wait_until(lambda: not any(get(f"{ACTUATOR}/state")[r]["watchdog"] for r in ADAPTIVE), timeout=90, every=2)
    return {"passed": waited is not None and waited <= 70, "requirement": "NFR-06",
            "safe_state_after_s": waited, "safe_state": {r: [state[r]["setpoint"], state[r]["fan_speed"]] for r in ADAPTIVE},
            "control_resumed_after_restart_s": back}


def detect(service, mode, room, signal, expect_quality, recover_quality="ok"):
    """Inject a sensor fault, measure until the pipeline flags it and the controller fails safe,
    then clear it and measure recovery."""
    inject(service, mode, room)
    _, flagged = wait_until(lambda: room_state(room)["quality"].get(signal) == expect_quality, timeout=150)
    _, failsafe = wait_until(lambda: controller_mode(room) == "failsafe", timeout=150)
    if flagged is not None and failsafe is not None:
        failsafe += flagged  # report time from the fault, not from the detection
    inject(service, "none")
    _, recovered = wait_until(lambda: room_state(room)["quality"].get(signal) == recover_quality
                              and controller_mode(room) != "failsafe", timeout=150)
    return flagged, failsafe, recovered


def test_f4_stuck_temperature():
    """F4 (NFR-05): the A1009 temperature sensor freezes at a plausible value (it keeps reporting,
    so it is never stale). The pipeline must flag it as stuck and the controller must fail safe."""
    flagged, failsafe, recovered = detect("sensor-temperature", "stuck", "A1009", "temperature", "stuck")
    return {"passed": failsafe is not None and failsafe <= 90, "requirement": "NFR-05",
            "flagged_stuck_s": flagged, "failsafe_s": failsafe, "recovered_s": recovered}


def test_f5_spike_humidity():
    """F5 (NFR-05): the humidity sensors report an impossible 150 %. Every such reading must be
    rejected, and since humidity is not safety critical the controller must keep deciding normally."""
    before = get(f"{PIPELINE}/api/metrics")["rejected"]
    inject("sensor-humidity", "spike")
    time.sleep(20)
    modes = {room: controller_mode(room) for room in ADAPTIVE}
    inject("sensor-humidity", "none")
    rejected = get(f"{PIPELINE}/api/metrics")["rejected"] - before
    return {"passed": rejected >= 3 * len(ALL_ROOMS) and all(m != "failsafe" for m in modes.values()),
            "requirement": "NFR-05", "rejected_readings": rejected, "controller_modes": modes}


def test_f6_occupancy_offline():
    """F6 (NFR-05): the A1013 people counter goes silent. After 30 s its data is stale and the
    controller must fail safe for that room only."""
    flagged, failsafe, recovered = detect("sensor-occupancy", "offline", "A1013", "occupancy", "stale")
    other = controller_mode("A1009")
    return {"passed": failsafe is not None and failsafe <= 90 and other != "failsafe", "requirement": "NFR-05",
            "flagged_stale_s": flagged, "failsafe_s": failsafe, "recovered_s": recovered, "other_room_mode": other}


def restart_count(cid):
    out = subprocess.run(["docker", "inspect", "-f", "{{.RestartCount}}", cid], capture_output=True, text=True).stdout
    return int(out.strip() or 0)


def test_f7_crash_sensor():
    """F7 (NFR-02): kill -9 the temperature sensor *process* inside its container (a real crash, the
    examination demo). The restart policy must bring it back and readings must flow again."""
    cid = subprocess.run(["docker", "compose", "ps", "-q", "sensor-temperature"], capture_output=True, text=True).stdout.strip()
    restarts = restart_count(cid)
    t = now()
    subprocess.run(["docker", "compose", "exec", "-T", "sensor-temperature", "sh", "-c", "kill -9 $(pidof app)"],
                   capture_output=True)
    _, restarted = wait_until(lambda: restart_count(cid) > restarts, timeout=60, every=0.5)
    _, back = wait_until(lambda: fresh_temperature_after(t), timeout=60, every=0.5)
    modes = {room: controller_mode(room) for room in ADAPTIVE}
    return {"passed": restarted is not None and back is not None and back <= 60, "requirement": "NFR-02",
            "restarted_by_restart_policy_s": restarted, "fresh_readings_after_s": back, "controller_modes_after": modes}


def test_f7b_docker_kill_sensor():
    """F7b (NFR-02): `docker kill -s KILL` the temperature sensor container (an operator kill). Docker treats
    this as a manual stop, so the restart policy does not apply: the system must degrade to failsafe and
    recover once an operator starts the container again."""
    cid = subprocess.run(["docker", "compose", "ps", "-q", "sensor-temperature"], capture_output=True, text=True).stdout.strip()
    t = now()
    subprocess.run(["docker", "kill", "-s", "KILL", cid], check=True, capture_output=True)
    _, failsafe = wait_until(lambda: controller_mode("A1009") == "failsafe", timeout=90)
    restarted_by_docker = False
    running, _ = wait_until(lambda: subprocess.run(["docker", "inspect", "-f", "{{.State.Running}}", cid],
                                                   capture_output=True, text=True).stdout.strip() == "true", timeout=20)
    if running:
        restarted_by_docker = True
    else:
        compose("start", "sensor-temperature")  # docker treats `docker kill` as a manual stop
    _, back = wait_until(lambda: fresh_temperature_after(t) and controller_mode("A1009") != "failsafe", timeout=120)
    return {"passed": failsafe is not None and back is not None and back <= 60, "requirement": "NFR-02",
            "failsafe_after_s": failsafe, "restarted_by_restart_policy": restarted_by_docker, "recovered_after_restart_s": back}


# ---------- take one service down: what happens, and does it come back? ----------

def decided_after(t, rooms=ADAPTIVE):
    st = get(f"{CONTROLLER}/api/status")["rooms"]
    return all(parse_time(st[r]["decided_at"]) > t and st[r]["applied"] for r in rooms)


def test_d1_baseline_down():
    """D1: the baseline controller dies. Its rooms must get the lease's safe state; the adaptive rooms are unaffected."""
    compose("stop", "baseline-controller")
    _, safe = wait_until(lambda: all(get(f"{ACTUATOR}/state")[r]["watchdog"] for r in ("A1010", "A1015")), timeout=120, every=2)
    adaptive_ok = decided_after(now() - timedelta(seconds=15))
    compose("start", "baseline-controller")
    _, back = wait_until(lambda: not any(get(f"{ACTUATOR}/state")[r]["watchdog"] for r in ("A1010", "A1015")), timeout=60, every=2)
    return {"passed": safe is not None and safe <= 70 and adaptive_ok, "baseline_rooms_safe_after_s": safe,
            "adaptive_rooms_unaffected": adaptive_ok, "baseline_back_after_s": back}


def test_d2_dashboard_and_evaluation_down():
    """D2: dashboard and evaluation die. Control must not notice; evaluation resumes from its saved state."""
    before = get("http://127.0.0.1:8099/api/report")["rooms"]["A1009"]["samples"]
    t = now()
    compose("stop", "dashboard", "evaluation")
    time.sleep(25)
    control_ok = decided_after(t)
    compose("start", "dashboard", "evaluation")
    after, waited = wait_until(lambda: get("http://127.0.0.1:8099/api/report")["rooms"]["A1009"]["samples"], timeout=60)
    dash, _ = wait_until(lambda: get("http://127.0.0.1:8093/api/summary")["rooms"], timeout=60)
    return {"passed": control_ok and after is not None and after >= before and bool(dash),
            "control_kept_deciding": control_ok, "evaluation_samples_before": before, "evaluation_samples_after_restart": after,
            "evaluation_back_after_s": waited}


def test_d3_occupancy_sim_down():
    """D3: the occupancy simulator (people + model clock) dies for 60 s. Services must extrapolate the clock
    and keep deciding on the last occupancy picture; after its restart the clock starts again (it is not persisted)."""
    sim_before = parse_time(get(f"{CONTROLLER}/api/status")["rooms"]["A1009"]["sim_time"])
    t = now()
    compose("stop", "occupancy-sim")
    time.sleep(60)
    st = get(f"{CONTROLLER}/api/status")["rooms"]["A1009"]
    extrapolated_min = (parse_time(st["sim_time"]) - sim_before).total_seconds() / 60
    modes = {r: controller_mode(r) for r in ADAPTIVE}
    compose("start", "occupancy-sim")
    _, back = wait_until(lambda: get(f"{OCCUPANCY}/api/state")["sim"]["clock"]["running"], timeout=60)
    clock_after = get(f"{OCCUPANCY}/api/state")["sim"]["clock"]["time"]
    return {"passed": extrapolated_min > 30 and decided_after(t), "model_minutes_extrapolated_in_60s": round(extrapolated_min),
            "controller_modes_during": modes, "occupancy_sim_back_after_s": back, "model_clock_after_restart": clock_after}


def test_d4_simulator_down():
    """D4: the physics simulator dies for 30 s ("the world freezes"). Sensors keep reporting BuildSim's last values;
    after restart the simulator must continue from BuildSim's state, not from defaults (energy meter keeps counting)."""
    energy_before = room_state("A1009")["readings"]["energy"]["value"]
    compose("stop", "simulator")
    time.sleep(30)
    q = room_state("A1009")["quality"]
    compose("start", "simulator")
    time.sleep(15)
    energy_after = room_state("A1009")["readings"]["energy"]["value"]
    return {"passed": energy_after >= energy_before, "quality_while_frozen": q,
            "energy_before_kwh": energy_before, "energy_after_restart_kwh": energy_after}


def test_d5_actuator_down():
    """D5: the actuator dies for 30 s. Commands fail (the controller reports applied=false), BuildSim keeps the last
    actuator state so the room units keep running, and commands land again after restart."""
    compose("stop", "hvac-actuator")
    time.sleep(25)
    st = get(f"{CONTROLLER}/api/status")["rooms"]
    applied_during = {r: st[r]["applied"] for r in ADAPTIVE}
    t = now()
    compose("start", "hvac-actuator")
    _, back = wait_until(lambda: decided_after(t), timeout=60)
    return {"passed": not any(applied_during.values()) and back is not None and back <= 30,
            "applied_during_outage": applied_during, "commands_land_again_after_s": back}


def test_d6_humidity_and_energy_sensors_down():
    """D6: the humidity and energy meters die. Humidity is not safety critical (no failsafe); the evaluation must
    skip samples without energy instead of scoring them."""
    skipped_before = get("http://127.0.0.1:8099/api/report")["groups"]["adaptive"]["skipped_bad_data"]
    compose("stop", "sensor-humidity", "sensor-energy")
    time.sleep(50)
    modes = {r: controller_mode(r) for r in ADAPTIVE}
    skipped = get("http://127.0.0.1:8099/api/report")["groups"]["adaptive"]["skipped_bad_data"] - skipped_before
    compose("start", "sensor-humidity", "sensor-energy")
    return {"passed": all(m != "failsafe" for m in modes.values()) and skipped > 0,
            "controller_modes": modes, "evaluation_samples_skipped": skipped}


TESTS = {"e2e": test_e2e, "f1": test_f1_buildsim_restart, "f2": test_f2_pipeline_outage, "f3": test_f3_controller_down,
         "f4": test_f4_stuck_temperature, "f5": test_f5_spike_humidity, "f6": test_f6_occupancy_offline,
         "f7": test_f7_crash_sensor, "f7b": test_f7b_docker_kill_sensor,
         "d1": test_d1_baseline_down, "d2": test_d2_dashboard_and_evaluation_down, "d3": test_d3_occupancy_sim_down,
         "d4": test_d4_simulator_down, "d5": test_d5_actuator_down, "d6": test_d6_humidity_and_energy_sensors_down}


def main():
    chosen = sys.argv[1:] or list(TESTS)
    results = json.loads(RESULTS.read_text()) if RESULTS.exists() else {}
    for name in chosen:
        print(f"--- {name}: {TESTS[name].__doc__.strip().splitlines()[0]}", flush=True)
        started = now()
        try:
            result = TESTS[name]()
        except Exception as e:
            result = {"passed": False, "error": repr(e)}
        result["ran_at"] = started.isoformat()
        results[name] = result
        print("    " + ("PASS" if result.get("passed") else "FAIL") + " " + json.dumps(result), flush=True)
        time.sleep(15)  # let the system settle before the next fault
    RESULTS.parent.mkdir(parents=True, exist_ok=True)
    RESULTS.write_text(json.dumps(results, indent=2))
    print(f"results written to {RESULTS}")


if __name__ == "__main__":
    main()
