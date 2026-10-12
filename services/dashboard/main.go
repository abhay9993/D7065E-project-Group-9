// Command dashboard shows room state, data quality, controller decisions and the
// evaluation. It polls the pipeline, both controllers, the actuator and the
// evaluation server-side and serves a single aggregated view, so the browser
// only ever talks to this process.
//
// It also puts the decisions into BuildSim's 3D viewer: a room-temperature
// heatmap layer and one alert per managed room (info = normal decision,
// warning = a sensor with bad data quality, critical = failsafe or watchdog).
// The dashboard is the only writer of those two BuildSim collections.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sort"
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
	Readings map[string]reading `json:"readings"`
	Quality  map[string]string  `json:"quality"`
}

type decision struct {
	Setpoint float64 `json:"setpoint"`
	FanSpeed float64 `json:"fan_speed"`
	Mode     string  `json:"mode"`
	Reason   string  `json:"reason"`
}

type controllerStatus struct {
	Rooms map[string]struct {
		LastCmd decision  `json:"last_command"`
		PAhead  float64   `json:"p_occupied_next_hour"`
		SimTime time.Time `json:"sim_time"`
	} `json:"rooms"`
	LastCycleMs float64        `json:"last_cycle_ms"`
	Forecast    map[string]any `json:"forecast"`
}

type appliedState struct {
	Setpoint float64 `json:"setpoint"`
	FanSpeed float64 `json:"fan_speed"`
	Reason   string  `json:"reason"`
	Source   string  `json:"source"`
	Watchdog bool    `json:"watchdog"`
}

type roomSummary struct {
	Room     string       `json:"room"`
	Group    string       `json:"group"`
	State    roomState    `json:"state"`
	Mode     string       `json:"mode"`
	PAhead   *float64     `json:"p_ahead,omitempty"`
	Applied  appliedState `json:"applied"`
	Severity string       `json:"severity"`
}

