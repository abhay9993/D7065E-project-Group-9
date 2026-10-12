#!/usr/bin/env python3
"""Turn the recorded evidence into the figures used in the report (docs/D7065E_Group9_Final_Report.tex).

    docker compose cp evaluation:/data/samples.jsonl docs/evidence/samples.jsonl
    curl -s 127.0.0.1:8099/api/report > docs/evidence/evaluation_report.json
    pip install matplotlib
    python3 scripts/plot_results.py

Reads docs/evidence/ and writes PNGs to docs/figures/.
"""

import json
from collections import defaultdict
from datetime import datetime, timedelta
from pathlib import Path

import matplotlib

matplotlib.use("Agg")
import matplotlib.pyplot as plt  # noqa: E402

ROOT = Path(__file__).resolve().parents[1]
EVIDENCE = ROOT / "docs" / "evidence"
FIGURES = ROOT / "docs" / "figures"
BAND = (20, 24)
PAIRS = [("A1009", "A1010"), ("A1013", "A1015")]  # (adaptive, baseline), size matched
COLORS = {"adaptive": "#2563eb", "baseline": "#ea580c"}
LABELS = {"adaptive": "our controller", "baseline": "fixed schedule"}  # the words used in the report


def t(s):
    return datetime.fromisoformat(s.replace("Z", "+00:00"))


def load_samples():
    rows = [json.loads(line) for line in (EVIDENCE / "samples.jsonl").read_text().splitlines() if line.strip()]
    for r in rows:
        r["t"] = t(r["sim_time"])
    return sorted(rows, key=lambda r: r["t"])


def per_day(rows):
    """Re-compute the evaluation's metrics per model day and group (same rules as services/evaluation)."""
    by_room = defaultdict(list)
    for r in rows:
        by_room[r["room"]].append(r)
    days = defaultdict(lambda: defaultdict(float))
    areas = json.loads((EVIDENCE / "evaluation_report.json").read_text())["rooms"]
    for room, rs in by_room.items():
        arrived = None
        for prev, cur in zip(rs, rs[1:]):
            step = (cur["t"] - prev["t"]).total_seconds() / 3600
            occ = cur["occupancy"] > 0
            if occ and not prev["occupancy"] > 0:
                arrived = cur["t"]
            if step <= 0 or step > 0.5:
                continue
            d = days[(cur["t"].date(), cur["group"])]
            d["m2_days"] += areas[room]["area_m2"] * step / 24
            d["hours"] += step
            d["energy"] += max(cur["energy_kwh"] - prev["energy_kwh"], 0)
            if occ:
                band = BAND[0] <= cur["temperature"] <= BAND[1]
                d["occ"] += step
                d["comfort"] += step * band
                if arrived and cur["t"] - arrived < timedelta(minutes=30):
                    d["arr"] += step
                    d["arr_ok"] += step * band
    return days


