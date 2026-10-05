package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eislab-cps/buildingsim/pkg/server"
	"github.com/spf13/cobra"
)

func TestStartCommandSafeLaptopDefaults(t *testing.T) {
	tests := []struct {
		flag string
		want string
	}{
		{"host", "127.0.0.1"},
		{"all-interfaces", "false"},
		{"port", "9090"},
		{"ui", server.UIModern},
		{"edit", "false"},
		{"edit-output", ""},
		{"log-file", ""},
	}
	for _, tt := range tests {
		flag := startCmd.Flags().Lookup(tt.flag)
		if flag == nil || flag.DefValue != tt.want {
			t.Errorf("--%s default=%v, want %q", tt.flag, flag, tt.want)
		}
	}
}

func TestResolveListenHost(t *testing.T) {
	tests := []struct {
		name          string
		host          string
		allInterfaces bool
		hostExplicit  bool
		want          string
		wantError     bool
	}{
		{name: "safe default", host: "127.0.0.1", want: "127.0.0.1"},
		{name: "explicit host", host: "192.0.2.10", hostExplicit: true, want: "192.0.2.10"},
		{name: "all interfaces", host: "127.0.0.1", allInterfaces: true, want: "0.0.0.0"},
		{name: "conflicting flags", host: "192.0.2.10", allInterfaces: true, hostExplicit: true, wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveListenHost(tt.host, tt.allInterfaces, tt.hostExplicit)
			if (err != nil) != tt.wantError {
				t.Fatalf("error=%v, wantError=%v", err, tt.wantError)
			}
			if got != tt.want {
				t.Errorf("host=%q, want %q", got, tt.want)
			}
		})
	}
}

func TestVersionMetadataHasDevelopmentDefaults(t *testing.T) {
	if version == "" || buildTime == "" {
		t.Fatal("version metadata must always be printable")
	}
}

func TestCheckHealth(t *testing.T) {
	service := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/healthz" {
			http.NotFound(writer, request)
			return
		}
		writer.WriteHeader(http.StatusOK)
	}))
	defer service.Close()

	command := &cobra.Command{}
	var output bytes.Buffer
	command.SetOut(&output)
	if err := checkHealth(command, service.URL+"/healthz"); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(output.String()) != "ok" {
		t.Fatalf("output=%q", output.String())
	}
}

func TestCheckHealthRejectsUnhealthyResponse(t *testing.T) {
	service := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "not ready", http.StatusServiceUnavailable)
	}))
	defer service.Close()
	if err := checkHealth(&cobra.Command{}, service.URL); err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("error=%v", err)
	}
}