type summary struct {
	Rooms      []roomSummary `json:"rooms"`
	SimTime    time.Time     `json:"sim_time"`
	Forecast   any           `json:"forecast,omitempty"`
	Evaluation any           `json:"evaluation,omitempty"`
	Pipeline   any           `json:"pipeline,omitempty"`
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

type config struct {
	pipelineURL, controllerURL, baselineURL, actuatorURL, evaluationURL, buildsimURL string
	level                                                                            string
	adaptive, baseline                                                               []string
}

func main() {
	cfg := config{
		pipelineURL:   getenv("PIPELINE_URL", "http://127.0.0.1:8090"),
		controllerURL: getenv("CONTROLLER_URL", "http://127.0.0.1:8092"),
		baselineURL:   getenv("BASELINE_CONTROLLER_URL", "http://127.0.0.1:8098"),
		actuatorURL:   getenv("ACTUATOR_URL", "http://127.0.0.1:8091"),
		evaluationURL: getenv("EVALUATION_URL", "http://127.0.0.1:8099"),
		buildsimURL:   getenv("BUILDSIM_URL", ""),
		level:         getenv("ROOM_LEVEL", "level0"),
		adaptive:      getenvList("ADAPTIVE_ROOMS", "A1009,A1013"),
		baseline:      getenvList("BASELINE_ROOMS", "A1010,A1015"),
	}
	listenAddr := getenv("LISTEN_ADDR", ":8093")
	refresh := getenvDuration("REFRESH_SECONDS", 5*time.Second)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client := &http.Client{Timeout: 5 * time.Second}
	c := &cache{}

	go pollLoop(ctx, client, c, cfg, refresh)

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

	log.Printf("dashboard: listening on %s (adaptive=%v baseline=%v)", listenAddr, cfg.adaptive, cfg.baseline)
	if err := http.ListenAndServe(listenAddr, mux); err != nil {
		log.Fatalf("dashboard: server stopped: %v", err)
	}
}

func pollLoop(ctx context.Context, client *http.Client, c *cache, cfg config, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		s := refreshSummary(ctx, client, cfg)
		c.set(s)
		if cfg.buildsimURL != "" {
			publishToBuildSim(ctx, client, cfg, s)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func refreshSummary(ctx context.Context, client *http.Client, cfg config) summary {
	var ctrl controllerStatus
	if err := getJSON(ctx, client, cfg.controllerURL+"/api/status", &ctrl); err != nil {
		log.Printf("dashboard: controller status: %v", err)
	}
	var applied map[string]appliedState
	if err := getJSON(ctx, client, cfg.actuatorURL+"/state", &applied); err != nil {
		log.Printf("dashboard: actuator state: %v", err)
	}

	s := summary{UpdatedAt: time.Now(), Forecast: ctrl.Forecast}
	groups := map[string][]string{"adaptive": cfg.adaptive, "baseline": cfg.baseline}
	for _, group := range []string{"adaptive", "baseline"} {
		for _, room := range groups[group] {
			rs := roomSummary{Room: room, Group: group, Applied: applied[room], Mode: "schedule"}
			if err := getJSON(ctx, client, cfg.pipelineURL+"/api/rooms/state?room="+url.QueryEscape(cfg.level+"/"+room), &rs.State); err != nil {
				log.Printf("dashboard: pipeline state for %s: %v", room, err)
			}
			if st, ok := ctrl.Rooms[room]; ok {
				rs.Mode, s.SimTime = st.LastCmd.Mode, st.SimTime
				p := st.PAhead
				rs.PAhead = &p
			}
			rs.Severity = severity(rs)
			s.Rooms = append(s.Rooms, rs)
		}
	}
	var evaluation, pipeline any
	if err := getJSON(ctx, client, cfg.evaluationURL+"/api/report", &evaluation); err == nil {
		s.Evaluation = evaluation
	}
	if err := getJSON(ctx, client, cfg.pipelineURL+"/api/metrics", &pipeline); err == nil {
		s.Pipeline = pipeline
	}
	return s
}

// severity: critical when the room is being run blind (failsafe or watchdog),
// warning when one of its sensors has bad data quality, info otherwise.
func severity(rs roomSummary) string {
	if rs.Applied.Watchdog || rs.Mode == "failsafe" {
		return "critical"
	}
	for _, q := range rs.State.Quality {
		if q != "ok" {
			return "warning"
		}
	}
	return "info"
}

type roomLayer struct {
	ID      string             `json:"id"`
	Label   string             `json:"label"`
	Unit    string             `json:"unit"`
	Source  string             `json:"source"`
	Minimum float64            `json:"minimum"`
	Maximum float64            `json:"maximum"`
	Palette []string           `json:"palette"`
	Values  map[string]float64 `json:"values"`
}

type alert struct {
	ID       string `json:"id"`
	Severity string `json:"severity"`
	Title    string `json:"title"`
	Message  string `json:"message"`
	Level    string `json:"level"`
	Room     string `json:"room"`
}

// publishToBuildSim replaces the room-layer and alert collections in BuildSim
// with the current picture, so the 3D viewer shows what the system decided.
func publishToBuildSim(ctx context.Context, client *http.Client, cfg config, s summary) {
	layer := roomLayer{ID: "hvac-temperature", Label: "HVAC room temperature (measured)", Unit: "°C",
		Source: "group 9 sensors", Minimum: 16, Maximum: 26,
		Palette: []string{"#2563eb", "#22c55e", "#facc15", "#ef4444"}, Values: map[string]float64{}}
	var alerts []alert
	for _, rs := range s.Rooms {
		if t, ok := rs.State.Readings["temperature"]; ok {
			layer.Values[cfg.level+"/"+rs.Room] = t.Value
		}
		alerts = append(alerts, alert{ID: "hvac-" + rs.Room, Severity: rs.Severity, Level: cfg.level, Room: rs.Room,
			Title:   fmt.Sprintf("%s %s: %s", rs.Room, rs.Group, rs.Mode),
			Message: alertMessage(rs)})
	}
	if err := putJSON(ctx, client, cfg.buildsimURL+"/api/room-layers", []roomLayer{layer}); err != nil {
		log.Printf("dashboard: publish room layer: %v", err)
	}
	if err := putJSON(ctx, client, cfg.buildsimURL+"/api/alerts", alerts); err != nil {
		log.Printf("dashboard: publish alerts: %v", err)
	}
}

func alertMessage(rs roomSummary) string {
	var bad []string
	for signal, q := range rs.State.Quality {
		if q != "ok" {
			bad = append(bad, signal+" "+q)
		}
	}
	sort.Strings(bad)
	msg := fmt.Sprintf("setpoint %.1f °C, fan %.0f %%: %s", rs.Applied.Setpoint, rs.Applied.FanSpeed, rs.Applied.Reason)
	if len(bad) > 0 {
		msg = "bad data (" + strings.Join(bad, ", ") + "). " + msg
	}
	return msg
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
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func putJSON(ctx context.Context, client *http.Client, target string, body any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, target, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

func serveIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(indexHTML))
}

const indexHTML = `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Smart HVAC Dashboard</title>
<style>
  body { font-family: system-ui, sans-serif; margin: 1.5rem; background: #0f172a; color: #e2e8f0; }
  h1 { font-size: 1.4rem; margin-bottom: 0.2rem; }
  h2 { font-size: 1.05rem; margin-top: 1.8rem; border-bottom: 1px solid #334155; padding-bottom: 0.3rem; }
  #updated { color: #94a3b8; }
  table { border-collapse: collapse; width: 100%; font-size: 0.9rem; }
  th, td { text-align: left; padding: 0.4rem 0.6rem; border-bottom: 1px solid #1e293b; vertical-align: top; }
  th { color: #94a3b8; font-weight: 600; }
  .ok { color: #4ade80; } .stale, .stuck, .warning { color: #fbbf24; } .critical, .fail { color: #f87171; }
  .mode { font-weight: 600; } .reason { color: #94a3b8; font-size: 0.8rem; }
  a { color: #93c5fd; }
</style>
</head>
<body>
  <h1>Smart HVAC Optimisation - Group 9</h1>
  <div id="updated"></div>
  <div>3D view with temperature layer and decision alerts: <a href="http://127.0.0.1:9090/?layer=hvac-temperature&floor=level0" target="_blank">BuildSim</a></div>

  <h2>Rooms</h2>
  <table><thead><tr><th>Room</th><th>Temp</th><th>Humidity</th><th>People</th><th>Energy</th><th>Data quality</th><th>Mode</th><th>Applied</th><th>P(occupied next hour)</th></tr></thead><tbody id="rooms"></tbody></table>

  <h2>Requirements (live evaluation)</h2>
  <table><thead><tr><th>ID</th><th>What</th><th>Target</th><th>Measured</th><th></th></tr></thead><tbody id="checks"></tbody></table>

  <h2>Adaptive vs. fixed schedule</h2>
  <table><thead><tr><th>Group</th><th>Rooms</th><th>Model days</th><th>Comfort when occupied</th><th>Comfort at arrival</th><th>kWh/m²/day</th><th>Skipped (bad data)</th></tr></thead><tbody id="groups"></tbody></table>

  <h2>Pre-heat forecast (occupancy prior)</h2>
  <table><thead><tr><th>Room</th><th>Arrivals</th><th>Pre-heated</th><th>False pre-heats</th></tr></thead><tbody id="forecast"></tbody></table>

  <h2>Pipeline</h2>
  <div id="pipeline"></div>
<script>
function f(v, d) { return (v === undefined || v === null) ? '-' : Number(v).toFixed(d); }
function esc(s) { return String(s === undefined ? '' : s).replace(/[&<>"]/g, function(c) { return {'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c]; }); }
async function refresh() {
  const s = await (await fetch('/api/summary')).json();
  document.getElementById('updated').textContent = 'Updated ' + new Date(s.updated_at).toLocaleTimeString() +
    (s.sim_time ? ' | model time ' + new Date(s.sim_time).toUTCString().slice(0, 22) : '');
  document.getElementById('rooms').innerHTML = (s.rooms || []).map(function(r) {
    const rd = (r.state && r.state.readings) || {}, q = (r.state && r.state.quality) || {};
    const quality = Object.keys(q).sort().map(function(k) { return '<span class="' + esc(q[k]) + '">' + esc(k) + ': ' + esc(q[k]) + '</span>'; }).join('<br>');
    const a = r.applied || {};
    return '<tr><td>' + esc(r.room) + '<br><span class="reason">' + esc(r.group) + '</span></td>' +
      '<td>' + f(rd.temperature && rd.temperature.value, 2) + ' °C</td>' +
      '<td>' + f(rd.humidity && rd.humidity.value, 1) + ' %</td>' +
      '<td>' + f(rd.occupancy && rd.occupancy.value, 0) + '</td>' +
      '<td>' + f(rd.energy && rd.energy.value, 2) + ' kWh</td>' +
      '<td>' + quality + '</td>' +
      '<td class="mode ' + esc(r.severity) + '">' + esc(a.watchdog ? 'watchdog' : r.mode) + '</td>' +
      '<td>' + f(a.setpoint, 1) + ' °C, fan ' + f(a.fan_speed, 0) + ' %<br><span class="reason">' + esc(a.reason) + '</span></td>' +
      '<td>' + (r.p_ahead === undefined || r.p_ahead < 0 ? (r.group === 'adaptive' ? 'learning' : '-') : f(r.p_ahead, 2)) + '</td></tr>';
  }).join('');
  const ev = s.evaluation || {};
  document.getElementById('checks').innerHTML = (ev.requirement_checks || []).map(function(c) {
    return '<tr><td>' + esc(c.id) + '</td><td>' + esc(c.what) + '</td><td>' + esc(c.target) + '</td><td>' + f(c.measured, 1) +
      '</td><td class="' + (c.pass ? 'ok">pass' : 'fail">not met') + '</td></tr>';
  }).join('');
  const groups = ev.groups || {};
  document.getElementById('groups').innerHTML = Object.keys(groups).sort().map(function(g) {
    const x = groups[g];
    return '<tr><td>' + esc(g) + '</td><td>' + esc((x.rooms || []).join(', ')) + '</td><td>' + f(x.model_days, 1) + '</td><td>' +
      f(x.comfort_pct_occupied, 1) + ' %</td><td>' + f(x.arrival_comfort_pct, 1) + ' %</td><td>' + f(x.kwh_per_m2_per_day, 3) + '</td><td>' + esc(x.skipped_bad_data) + '</td></tr>';
  }).join('');
  const fc = s.forecast || {};
  document.getElementById('forecast').innerHTML = Object.keys(fc).sort().map(function(room) {
    const x = fc[room];
    return '<tr><td>' + esc(room) + '</td><td>' + esc(x.arrivals) + '</td><td>' + esc(x.preheated_arrivals) + '</td><td>' + esc(x.false_preheats) + '</td></tr>';
  }).join('');
  const p = s.pipeline || {};
  document.getElementById('pipeline').textContent = 'accepted ' + p.accepted + ', rejected ' + p.rejected +
    ', duplicates ' + p.duplicates + ', replayed at start ' + p.replayed + ', last write ' + (p.last_write || '-');
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
		if d, err := time.ParseDuration(v); err == nil {
			return d // e.g. "500ms", used by the stress test
		}
	}
	return fallback
}
