#!/usr/bin/env bash
# Start a clean adaptive-vs-baseline experiment on the running stack.
#
# - forgets the controller's learned prior and the evaluation's evidence (their volumes),
# - sets the model clock to Monday 00:00 at 60x (one model day = 24 real minutes),
# - restarts the controller and the evaluation.
#
# Let it run for a few hours, then collect the evidence:
#   curl -s 127.0.0.1:8099/api/report > docs/evidence/evaluation_report.json
#   docker compose cp evaluation:/data/samples.jsonl docs/evidence/samples.jsonl
#   python3 scripts/plot_results.py
set -euo pipefail
cd "$(dirname "$0")/.."

START_DATE=${1:-2026-10-12}   # a Monday

docker compose rm -sf controller evaluation
docker volume rm -f "$(basename "$PWD" | tr '[:upper:]' '[:lower:]')_controller-data" \
                    "$(basename "$PWD" | tr '[:upper:]' '[:lower:]')_evaluation-data"
curl -fsS -X POST 127.0.0.1:8081/api/clock -H 'Content-Type: application/json' \
  -d "{\"date\":\"$START_DATE\",\"time\":\"00:00\",\"factor\":60,\"running\":true}" >/dev/null
docker compose up -d --no-deps controller evaluation   # --no-deps: do not recreate BuildSim etc.
echo "experiment started at model time $START_DATE 00:00 ($(date -u +%FT%TZ) real)"
