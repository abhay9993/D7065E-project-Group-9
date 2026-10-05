#!/bin/bash
# Set room highlights with colors
# Usage: ./set_highlights.sh [host:port] [session_id]

DIR="$(cd "$(dirname "$0")" && pwd)"
source "$DIR/find_session.sh" "$@"

echo ""
echo "=== Highlighting Rooms ==="
curl -s -X PUT http://$BASE/api/sessions/$SESSION/highlights -H 'Content-Type: application/json' -d '[
  {"level": "level0", "room": "1542", "color": "#ff0000", "opacity": 0.8},
  {"level": "level0", "room": "1548", "color": "#00ff00", "opacity": 0.5},
  {"level": "level0", "room": "1552", "color": "#ffaa00", "opacity": 0.6}
]' | python3 -m json.tool