def fig_day_trace(rows):
    """One full weekday of the size-matched pair A1009 (adaptive) / A1010 (baseline)."""
    weekdays = sorted({r["t"].date() for r in rows if r["t"].weekday() < 5})
    day = weekdays[len(weekdays) // 2] if len(weekdays) > 2 else weekdays[-1]
    fig, axes = plt.subplots(2, 1, figsize=(10, 6), sharex=True)
    for ax, (adaptive, baseline) in zip(axes, PAIRS):
        for room, group in ((adaptive, "adaptive"), (baseline, "baseline")):
            rs = [r for r in rows if r["room"] == room and r["t"].date() == day]
            x = [r["t"].hour + r["t"].minute / 60 for r in rs]
            ax.plot(x, [r["temperature"] for r in rs], color=COLORS[group], label=f"{room} ({LABELS[group]})")
            for r, xi in zip(rs, x):
                if r["occupancy"] > 0:
                    ax.axvspan(xi - 1 / 12, xi + 1 / 12, color=COLORS[group], alpha=0.08, lw=0)
        ax.axhspan(*BAND, color="#16a34a", alpha=0.08)
        ax.set_ylabel("°C")
        ax.set_ylim(15, 25)
        ax.legend(loc="lower right", fontsize=8)
    axes[-1].set_xlabel(f"model time on {day} (shaded = someone in the room)")
    axes[-1].set_xlim(0, 24)
    fig.suptitle("Temperature over one weekday: our controller vs the fixed schedule (green band = 20–24 °C)")
    fig.tight_layout()
    fig.savefig(FIGURES / "day_trace.png", dpi=120)


def fig_daily(days):
    # only complete days (a partly recorded day distorts kWh per day)
    dates = sorted({d for (d, g), m in days.items() if m.get("m2_days", 0) > 0 and
                    all(days.get((d, gg), {}).get("hours", 0) >= 20 * 2 for gg in ("adaptive", "baseline"))})
    labels = [d.strftime("%a %d") for d in dates]
    # arrival comfort: every weekday with arrivals, also a partly recorded last day (its mornings are complete);
    # weekend "arrivals" are night guards walking through, so weekends are left out
    arr_dates = sorted({d for (d, g), m in days.items() if d.weekday() < 5 and m.get("arr")})
    fig, (ax1, ax2) = plt.subplots(1, 2, figsize=(11, 4))
    width = 0.4
    for i, group in enumerate(("adaptive", "baseline")):
        energy = []
        for d in dates:
            m = days.get((d, group), {})
            energy.append(m["energy"] / m["m2_days"] if m and m.get("m2_days") else 0)
        arrival = []
        for d in arr_dates:
            m = days.get((d, group), {})
            arrival.append(100 * m["arr_ok"] / m["arr"] if m.get("arr") else float("nan"))
        xs = [x + (i - 0.5) * width for x in range(len(dates))]
        ax1.bar(xs, energy, width, color=COLORS[group], label=LABELS[group])
        ax2.plot(range(len(arr_dates)), arrival, "o-", color=COLORS[group], label=LABELS[group])
    ax1.set_xticks(range(len(dates)), labels, rotation=45, fontsize=8)
    ax1.set_ylabel("kWh per m² per day")
    ax1.set_title("Energy per model day")
    ax1.set_ylim(0, max(ax1.get_ylim()[1], 0.1) * 1.25)  # room for the legend above the bars
    ax1.legend(loc="upper right", ncol=2)
    ax2.set_xticks(range(len(arr_dates)), [d.strftime("%a %d") for d in arr_dates], rotation=45, fontsize=8)
    ax2.set_ylabel("% of first 30 min after arrival in 20–24 °C")
    ax2.set_ylim(0, 105)
    ax2.set_title("Comfort at arrival on weekdays (the learned table at work)")
    ax2.legend()
    fig.tight_layout()
    fig.savefig(FIGURES / "daily_energy_arrival.png", dpi=120)


def fig_stress():
    path = EVIDENCE / "stress_results.json"
    if not path.exists():
        return
    results = json.loads(path.read_text())
    normal = [r for r in results if r["tick_s"] == 5]
    at300 = sorted([r for r in results if r["rooms"] == 300], key=lambda r: r["decide_s"], reverse=True)
    fig, (ax1, ax2) = plt.subplots(1, 2, figsize=(11, 4))
    ax1.plot([r["rooms"] for r in normal], [r["sim_tick_ms_max"] for r in normal], "o-", label="simulator tick")
    ax1.plot([r["rooms"] for r in normal], [r["controller_cycle_ms_max"] for r in normal], "s-", label="controller cycle")
    ax1.set_xlabel("rooms under control (default rate: tick 5 s, decide 10 s)")
    ax1.set_ylabel("ms, worst of 6 samples")
    ax1.set_title("Scaling the building: far below the 5 000 / 10 000 ms budgets")
    ax1.legend(fontsize=8)
    rate = [10 / r["decide_s"] for r in at300]
    ax2.plot(rate, [r["controller_cycle_ms_max"] / (r["decide_s"] * 1000) for r in at300], "s-", color="#ff7f0e", label="controller cycle / budget")
    ax2.plot(rate, [r["sim_tick_ms_max"] / (r["tick_s"] * 1000) for r in at300], "o-", color="#1f77b4", label="simulator tick / budget")
    ax2.axhline(1, color="red", ls="--", lw=1)
    ax2.text(rate[0], 1.05, "breaking point (cycle longer than its interval)", color="red", fontsize=8)
    ax2.set_xscale("log")
    ax2.set_xticks(rate, [f"{x:g}x" for x in rate])
    ax2.set_xlabel("update rate vs default, 300 rooms")
    ax2.set_ylabel("time used / time available")
    ax2.set_title("Scaling the rate: the controller breaks first (25x)")
    ax2.legend(fontsize=8)
    fig.tight_layout()
    fig.savefig(FIGURES / "stress.png", dpi=120)


def main():
    FIGURES.mkdir(parents=True, exist_ok=True)
    rows = load_samples()
    fig_day_trace(rows)
    fig_daily(per_day(rows))
    fig_stress()
    print(f"figures written to {FIGURES}")


if __name__ == "__main__":
    main()
