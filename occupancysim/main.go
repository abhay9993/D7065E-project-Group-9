// Command occupancy simulates people moving through a BuildSim floor and
// publishes their positions and room occupancy to BuildSim. It serves a web UI
// for the simulation parameters and clock on its own port.
package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata" // the scratch image has no zoneinfo; TZ=Europe/Stockholm must still work

	"github.com/eislab-cps/D7065E/occupancysim/internal/api"
	"github.com/eislab-cps/D7065E/occupancysim/internal/buildsim"
	"github.com/eislab-cps/D7065E/occupancysim/internal/sim"
)

//go:embed all:ui/dist
var uiFS embed.FS

func main() {
	buildsimURL := flag.String("buildsim", envOr("BUILDSIM_URL", "http://127.0.0.1:9090"), "BuildSim base URL as seen from this process")
	publicURL := flag.String("buildsim-public", envOr("BUILDSIM_PUBLIC_URL", ""), "BuildSim base URL as seen from the browser (defaults to -buildsim)")
	listen := flag.String("listen", envOr("OCCUPANCY_LISTEN", ":8081"), "address for the occupancy UI and API")
	configPath := flag.String("config", envOr("OCCUPANCY_CONFIG", ""), "optional JSON file with parameters; updated from the UI")
	levels := flag.String("levels", envOr("OCCUPANCY_LEVELS", ""), "comma-separated floors to simulate (default: every floor of the building)")
	factor := flag.Float64("factor", envFloat("OCCUPANCY_FACTOR", 60), "initial simulated seconds per real second")
	start := flag.String("start", envOr("OCCUPANCY_START", ""), "initial simulated time, RFC3339 or HH:MM (default: 07:30 on the next weekday)")
	paused := flag.Bool("paused", false, "start with the clock stopped")
	flag.Parse()

	params, err := api.LoadParameters(*configPath)
	if err != nil {
		log.Fatalf("load parameters: %v", err)
	}
	if *publicURL == "" {
		*publicURL = *buildsimURL
	}
	startTime, err := parseStart(*start, time.Now())
	if err != nil {
		log.Fatalf("parse -start: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client := buildsim.New(*buildsimURL)
	wanted := splitLevels(*levels)
	if len(wanted) == 0 {
		wanted = waitForLevels(ctx, client)
	}
	var plans []sim.FloorPlan
	for _, level := range wanted {
		plan := waitForFloor(ctx, client, level)
		if ctx.Err() != nil {
			return
		}
		plans = append(plans, plan)
	}
	if ctx.Err() != nil {
		return
	}
	simulation, err := sim.New(plans, params, startTime)
	if err != nil {
		log.Fatalf("configure simulation: %v", err)
	}
	if err := simulation.SetFactor(*factor); err != nil {
		log.Fatalf("invalid -factor: %v", err)
	}
	simulation.SetRunning(!*paused)
	rooms := map[string]map[sim.RoomRole]int{}
	for _, room := range simulation.Rooms() {
		if rooms[room.Level] == nil {
			rooms[room.Level] = map[sim.RoomRole]int{}
		}
		rooms[room.Level][room.Role]++
	}
	snapshot := simulation.Snapshot(0)
	for _, level := range simulation.Levels() {
		count := rooms[level]
		log.Printf("floor %s: %d offices, %d lecture rooms, %d fika rooms, %d corridors",
			level, count[sim.RoleOffice], count[sim.RoleLecture], count[sim.RoleFika], count[sim.RoleCorridor])
	}
	for _, e := range snapshot.Entrances {
		log.Printf("entrance %q on %s at %v -> walkable node %s", e.Name, e.Level, e.Position, e.Node)
	}
	log.Printf("simulated clock starts at %s (factor %gx)", startTime.Format(time.RFC3339), *factor)

	go runClock(ctx, simulation)
	publisher := buildsim.NewPublisher(client, simulation, *publicURL)
	publisherDone := make(chan struct{})
	go func() {
		defer close(publisherDone)
		publisher.Run(ctx)
	}()

	ui, err := fs.Sub(uiFS, "ui/dist")
	if err != nil {
		log.Fatalf("embedded UI: %v", err)
	}
	server := &http.Server{
		Addr:              *listen,
		Handler:           (&api.Server{Sim: simulation, Publisher: publisher, UI: ui, ConfigPath: *configPath}).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		log.Printf("occupancy UI listening on http://%s", displayAddress(*listen))
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("serve: %v", err)
		}
	}()

	<-ctx.Done()
	log.Printf("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
	<-publisherDone
}

// runClock advances the simulation from wall-clock time and the speed factor.
func runClock(ctx context.Context, simulation *sim.Simulation) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	last := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			elapsed := now.Sub(last)
			last = now
			if !simulation.Running() {
				continue
			}
			simulation.Advance(time.Duration(float64(elapsed) * simulation.Factor()))
		}
	}
}

// splitLevels parses the comma-separated -levels flag.
func splitLevels(value string) []string {
	var levels []string
	for _, level := range strings.Split(value, ",") {
		if level = strings.TrimSpace(level); level != "" {
			levels = append(levels, level)
		}
	}
	return levels
}

// waitForLevels asks BuildSim which floors the building has.
func waitForLevels(ctx context.Context, client *buildsim.Client) []string {
	for attempt := 1; ; attempt++ {
		levels, err := client.Levels(ctx)
		if err == nil {
			return levels
		}
		if ctx.Err() != nil {
			return nil
		}
		if attempt == 1 || attempt%15 == 0 {
			log.Printf("waiting for BuildSim: %v", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(2 * time.Second):
		}
	}
}

func waitForFloor(ctx context.Context, client *buildsim.Client, level string) sim.FloorPlan {
	for attempt := 1; ; attempt++ {
		plan, err := client.Floor(ctx, level)
		if err == nil {
			log.Printf("loaded floor %s from %s: %d rooms, %d walkable nodes", level, client.BaseURL, len(plan.Rooms), len(plan.Nodes))
			return plan
		}
		if ctx.Err() != nil {
			return sim.FloorPlan{}
		}
		if attempt == 1 || attempt%15 == 0 {
			log.Printf("waiting for BuildSim: %v", err)
		}
		select {
		case <-ctx.Done():
			return sim.FloorPlan{}
		case <-time.After(2 * time.Second):
		}
	}
}

// parseStart accepts RFC3339, "HH:MM" (today or the next weekday), or nothing.
func parseStart(value string, now time.Time) (time.Time, error) {
	if value == "" {
		value = "07:30"
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t, nil
	}
	minutes, err := sim.ParseClock(value)
	if err != nil {
		return time.Time{}, fmt.Errorf("use RFC3339 or HH:MM: %w", err)
	}
	day := now
	for day.Weekday() == time.Saturday || day.Weekday() == time.Sunday {
		day = day.AddDate(0, 0, 1)
	}
	return time.Date(day.Year(), day.Month(), day.Day(), int(minutes)/60, int(minutes)%60, 0, 0, now.Location()), nil
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envFloat(key string, fallback float64) float64 {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.ParseFloat(value, 64); err == nil {
			return parsed
		}
	}
	return fallback
}

func displayAddress(listen string) string {
	if len(listen) > 0 && listen[0] == ':' {
		return "127.0.0.1" + listen
	}
	return listen
}
