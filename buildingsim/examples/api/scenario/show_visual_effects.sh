#!/usr/bin/env bash
set -euo pipefail

BASE_URL=${1:-http://127.0.0.1:9090}
BASE_URL=${BASE_URL%/}

put() {
  local endpoint=$1
  local payload=$2
  curl --fail-with-body --silent --show-error \
    -X PUT "$BASE_URL$endpoint" \
    -H 'Content-Type: application/json' \
    --data "$payload" >/dev/null
}

curl --fail-with-body --silent --show-error \
  -X POST "$BASE_URL/api/equipment/bulk" \
  -H 'Content-Type: application/json' \
  --data '[
    {
      "id":"demo-smoke-A109","name":"Smoke detector A109","type":"smoke_detector",
      "category":"fire safety","level":"level0","room":"A109","position":[260,246],"height":10,
      "status":"warning","sensors":[{"id":"demo-smoke-value","name":"Smoke","type":"smoke","data_type":"text","unit":"%/m","value":"8.4"}]
    },
    {
      "id":"demo-sprinkler-A109","name":"Sprinkler A109","type":"sprinkler",
      "category":"fire safety","level":"level0","room":"A109","position":[267,250],"height":11,
      "status":"running","actuators":[{"id":"demo-sprinkler-state","name":"Valve","type":"water_valve","state":"open"}]
    },
    {
      "id":"demo-light-A110","name":"Ceiling light A110","type":"light_fixture",
      "category":"lighting","level":"level0","room":"A110","position":[289,237],"height":10,
      "status":"running","actuators":[{"id":"demo-light-level","name":"Dimmer","type":"dimmer","state":"85"}]
    },
    {
      "id":"demo-lock-A109","name":"Door lock A109","type":"door_lock",
      "category":"access control","level":"level0","room":"A109","position":[278.72,261.33],"height":5,
      "status":"stopped","actuators":[{"id":"demo-lock-state","name":"Lock","type":"lock","state":"locked"}]
    }
  ]' >/dev/null

put /api/room-layers '[
  {
    "id":"temperature","label":"Simulated room temperature","unit":"°C","source":"simulation truth",
    "minimum":18,"maximum":40,"opacity":0.78,
    "palette":["#2563eb","#22c55e","#facc15","#f97316","#dc2626"],
    "values":{
      "level0/A104":21.0,"level0/A105":21.5,"level0/A106":20.8,"level0/A107":22.0,
      "level0/A108":25.4,"level0/A109":38.2,"level0/A110":29.6,"level0/A111":23.1,
      "level0/A113":22.4,"level0/A114":21.8,"level0/A115":21.2
    }
  },
  {
    "id":"fire-risk","label":"Estimated fire risk","unit":"%","source":"decision-service estimate",
    "minimum":0,"maximum":100,"opacity":0.74,
    "palette":["#0f766e","#facc15","#f97316","#dc2626"],
    "values":{"level0/A108":32,"level0/A109":96,"level0/A110":68,"level0/A111":24}
  }
]'

put /api/effects '[
  {"id":"fire-A109","type":"fire","label":"Fire source","level":"level0","room":"A109","position":[264,249],"radius":7,"height":15,"intensity":0.95},
  {"id":"smoke-A109","type":"smoke","label":"Smoke plume","level":"level0","room":"A109","position":[269,247],"radius":8,"height":18,"intensity":0.8},
  {"id":"spray-A109","type":"sprinkler","label":"Sprinkler active","level":"level0","room":"A109","position":[260,252],"radius":9,"height":12,"intensity":0.9},
  {"id":"warning-A110","type":"warning","level":"level0","room":"A110","radius":8,"intensity":0.75}
]'

put /api/entities '[
  {"id":"demo-person","name":"Evacuation warden","type":"woman","level":"level0","room":"A110","position":[292,240],"heading":220,"status":"evacuating","transition_ms":900},
  {"id":"demo-robot","name":"Cleaning robot","type":"cleaning_robot","level":"level0","room":"A108","position":[258,273],"heading":35,"status":"returning to dock","transition_ms":1200}
]'

put /api/room-appearance '[
  {"level":"level0","room":"A110","color":"#ffd166","brightness":0.85},
  {"level":"level0","room":"A111","color":"#9bd7ff","brightness":0.45}
]'

put /api/doors '[
  {"id":"door-A109","name":"A109 access door","kind":"door","level":"level0","room":"A109","entry_node_id":524,"state":"closed","lock_state":"locked"},
  {"id":"east-entrance","name":"East entrance","kind":"entrance","level":"level0","position":[337.75,258.3],"state":"open","lock_state":"unlocked"},
  {"id":"east-fire-exit","name":"East fire exit","kind":"fire_exit","level":"level0","position":[337.75,251.5],"state":"closed","lock_state":"unlocked"}
]'

put /api/alerts '[
  {"id":"fire-confirmed","severity":"critical","title":"Fire confirmed in A109","message":"Sprinkler active; A109 access door is locked and excluded from evacuation routes.","level":"level0","room":"A109"},
  {"id":"evacuation","severity":"warning","title":"Evacuation in progress","message":"Use the east fire exit; smoke estimate is increasing toward A110.","level":"level0","room":"A110"}
]'

printf 'Demo published. Open %s/?layer=temperature\n' "$BASE_URL"
printf 'Run clear_visual_effects.sh to remove the transient visualization state.\n'
