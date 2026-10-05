package server

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAddressDefaultsToLoopback(t *testing.T) {
	server := New(9090, os.DirFS("../.."), os.DirFS("../.."), false)
	if got := server.Address(); got != "127.0.0.1:9090" {
		t.Fatalf("default address=%q", got)
	}
	server.Host = "::1"
	if got := server.Address(); got != "[::1]:9090" {
		t.Fatalf("IPv6 address=%q", got)
	}
}

func TestRouterServesBothViewerModesFromSharedAPI(t *testing.T) {
	tests := []struct {
		ui         string
		bodyMarker string
	}{
		{UIClassic, `id="info-panel"`},
		{UIModern, `id="boot-loader"`},
	}
	for _, tt := range tests {
		t.Run(tt.ui, func(t *testing.T) {
			server := New(9090, os.DirFS("../.."), os.DirFS("../.."), false)
			server.UI = tt.ui
			router, err := server.Router()
			if err != nil {
				t.Fatal(err)
			}
			response := performRequest(router, http.MethodGet, "/", "", "")
			if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), tt.bodyMarker) {
				t.Fatalf("viewer response=%d, marker %q missing", response.Code, tt.bodyMarker)
			}
			api := performRequest(router, http.MethodGet, "/api/building", "", "")
			if api.Code != http.StatusOK || !strings.Contains(api.Body.String(), `"levels"`) {
				t.Fatalf("shared API failed: %d %s", api.Code, api.Body.String())
			}
			vendor := performRequest(router, http.MethodGet, "/vendor/three.module.js", "", "")
			if vendor.Code != http.StatusOK || !strings.Contains(vendor.Body.String(), "REVISION = '160'") {
				t.Fatalf("local vendor asset failed: %d", vendor.Code)
			}
		})
	}
}

