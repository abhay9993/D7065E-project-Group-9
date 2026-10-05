// Command occupancy-sensor reads the room's person count from BuildSim's global
// occupancy map and republishes it to the data pipeline on an interval.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type person struct {
	ID string `json:"id"`
}

type roomOccupancy struct {
	Persons []person `json:"persons"`
}

type reading struct {
	Room      string    `json:"room"`
	Type      string    `json:"type"`
	Value     float64   `json:"value"`
	Unit      string    `json:"unit"`
	Timestamp time.Time `json:"timestamp"`
}

func main() {
	buildsimURL := getenv("BUILDSIM_URL", "http://127.0.0.1:9090")
	pipelineURL := getenv("PIPELINE_URL", "http://127.0.0.1:8090")
	level := getenv("ROOM_LEVEL", "level0")
	rooms := getenvList("ROOM_NAMES", "A1009,A1010,A1013")
	listenAddr := getenv("LISTEN_ADDR", ":8097")
	poll := getenvDuration("POLL_SECONDS", 5*time.Second)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go serveHealth(listenAddr)

	client := &http.Client{Timeout: 5 * time.Second}

	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Println("occupancy-sensor: shutting down")
			return
		case <-ticker.C:
			occupancy, err := fetchOccupancy(ctx, client, buildsimURL)
			if err != nil {
				log.Printf("occupancy-sensor: read buildsim: %v", err)
				continue
			}
			for _, room := range rooms {
				roomKey := level + "/" + room
				count := len(occupancy[roomKey].Persons)
				r := reading{Room: roomKey, Type: "occupancy", Value: float64(count), Unit: "count", Timestamp: time.Now()}
				if err := publish(ctx, client, pipelineURL, r); err != nil {
					log.Printf("occupancy-sensor[%s]: publish: %v", room, err)
				}
			}
		}
	}
}

func fetchOccupancy(ctx context.Context, client *http.Client, baseURL string) (map[string]roomOccupancy, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/occupancy", nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("buildsim returned HTTP %d", resp.StatusCode)
	}
	var occ map[string]roomOccupancy
	if err := json.NewDecoder(resp.Body).Decode(&occ); err != nil {
		return nil, err
	}
	return occ, nil
}

func publish(ctx context.Context, client *http.Client, pipelineURL string, r reading) error {
	body, err := json.Marshal(r)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, pipelineURL+"/api/readings", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("pipeline returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func serveHealth(addr string) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Printf("occupancy-sensor: health server stopped: %v", err)
	}
}

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
