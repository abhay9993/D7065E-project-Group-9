// Command dashboard shows room state, occupancy, and recent controller
// decisions. It polls the pipeline, controller, and actuator server-side and
// serves a single aggregated view, so the browser only ever talks to this
// process.
package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type reading struct {
	Value     float64   `json:"value"`
	Unit      string    `json:"unit"`
	Timestamp time.Time `json:"timestamp"`
}

type roomState struct {
	Readings map[string]reading  `json:"readings"`
	Stale    map[string]bool     `json:"stale"`
	Forecast map[string]forecast `json:"forecast"`
}

// forecast mirrors the pipeline's short-horizon linear trend for one signal.
type forecast struct {
	SlopePerMin float64 `json:"slope_per_min"`
	Predicted5m float64 `json:"predicted_5m"`
	Samples     int     `json:"samples"`
}

type roomSummary struct {
	Room      string    `json:"room"`
	RoomState roomState `json:"room_state"`
	Controller any      `json:"controller"`
	Actuator   any      `json:"actuator"`
}

type summary struct {
	Rooms      []roomSummary `json:"rooms"`
	Evaluation any           `json:"evaluation,omitempty"`
	UpdatedAt  time.Time     `json:"updated_at"`
}

type cache struct {
	mu  sync.RWMutex
	val summary
}

func (c *cache) set(s summary) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.val = s
}

func (c *cache) get() summary {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.val
}

func main() {
	pipelineURL := getenv("PIPELINE_URL", "http://127.0.0.1:8090")
	controllerURL := getenv("CONTROLLER_URL", "http://127.0.0.1:8092")
	baselineControllerURL := getenv("BASELINE_CONTROLLER_URL", "")
	actuatorURL := getenv("ACTUATOR_URL", "http://127.0.0.1:8091")
	evaluationURL := getenv("EVALUATION_URL", "")
	level := getenv("ROOM_LEVEL", "level0")
	rooms := getenvList("ROOM_NAMES", "A1009,A1010,A1013")
	listenAddr := getenv("LISTEN_ADDR", ":8093")
	refresh := getenvDuration("REFRESH_SECONDS", 5*time.Second)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client := &http.Client{Timeout: 5 * time.Second}
	c := &cache{}

	go pollLoop(ctx, client, c, pipelineURL, controllerURL, baselineControllerURL, actuatorURL, evaluationURL, level, rooms, refresh)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", serveIndex)
	mux.HandleFunc("GET /api/summary", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(c.get())
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	log.Printf("dashboard: listening on %s (rooms=%v)", listenAddr, rooms)
	if err := http.ListenAndServe(listenAddr, mux); err != nil {
		log.Fatalf("dashboard: server stopped: %v", err)
	}
}

func pollLoop(ctx context.Context, client *http.Client, c *cache, pipelineURL, controllerURL, baselineControllerURL, actuatorURL, evaluationURL, level string, rooms []string, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		refreshSummary(ctx, client, c, pipelineURL, controllerURL, baselineControllerURL, actuatorURL, evaluationURL, level, rooms)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func refreshSummary(ctx context.Context, client *http.Client, c *cache, pipelineURL, controllerURL, baselineControllerURL, actuatorURL, evaluationURL, level string, rooms []string) {
	var controllerStatus map[string]any
	if err := getJSON(ctx, client, controllerURL+"/api/status", &controllerStatus); err != nil {
		log.Printf("dashboard: controller status: %v", err)
	}
	if baselineControllerURL != "" {
		var baselineStatus map[string]any
		if err := getJSON(ctx, client, baselineControllerURL+"/api/status", &baselineStatus); err != nil {
			log.Printf("dashboard: baseline controller status: %v", err)
		} else {
			if controllerStatus == nil {
				controllerStatus = map[string]any{}
			}
			for room, st := range baselineStatus {
				controllerStatus[room] = st
			}
		}
	}
	var actuatorState map[string]any
	if err := getJSON(ctx, client, actuatorURL+"/state", &actuatorState); err != nil {
		log.Printf("dashboard: actuator state: %v", err)
	}

	s := summary{UpdatedAt: time.Now()}
	for _, room := range rooms {
		roomKey := level + "/" + room
		rs := roomSummary{Room: roomKey, Controller: controllerStatus[room], Actuator: actuatorState[room]}
		if err := getJSON(ctx, client, pipelineURL+"/api/rooms/state?room="+url.QueryEscape(roomKey), &rs.RoomState); err != nil {
			log.Printf("dashboard: pipeline state for %s: %v", roomKey, err)
		}
		s.Rooms = append(s.Rooms, rs)
	}
	if evaluationURL != "" {
		var evaluationReport any
		if err := getJSON(ctx, client, evaluationURL+"/api/report", &evaluationReport); err != nil {
			log.Printf("dashboard: evaluation report: %v", err)
		} else {
			s.Evaluation = evaluationReport
		}
	}
	c.set(s)
}

func getJSON(ctx context.Context, client *http.Client, target string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(out)
}

func serveIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(indexHTML))
}

