// Command temperature-sensor reads the room's temperature from BuildSim (the
// device observation) and republishes it to the data pipeline on an interval.
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

type sensorValue struct {
	Value string `json:"value"`
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
	listenAddr := getenv("LISTEN_ADDR", ":8095")
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
			log.Println("temperature-sensor: shutting down")
			return
		case <-ticker.C:
			for _, room := range rooms {
				value, err := fetchSensorValue(ctx, client, buildsimURL, room+"-temp")
				if err != nil {
					log.Printf("temperature-sensor[%s]: read buildsim: %v", room, err)
					continue
				}
				r := reading{Room: level + "/" + room, Type: "temperature", Value: value, Unit: "°C", Timestamp: time.Now()}
				if err := publish(ctx, client, pipelineURL, r); err != nil {
					log.Printf("temperature-sensor[%s]: publish: %v", room, err)
				}
			}
		}
	}
}

func fetchSensorValue(ctx context.Context, client *http.Client, baseURL, sensorID string) (float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/sensors/"+sensorID, nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("buildsim returned HTTP %d", resp.StatusCode)
	}
	var s sensorValue
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return 0, err
	}
	return strconv.ParseFloat(s.Value, 64)
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
		log.Printf("temperature-sensor: health server stopped: %v", err)
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
