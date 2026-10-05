#!/usr/bin/env bash
set -euo pipefail

# Publish one room-level scalar field. BuildSim interpolates the palette between
# minimum and maximum and colours every room for which a value is supplied.
# Usage: ./set_temperature_heatmap.sh [BuildSim base URL]

BASE_URL=${1:-http://127.0.0.1:9090}
BASE_URL=${BASE_URL%/}

curl --fail-with-body --silent --show-error \
  -X PUT "$BASE_URL/api/room-layers" \
  -H 'Content-Type: application/json' \
  --data '[{
    "id":"temperature",
    "label":"Simulated room temperature",
    "unit":"°C",
    "source":"simulation truth",
    "minimum":18,
    "maximum":35,
    "opacity":0.78,
    "palette":["#2563eb","#22c55e","#facc15","#f97316","#dc2626"],
    "values":{
      "level0/A104":19.2,
      "level0/A105":20.1,
      "level0/A106":21.0,
      "level0/A107":22.4,
      "level0/A108":25.8,
      "level0/A109":32.6,
      "level0/A110":28.3,
      "level0/A111":23.5,
      "level0/A113":21.7,
      "level0/A114":20.5,
      "level0/A115":19.8
    }
  }]' >/dev/null

printf 'Temperature room layer published. Open %s/?layer=temperature&floor=level0\n' "$BASE_URL"
printf 'Re-run this script with changed values to publish the next complete snapshot.\n'