const indexHTML = `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<title>Smart HVAC Dashboard</title>
<style>
  body { font-family: system-ui, sans-serif; margin: 2rem; background: #0f172a; color: #e2e8f0; }
  h1 { font-size: 1.4rem; }
  h2.room-title { margin-top: 2rem; font-size: 1.1rem; border-bottom: 1px solid #334155; padding-bottom: 0.3rem; }
  .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 1rem; margin-top: 1rem; }
  .card { background: #1e293b; border-radius: 8px; padding: 1rem; }
  .card h3 { margin: 0 0 0.5rem; font-size: 0.95rem; color: #94a3b8; }
  .value { font-size: 1.6rem; font-weight: 600; }
  .stale { color: #f87171; }
  pre { white-space: pre-wrap; word-break: break-word; font-size: 0.8rem; background: #1e293b; border-radius: 6px; padding: 0.75rem; }
  #updated { color: #94a3b8; }
</style>
</head>
<body>
  <h1>Smart HVAC Optimisation - Dashboard</h1>
  <div id="updated"></div>
  <h2 class="room-title">Evaluation (adaptive vs. baseline)</h2>
  <pre id="evaluation"></pre>
  <div id="rooms"></div>
<script>
async function refresh() {
  const res = await fetch('/api/summary');
  const s = await res.json();
  document.getElementById('updated').textContent = 'Updated ' + new Date(s.updated_at).toLocaleTimeString();
  document.getElementById('evaluation').textContent = s.evaluation ? JSON.stringify(s.evaluation.comparison, null, 2) : '(evaluation service not configured)';
  const rooms = s.rooms || [];
  document.getElementById('rooms').innerHTML = rooms.map(function(room) {
    const readings = (room.room_state && room.room_state.readings) || {};
    const stale = (room.room_state && room.room_state.stale) || {};
    const cards = Object.keys(readings).map(function(type) {
      const r = readings[type];
      const cls = stale[type] ? 'value stale' : 'value';
      const f = (room.room_state && room.room_state.forecast && room.room_state.forecast[type]) || null;
      const trendLine = f ? '<div style="font-size:0.75rem;color:#94a3b8">trend ' + f.slope_per_min.toFixed(2) + '/min &rarr; ' + f.predicted_5m.toFixed(1) + ' in 5min</div>' : '';
      return '<div class="card"><h3>' + type + '</h3><div class="' + cls + '">' + r.value + ' ' + (r.unit || '') + '</div>' + trendLine + '</div>';
    }).join('');
    return '<h2 class="room-title">' + room.room + '</h2>' +
      '<div class="grid">' + cards + '</div>' +
      '<pre>controller: ' + JSON.stringify(room.controller) + '\\nactuator:   ' + JSON.stringify(room.actuator) + '</pre>';
  }).join('');
}
refresh();
setInterval(refresh, 5000);
</script>
</body>
</html>`

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// getenvList splits a comma-separated env var into trimmed, non-empty entries.
func getenvList(key, fallback string) []string {
	raw := getenv(key, fallback)
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func getenvDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if seconds, err := strconv.Atoi(v); err == nil {
			return time.Duration(seconds) * time.Second
		}
	}
	return fallback
}
