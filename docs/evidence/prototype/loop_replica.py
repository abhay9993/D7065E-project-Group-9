"""Offline replica of Group 9's simulator model + controller, to explain the live numbers.

Copies the constants from services/simulator/main.go and services/controller/main.go.
"""

ENV, BODY, HVAC = 0.01, 0.03, 0.08
HUM_ENV, HUM_OCC, HUM_HVAC, BASE_H = 0.01, 0.4, 0.05, 40.0


def step(T, H, dt, occ, sp, fan_pct, out):
    f = min(max(fan_pct / 100, 0), 1)
    T += (ENV * (out - T) + BODY * occ + HVAC * f * (sp - T)) * dt
    H += (HUM_ENV * (BASE_H - H) + HUM_OCC * occ - HUM_HVAC * f * H) * dt
    return T, min(max(H, 15), 90)


def decide_current(T, occ, _state):
    if occ <= 0:
        return 18.0, 0.0
    sp = 22.0
    d = abs(T - sp)
    fan = min(max(d * 40, 0), 100)
    if d <= 1.0:
        fan = min(fan, 20)
    return sp, fan


def decide_no_band_cap(T, occ, _state):
    if occ <= 0:
        return 18.0, 0.0
    sp = 22.0
    return sp, min(max(abs(T - sp) * 40, 0), 100)


def decide_pi(T, occ, state):
    """Proportional + integral on the error; integral removes the steady-state offset."""
    if occ <= 0:
        state["i"] = 0.0
        return 18.0, 0.0
    sp = 22.0
    err = sp - T
    state["i"] = min(max(state.get("i", 0.0) + err * 10 * 0.5, -60), 60)  # dt=10 s, Ki=0.5
    return sp, min(max(abs(err) * 40 + abs(state["i"]), 0), 100)


def run(decider, out, minutes=30, tick=5, decide_every=10, occ=1, T0=None):
    T, H = (out if T0 is None else T0), 40.0
    sp, fan, state, t = 18.0, 0.0, {}, 0
    in_band = n = 0
    fan_sum = 0.0
    while t < minutes * 60:
        if t % decide_every == 0:
            sp, fan = decider(T, occ, state)
        T, H = step(T, H, tick, occ, sp, fan, out)
        t += tick
        if t > 120:  # ignore the first 2 min of warm-up
            n += 1
            in_band += abs(T - 22.0) <= 1.0
            fan_sum += fan
    return T, H, 100 * in_band / n, fan_sum / n


print("Steady state after 30 min, 1 occupant (real-time seconds, as in the running stack)")
print(f"{'controller':<22}{'outdoor':>8}{'T_end':>8}{'RH_end':>8}{'in-band%':>10}{'avg fan':>9}")
for name, dec in [("current (P + 20% cap)", decide_current), ("P without band cap", decide_no_band_cap), ("PI", decide_pi)]:
    for out in (4, 10, 16):
        T, H, band, fan = run(dec, out)
        print(f"{name:<22}{out:>8}{T:>8.2f}{H:>8.1f}{band:>10.1f}{fan:>9.1f}")

print("\nSetback check: empty room, fan 0, setpoint 18 -> where does it settle?")
for out in (4, 10, 16):
    T, H, *_ = run(decide_current, out, occ=0, T0=21.0)
    print(f"  outdoor {out:>2} C -> room {T:.2f} C (setpoint 18 is ignored when fan = 0)")

print("\nExplicit-Euler stability: k*dt must stay < 2 (k = ENV + HVAC*fan)")
for tick in (5, 10, 20, 25, 30):
    k = ENV + HVAC * 1.0
    T, *_ = run(decide_no_band_cap, 10, minutes=10, tick=tick, decide_every=tick)
    print(f"  TICK_SECONDS={tick:>2}: k*dt={k*tick:.2f} -> T after 10 min = {T:,.1f} C")

print("\nOccupancy 'forecast': slope over last 24 samples, predicted = last + 5*slope")


def trend(vals, dt_min=5 / 60):
    n = len(vals)
    xs = [i * dt_min for i in range(n)]
    mx, my = sum(xs) / n, sum(vals) / n
    sxx = sum((x - mx) ** 2 for x in xs)
    slope = sum((x - mx) * (y - my) for x, y in zip(xs, vals)) / sxx if sxx else 0
    return slope, vals[-1] + 5 * slope


cases = {
    "empty for 2 min (someone about to arrive)": [0] * 24,
    "person just LEFT (was in for last 60 s)": [0] * 12 + [1] * 11 + [0],
    "person present whole window": [1] * 24,
}
for label, v in cases.items():
    s, p = trend(v)
    fires = v[-1] == 0 and s > 0 and p >= 0.5
    print(f"  {label:<45} slope={s:+.2f}/min pred5m={p:+.2f} anticipates={fires}")