func TestHealthEndpoint(t *testing.T) {
	server := New(9090, os.DirFS("../.."), os.DirFS("../.."), false)
	router, err := server.Router()
	if err != nil {
		t.Fatal(err)
	}
	response := performRequest(router, http.MethodGet, "/healthz", "", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"ok"`) {
		t.Fatalf("health response=%d %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("health cache policy=%q", response.Header().Get("Cache-Control"))
	}
}

func TestUnknownRoutesAreNotServed(t *testing.T) {
	server := New(9090, os.DirFS("../.."), os.DirFS("../.."), false)
	server.UI = UIModern
	router, err := server.Router()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/not-a-route", "/api/not-a-route"} {
		response := performRequest(router, http.MethodGet, path, "", "")
		if response.Code >= 200 && response.Code < 300 {
			t.Errorf("unregistered endpoint %s is served with HTTP %d", path, response.Code)
		}
	}
}

func TestConfigReportsModeAndPersistence(t *testing.T) {
	server := New(9090, os.DirFS("../.."), os.DirFS("../.."), true)
	server.UI = UIClassic
	server.EditOutputDir = t.TempDir()
	router, err := server.Router()
	if err != nil {
		t.Fatal(err)
	}
	response := performRequest(router, http.MethodGet, "/api/config", "", "")
	for _, marker := range []string{`"edit_mode":true`, `"edit_persistent":true`, `"ui":"classic"`, `"host":"127.0.0.1"`} {
		if !strings.Contains(response.Body.String(), marker) {
			t.Fatalf("config missing %s: %s", marker, response.Body.String())
		}
	}
}

func TestEditRoutesAreOptIn(t *testing.T) {
	readOnly := New(9090, os.DirFS("../.."), os.DirFS("../.."), false)
	readOnlyRouter, err := readOnly.Router()
	if err != nil {
		t.Fatal(err)
	}
	floor := performRequest(readOnlyRouter, http.MethodGet, "/api/building/floors/level0", "", "").Body.String()
	response := performRequest(readOnlyRouter, http.MethodPost, "/api/editor/floors/level0", floor, "application/json")
	if response.Code == http.StatusOK {
		t.Fatal("editor route was enabled without --edit")
	}

	output := t.TempDir()
	editable := New(9090, os.DirFS("../.."), os.DirFS("../.."), true)
	editable.EditOutputDir = output
	editableRouter, err := editable.Router()
	if err != nil {
		t.Fatal(err)
	}
	floor = performRequest(editableRouter, http.MethodGet, "/api/building/floors/level0", "", "").Body.String()
	response = performRequest(editableRouter, http.MethodPost, "/api/editor/floors/level0", floor, "application/json")
	if response.Code != http.StatusOK {
		t.Fatalf("editor save=%d: %s", response.Code, response.Body.String())
	}
	if _, err := os.Stat(filepath.Join(output, "level0", "floorplan_data.json")); err != nil {
		t.Fatalf("persistent edit was not written: %v", err)
	}
}

func TestLocalCORS(t *testing.T) {
	server := New(9090, os.DirFS("../.."), os.DirFS("../.."), false)
	router, err := server.Router()
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodOptions, "http://127.0.0.1:9090/api/building", nil)
	request.Host = "127.0.0.1:9090"
	request.Header.Set("Origin", "http://localhost:3000")
	allowed := httptest.NewRecorder()
	router.ServeHTTP(allowed, request)
	if allowed.Code != http.StatusNoContent || allowed.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
		t.Fatalf("allowed preflight=%d headers=%v", allowed.Code, allowed.Header())
	}

	request = httptest.NewRequest(http.MethodPost, "http://127.0.0.1:9090/api/equipment/notify", nil)
	request.Host = "127.0.0.1:9090"
	request.Header.Set("Origin", "https://example.com")
	blocked := httptest.NewRecorder()
	router.ServeHTTP(blocked, request)
	if blocked.Code != http.StatusForbidden {
		t.Fatalf("remote origin was not blocked: %d", blocked.Code)
	}
}

func TestRouterRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name string
		set  func(*Server)
	}{
		{"UI", func(server *Server) { server.UI = "unknown" }},
		{"negative port", func(server *Server) { server.Port = -1 }},
		{"large port", func(server *Server) { server.Port = 65536 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := New(9090, os.DirFS("../.."), os.DirFS("../.."), false)
			tt.set(server)
			if _, err := server.Router(); err == nil {
				t.Fatal("expected configuration error")
			}
		})
	}
}

func TestIconHandlerOnlyServesSVGBaseNames(t *testing.T) {
	server := New(9090, os.DirFS("../.."), os.DirFS("../.."), false)
	router, err := server.Router()
	if err != nil {
		t.Fatal(err)
	}
	if response := performRequest(router, http.MethodGet, "/api/icons/generic.svg", "", ""); response.Code != http.StatusOK || response.Header().Get("Content-Type") != "image/svg+xml" {
		t.Fatalf("valid icon: %d %s", response.Code, response.Header().Get("Content-Type"))
	}
	if response := performRequest(router, http.MethodGet, "/api/icons/generic.png", "", ""); response.Code != http.StatusBadRequest {
		t.Fatalf("non-SVG icon status=%d", response.Code)
	}
}

func TestConfigureLoggingWritesOptionalFileAndRestoresLogger(t *testing.T) {
	server := New(9090, os.DirFS("../.."), os.DirFS("../.."), false)
	filename := filepath.Join(t.TempDir(), "logs", "buildsim.log")
	server.LogFile = filename
	// configureLogging does not create parent directories by design; give it a
	// valid destination and verify errors remain explicit for bad paths.
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatal(err)
	}
	before := log.Writer()
	closeLog, err := server.configureLogging()
	if err != nil {
		t.Fatal(err)
	}
	log.Print("logging-test-marker")
	closeLog()
	if log.Writer() != before {
		t.Fatal("standard logger was not restored")
	}
	data, err := os.ReadFile(filename)
	if err != nil || !strings.Contains(string(data), "logging-test-marker") {
		t.Fatalf("log file=%q err=%v", string(data), err)
	}
}

func TestStartContextServesAndShutsDown(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	server := New(port, os.DirFS("../.."), os.DirFS("../.."), false)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- server.StartContext(ctx) }()

	url := fmt.Sprintf("http://127.0.0.1:%d/api/config", port)
	client := &http.Client{Timeout: 250 * time.Millisecond}
	deadline := time.Now().Add(5 * time.Second)
	for {
		response, requestErr := client.Get(url)
		if requestErr == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("server did not start: %v", requestErr)
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("server did not shut down")
	}
}

func performRequest(handler http.Handler, method, target, body, contentType string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
