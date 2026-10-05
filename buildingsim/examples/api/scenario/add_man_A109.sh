#!/bin/bash
# Add a man to room A109
# Usage: ./add_man_A109.sh [host:port]

BASE=${1:-127.0.0.1:9090}

echo "=== Adding man to A109 ==="
curl -s -X PUT http://$BASE/api/occupancy -H 'Content-Type: application/json' -d '{
  "level0/A109": {
    "persons": [{"id": "man-1", "name": "Johan", "icon": "man"}],
    "aliens": []
  }
}' | python3 -m json.tool
