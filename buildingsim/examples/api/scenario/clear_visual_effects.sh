#!/usr/bin/env bash
set -euo pipefail

BASE_URL=${1:-http://127.0.0.1:9090}
BASE_URL=${BASE_URL%/}

for endpoint in room-layers effects entities room-appearance doors alerts; do
  curl --fail-with-body --silent --show-error \
    -X PUT "$BASE_URL/api/$endpoint" \
    -H 'Content-Type: application/json' \
    --data '[]' >/dev/null
done

printf 'Transient visualization state cleared. Demo equipment remains registered.\n'
